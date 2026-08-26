package mass

import (
	"math"

	"github.com/example/aac-block-masonry-admission-closure/internal/domain"
)

// ReclaimPPM computes the integer parts-per-million reclaim ratio
// reclaim/total, using overflow-checked 128-bit intermediate multiplication.
// A zero or negative total is rejected.
func ReclaimPPM(reclaimGrams, totalGrams int64) (int64, error) {
	if totalGrams <= 0 {
		return 0, domain.New(domain.CodeInvalidArgument, "total input mass must be positive")
	}
	if reclaimGrams < 0 {
		return 0, domain.New(domain.CodeInvalidArgument, "reclaim mass must be non-negative")
	}
	// reclaim * 1e6 may overflow int64; use 128-bit math.
	hi, lo := mulU64(uint64(reclaimGrams), 1_000_000)
	if hi != 0 || lo > uint64(math.MaxInt64) {
		return 0, domain.New(domain.CodeFixedPointOverflow, "reclaim ratio overflow")
	}
	return int64(lo) / totalGrams, nil
}

// CheckReclaim enforces the locked reclaim upper bound: the integer-PPM reclaim
// ratio must not exceed maxPPM.
func CheckReclaim(reclaimGrams, totalGrams, maxPPM int64) error {
	ppm, err := ReclaimPPM(reclaimGrams, totalGrams)
	if err != nil {
		return err
	}
	if ppm > maxPPM {
		return domain.Newf(domain.CodeReclaimRatioExceeded, "reclaim ratio %d ppm exceeds limit %d ppm", ppm, maxPPM)
	}
	return nil
}

// mulU64 multiplies two unsigned 64-bit values, returning the 128-bit result as
// high and low words.
func mulU64(a, b uint64) (hi, lo uint64) {
	return mul64(a, b)
}

// mul64 is a branch-free 128-bit multiplication of two uint64 values.
func mul64(a, b uint64) (uint64, uint64) {
	al, ah := uint64(uint32(a)), uint64(a>>32)
	bl, bh := uint64(uint32(b)), uint64(b>>32)
	ll := al * bl
	lh := al*bh + ah*bl
	lo := ll + (lh << 32)
	hi := ah*bh + (lh >> 32)
	// carry from lo into hi
	if lo < ll {
		hi++
	}
	return hi, lo
}
