package evidence

import (
	"github.com/example/aac-block-masonry-admission-closure/internal/domain"
)

// AutoclavePoint is one temperature/pressure acquisition point appended in
// logical-time order during autoclave. All values use integer fixed point so
// the derived ramp rates and equivalent holding time are cross-platform
// deterministic.
type AutoclavePoint struct {
	LogicalTime domain.LogicalTime `json:"logical_time"`
	Pressure    domain.Fixed       `json:"pressure_kpa"`
	Temperature domain.Fixed       `json:"temperature_c"`
}

// AutoclaveResult is the deterministic fixed-point outcome of a validated
// autoclave curve.
type AutoclaveResult struct {
	PeakPressure   domain.Fixed       `json:"peak_pressure_kpa"`
	RampUpRate     domain.Fixed       `json:"ramp_up_rate"`
	RampDownRate   domain.Fixed       `json:"ramp_down_rate"`
	EquivalentHold domain.Fixed       `json:"equivalent_hold"`
	HoldStart      domain.LogicalTime `json:"hold_start"`
	HoldEnd        domain.LogicalTime `json:"hold_end"`
}

// AnalyzeAutoclave validates an autoclave pressure curve and computes the ramp
// rates and equivalent holding time. It rejects empty input, reversed or
// duplicated logical times, a curve that never reaches the hold pressure, and
// any gap below the hold pressure during the holding phase. All arithmetic uses
// overflow-checked fixed point with half-away-from-zero rounding.
func AnalyzeAutoclave(points []AutoclavePoint, holdPressure domain.Fixed) (AutoclaveResult, error) {
	if len(points) < 3 {
		return AutoclaveResult{}, domain.New(domain.CodeGridInvalid, "autoclave curve requires at least three points")
	}
	// Strictly increasing logical time; reject reversal and duplicates.
	for i := 1; i < len(points); i++ {
		if !points[i].LogicalTime.After(points[i-1].LogicalTime) {
			return AutoclaveResult{}, domain.Newf(domain.CodeLogicalTimeReversed, "autoclave time not strictly increasing at point %d", i)
		}
	}

	// Locate the holding phase: first and last point at or above hold pressure.
	h0, h1 := -1, -1
	peak := points[0].Pressure
	for i := range points {
		cmp, err := points[i].Pressure.Cmp(peak)
		if err != nil {
			return AutoclaveResult{}, err
		}
		if cmp > 0 {
			peak = points[i].Pressure
		}
		atHold, err := points[i].Pressure.Cmp(holdPressure)
		if err != nil {
			return AutoclaveResult{}, err
		}
		if atHold >= 0 {
			if h0 == -1 {
				h0 = i
			}
			h1 = i
		}
	}
	if h0 == -1 {
		return AutoclaveResult{}, domain.New(domain.CodeGridInvalid, "autoclave curve never reaches hold pressure")
	}
	// Reject a gap: any point inside [h0, h1] must remain at or above hold.
	for i := h0; i <= h1; i++ {
		atHold, err := points[i].Pressure.Cmp(holdPressure)
		if err != nil {
			return AutoclaveResult{}, err
		}
		if atHold < 0 {
			return AutoclaveResult{}, domain.New(domain.CodeGridInvalid, "autoclave hold phase has a pressure gap")
		}
	}
	// A valid curve must have both a rising and a falling segment.
	if h0 < 1 || h1 > len(points)-2 {
		return AutoclaveResult{}, domain.New(domain.CodeGridInvalid, "autoclave curve must include rising and falling segments")
	}

	// Ramp-up rate = (hold entry pressure - initial pressure) / elapsed time.
	upDelta := points[h0].Pressure
	upDelta, err := upDelta.Sub(points[0].Pressure)
	if err != nil {
		return AutoclaveResult{}, err
	}
	upTime := timeFixed(points[h0].LogicalTime - points[0].LogicalTime)
	rampUp, err := upDelta.Div(upTime)
	if err != nil {
		return AutoclaveResult{}, err
	}

	// Ramp-down rate = (hold exit pressure - final pressure) / elapsed time.
	dnDelta := points[h1].Pressure
	dnDelta, err = dnDelta.Sub(points[len(points)-1].Pressure)
	if err != nil {
		return AutoclaveResult{}, err
	}
	dnTime := timeFixed(points[len(points)-1].LogicalTime - points[h1].LogicalTime)
	rampDown, err := dnDelta.Div(dnTime)
	if err != nil {
		return AutoclaveResult{}, err
	}

	// Equivalent holding time is the elapsed logical time inside the hold phase.
	hold := timeFixed(points[h1].LogicalTime - points[h0].LogicalTime)

	return AutoclaveResult{
		PeakPressure:   peak,
		RampUpRate:     rampUp,
		RampDownRate:   rampDown,
		EquivalentHold: hold,
		HoldStart:      points[h0].LogicalTime,
		HoldEnd:        points[h1].LogicalTime,
	}, nil
}

// timeFixed wraps an integer logical-time delta as a scale-0 fixed-point value.
func timeFixed(d domain.LogicalTime) domain.Fixed {
	f, _ := domain.NewFixed(int64(d), 0)
	return f
}
