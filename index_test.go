package dataforge

import (
	"reflect"
	"sort"
	"testing"
)

// sortedGroups normalizes Groups() output for comparison: each member list
// is sorted, and the result is keyed by representative id.
func sortedGroups(ix *Index) map[uint64][]uint64 {
	raw := ix.Groups()
	out := make(map[uint64][]uint64, len(raw))
	for rep, members := range raw {
		sort.Slice(members, func(i, j int) bool { return members[i] < members[j] })
		out[rep] = members
	}
	return out
}

// The scenario from the requirements: (10,0) (20,7) (30,511) (5,63).
// After the third add there must be two clusters; the fourth merges them
// into one with representative 5, even though the new fingerprint is at
// distance 6 from the first cluster's representative fingerprint (the
// bridge runs through non-representative member 20).
func TestRequiredSequence(t *testing.T) {
	ix := New()
	ix.Add(10, 0)
	ix.Add(20, 7)
	ix.Add(30, 511)

	if got := len(ix.Groups()); got != 2 {
		t.Fatalf("after 3 adds: want 2 clusters, got %d", got)
	}
	if rep, _ := ix.Lookup(10); rep != 10 {
		t.Fatalf("Lookup(10) = %d, want 10", rep)
	}
	if rep, _ := ix.Lookup(20); rep != 10 {
		t.Fatalf("Lookup(20) = %d, want 10", rep)
	}
	if rep, _ := ix.Lookup(30); rep != 30 {
		t.Fatalf("Lookup(30) = %d, want 30", rep)
	}

	ix.Add(5, 63)

	want := map[uint64][]uint64{5: {5, 10, 20, 30}}
	if got := sortedGroups(ix); !reflect.DeepEqual(got, want) {
		t.Fatalf("after 4 adds: got %v, want %v", got, want)
	}
	for _, id := range []uint64{10, 20, 30, 5} {
		if rep, _ := ix.Lookup(id); rep != 5 {
			t.Fatalf("Lookup(%d) = %d, want 5", id, rep)
		}
	}
}

// Every permutation of the same four records must yield the same final
// grouping and representative.
func TestInsertionOrderInvariance(t *testing.T) {
	records := [][2]uint64{{10, 0}, {20, 7}, {30, 511}, {5, 63}}
	want := map[uint64][]uint64{5: {5, 10, 20, 30}}

	perm := []int{0, 1, 2, 3}
	var visit func(i int)
	visit = func(i int) {
		if i == len(perm) {
			ix := New()
			for _, p := range perm {
				ix.Add(records[p][0], records[p][1])
			}
			if got := sortedGroups(ix); !reflect.DeepEqual(got, want) {
				t.Fatalf("permutation %v: got %v, want %v", perm, got, want)
			}
			return
		}
		for j := i; j < len(perm); j++ {
			perm[i], perm[j] = perm[j], perm[i]
			visit(i + 1)
			perm[i], perm[j] = perm[j], perm[i]
		}
	}
	visit(0)
}

func TestLookupUnknown(t *testing.T) {
	ix := New()
	ix.Add(1, 42)
	if _, ok := ix.Lookup(999); ok {
		t.Fatal("Lookup of unknown id must report ok=false")
	}
}
