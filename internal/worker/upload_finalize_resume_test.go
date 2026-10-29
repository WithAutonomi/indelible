package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	antd "github.com/WithAutonomi/ant-sdk/antd-go"
)

func partialErr(failed uint64, retryable, known bool) error {
	return &antd.PartialUploadError{
		AntdError:      antd.AntdError{StatusCode: 502, Message: "Partial upload"},
		ChunksStored:   100 - failed,
		ChunksFailed:   failed,
		TotalChunks:    100,
		Retryable:      retryable,
		RetentionKnown: known,
	}
}

// fakeFinalize returns the scripted outcomes in order and counts the calls.
type fakeFinalize struct {
	outcomes []error // nil = success
	calls    int
}

func (f *fakeFinalize) call() (*antd.FinalizeUploadResult, error) {
	i := f.calls
	f.calls++
	if i >= len(f.outcomes) {
		return nil, errors.New("fake finalize called too often")
	}
	if f.outcomes[i] == nil {
		return &antd.FinalizeUploadResult{ChunksStored: 100}, nil
	}
	return nil, f.outcomes[i]
}

func shortResumeDelays(t *testing.T) {
	t.Helper()
	saved := finalizeResumeDelays
	finalizeResumeDelays = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond, time.Millisecond, time.Millisecond}
	t.Cleanup(func() { finalizeResumeDelays = saved })
}

func TestFinalizeWithResume(t *testing.T) {
	shortResumeDelays(t)
	w, _, upload, _ := newInflightEnv(t, 0)
	w.markActive(upload)

	cases := []struct {
		name      string
		outcomes  []error
		wantCalls int
		wantOK    bool
	}{
		{"first call succeeds", []error{nil}, 1, true},
		{"retryable partial then success", []error{partialErr(10, true, true), partialErr(4, true, true), nil}, 3, true},
		{"no progress stops", []error{partialErr(10, true, true), partialErr(10, true, true)}, 2, false},
		{"retries run out", []error{
			partialErr(60, true, true), partialErr(50, true, true), partialErr(40, true, true),
			partialErr(30, true, true), partialErr(20, true, true), partialErr(10, true, true),
		}, 6, false},
		{"nothing retained is not repeated", []error{partialErr(10, false, true)}, 1, false},
		{"unknown retention is not repeated", []error{partialErr(10, false, false)}, 1, false},
		{"other error is not repeated", []error{errors.New("network down")}, 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeFinalize{outcomes: tc.outcomes}
			result, err := w.finalizeWithResume(context.Background(), upload, "up-1", f.call)
			if f.calls != tc.wantCalls {
				t.Errorf("finalize calls = %d, want %d", f.calls, tc.wantCalls)
			}
			if tc.wantOK && (err != nil || result == nil) {
				t.Errorf("got err %v, want success", err)
			}
			if !tc.wantOK && err == nil {
				t.Errorf("got success, want the last error")
			}
		})
	}
}

// A cancelled context ends the wait without another finalize call.
func TestFinalizeWithResumeCancelled(t *testing.T) {
	saved := finalizeResumeDelays
	finalizeResumeDelays = []time.Duration{time.Hour}
	t.Cleanup(func() { finalizeResumeDelays = saved })
	w, _, upload, _ := newInflightEnv(t, 0)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := &fakeFinalize{outcomes: []error{partialErr(10, true, true), nil}}
	if _, err := w.finalizeWithResume(ctx, upload, "up-1", f.call); err == nil {
		t.Fatalf("cancelled resume reported success")
	}
	if f.calls != 1 {
		t.Errorf("finalize calls = %d after cancel, want 1", f.calls)
	}
}

// Wave-batch: unknown retention stops for reconciliation (no re-prepare, no
// second payment); anything else keeps the re-prepare retry (V2-1774).
func TestWaveFinalizeError(t *testing.T) {
	unknown := waveFinalizeError("up-1", partialErr(10, false, false))
	if got := classifyFailure(unknown, true, true); got != outcomePreservePaid {
		t.Errorf("unknown retention classified %v, want outcomePreservePaid", got)
	}
	for _, err := range []error{partialErr(10, true, true), partialErr(10, false, true), errors.New("network down")} {
		wrapped := waveFinalizeError("up-1", err)
		if !errors.Is(wrapped, errFinalizeFailed) {
			t.Errorf("%v: not errFinalizeFailed", err)
		}
		if got := classifyFailure(wrapped, true, true); got != outcomeRetry {
			t.Errorf("%v classified %v, want outcomeRetry", err, got)
		}
	}
}
