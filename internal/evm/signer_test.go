package evm

import (
	"math/big"
	"testing"

	antd "github.com/WithAutonomi/ant-sdk/antd-go"
)

// mkPool builds a full 16-candidate ABI pool. Fewer than 16 amounts are
// padded with nils (counted as zero quotes), mirroring how a partially
// populated pool would price.
func mkPool(amounts ...int64) MerklePoolCommitment {
	var pc MerklePoolCommitment
	for i, a := range amounts {
		pc.Candidates[i] = MerkleCandidateNode{Amount: big.NewInt(a)}
	}
	return pc
}

func full16(v int64) []int64 {
	out := make([]int64, 16)
	for i := range out {
		out[i] = v
	}
	return out
}

func TestMaxMerklePayout(t *testing.T) {
	// PaymentVaultV2 charges median16(winner pool) * 2^depth; the bound takes
	// the highest pool median.
	oneToSixteen := make([]int64, 16)
	for i := range oneToSixteen {
		oneToSixteen[i] = int64(i + 1)
	}

	tests := []struct {
		name        string
		depth       int
		commitments []MerklePoolCommitment
		want        int64
	}{
		{"empty", 4, nil, 0},
		{"uniform pool: median times 2^depth", 3, []MerklePoolCommitment{mkPool(full16(10)...)}, 80},
		{"median16 is the upper median (index 8)", 2, []MerklePoolCommitment{mkPool(oneToSixteen...)}, 36}, // sorted[8] = 9, << 2
		{"highest pool median wins", 4, []MerklePoolCommitment{mkPool(full16(5)...), mkPool(full16(7)...)}, 112},
		// 8 explicit amounts + 8 nil-padded zeros: the zeros sort first, so
		// sorted index 8 is the smallest real quote (10); depth 0 leaves it
		// unshifted.
		{"nil amounts count as zero quotes", 0, []MerklePoolCommitment{mkPool(10, 20, 30, 40, 50, 60, 70, 80)}, 10},
		{"depth clamped to contract max 8", 20, []MerklePoolCommitment{mkPool(full16(1)...)}, 256},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := maxMerklePayout(tt.depth, tt.commitments)
			if got.Cmp(big.NewInt(tt.want)) != 0 {
				t.Errorf("maxMerklePayout = %s, want %d", got, tt.want)
			}
		})
	}
}

func TestMaxMerkleBatchesPayout(t *testing.T) {
	pool := func(amounts ...string) antd.PoolCommitmentEntry {
		var pc antd.PoolCommitmentEntry
		for _, a := range amounts {
			pc.Candidates = append(pc.Candidates, antd.CandidateNodeEntry{Amount: a})
		}
		return pc
	}

	tests := []struct {
		name    string
		batches []antd.MerkleBatchEntry
		want    string
	}{
		{"empty", nil, "0"},
		{"single batch: max pool median shifted by depth", []antd.MerkleBatchEntry{
			{Depth: 3, PoolCommitments: []antd.PoolCommitmentEntry{
				pool("10", "70", "30"), // sorted [10 30 70], median idx 1 = 30
				pool("5", "9"),         // median idx 1 = 9
			}},
		}, "240"}, // 30 << 3
		{"multi batch sums per-batch charges", []antd.MerkleBatchEntry{
			{Depth: 3, PoolCommitments: []antd.PoolCommitmentEntry{pool("10", "70", "30"), pool("5", "9")}}, // 240
			{Depth: 1, PoolCommitments: []antd.PoolCommitmentEntry{pool("100")}},                            // 100 << 1
		}, "440"},
		{"unparseable amounts count as zero quotes", []antd.MerkleBatchEntry{
			{Depth: 1, PoolCommitments: []antd.PoolCommitmentEntry{pool("not-a-number", "40")}}, // sorted [0 40] median idx 1 = 40
		}, "80"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MaxMerkleBatchesPayout(tt.batches)
			if got.String() != tt.want {
				t.Errorf("MaxMerkleBatchesPayout = %s, want %s", got, tt.want)
			}
		})
	}
}
