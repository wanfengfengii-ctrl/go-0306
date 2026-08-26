// Package retest implements the 性能复验与终局仲裁器 component: size and
// performance sample consumption, anomaly indexing, retest-set generation,
// generation isolation, dual-person review qualification and the single-writer
// final barrier, guaranteeing old evidence stays permanently readable while the
// current conclusion is decided only by the latest generation.
package retest

import (
	"github.com/example/aac-block-masonry-admission-closure/internal/domain"
)

// Metric identifies a measured test metric.
type Metric string

const (
	MetricSizeDeviation       Metric = "size_deviation"
	MetricDryDensity          Metric = "dry_density"
	MetricMoistureContent     Metric = "moisture_content"
	MetricCompressiveStrength Metric = "compressive_strength"
	MetricFreezeThawLoss      Metric = "freeze_thaw_mass_loss"
)

// Anomaly is a normalized anomaly type that may trigger a retest.
type Anomaly string

const (
	AnomalyCollapse     Anomaly = "collapse"
	AnomalyUnderRising  Anomaly = "under_rising"
	AnomalyAdhesion     Anomaly = "cutting_adhesion"
	AnomalyBrokenWire   Anomaly = "broken_wire"
	AnomalyCurveGap     Anomaly = "autoclave_curve_gap"
	AnomalySurfaceCrack Anomaly = "surface_crack"
	AnomalyDensityDev   Anomaly = "density_deviation"
	AnomalyLowStrength  Anomaly = "low_strength"
)

// TestResult is one fixed-point measurement against a threshold.
type TestResult struct {
	Sample    string             `json:"sample"`
	Metric    Metric             `json:"metric"`
	Value     domain.Fixed       `json:"value"`
	Threshold domain.Fixed       `json:"threshold"`
	Pass      bool               `json:"pass"`
	ConsumeOp domain.OperationID `json:"consume_op"`
	Anomaly   Anomaly            `json:"anomaly,omitempty"`
}

// RetestSet is a deterministic, uniquely-keyed retest supersession.
type RetestSet struct {
	Key        string            `json:"key"`
	Generation domain.Generation `json:"generation"`
	Anomaly    Anomaly           `json:"anomaly"`
	Source     string            `json:"source"`
	Members    []string          `json:"members"` // affected blocks, ordered
	Late       bool              `json:"late"`
}

// Review is an independent qualified-person review of one generation.
type Review struct {
	Person     string             `json:"person"`
	Qualified  bool               `json:"qualified"`
	Generation domain.Generation  `json:"generation"`
	Summary    string             `json:"summary"`
	SignedAt   domain.LogicalTime `json:"signed_at"`
}

// VerdictKind is the single-writer terminal decision.
type VerdictKind string

const (
	VerdictAdmit   VerdictKind = "admit"
	VerdictIsolate VerdictKind = "isolate"
	VerdictCancel  VerdictKind = "cancel"
)

// FinalVerdict is the immutable, single-slot terminal record.
type FinalVerdict struct {
	Task       string         `json:"task"`
	Kind       VerdictKind    `json:"kind"`
	Credential string         `json:"credential"`
	Reason     string         `json:"reason"`
	Evidence   []string       `json:"evidence"` // ordered evidence summary
	Version    domain.Version `json:"version"`
}

// FinalSlot enforces the single-writer terminal barrier via compare-and-set on
// the task version.
type FinalSlot struct {
	Task    string         `json:"task"`
	Verdict *FinalVerdict  `json:"verdict"`
	Version domain.Version `json:"version"`
}

// Set writes the verdict only once; concurrent or late writers receive
// FINAL_ALREADY_SET with the existing verdict.
func (s *FinalSlot) Set(v FinalVerdict, expected domain.Version) error {
	if s.Verdict != nil {
		return domain.Newf(domain.CodeFinalAlreadySet, "final verdict %s already set", s.Verdict.Credential)
	}
	if expected != s.Version {
		return domain.New(domain.CodeGenerationConflict, "stale version for final slot")
	}
	s.Verdict = &v
	s.Version = expected + 1
	return nil
}
