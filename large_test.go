package dataforge

import (
	"math/rand"
	"runtime"
	"testing"
	"time"
)

// TestLargeScale is the capacity/performance test. Run it on its own with:
//
//	go test -run TestLargeScale -v .
//
// It adds 200k uniformly random distinct fingerprints, 100k distinct
// fingerprints sharing the same high 32 bits, and 100k records reusing one
// existing fingerprint, all shuffled with a fixed seed. The Add phase must
// finish within 60s and 256 MiB of peak heap.
func TestLargeScale(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large-scale test in -short mode")
	}
	const (
		nUniform = 200_000
		nHigh32  = 100_000
		nDup     = 100_000
	)
	rng := rand.New(rand.NewSource(26100221))

	type rec struct {
		id uint64
		fp uint64
	}
	seen := make(map[uint64]struct{}, nUniform+nHigh32)
	records := make([]rec, 0, nUniform+nHigh32+nDup)

	for len(records) < nUniform {
		fp := rng.Uint64()
		if _, ok := seen[fp]; ok {
			continue
		}
		seen[fp] = struct{}{}
		records = append(records, rec{uint64(len(records) + 1), fp})
	}
	const high = uint64(0xDEADBEEF) << 32
	for len(records) < nUniform+nHigh32 {
		fp := high | uint64(rng.Uint32())
		if _, ok := seen[fp]; ok {
			continue
		}
		seen[fp] = struct{}{}
		records = append(records, rec{uint64(len(records) + 1), fp})
	}
	dupFp := records[0].fp // reuse one existing fingerprint
	dupIDs := make([]uint64, 0, nDup)
	for len(records) < nUniform+nHigh32+nDup {
		id := uint64(len(records) + 1)
		dupIDs = append(dupIDs, id)
		records = append(records, rec{id, dupFp})
	}
	rng.Shuffle(len(records), func(i, j int) { records[i], records[j] = records[j], records[i] })
	seen = nil

	ix := New()
	var mem runtime.MemStats
	var peakHeap uint64
	start := time.Now()
	for i, r := range records {
		ix.Add(r.id, r.fp)
		if i&4095 == 0 {
			runtime.ReadMemStats(&mem)
			if mem.HeapAlloc > peakHeap {
				peakHeap = mem.HeapAlloc
			}
		}
	}
	runtime.ReadMemStats(&mem)
	if mem.HeapAlloc > peakHeap {
		peakHeap = mem.HeapAlloc
	}
	addElapsed := time.Since(start)

	const (
		addBudget  = 60 * time.Second
		heapBudget = 256 << 20
	)
	t.Logf("Add phase: %d records in %v, peak HeapAlloc = %d MiB",
		len(records), addElapsed.Round(time.Millisecond), peakHeap>>20)
	if addElapsed > addBudget {
		t.Errorf("Add phase took %v, budget %v", addElapsed, addBudget)
	}
	if peakHeap > heapBudget {
		t.Errorf("peak heap %d MiB exceeds budget 256 MiB", peakHeap>>20)
	}

	// Verification (outside the Add-phase budget).
	groups := ix.Groups()
	total := 0
	dupRep := uint64(0)
	for rep, members := range groups {
		total += len(members)
		var minID uint64 = ^uint64(0)
		for _, id := range members {
			if id < minID {
				minID = id
			}
			if id >= uint64(nUniform+nHigh32+1) {
				if dupRep == 0 {
					dupRep = rep
				} else if rep != dupRep {
					t.Fatalf("duplicate-fingerprint records split across clusters %d and %d", dupRep, rep)
				}
			}
		}
		if rep != minID {
			t.Fatalf("cluster rep %d is not the min member id %d", rep, minID)
		}
	}
	if total != len(records) {
		t.Fatalf("groups contain %d ids, want %d (each id exactly once)", total, len(records))
	}
	if len(dupIDs) != nDup {
		t.Fatalf("lost duplicate ids: %d", len(dupIDs))
	}
	ownerRep, ok := ix.Lookup(1) // id 1 owns dupFp
	if !ok || ownerRep != dupRep {
		t.Fatalf("duplicate cluster rep = %d, Lookup(1) = %d (ok=%v)", dupRep, ownerRep, ok)
	}
	t.Logf("clusters: %d, duplicate-fingerprint cluster size: %d", len(groups), len(groups[dupRep]))
}
