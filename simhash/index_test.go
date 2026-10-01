package simhash

import (
	"reflect"
	"testing"
)

// The scenario from the spec: after the third Add there must be two clusters;
// the fourth fingerprint is at distance 6 from the first cluster's
// representative but links through a non-representative member, merging
// everything into one cluster represented by 5.
func TestSpecifiedSequence(t *testing.T) {
	x := New()
	x.Add(10, 0)
	x.Add(20, 7)
	x.Add(30, 511)

	if got, _ := x.Lookup(10); got != 10 {
		t.Fatalf("Lookup(10) = %d, want 10", got)
	}
	if got, _ := x.Lookup(20); got != 10 {
		t.Fatalf("Lookup(20) = %d, want 10 (0 vs 7 differ in 3 bits)", got)
	}
	if got, _ := x.Lookup(30); got != 30 {
		t.Fatalf("Lookup(30) = %d, want 30 (still its own cluster)", got)
	}
	if n := len(x.Groups()); n != 2 {
		t.Fatalf("after 3 adds: %d groups, want 2", n)
	}

	x.Add(5, 63)
	for _, id := range []uint64{5, 10, 20, 30} {
		if got, _ := x.Lookup(id); got != 5 {
			t.Fatalf("Lookup(%d) = %d, want 5", id, got)
		}
	}
	groups := x.Groups()
	want := []Group{{Representative: 5, Members: []uint64{5, 10, 20, 30}}}
	if !reflect.DeepEqual(groups, want) {
		t.Fatalf("Groups() = %+v, want %+v", groups, want)
	}
}

// The final grouping and representatives must not depend on insertion order.
func TestInsertionOrderIndependence(t *testing.T) {
	records := [][2]uint64{{10, 0}, {20, 7}, {30, 511}, {5, 63}}
	want := []Group{{Representative: 5, Members: []uint64{5, 10, 20, 30}}}

	perm := []int{0, 1, 2, 3}
	var permute func(k int)
	permute = func(k int) {
		if k == len(perm) {
			x := New()
			for _, i := range perm {
				x.Add(records[i][0], records[i][1])
			}
			if got := x.Groups(); !reflect.DeepEqual(got, want) {
				t.Fatalf("order %v: Groups() = %+v, want %+v", perm, got, want)
			}
			for _, r := range records {
				if got, _ := x.Lookup(r[0]); got != 5 {
					t.Fatalf("order %v: Lookup(%d) = %d, want 5", perm, r[0], got)
				}
			}
			return
		}
		for i := k; i < len(perm); i++ {
			perm[k], perm[i] = perm[i], perm[k]
			permute(k + 1)
			perm[k], perm[i] = perm[i], perm[k]
		}
	}
	permute(0)
}

func TestDuplicateIDsPanic(t *testing.T) {
	x := New()
	x.Add(1, 42)
	defer func() {
		if recover() == nil {
			t.Fatal("Add with duplicate id did not panic")
		}
	}()
	x.Add(1, 42)
}

func TestLookupMissing(t *testing.T) {
	x := New()
	if _, ok := x.Lookup(999); ok {
		t.Fatal("Lookup of unknown id returned ok=true")
	}
}

// Many records sharing one fingerprint must all land in one cluster and must
// not disappear from Groups.
func TestMassDuplicateFingerprints(t *testing.T) {
	x := New()
	const n = 1000
	for i := 0; i < n; i++ {
		x.Add(uint64(i+1), 0xDEADBEEF)
	}
	groups := x.Groups()
	if len(groups) != 1 {
		t.Fatalf("%d groups, want 1", len(groups))
	}
	if len(groups[0].Members) != n {
		t.Fatalf("cluster has %d members, want %d", len(groups[0].Members), n)
	}
	if groups[0].Representative != 1 {
		t.Fatalf("representative = %d, want 1", groups[0].Representative)
	}
}
