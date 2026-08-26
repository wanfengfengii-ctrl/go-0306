// Package catalog implements the 砌块配方与质量规则目录 component: a versioned
// directory of raw-material requirements, recipe summaries, reclaim limits,
// geometry constraints, wire-life windows, autoclave programs, sampling maps
// and quality thresholds, together with task-lock snapshots and staleness
// checks.
package catalog

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/example/aac-block-masonry-admission-closure/internal/domain"
)

// MaterialClass is a raw-material category that may be locked into a recipe.
type MaterialClass string

const (
	MaterialFlyAsh   MaterialClass = "fly_ash"
	MaterialMortar   MaterialClass = "mortar"
	MaterialCement   MaterialClass = "cement"
	MaterialLime     MaterialClass = "lime"
	MaterialWater    MaterialClass = "water"
	MaterialAluminum MaterialClass = "aluminum_suspension"
	MaterialReclaim  MaterialClass = "reclaim_slurry"
)

// BatchRef identifies a locked raw-material batch within a material class.
type BatchRef struct {
	Class MaterialClass `json:"class"`
	Batch string        `json:"batch"`
}

// MaterialRange is the inclusive integer-gram bounds for one material class.
type MaterialRange struct {
	Class MaterialClass `json:"class"`
	MinG  int64         `json:"min_grams"`
	MaxG  int64         `json:"max_grams"`
}

// RecipeRuleSnapshot is an immutable, versioned capture of a recipe and its
// quality rules. Once a task locks against a snapshot, later catalog updates
// must not affect the in-flight task.
type RecipeRuleSnapshot struct {
	Version        string          `json:"version"`
	RecipeSummary  string          `json:"recipe_summary"`
	Materials      []MaterialRange `json:"materials"`
	AllowedBatches []BatchRef      `json:"allowed_batches"`
	ReclaimMaxPPM  int64           `json:"reclaim_max_ppm"` // integer fixed point, scale 6
	WireLifeWindow int64           `json:"wire_life_window"`
	// ProgramIDs reference validated autoclave, standing and sampling
	// programs held by the directory; the snapshot pins them by stable id.
	ProgramIDs map[string]string `json:"program_ids"`
	// Thresholds are the locked quality acceptance bounds keyed by metric.
	Thresholds map[string]ThresholdRule `json:"thresholds,omitempty"`
}

// ThresholdRule is one locked acceptance bound for a measured metric, expressed
// as integer fixed-point values with an explicit scale.
type ThresholdRule struct {
	Metric string       `json:"metric"`
	Min    domain.Fixed `json:"min,omitempty"`
	Max    domain.Fixed `json:"max,omitempty"`
	Scale  int          `json:"scale"`
}

// SummaryHash returns a deterministic digest of the rule snapshot used for
// staleness comparison across task locks.
func (r RecipeRuleSnapshot) SummaryHash() string {
	h := sha256.New()
	h.Write([]byte(r.Version))
	h.Write([]byte(r.RecipeSummary))
	for _, m := range r.Materials {
		h.Write([]byte(m.Class))
	}
	sum := h.Sum(nil)
	return hex.EncodeToString(sum)
}

// LockRequest is the input to task locking. It must be fully resolved and
// validated against the directory before a snapshot is minted.
type LockRequest struct {
	RuleVersion string     `json:"rule_version"`
	Batches     []BatchRef `json:"batches"`
	RecipeHash  string     `json:"recipe_hash"`
	BodyIDs     []string   `json:"body_ids"`
}

// Directory is the read-side contract of the recipe and quality rule catalog.
// Implementations own versioning, batch validation and staleness detection.
type Directory interface {
	// Snapshot resolves a rule version into an immutable RecipeRuleSnapshot.
	Snapshot(version string) (RecipeRuleSnapshot, error)
	// ValidateLock checks that a lock request is internally consistent and
	// matches the current catalog state, returning a deterministically sorted
	// list of failures.
	ValidateLock(req LockRequest) (RecipeRuleSnapshot, []domain.Reason)
}
