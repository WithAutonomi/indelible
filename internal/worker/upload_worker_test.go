package worker

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	antd "github.com/WithAutonomi/ant-sdk/antd-go"

	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/evm"
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

// --- classifyFailure (V2-425 / V2-426 failure decision tree) ---

func TestClassifyFailure(t *testing.T) {
	transient := &antd.NetworkError{AntdError: antd.AntdError{StatusCode: 502, Message: "net"}}
	confTimeout := fmt.Errorf("payment: %w", evm.ErrConfirmationTimeout)
	waveFinalize := errors.Join(errFinalizeFailed, transient)
	merklePaid := errors.Join(errPaidNoRetry, errors.New("finalize reverted"))
	permanent := &antd.BadRequestError{AntdError: antd.AntdError{StatusCode: 400, Message: "bad"}}
	generic := errors.New("boom")
	// The exact wrapping shapes the multi-batch merkle branch produces (V2-1056):
	merkleMidPlanPaid := fmt.Errorf("EVM merkle payment failed at batch 3/5 (2 paid): %w",
		errors.Join(errPaidNoRetry, errors.New("transaction reverted")))
	merkleMidPlanTimeout := fmt.Errorf("EVM merkle payment failed (batch 3/5): %w", evm.ErrConfirmationTimeout)
	merklePartialFinalize := errors.Join(errPaidNoRetry, &antd.PartialUploadError{
		AntdError: antd.AntdError{StatusCode: 502, Message: "partial"}, ChunksStored: 300, ChunksFailed: 12, TotalChunks: 312})

	tests := []struct {
		name        string
		err         error
		paymentMade bool
		canRetry    bool
		want        uploadOutcome
	}{
		{"confirmation timeout → preserve unconfirmed", confTimeout, false, true, outcomePreserveUnconfirmed},
		{"confirmation timeout wins even if paid", confTimeout, true, true, outcomePreserveUnconfirmed},
		{"merkle paid-no-retry → preserve paid", merklePaid, true, true, outcomePreservePaid},
		{"wave finalize retries while attempts remain", waveFinalize, true, true, outcomeRetry},
		{"wave finalize exhausted + paid → preserve", waveFinalize, true, false, outcomePreservePaid},
		{"transient retries when nothing paid", transient, false, true, outcomeRetry},
		{"transient NOT retried after payment → preserve", transient, true, true, outcomePreservePaid},
		{"transient exhausted, nothing paid → abandon", transient, false, false, outcomeAbandon},
		{"permanent error, nothing paid → abandon immediately", permanent, false, true, outcomeAbandon},
		{"generic error after payment → preserve", generic, true, true, outcomePreservePaid},
		{"generic error, nothing paid → abandon", generic, false, true, outcomeAbandon},
		{"merkle mid-plan definitive failure after paid batches → preserve paid", merkleMidPlanPaid, true, true, outcomePreservePaid},
		{"merkle mid-plan confirmation timeout → preserve unconfirmed", merkleMidPlanTimeout, true, true, outcomePreserveUnconfirmed},
		{"merkle multi finalize partial → preserve paid (not transient-retried)", merklePartialFinalize, true, true, outcomePreservePaid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyFailure(tt.err, tt.paymentMade, tt.canRetry); got != tt.want {
				t.Errorf("classifyFailure = %d, want %d", got, tt.want)
			}
		})
	}
}

// --- estimatedUploadCost (V2-431 merkle cost ceiling) ---

func TestEstimatedUploadCost_Wave(t *testing.T) {
	p := &antd.PrepareUploadResult{PaymentType: "wave_batch", TotalAmount: "12345"}
	if got := estimatedUploadCost(p); got.String() != "12345" {
		t.Errorf("wave cost = %s, want 12345", got)
	}
}

func TestEstimatedUploadCost_WaveUnparseable(t *testing.T) {
	p := &antd.PrepareUploadResult{PaymentType: "wave_batch", TotalAmount: ""}
	if got := estimatedUploadCost(p); got.Sign() != 0 {
		t.Errorf("empty wave cost = %s, want 0", got)
	}
}

func TestEstimatedUploadCost_Merkle(t *testing.T) {
	// Two pools; cost ceiling = sum of the max candidate amount in each pool.
	p := &antd.PrepareUploadResult{
		PaymentType: "merkle",
		PoolCommitments: []antd.PoolCommitmentEntry{
			{Candidates: []antd.CandidateNodeEntry{{Amount: "10"}, {Amount: "70"}, {Amount: "30"}}},
			{Candidates: []antd.CandidateNodeEntry{{Amount: "5"}, {Amount: "9"}}},
		},
	}
	if got := estimatedUploadCost(p); got.String() != "79" { // 70 + 9
		t.Errorf("merkle cost = %s, want 79", got)
	}
}

func TestEstimatedUploadCost_MerkleMultiBatch(t *testing.T) {
	// Ceiling sums across ALL payment batches, not just the legacy mirror.
	p := &antd.PrepareUploadResult{
		PaymentType: "merkle",
		MerkleBatches: []antd.MerkleBatchEntry{
			{PoolCommitments: []antd.PoolCommitmentEntry{
				{Candidates: []antd.CandidateNodeEntry{{Amount: "10"}, {Amount: "70"}}},
			}},
			{PoolCommitments: []antd.PoolCommitmentEntry{
				{Candidates: []antd.CandidateNodeEntry{{Amount: "5"}, {Amount: "9"}}},
				{Candidates: []antd.CandidateNodeEntry{{Amount: "100"}}},
			}},
		},
	}
	if got := estimatedUploadCost(p); got.String() != "179" { // 70 + 9 + 100
		t.Errorf("multi-batch merkle cost = %s, want 179", got)
	}
}

func TestEstimatedUploadCost_MerkleEmptyBothIsZero(t *testing.T) {
	// No batches and no legacy fields: estimate is zero — the precheck passes
	// and the payment branch then fails with the clear version-gap error.
	p := &antd.PrepareUploadResult{PaymentType: "merkle"}
	if got := estimatedUploadCost(p); got.Sign() != 0 {
		t.Errorf("empty merkle cost = %s, want 0", got)
	}
}

// --- multi-batch merkle helpers (V2-1056) ---

func fullPool(amount string) antd.PoolCommitmentEntry {
	pc := antd.PoolCommitmentEntry{PoolHash: "0x01"}
	for i := 0; i < evm.MerklePoolCandidateCount; i++ {
		pc.Candidates = append(pc.Candidates, antd.CandidateNodeEntry{Amount: amount})
	}
	return pc
}

func TestMerkleBatchPlan_UsesBatches(t *testing.T) {
	batches := []antd.MerkleBatchEntry{
		{Depth: 8, MerklePaymentTimestamp: 111},
		{Depth: 6, MerklePaymentTimestamp: 222},
		{Depth: 4, MerklePaymentTimestamp: 333},
	}
	got, err := merkleBatchPlan(&antd.PrepareUploadResult{PaymentType: "merkle", MerkleBatches: batches})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 || got[0].MerklePaymentTimestamp != 111 || got[2].MerklePaymentTimestamp != 333 {
		t.Errorf("plan = %+v, want the 3 batches in order", got)
	}
}

func TestMerkleBatchPlan_LegacyFallback(t *testing.T) {
	p := &antd.PrepareUploadResult{
		PaymentType:            "merkle",
		Depth:                  5,
		PoolCommitments:        []antd.PoolCommitmentEntry{fullPool("10")},
		MerklePaymentTimestamp: 999,
	}
	got, err := merkleBatchPlan(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Depth != 5 || got[0].MerklePaymentTimestamp != 999 || len(got[0].PoolCommitments) != 1 {
		t.Errorf("plan = %+v, want one batch synthesized from the legacy fields", got)
	}
}

func TestMerkleBatchPlan_PrefersBatchesOverLegacyMirror(t *testing.T) {
	// antd >= 0.12.0 single-batch shape: MerkleBatches[0] AND the legacy mirror.
	p := &antd.PrepareUploadResult{
		PaymentType:            "merkle",
		Depth:                  5,
		PoolCommitments:        []antd.PoolCommitmentEntry{fullPool("10")},
		MerklePaymentTimestamp: 999,
		MerkleBatches:          []antd.MerkleBatchEntry{{Depth: 5, MerklePaymentTimestamp: 999}},
	}
	got, err := merkleBatchPlan(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || len(got[0].PoolCommitments) != 0 {
		t.Errorf("plan = %+v, want MerkleBatches verbatim (not a legacy synthesis)", got)
	}
}

func TestMerkleBatchPlan_EmptyBothVersionGapError(t *testing.T) {
	_, err := merkleBatchPlan(&antd.PrepareUploadResult{PaymentType: "merkle"})
	if err == nil {
		t.Fatal("want error for merkle prepare with no batches and no legacy fields")
	}
	if !strings.Contains(err.Error(), "antd >= 0.12.0") {
		t.Errorf("error %q should name the version gap", err)
	}
}

func TestValidateMerkleBatches(t *testing.T) {
	shortPool := antd.PoolCommitmentEntry{Candidates: make([]antd.CandidateNodeEntry, 15)}
	for i := range shortPool.Candidates {
		shortPool.Candidates[i].Amount = "1"
	}
	badAmountPool := fullPool("1")
	badAmountPool.Candidates[3].Amount = "not-a-number"

	tests := []struct {
		name    string
		batches []antd.MerkleBatchEntry
		wantErr string // empty = valid
	}{
		{"valid multi-batch", []antd.MerkleBatchEntry{
			{PoolCommitments: []antd.PoolCommitmentEntry{fullPool("1"), fullPool("2")}},
			{PoolCommitments: []antd.PoolCommitmentEntry{fullPool("3")}},
		}, ""},
		{"batch without pools", []antd.MerkleBatchEntry{
			{PoolCommitments: []antd.PoolCommitmentEntry{fullPool("1")}},
			{},
		}, "batch 2/2 has no pool commitments"},
		{"wrong candidate count names batch and pool", []antd.MerkleBatchEntry{
			{PoolCommitments: []antd.PoolCommitmentEntry{fullPool("1")}},
			{PoolCommitments: []antd.PoolCommitmentEntry{fullPool("1"), shortPool}},
		}, "batch 2/2 pool 1: expected 16 candidates, got 15"},
		{"unparseable amount names batch and pool", []antd.MerkleBatchEntry{
			{PoolCommitments: []antd.PoolCommitmentEntry{badAmountPool}},
		}, `batch 1/1 pool 0: invalid candidate amount "not-a-number"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateMerkleBatches(tt.batches)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || err.Error() != tt.wantErr {
				t.Errorf("error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestPadWinnerList(t *testing.T) {
	got := padWinnerList([]string{"0xa", "0xb"}, 5)
	want := []string{"0xa", "0xb", "", "", ""}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("padded[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if full := padWinnerList([]string{"0xa"}, 1); len(full) != 1 || full[0] != "0xa" {
		t.Errorf("already-full list changed: %v", full)
	}
	if empty := padWinnerList(nil, 2); len(empty) != 2 || empty[0] != "" || empty[1] != "" {
		t.Errorf("nil winners = %v, want two empty entries", empty)
	}
}

func TestShouldSalvageMerkle(t *testing.T) {
	definitive := errors.New("transaction reverted: 0xdead")
	timeout := fmt.Errorf("payment: %w", evm.ErrConfirmationTimeout)

	if !shouldSalvageMerkle(definitive, 2) {
		t.Error("definitive failure with paid batches should salvage")
	}
	if shouldSalvageMerkle(definitive, 0) {
		t.Error("nothing paid → nothing to salvage")
	}
	if shouldSalvageMerkle(timeout, 2) {
		t.Error("confirmation timeout must NOT salvage — the tx may still mine")
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
