package evm

import (
	"math/big"
	"testing"

	antd "github.com/WithAutonomi/ant-sdk/antd-go"
)

func TestMaxMerklePayout(t *testing.T) {
	mk := func(amounts ...int64) MerklePoolCommitment {
		var pc MerklePoolCommitment
		for i, a := range amounts {
			pc.Candidates[i] = MerkleCandidateNode{Amount: big.NewInt(a)}
		}
		// remaining candidates stay zero-valued (Amount == nil)
		return pc
	}

	tests := []struct {
		name        string
		commitments []MerklePoolCommitment
		want        int64
	}{
		{"empty", nil, 0},
		{"single pool, max is last", []MerklePoolCommitment{mk(10, 50, 30)}, 50},
		{"single pool, max is first", []MerklePoolCommitment{mk(99, 1, 2)}, 99},
		{"two pools sum of maxes", []MerklePoolCommitment{mk(10, 50), mk(7, 3, 100)}, 150},
		{"pool with all nil candidates", []MerklePoolCommitment{{}}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := maxMerklePayout(tt.commitments)
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
	batch := func(pools ...antd.PoolCommitmentEntry) antd.MerkleBatchEntry {
		return antd.MerkleBatchEntry{Depth: 8, PoolCommitments: pools}
	}

	tests := []struct {
		name    string
		batches []antd.MerkleBatchEntry
		want    string
	}{
		{"empty", nil, "0"},
		{"single batch equals per-batch max sum", []antd.MerkleBatchEntry{
			batch(pool("10", "70", "30"), pool("5", "9")),
		}, "79"},
		{"multi batch sums across batches", []antd.MerkleBatchEntry{
			batch(pool("10", "70", "30"), pool("5", "9")), // 79
			batch(pool("100"), pool("1", "2", "3")),       // 103
		}, "182"},
		{"unparseable amounts contribute zero", []antd.MerkleBatchEntry{
			batch(pool("not-a-number", "40")),
			batch(pool("", "7")),
		}, "47"},
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
