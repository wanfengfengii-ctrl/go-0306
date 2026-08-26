package catalog

import (
	"github.com/example/aac-block-masonry-admission-closure/internal/domain"
)

// MemoryDirectory is an in-memory recipe and quality rule directory. It holds
// a set of immutable snapshots keyed by rule version and performs deterministic
// lock validation.
type MemoryDirectory struct {
	snapshots map[string]RecipeRuleSnapshot
}

// NewMemoryDirectory returns an empty in-memory directory.
func NewMemoryDirectory() *MemoryDirectory {
	return &MemoryDirectory{snapshots: make(map[string]RecipeRuleSnapshot)}
}

// Register stores a snapshot under its version, replacing any prior value.
func (d *MemoryDirectory) Register(s RecipeRuleSnapshot) {
	d.snapshots[s.Version] = s
}

// RegisterAll stores a batch of snapshots, used to restore persisted catalog
// state during startup recovery.
func (d *MemoryDirectory) RegisterAll(all map[string]RecipeRuleSnapshot) {
	for _, s := range all {
		d.snapshots[s.Version] = s
	}
}

// Snapshot resolves a version to its immutable snapshot.
func (d *MemoryDirectory) Snapshot(version string) (RecipeRuleSnapshot, error) {
	s, ok := d.snapshots[version]
	if !ok {
		return RecipeRuleSnapshot{}, domain.Newf(domain.CodeStaleRule, "unknown rule version %s", version)
	}
	return s, nil
}

// ValidateLock checks a lock request against the catalog. It returns the
// pinned snapshot on success or a deterministically sorted reason list.
func (d *MemoryDirectory) ValidateLock(req LockRequest) (RecipeRuleSnapshot, []domain.Reason) {
	s, err := d.Snapshot(req.RuleVersion)
	if err != nil {
		return RecipeRuleSnapshot{}, []domain.Reason{{Code: domain.CodeStaleRule, Msg: err.Error()}}
	}
	var reasons []domain.Reason
	if req.RecipeHash != s.SummaryHash() {
		reasons = append(reasons, domain.Reason{Code: domain.CodeStaleRule, Field: "recipe_hash", Msg: "recipe summary is stale"})
	}
	for _, b := range req.Batches {
		if !allowed(s.AllowedBatches, b) {
			reasons = append(reasons, domain.Reason{Code: domain.CodeRawBatchMismatch, Field: string(b.Class), Msg: "batch not allowed for material class"})
		}
	}
	if len(req.BodyIDs) == 0 {
		reasons = append(reasons, domain.Reason{Code: domain.CodeDuplicateBody, Field: "body_ids", Msg: "at least one body id required"})
	}
	if len(reasons) > 0 {
		return RecipeRuleSnapshot{}, reasons
	}
	return s, nil
}

func allowed(batches []BatchRef, b BatchRef) bool {
	for _, a := range batches {
		if a.Class == b.Class && a.Batch == b.Batch {
			return true
		}
	}
	return false
}
