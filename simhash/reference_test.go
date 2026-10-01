package simhash

import (
	"math/bits"
	"math/rand"
	"reflect"
	"sort"
	"testing"
)

// bruteForce is an independent, exhaustive reference implementation: it keeps
// every record and recomputes connected components from the full pairwise
// distance relation on demand.
type bruteForce struct {
	ids []uint64
	fps []uint64
}

func (b *bruteForce) add(id, fp uint64) {
	b.ids = append(b.ids, id)
	b.fps = append(b.fps, fp)
}

func (b *bruteForce) groups() []Group {
	n := len(b.ids)
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if bits.OnesCount64(b.fps[i]^b.fps[j]) <= 3 {
				ri, rj := find(i), find(j)
				if ri != rj {
					parent[rj] = ri
				}
			}
		}
	}
	byRoot := map[int][]uint64{}
	for i := 0; i < n; i++ {
		r := find(i)
		byRoot[r] = append(byRoot[r], b.ids[i])
	}
	groups := make([]Group, 0, len(byRoot))
	for _, members := range byRoot {
		sort.Slice(members, func(a, c int) bool { return members[a] < members[c] })
		groups = append(groups, Group{Representative: members[0], Members: members})
	}
	sort.Slice(groups, func(a, c int) bool { return groups[a].Representative < groups[c].Representative })
	return groups
}

// fingerprintPool builds a pool stressing the required edge cases: distances
// of exactly 3 and 4, differing most-significant bits, duplicate
// fingerprints, and multi-hop chains that merge into large clusters.
func fingerprintPool(rng *rand.Rand) []uint64 {
	var pool []uint64

	// Random bases plus variants at distance 1..4 (4 must NOT link directly).
	for b := 0; b < 30; b++ {
		base := rng.Uint64()
		pool = append(pool, base)
		for d := 1; d <= 4; d++ {
			v := base
			for _, pos := range rng.Perm(64)[:d] {
				v ^= 1 << pos
			}
			pool = append(pool, v)
		}
	}

	// Pairs differing only in the most significant bit (distance 1 across
	// the top bit) and at distance 3/4 including the top bit.
	for i := 0; i < 20; i++ {
		base := rng.Uint64() & 0x7FFFFFFFFFFFFFFF
		pool = append(pool, base, base|(1<<63))
		v := base | (1 << 63)
		for _, pos := range rng.Perm(63)[:2] {
			v ^= 1 << pos
		}
		pool = append(pool, v) // distance 3 from base, 2 from base|top
		w := base
		for _, pos := range rng.Perm(63)[:3] {
			w ^= 1 << pos
		}
		w |= 1 << 63
		pool = append(pool, w) // distance 4 from base: must not link
	}

	// Multi-hop chains: each hop is distance 2, endpoints are far apart, so
	// large clusters form only through transitivity.
	for c := 0; c < 10; c++ {
		v := rng.Uint64()
		for hop := 0; hop < 12; hop++ {
			pool = append(pool, v)
			for _, pos := range rng.Perm(64)[:2] {
				v ^= 1 << pos
			}
		}
	}

	// Exact duplicates of earlier pool entries.
	for i := 0; i < 30; i++ {
		pool = append(pool, pool[rng.Intn(len(pool))])
	}

	return pool
}

// TestAgainstReference feeds random sequences and, after EVERY Add, compares
// the full grouping (not just cluster counts) against the brute-force
// reference, including per-id Lookup results.
func TestAgainstReference(t *testing.T) {
	rng := rand.New(rand.NewSource(20261001))
	pool := fingerprintPool(rng)

	for trial := 0; trial < 25; trial++ {
		x := New()
		ref := &bruteForce{}
		n := 60 + rng.Intn(60)
		ids := rng.Perm(1000000)[:n] // unique, unsorted ids
		for step := 0; step < n; step++ {
			id := uint64(ids[step] + 1)
			fp := pool[rng.Intn(len(pool))]
			x.Add(id, fp)
			ref.add(id, fp)

			got, want := x.Groups(), ref.groups()
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("trial %d step %d: Groups() = %+v, want %+v", trial, step, got, want)
			}
			for _, g := range want {
				for _, id := range g.Members {
					rep, ok := x.Lookup(id)
					if !ok || rep != g.Representative {
						t.Fatalf("trial %d step %d: Lookup(%d) = %d,%v want %d,true",
							trial, step, id, rep, ok, g.Representative)
					}
				}
			}
		}
	}
}
