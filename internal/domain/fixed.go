package domain

import (
	"encoding/json"
	"fmt"
	"math"
	"math/bits"
)

// Fixed represents an integer fixed-point quantity with an explicit positive
// decimal scale. It guarantees cross-platform deterministic arithmetic and
// half-away-from-zero rounding as required by the domain rules.
//
// The zero value is "0" with scale 0 and must not be used for arithmetic;
// construct values with NewFixed or FromScaled.
type Fixed struct {
	scaled int64
	scale  int
}

// NewFixed constructs a Fixed value from an integer significand and a
// non-negative decimal scale.
func NewFixed(significand int64, scale int) (Fixed, error) {
	if scale < 0 {
		return Fixed{}, New(CodeInvalidArgument, "fixed-point scale must be non-negative")
	}
	if scale > 18 {
		return Fixed{}, New(CodeInvalidArgument, "fixed-point scale exceeds supported range")
	}
	return Fixed{scaled: significand, scale: scale}, nil
}

// Scale reports the decimal scale of the value.
func (f Fixed) Scale() int { return f.scale }

// Scaled reports the raw integer significand.
func (f Fixed) Scaled() int64 { return f.scaled }

// MarshalJSON serializes the fixed-point value as its integer significand and
// decimal scale so it round-trips deterministically across persistence and the
// HTTP boundary.
func (f Fixed) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Scaled int64 `json:"scaled"`
		Scale  int   `json:"scale"`
	}{f.scaled, f.scale})
}

// UnmarshalJSON restores a fixed-point value from its scaled/scale pair.
func (f *Fixed) UnmarshalJSON(b []byte) error {
	var raw struct {
		Scaled int64 `json:"scaled"`
		Scale  int   `json:"scale"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if raw.Scale < 0 {
		return fmt.Errorf("negative fixed-point scale")
	}
	f.scaled = raw.Scaled
	f.scale = raw.Scale
	return nil
}

// Sign reports -1, 0 or 1.
func (f Fixed) Sign() int {
	switch {
	case f.scaled < 0:
		return -1
	case f.scaled > 0:
		return 1
	default:
		return 0
	}
}

// Cmp compares f and o at a common scale, returning -1, 0 or 1. It rejects
// intermediate rescaling overflow.
func (f Fixed) Cmp(o Fixed) (int, error) {
	av, bv, _, err := rescale(f, o)
	if err != nil {
		return 0, err
	}
	switch {
	case av < bv:
		return -1, nil
	case av > bv:
		return 1, nil
	default:
		return 0, nil
	}
}

// rescale aligns two values to a common scale, rejecting overflow.
func rescale(a, b Fixed) (int64, int64, int, error) {
	scale := a.scale
	if b.scale > scale {
		scale = b.scale
	}
	av, err := a.scaleTo(scale)
	if err != nil {
		return 0, 0, 0, err
	}
	bv, err := b.scaleTo(scale)
	if err != nil {
		return 0, 0, 0, err
	}
	return av, bv, scale, nil
}

func (f Fixed) scaleTo(target int) (int64, error) {
	if f.scale == target {
		return f.scaled, nil
	}
	if f.scale > target {
		return 0, New(CodeFixedPointOverflow, "cannot reduce scale")
	}
	diff := target - f.scale
	if diff > 18 {
		return 0, New(CodeFixedPointOverflow, "scale difference exceeds supported range")
	}
	factor := int64(1)
	for i := 0; i < diff; i++ {
		if factor > math.MaxInt64/10 {
			return 0, New(CodeFixedPointOverflow, "scale factor overflow")
		}
		factor *= 10
	}
	hi, lo := bits.Mul64(uint64(f.scaled), uint64(factor))
	if hi != 0 {
		return 0, New(CodeFixedPointOverflow, "fixed-point scaling overflow")
	}
	if uint64(int64(lo)) != lo {
		return 0, New(CodeFixedPointOverflow, "fixed-point scaling overflow")
	}
	return int64(lo), nil
}

// Add returns a+b, rejecting intermediate overflow.
func (a Fixed) Add(b Fixed) (Fixed, error) {
	av, bv, scale, err := rescale(a, b)
	if err != nil {
		return Fixed{}, err
	}
	sum, ok := addChecked(av, bv)
	if !ok {
		return Fixed{}, New(CodeFixedPointOverflow, "fixed-point addition overflow")
	}
	return Fixed{scaled: sum, scale: scale}, nil
}

// Sub returns a-b, rejecting intermediate overflow.
func (a Fixed) Sub(b Fixed) (Fixed, error) {
	av, bv, scale, err := rescale(a, b)
	if err != nil {
		return Fixed{}, err
	}
	diff, ok := subChecked(av, bv)
	if !ok {
		return Fixed{}, New(CodeFixedPointOverflow, "fixed-point subtraction overflow")
	}
	return Fixed{scaled: diff, scale: scale}, nil
}

// Mul returns a*b with half-away-from-zero rounding at the target scale.
func (a Fixed) Mul(b Fixed) (Fixed, error) {
	if a.scale != b.scale {
		return Fixed{}, New(CodeInvalidArgument, "multiplication requires equal scales")
	}
	hi, lo := bits.Mul64(uint64(a.scaled), uint64(b.scaled))
	if hi != 0 {
		return Fixed{}, New(CodeFixedPointOverflow, "fixed-point multiplication overflow")
	}
	signed := int64(lo)
	if a.scaled < 0 {
		signed = -signed
	}
	// The product now lives at scale 2*scale; round half-away-from-zero down
	// to the original scale.
	rounded := roundHalfAwayFromZero(signed, a.scale)
	return Fixed{scaled: rounded, scale: a.scale}, nil
}

// Div returns a/b with half-away-from-zero rounding at the target scale.
func (a Fixed) Div(b Fixed) (Fixed, error) {
	if b.scaled == 0 {
		return Fixed{}, New(CodeInvalidArgument, "division by zero")
	}
	if a.scale != b.scale {
		return Fixed{}, New(CodeInvalidArgument, "division requires equal scales")
	}
	if a.scaled == math.MinInt64 {
		return Fixed{}, New(CodeFixedPointOverflow, "dividend underflow")
	}
	if b.scaled == math.MinInt64 {
		return Fixed{}, New(CodeFixedPointOverflow, "divisor underflow")
	}
	scale := a.scale
	an := abs64(a.scaled)
	bn := abs64(b.scaled)
	// Scale the numerator by 10^scale using 128-bit intermediate math so the
	// quotient keeps the original scale. Reject on overflow of the high word.
	hi, lo := bits.Mul64(uint64(an), pow10(scale))
	if hi != 0 {
		return Fixed{}, New(CodeFixedPointOverflow, "fixed-point division overflow")
	}
	q := lo / uint64(bn)
	rem := lo % uint64(bn)
	// Half-away-from-zero rounding on the positive magnitude.
	if rem*2 >= uint64(bn) {
		q++
	}
	neg := (a.scaled < 0) != (b.scaled < 0)
	if neg {
		if q > uint64(math.MaxInt64)+1 {
			return Fixed{}, New(CodeFixedPointOverflow, "fixed-point division overflow")
		}
		return Fixed{scaled: -int64(q), scale: scale}, nil
	}
	if q > uint64(math.MaxInt64) {
		return Fixed{}, New(CodeFixedPointOverflow, "fixed-point division overflow")
	}
	return Fixed{scaled: int64(q), scale: scale}, nil
}

// roundHalfAwayFromZero rounds an integer value at the given decimal scale,
// discarding scale low-order decimal digits with half-away-from-zero rounding.
func roundHalfAwayFromZero(v int64, scale int) int64 {
	if scale <= 0 {
		return v
	}
	div := int64(pow10(scale))
	q := v / div
	r := v % div
	if r < 0 {
		r = -r
	}
	if r*2 >= div {
		if v >= 0 {
			q++
		} else {
			q--
		}
	}
	return q
}

func pow10(n int) uint64 {
	p := uint64(1)
	for i := 0; i < n; i++ {
		p *= 10
	}
	return p
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func addChecked(a, b int64) (int64, bool) {
	sum := a + b
	if (b > 0 && sum < a) || (b < 0 && sum > a) {
		return 0, false
	}
	return sum, true
}

func subChecked(a, b int64) (int64, bool) {
	if b == math.MinInt64 {
		return 0, false
	}
	return addChecked(a, -b)
}
