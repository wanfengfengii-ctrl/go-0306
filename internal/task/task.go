// Package task implements the 生产任务及物料谱系聚合 component: the task
// generation and body-stage state machine plus the append-only material
// lineage of "raw batch → mix pan → cast body → cut blocks → samples and
// offcuts" with unique parent edges and cycle/multi-parent/out-of-order
// detection.
package task

import (
	"github.com/example/aac-block-masonry-admission-closure/internal/domain"
)

// Stage is one step of the strict body-stage prefix enforced by the domain
// rules.
type Stage string

const (
	StageDosing    Stage = "dosing"
	StageMixing    Stage = "mixing"
	StagePouring   Stage = "pouring"
	StageRising    Stage = "rising"
	StageStanding  Stage = "standing"
	StageDemold    Stage = "demold"
	StageLongCut   Stage = "longitudinal_cut"
	StageCrossCut  Stage = "cross_cut"
	StageGrouping  Stage = "grouping"
	StageAutoclave Stage = "autoclave"
	StageCooling   Stage = "cooling"
)

// stageOrder is the canonical prefix; a body must advance strictly through it.
var stageOrder = []Stage{
	StageDosing, StageMixing, StagePouring, StageRising, StageStanding,
	StageDemold, StageLongCut, StageCrossCut, StageGrouping, StageAutoclave,
	StageCooling,
}

// StageIndex returns the position of s in the canonical prefix, or -1 if
// unknown.
func StageIndex(s Stage) int {
	for i, st := range stageOrder {
		if st == s {
			return i
		}
	}
	return -1
}

// TaskStatus is the lifecycle status of a production task.
type TaskStatus string

const (
	TaskLocked     TaskStatus = "locked"
	TaskInProgress TaskStatus = "in_progress"
	TaskClosed     TaskStatus = "closed"
)

// TaskGeneration records one retest coverage supersession. Generation 0 is
// the initial lock.
type TaskGeneration struct {
	Generation domain.Generation `json:"generation"`
	Current    bool              `json:"current"`
	Coverage   []string          `json:"coverage"` // affected block ids, ordered
}

// ProductionTask is the aggregate root and consistency boundary for one
// factory production task.
type ProductionTask struct {
	ID              string             `json:"id"`
	Factory         string             `json:"factory"`
	ProductionBatch string             `json:"production_batch"`
	Generation      domain.Generation  `json:"generation"`
	LockSummary     string             `json:"lock_summary"`
	MixPlan         string             `json:"mix_plan"`
	Status          TaskStatus         `json:"status"`
	Clock           domain.LogicalTime `json:"clock"`
	Version         domain.Version     `json:"version"`
	Generations     []TaskGeneration   `json:"generations"`
	FinalRef        string             `json:"final_ref,omitempty"`
	BodyIDs         []string           `json:"body_ids,omitempty"`
}

// Tick advances the aggregate logical clock and returns the new time, ensuring
// monotonic non-decreasing ordering.
func (t *ProductionTask) Tick() domain.LogicalTime {
	t.Clock++
	return t.Clock
}

// BumpVersion advances the optimistic concurrency version.
func (t *ProductionTask) BumpVersion() domain.Version {
	t.Version++
	return t.Version
}

// CurrentGeneration returns the most recent generation index.
func (t *ProductionTask) CurrentGeneration() domain.Generation {
	if len(t.Generations) == 0 {
		return 0
	}
	return t.Generations[len(t.Generations)-1].Generation
}
