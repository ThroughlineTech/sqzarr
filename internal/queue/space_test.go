package queue

import (
	"math"
	"testing"
)

func TestAvailableSpace(t *testing.T) {
	for _, tc := range []struct {
		name      string
		blocks    uint64
		blockSize int64
		needed    uint64
		want      bool
		wantErr   bool
	}{
		{"negative block size", 100, -1, 1, false, true},
		{"zero block size", 100, 0, 1, false, true},
		{"exact capacity", 2, 4096, 8192, true, false},
		{"partial block requires another", 2, 4096, 8193, false, false},
		{"huge filesystem does not wrap", math.MaxUint64, 4096, 8192, true, false},
		{"maximum byte request", math.MaxUint64, 1, math.MaxUint64, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := availableSpace(tc.blocks, tc.blockSize, tc.needed)
			if got != tc.want || (err != nil) != tc.wantErr {
				t.Fatalf("got (%v, %v), want (%v, error=%v)", got, err, tc.want, tc.wantErr)
			}
		})
	}
}
