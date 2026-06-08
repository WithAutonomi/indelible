package evm

import (
	"math/big"
	"testing"
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
