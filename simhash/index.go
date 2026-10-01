// Package simhash implements DataForge's incremental SimHash dedup index.
//
// Records carry a unique uint64 id and a 64-bit fingerprint. Two records are
// linked when their fingerprints are within Hamming distance 3, and clusters
// are the connected components of that link graph. The cluster representative
// is always the smallest member id. Candidate lookups use a 4-band pigeonhole
// index so Add never scans the full history, and identical fingerprints are
// indexed once so mass duplicates cost O(1) distance computations each.
package simhash

import "sort"

// Group is one cluster: its representative (the minimum member id) and all
// member ids in ascending order.
type Group struct {
	Representative uint64
	Members        []uint64
}

// Index is an incremental exact Hamming-distance-<=3 clustering index over
// 64-bit fingerprints. It is not safe for concurrent use.
type Index struct {
	ids     []uint64          // ids[i] is the id of record i
	fps     []uint64          // fps[i] is the fingerprint of record i
	parent  []uint32          // union-find parent per record
	rank    []uint8           // union-find rank per record
	repMin  []uint64          // repMin[root] is the minimum id in the root's cluster
	byID    map[uint64]uint32 // id -> record index
	fpOwner map[uint64]uint32 // fingerprint -> first record index carrying it
	tables  [numBands]map[uint16][]uint32
	seen    []uint32 // per-record generation stamp for candidate dedup
	gen     uint32   // current candidate-scan generation
}

// New returns an empty Index.
func New() *Index {
	x := &Index{
		byID:    make(map[uint64]uint32),
		fpOwner: make(map[uint64]uint32),
	}
	for b := 0; b < numBands; b++ {
		x.tables[b] = make(map[uint16][]uint32)
	}
	return x
}

// find returns the root of record i, compressing the path.
func (x *Index) find(i uint32) uint32 {
	root := i
	for x.parent[root] != root {
		root = x.parent[root]
	}
	for x.parent[i] != root {
		x.parent[i], i = root, x.parent[i]
	}
	return root
}

// union merges the clusters of records a and b, keeping the smaller minimum
// id as the cluster representative.
func (x *Index) union(a, b uint32) {
	ra, rb := x.find(a), x.find(b)
	if ra == rb {
		return
	}
	if x.rank[ra] < x.rank[rb] {
		ra, rb = rb, ra
	}
	x.parent[rb] = ra
	if x.rank[ra] == x.rank[rb] {
		x.rank[ra]++
	}
	if x.repMin[rb] < x.repMin[ra] {
		x.repMin[ra] = x.repMin[rb]
	}
}

// Add inserts a record. All cluster merges caused by this insert are complete
// when Add returns. It panics if id was already added.
func (x *Index) Add(id uint64, fingerprint uint64) {
	if _, dup := x.byID[id]; dup {
		panic("simhash: duplicate id")
	}
	idx := uint32(len(x.ids))
	x.ids = append(x.ids, id)
	x.fps = append(x.fps, fingerprint)
	x.parent = append(x.parent, idx)
	x.rank = append(x.rank, 0)
	x.repMin = append(x.repMin, id)
	x.seen = append(x.seen, 0)
	x.byID[id] = idx

	// A fingerprint already in the index contributes identical edges through
	// its first carrier, so duplicates only need a single union and are never
	// inserted into the band tables.
	if owner, ok := x.fpOwner[fingerprint]; ok {
		x.union(idx, owner)
		return
	}
	x.fpOwner[fingerprint] = idx

	x.gen++
	for b := 0; b < numBands; b++ {
		key := bandKey(fingerprint, b)
		for _, cand := range x.tables[b][key] {
			if x.seen[cand] == x.gen {
				continue
			}
			x.seen[cand] = x.gen
			if hamming(fingerprint, x.fps[cand]) <= 3 {
				x.union(idx, cand)
			}
		}
		x.tables[b][key] = append(x.tables[b][key], idx)
	}
}

// Lookup returns the representative (minimum id) of the cluster currently
// containing id, and whether id exists.
func (x *Index) Lookup(id uint64) (uint64, bool) {
	idx, ok := x.byID[id]
	if !ok {
		return 0, false
	}
	return x.repMin[x.find(idx)], true
}

// Groups returns all clusters, sorted by representative. Every added id
// appears in exactly one cluster.
func (x *Index) Groups() []Group {
	byRoot := make(map[uint32][]uint64)
	for i := range x.ids {
		root := x.find(uint32(i))
		byRoot[root] = append(byRoot[root], x.ids[i])
	}
	groups := make([]Group, 0, len(byRoot))
	for root, members := range byRoot {
		sort.Slice(members, func(a, b int) bool { return members[a] < members[b] })
		groups = append(groups, Group{Representative: x.repMin[root], Members: members})
	}
	sort.Slice(groups, func(a, b int) bool { return groups[a].Representative < groups[b].Representative })
	return groups
}
