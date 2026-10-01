// Package dataforge implements an incremental SimHash dedup index.
//
// Records carry a unique uint64 id and a 64-bit fingerprint computed
// upstream. Two records are connected when their fingerprints differ in at
// most 3 bits; clusters are the connected components of that graph. The
// representative of a cluster is always the smallest id it contains.
//
// Candidate generation uses 4-band indexing: bit position p of a
// fingerprint belongs to band p&3, so each band holds 16 interleaved bits.
// By the pigeonhole principle, two fingerprints at Hamming distance <= 3
// share at least one of the 4 bands exactly, so an exact-distance filter
// over band-collision candidates is complete. Interleaving keeps bands
// sensitive to bits spread over the whole word, which avoids candidate
// explosions on inputs sharing a common 32-bit half.
package dataforge

import "math/bits"

const (
	numBands  = 4
	bandBytes = 8
	maxDist   = 3
)

// bandTables[band][bytePos][byteValue] is the compressed contribution of
// one fingerprint byte to one band key. Byte j holds bit positions
// 8j..8j+7; bit p feeds band p&3 at compressed position p>>2, so each byte
// contributes exactly 2 bits to each band.
var bandTables [numBands][bandBytes][256]uint16

func init() {
	for b := 0; b < numBands; b++ {
		for j := 0; j < bandBytes; j++ {
			for v := 0; v < 256; v++ {
				var k uint16
				if v>>b&1 != 0 {
					k |= 1 << (2 * j)
				}
				if v>>(b+4)&1 != 0 {
					k |= 1 << (2*j + 1)
				}
				bandTables[b][j][v] = k
			}
		}
	}
}

func bandKeys(fp uint64) (keys [numBands]uint16) {
	for j := 0; j < bandBytes; j++ {
		by := byte(fp >> (8 * j))
		for b := 0; b < numBands; b++ {
			keys[b] |= bandTables[b][j][by]
		}
	}
	return
}

// Index is a single-threaded incremental dedup index. It is not safe for
// concurrent use.
type Index struct {
	fps    []uint64         // distinct fingerprints, addressed by slot
	fpSlot map[uint64]int32 // fingerprint -> slot
	idSlot map[uint64]int32 // record id -> slot of its fingerprint
	parent []int32          // union-find over slots
	size   []int32          // union-find subtree size, valid at roots
	minID  []uint64         // cluster representative, valid at roots
	tables [numBands]map[uint16][]int32
	seen   []int32 // per-slot candidate dedupe stamp
	gen    int32   // current candidate-gathering generation
}

// New returns an empty Index.
func New() *Index {
	ix := &Index{
		fpSlot: make(map[uint64]int32),
		idSlot: make(map[uint64]int32),
	}
	for b := 0; b < numBands; b++ {
		ix.tables[b] = make(map[uint16][]int32)
	}
	return ix
}

func (ix *Index) find(s int32) int32 {
	r := s
	for ix.parent[r] != r {
		r = ix.parent[r]
	}
	for ix.parent[s] != r {
		ix.parent[s], s = r, ix.parent[s]
	}
	return r
}

func (ix *Index) union(a, b int32) {
	ra, rb := ix.find(a), ix.find(b)
	if ra == rb {
		return
	}
	if ix.size[ra] < ix.size[rb] {
		ra, rb = rb, ra
	}
	ix.parent[rb] = ra
	ix.size[ra] += ix.size[rb]
	if ix.minID[rb] < ix.minID[ra] {
		ix.minID[ra] = ix.minID[rb]
	}
}

// Add inserts a record. When Add returns, every cluster merge triggered by
// this insertion is already in effect. Re-adding an existing id is a no-op.
func (ix *Index) Add(id uint64, fingerprint uint64) {
	if _, dup := ix.idSlot[id]; dup {
		return
	}
	if slot, ok := ix.fpSlot[fingerprint]; ok {
		// Duplicate fingerprint: same cluster, no candidate scan needed.
		ix.idSlot[id] = slot
		if r := ix.find(slot); id < ix.minID[r] {
			ix.minID[r] = id
		}
		return
	}

	slot := int32(len(ix.fps))
	ix.fps = append(ix.fps, fingerprint)
	ix.fpSlot[fingerprint] = slot
	ix.idSlot[id] = slot
	ix.parent = append(ix.parent, slot)
	ix.size = append(ix.size, 1)
	ix.minID = append(ix.minID, id)
	ix.seen = append(ix.seen, 0)

	keys := bandKeys(fingerprint)
	ix.gen++
	for b := 0; b < numBands; b++ {
		for _, cand := range ix.tables[b][keys[b]] {
			if ix.seen[cand] == ix.gen {
				continue
			}
			ix.seen[cand] = ix.gen
			if bits.OnesCount64(fingerprint^ix.fps[cand]) <= maxDist {
				ix.union(slot, cand)
			}
		}
	}
	for b := 0; b < numBands; b++ {
		ix.tables[b][keys[b]] = append(ix.tables[b][keys[b]], slot)
	}
}

// Lookup returns the current representative (smallest id) of the cluster
// containing id. ok is false when id was never added.
func (ix *Index) Lookup(id uint64) (rep uint64, ok bool) {
	slot, ok := ix.idSlot[id]
	if !ok {
		return 0, false
	}
	return ix.minID[ix.find(slot)], true
}

// Groups returns all clusters as a map from representative id to the full
// list of member ids. Every added id appears in exactly one group.
func (ix *Index) Groups() map[uint64][]uint64 {
	out := make(map[uint64][]uint64, len(ix.fps))
	for id, slot := range ix.idSlot {
		rep := ix.minID[ix.find(slot)]
		out[rep] = append(out[rep], id)
	}
	return out
}
