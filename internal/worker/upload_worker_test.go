package worker

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	antd "github.com/WithAutonomi/ant-sdk/antd-go"

	"github.com/WithAutonomi/indelible/internal/config"
)

// --- calcGasBackoff tests ---

func TestCalcGasBackoff_Attempt1(t *testing.T) {
	before := time.Now().UTC()
	result := calcGasBackoff(1)
	after := time.Now().UTC()

	expected := 5 * time.Minute
	lo := before.Add(expected)
	hi := after.Add(expected)

	if result.Before(lo) || result.After(hi) {
		t.Errorf("attempt 1: got %v, want ~%v from now", result.Sub(before), expected)
	}
}

func TestCalcGasBackoff_Attempt2(t *testing.T) {
	before := time.Now().UTC()
	result := calcGasBackoff(2)

	diff := result.Sub(before)
	if diff < 14*time.Minute || diff > 16*time.Minute {
		t.Errorf("attempt 2: got %v, want ~15m", diff)
	}
}

func TestCalcGasBackoff_Attempt3(t *testing.T) {
	before := time.Now().UTC()
	result := calcGasBackoff(3)

	diff := result.Sub(before)
	if diff < 44*time.Minute || diff > 46*time.Minute {
		t.Errorf("attempt 3: got %v, want ~45m", diff)
	}
}

func TestCalcGasBackoff_Attempt4(t *testing.T) {
	before := time.Now().UTC()
	result := calcGasBackoff(4)

	diff := result.Sub(before)
	if diff < 119*time.Minute || diff > 121*time.Minute {
		t.Errorf("attempt 4: got %v, want ~2h", diff)
	}
}

func TestCalcGasBackoff_Attempt5(t *testing.T) {
	before := time.Now().UTC()
	result := calcGasBackoff(5)

	diff := result.Sub(before)
	if diff < 239*time.Minute || diff > 241*time.Minute {
		t.Errorf("attempt 5: got %v, want ~4h", diff)
	}
}

func TestCalcGasBackoff_Attempt6(t *testing.T) {
	before := time.Now().UTC()
	result := calcGasBackoff(6)

	diff := result.Sub(before)
	if diff < 359*time.Minute || diff > 361*time.Minute {
		t.Errorf("attempt 6: got %v, want ~6h", diff)
	}
}

func TestCalcGasBackoff_Attempt7Plus_NextCheapWindow(t *testing.T) {
	result := calcGasBackoff(7)

	if result.Hour() != 2 || result.Minute() != 0 || result.Second() != 0 {
		t.Errorf("attempt 7: got %v, want next 02:00 UTC", result)
	}

	// Must be in the future
	if !result.After(time.Now().UTC()) {
		t.Error("attempt 7 should return a future time")
	}
}

func TestCalcGasBackoff_Attempt10_StillCheapWindow(t *testing.T) {
	result := calcGasBackoff(10)
	if result.Hour() != 2 || result.Minute() != 0 {
		t.Errorf("attempt 10: got %v, want next 02:00 UTC", result)
	}
}

func TestCalcGasBackoff_AttemptZero_SameAsOne(t *testing.T) {
	// attempt <= 1 case handles 0 too
	before := time.Now().UTC()
	result := calcGasBackoff(0)

	diff := result.Sub(before)
	if diff < 4*time.Minute || diff > 6*time.Minute {
		t.Errorf("attempt 0: got %v, want ~5m", diff)
	}
}

// --- nextCheapWindow tests ---

func TestNextCheapWindow_Before0200(t *testing.T) {
	// At 01:00 UTC, next cheap window should be same day 02:00
	now := time.Date(2025, 6, 15, 1, 0, 0, 0, time.UTC)
	result := nextCheapWindow(now)

	expected := time.Date(2025, 6, 15, 2, 0, 0, 0, time.UTC)
	if !result.Equal(expected) {
		t.Errorf("at 01:00: got %v, want %v", result, expected)
	}
}

func TestNextCheapWindow_At0000(t *testing.T) {
	now := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)
	result := nextCheapWindow(now)

	expected := time.Date(2025, 6, 15, 2, 0, 0, 0, time.UTC)
	if !result.Equal(expected) {
		t.Errorf("at 00:00: got %v, want %v", result, expected)
	}
}

func TestNextCheapWindow_At0159(t *testing.T) {
	now := time.Date(2025, 6, 15, 1, 59, 59, 0, time.UTC)
	result := nextCheapWindow(now)

	expected := time.Date(2025, 6, 15, 2, 0, 0, 0, time.UTC)
	if !result.Equal(expected) {
		t.Errorf("at 01:59: got %v, want %v", result, expected)
	}
}

func TestNextCheapWindow_After0200(t *testing.T) {
	// At 03:00 UTC, cheap window is ongoing but didn't help => next day
	now := time.Date(2025, 6, 15, 3, 0, 0, 0, time.UTC)
	result := nextCheapWindow(now)

	expected := time.Date(2025, 6, 16, 2, 0, 0, 0, time.UTC)
	if !result.Equal(expected) {
		t.Errorf("at 03:00: got %v, want %v", result, expected)
	}
}

func TestNextCheapWindow_At0200_Exactly(t *testing.T) {
	// Exactly 02:00 => hour >= 2 => next day
	now := time.Date(2025, 6, 15, 2, 0, 0, 0, time.UTC)
	result := nextCheapWindow(now)

	expected := time.Date(2025, 6, 16, 2, 0, 0, 0, time.UTC)
	if !result.Equal(expected) {
		t.Errorf("at 02:00: got %v, want %v", result, expected)
	}
}

func TestNextCheapWindow_At2300(t *testing.T) {
	now := time.Date(2025, 6, 15, 23, 0, 0, 0, time.UTC)
	result := nextCheapWindow(now)

	expected := time.Date(2025, 6, 16, 2, 0, 0, 0, time.UTC)
	if !result.Equal(expected) {
		t.Errorf("at 23:00: got %v, want %v", result, expected)
	}
}

func TestNextCheapWindow_At1200(t *testing.T) {
	now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	result := nextCheapWindow(now)

	expected := time.Date(2025, 6, 16, 2, 0, 0, 0, time.UTC)
	if !result.Equal(expected) {
		t.Errorf("at 12:00: got %v, want %v", result, expected)
	}
}

func TestNextCheapWindow_EndOfMonth(t *testing.T) {
	now := time.Date(2025, 6, 30, 23, 30, 0, 0, time.UTC)
	result := nextCheapWindow(now)

	expected := time.Date(2025, 7, 1, 2, 0, 0, 0, time.UTC)
	if !result.Equal(expected) {
		t.Errorf("end of month: got %v, want %v", result, expected)
	}
}

func TestNextCheapWindow_EndOfYear(t *testing.T) {
	now := time.Date(2025, 12, 31, 15, 0, 0, 0, time.UTC)
	result := nextCheapWindow(now)

	expected := time.Date(2026, 1, 1, 2, 0, 0, 0, time.UTC)
	if !result.Equal(expected) {
		t.Errorf("end of year: got %v, want %v", result, expected)
	}
}

// --- isTransientAntdError / isPermanentAntdError tests ---

func TestIsTransientAntdError_NetworkError(t *testing.T) {
	err := &antd.NetworkError{AntdError: antd.AntdError{StatusCode: 502, Message: "network unreachable"}}
	if !isTransientAntdError(err) {
		t.Error("NetworkError should be transient")
	}
	if isPermanentAntdError(err) {
		t.Error("NetworkError should NOT be permanent")
	}
}

func TestIsTransientAntdError_ServiceUnavailable(t *testing.T) {
	err := &antd.ServiceUnavailableError{AntdError: antd.AntdError{StatusCode: 503, Message: "wallet missing"}}
	if !isTransientAntdError(err) {
		t.Error("ServiceUnavailableError should be transient")
	}
	if isPermanentAntdError(err) {
		t.Error("ServiceUnavailableError should NOT be permanent")
	}
}

func TestIsPermanentAntdError_BadRequest(t *testing.T) {
	err := &antd.BadRequestError{AntdError: antd.AntdError{StatusCode: 400, Message: "invalid params"}}
	if !isPermanentAntdError(err) {
		t.Error("BadRequestError should be permanent")
	}
	if isTransientAntdError(err) {
		t.Error("BadRequestError should NOT be transient")
	}
}

func TestIsPermanentAntdError_TooLarge(t *testing.T) {
	err := &antd.TooLargeError{AntdError: antd.AntdError{StatusCode: 413, Message: "payload too large"}}
	if !isPermanentAntdError(err) {
		t.Error("TooLargeError should be permanent")
	}
	if isTransientAntdError(err) {
		t.Error("TooLargeError should NOT be transient")
	}
}

func TestIsTransientAntdError_InternalError(t *testing.T) {
	err := &antd.InternalError{AntdError: antd.AntdError{StatusCode: 500, Message: "internal"}}
	// InternalError is neither transient nor permanent by the current classification
	if isTransientAntdError(err) {
		t.Error("InternalError should NOT be classified as transient")
	}
	if isPermanentAntdError(err) {
		t.Error("InternalError should NOT be classified as permanent")
	}
}

func TestIsTransientAntdError_PaymentError(t *testing.T) {
	err := &antd.PaymentError{AntdError: antd.AntdError{StatusCode: 402, Message: "insufficient funds"}}
	if isTransientAntdError(err) {
		t.Error("PaymentError should NOT be transient")
	}
	if isPermanentAntdError(err) {
		t.Error("PaymentError should NOT be classified as permanent (it's not BadRequest or TooLarge)")
	}
}

func TestIsTransientAntdError_NotFoundError(t *testing.T) {
	err := &antd.NotFoundError{AntdError: antd.AntdError{StatusCode: 404, Message: "not found"}}
	if isTransientAntdError(err) {
		t.Error("NotFoundError should NOT be transient")
	}
	if isPermanentAntdError(err) {
		t.Error("NotFoundError should NOT be classified as permanent")
	}
}

func TestIsTransientAntdError_GenericError(t *testing.T) {
	err := errors.New("something went wrong")
	if isTransientAntdError(err) {
		t.Error("generic error should NOT be transient")
	}
	if isPermanentAntdError(err) {
		t.Error("generic error should NOT be permanent")
	}
}

func TestIsTransientAntdError_WrappedNetworkError(t *testing.T) {
	inner := &antd.NetworkError{AntdError: antd.AntdError{StatusCode: 502, Message: "timeout"}}
	wrapped := errors.Join(errors.New("upload context"), inner)
	if !isTransientAntdError(wrapped) {
		t.Error("wrapped NetworkError should still be transient")
	}
}

func TestIsPermanentAntdError_WrappedBadRequest(t *testing.T) {
	inner := &antd.BadRequestError{AntdError: antd.AntdError{StatusCode: 400, Message: "bad"}}
	wrapped := errors.Join(errors.New("validation failed"), inner)
	if !isPermanentAntdError(wrapped) {
		t.Error("wrapped BadRequestError should still be permanent")
	}
}

// --- TempUploadDir tests ---

func TestTempUploadDir_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{DataDir: tmpDir}

	dir := TempUploadDir(cfg)

	expected := filepath.Join(tmpDir, "uploads", "tmp")
	if dir != expected {
		t.Errorf("dir = %q, want %q", dir, expected)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("path should be a directory")
	}
}

func TestTempUploadDir_IdempotentCreation(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{DataDir: tmpDir}

	// Call twice -- second call should not fail
	dir1 := TempUploadDir(cfg)
	dir2 := TempUploadDir(cfg)

	if dir1 != dir2 {
		t.Errorf("dir1 = %q, dir2 = %q, should be equal", dir1, dir2)
	}
}

func TestTempUploadDir_WritableDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{DataDir: tmpDir}

	dir := TempUploadDir(cfg)

	// Verify we can write a file in the directory
	testFile := filepath.Join(dir, "test.tmp")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("cannot write to temp dir: %v", err)
	}
	os.Remove(testFile)
}

// --- errGasBackoff sentinel ---

func TestErrGasBackoff_IsSentinel(t *testing.T) {
	if !errors.Is(errGasBackoff, errGasBackoff) {
		t.Error("errGasBackoff should match itself with errors.Is")
	}

	wrapped := errors.New("other error")
	if errors.Is(wrapped, errGasBackoff) {
		t.Error("unrelated error should not match errGasBackoff")
	}
}

// --- Constants ---

func TestConstants(t *testing.T) {
	if circuitBreakerThreshold != 5 {
		t.Errorf("circuitBreakerThreshold = %d, want 5", circuitBreakerThreshold)
	}
	if circuitBreakerBaseCooldown != 30*time.Second {
		t.Errorf("circuitBreakerBaseCooldown = %v, want 30s", circuitBreakerBaseCooldown)
	}
	if maxTransientRetries != 3 {
		t.Errorf("maxTransientRetries = %d, want 3", maxTransientRetries)
	}
	if maxGasBackoffAttempts != 10 {
		t.Errorf("maxGasBackoffAttempts = %d, want 10", maxGasBackoffAttempts)
	}
}
