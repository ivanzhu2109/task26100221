package simhash

import (
	"math/rand"
	"runtime"
	"testing"
	"time"
)

// TestLargeScale is the fixed-seed capacity test. Run it on its own with:
//
//	go test ./simhash/ -run TestLargeScale -v
//
// It adds 200k uniformly random distinct fingerprints, 100k distinct
// fingerprints sharing the same high 32 bits, and 100k records reusing one
// existing fingerprint (400k records total), shuffled into one stream.
// The Add phase must finish within 60s with peak heap <= 256 MiB.
func TestLargeScale(t *testing.T) {
	rng := rand.New(rand.NewSource(42))

	type rec struct {
		id uint64
		fp uint64
	}
	const (
		nUniform = 200_000
		nHigh32  = 100_000
		nDup     = 100_000
	)
	records := make([]rec, 0, nUniform+nHigh32+nDup)
	seenFP := make(map[uint64]struct{}, nUniform+nHigh32)

	var nextID uint64
	for len(records) < nUniform {
		fp := rng.Uint64()
		if _, ok := seenFP[fp]; ok {
			continue
		}
		seenFP[fp] = struct{}{}
		nextID++
		records = append(records, rec{nextID, fp})
	}
	dupFP := records[0].fp // existing fingerprint reused by the 100k duplicates

	const sharedHigh = uint64(0xABCD1234) << 32
	for len(records) < nUniform+nHigh32 {
		fp := sharedHigh | uint64(rng.Uint32())
		if _, ok := seenFP[fp]; ok {
			continue
		}
		seenFP[fp] = struct{}{}
		nextID++
		records = append(records, rec{nextID, fp})
	}
	for i := 0; i < nDup; i++ {
		nextID++
		records = append(records, rec{nextID, dupFP})
	}
	total := len(records)

	rng.Shuffle(total, func(i, j int) { records[i], records[j] = records[j], records[i] })

	// Sample HeapAlloc in the background to approximate peak heap.
	var peak uint64
	stop := make(chan struct{})
	go func() {
		var ms runtime.MemStats
		for {
			select {
			case <-stop:
				return
			default:
				runtime.ReadMemStats(&ms)
				if ms.HeapAlloc > peak {
					peak = ms.HeapAlloc
				}
				time.Sleep(2 * time.Millisecond)
			}
		}
	}()

	runtime.GC()
	x := New()
	start := time.Now()
	for _, r := range records {
		x.Add(r.id, r.fp)
	}
	elapsed := time.Since(start)
	close(stop)

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	if ms.HeapAlloc > peak {
		peak = ms.HeapAlloc
	}
	t.Logf("Add phase: %d records in %v (%.0f rec/s)", total, elapsed, float64(total)/elapsed.Seconds())
	t.Logf("peak HeapAlloc: %.1f MiB", float64(peak)/float64(uint64(1)<<20))

	if elapsed > 60*time.Second {
		t.Fatalf("Add phase took %v, budget is 60s", elapsed)
	}
	if peak > 256<<20 {
		t.Fatalf("peak heap %.1f MiB exceeds 256 MiB budget", float64(peak)/float64(uint64(1)<<20))
	}

	// Every id appears exactly once; every representative is the cluster min.
	groups := x.Groups()
	count := make(map[uint64]int, total)
	dupGroupRep := uint64(0)
	for _, g := range groups {
		min := g.Members[0]
		for _, id := range g.Members {
			if id < min {
				min = id
			}
			count[id]++
		}
		if g.Representative != min {
			t.Fatalf("group representative %d != min member %d", g.Representative, min)
		}
	}
	if len(count) != total {
		t.Fatalf("Groups() covers %d distinct ids, want %d", len(count), total)
	}
	for id, n := range count {
		if n != 1 {
			t.Fatalf("id %d appears %d times in Groups()", id, n)
		}
	}

	// All 100k duplicate-fingerprint records share one cluster with the
	// original carrier of that fingerprint.
	carrierRep, ok := x.Lookup(1) // id 1 carries dupFP
	if !ok {
		t.Fatal("Lookup(1) failed")
	}
	dupGroupRep = carrierRep
	for _, r := range records {
		if r.fp != dupFP {
			continue
		}
		rep, ok := x.Lookup(r.id)
		if !ok || rep != dupGroupRep {
			t.Fatalf("duplicate-fingerprint record id=%d has rep %d,%v, want %d,true", r.id, rep, ok, dupGroupRep)
		}
	}

	// Lookup agrees with Groups() for every id.
	repOf := make(map[uint64]uint64, total)
	for _, g := range groups {
		for _, id := range g.Members {
			repOf[id] = g.Representative
		}
	}
	for id, want := range repOf {
		if got, _ := x.Lookup(id); got != want {
			t.Fatalf("Lookup(%d) = %d, want %d", id, got, want)
		}
	}
	t.Logf("clusters: %d", len(groups))
}
