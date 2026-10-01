package dataforge

import (
	"math/bits"
	"math/rand"
	"reflect"
	"sort"
	"testing"
)

// referenceGroups is an independent brute-force implementation: it rebuilds
// the full O(n^2) distance graph on every call and returns clusters keyed
// by their smallest id, with sorted member lists. Only usable for small n.
func referenceGroups(ids []uint64, fps []uint64) map[uint64][]uint64 {
	n := len(ids)
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if bits.OnesCount64(fps[i]^fps[j]) <= 3 {
				parent[find(i)] = find(j)
			}
		}
	}
	byRoot := make(map[int][]uint64)
	for i := 0; i < n; i++ {
		r := find(i)
		byRoot[r] = append(byRoot[r], ids[i])
	}
	out := make(map[uint64][]uint64, len(byRoot))
	for _, members := range byRoot {
		sort.Slice(members, func(i, j int) bool { return members[i] < members[j] })
		out[members[0]] = members
	}
	return out
}

// makeRandomRecords builds a deterministic sequence exercising distance-3
// and distance-4 pairs, top-bit differences, duplicate fingerprints, and
// multi-hop chains that merge into large clusters.
func makeRandomRecords(rng *rand.Rand, n int) (ids []uint64, fps []uint64) {
	var centers []uint64
	for i := 0; i < 24; i++ {
		centers = append(centers, rng.Uint64())
	}
	// A hand-built multi-hop chain: each hop flips exactly 3 fresh bits, so
	// endpoints are far apart yet every hop is an edge. Bit 63 included.
	chain := uint64(0)
	for hop := 0; hop < 6; hop++ {
		centers = append(centers, chain)
		chain ^= 1 << (9 * hop) // bits 0,9,18,27,36,45
		chain ^= 1 << (9*hop + 1)
		chain ^= 1 << (9*hop + 2)
	}
	centers = append(centers, chain^(1<<63)) // far in value, close to chain end? no: 1 bit from chain end

	used := make(map[uint64]bool)
	nextID := uint64(1)
	for len(ids) < n {
		var fp uint64
		switch r := rng.Intn(10); {
		case r < 5 && len(fps) > 0:
			// Mutate a center by 0..4 bits (0 gives duplicate fingerprints
			// whenever the center was already emitted).
			fp = centers[rng.Intn(len(centers))]
			for k := rng.Intn(5); k > 0; k-- {
				fp ^= 1 << rng.Intn(64)
			}
		case r < 7 && len(fps) > 0:
			// Exact duplicate of a random earlier fingerprint.
			fp = fps[rng.Intn(len(fps))]
		case r < 9 && len(fps) > 0:
			// Near copy of an earlier fingerprint at distance exactly 3 or 4.
			fp = fps[rng.Intn(len(fps))]
			d := 3 + rng.Intn(2)
			for k := 0; k < d; k++ {
				fp ^= 1 << rng.Intn(64)
			}
		default:
			fp = rng.Uint64()
		}
		ids = append(ids, nextID)
		nextID++
		fps = append(fps, fp)
		_ = used
	}
	return ids, fps
}

// After every single Add, the index must reproduce the brute-force grouping
// exactly, and every Lookup must agree with it.
func TestAgainstReference(t *testing.T) {
	rng := rand.New(rand.NewSource(20261001))
	ids, fps := makeRandomRecords(rng, 400)
	assertSampleCoverage(t, ids, fps)

	ix := New()
	for k := range ids {
		ix.Add(ids[k], fps[k])

		want := referenceGroups(ids[:k+1], fps[:k+1])
		if got := sortedGroups(ix); !reflect.DeepEqual(got, want) {
			t.Fatalf("after add #%d (id=%d fp=%x):\ngot  %v\nwant %v", k+1, ids[k], fps[k], got, want)
		}
		for j := 0; j <= k; j++ {
			rep, ok := ix.Lookup(ids[j])
			if !ok {
				t.Fatalf("after add #%d: Lookup(%d) not found", k+1, ids[j])
			}
			found := false
			for wantRep, members := range want {
				for _, m := range members {
					if m == ids[j] {
						if rep != wantRep {
							t.Fatalf("after add #%d: Lookup(%d)=%d, want %d", k+1, ids[j], rep, wantRep)
						}
						found = true
					}
				}
			}
			if !found {
				t.Fatalf("after add #%d: id %d missing from reference", k+1, ids[j])
			}
		}
	}
}

// assertSampleCoverage fails unless the generated sample actually contains
// the required structural features, so the cross-check cannot silently
// degenerate to trivial input.
func assertSampleCoverage(t *testing.T, ids, fps []uint64) {
	t.Helper()
	var dist3, dist4, topBit, dupes int
	seen := make(map[uint64]bool)
	for _, fp := range fps {
		if seen[fp] {
			dupes++
		}
		seen[fp] = true
	}
	for i := 0; i < len(fps); i++ {
		for j := i + 1; j < len(fps); j++ {
			switch d := bits.OnesCount64(fps[i] ^ fps[j]); {
			case d == 3:
				dist3++
			case d == 4:
				dist4++
			}
			if fps[i]>>63 != fps[j]>>63 && bits.OnesCount64(fps[i]^fps[j]) <= 4 {
				topBit++
			}
		}
	}
	maxCluster := 0
	for _, members := range referenceGroups(ids, fps) {
		if len(members) > maxCluster {
			maxCluster = len(members)
		}
	}
	t.Logf("sample features: dist==3 pairs=%d dist==4 pairs=%d top-bit pairs=%d duplicate fps=%d maxCluster=%d",
		dist3, dist4, topBit, dupes, maxCluster)
	if dist3 == 0 || dist4 == 0 || topBit == 0 || dupes == 0 || maxCluster < 8 {
		t.Fatalf("sample lacks required features: dist3=%d dist4=%d topBit=%d dupes=%d maxCluster=%d",
			dist3, dist4, topBit, dupes, maxCluster)
	}
}
