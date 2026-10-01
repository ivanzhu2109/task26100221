package simhash

import "math/bits"

// numBands is the number of bit-position groups the 64 fingerprint bits are
// partitioned into. If two fingerprints differ in at most 3 bit positions, at
// least one of the 4 bands contains no differing bit, so the two share that
// band's 16-bit key exactly (pigeonhole).
const numBands = 4

// bandKey extracts the 16 fingerprint bits at positions p where p%numBands==band
// and packs them contiguously. Interleaving bit positions across bands (rather
// than using contiguous 16-bit slices) keeps adversarial inputs such as "same
// high 32 bits" from colliding wholesale in any single band.
func bandKey(fp uint64, band int) uint16 {
	var key uint16
	for i := 0; i < 16; i++ {
		bit := (fp >> (band + i*numBands)) & 1
		key |= uint16(bit) << i
	}
	return key
}

// hamming returns the Hamming distance between two fingerprints.
func hamming(a, b uint64) int {
	return bits.OnesCount64(a ^ b)
}
