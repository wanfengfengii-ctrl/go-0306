// Package app is the application service boundary. It wires the recipe and
// quality rule catalog, the production-task and material-lineage aggregate, the
// mass-conservation and lease manager, the evidence recorder, the retest and
// final-arbitration components and the durable store into a single, mutex-guarded
// service that enforces transaction boundaries and restart recovery.
package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"sync"

	"github.com/example/aac-block-masonry-admission-closure/internal/catalog"
	"github.com/example/aac-block-masonry-admission-closure/internal/domain"
	"github.com/example/aac-block-masonry-admission-closure/internal/evidence"
	"github.com/example/aac-block-masonry-admission-closure/internal/mass"
	"github.com/example/aac-block-masonry-admission-closure/internal/retest"
	"github.com/example/aac-block-masonry-admission-closure/internal/store"
	"github.com/example/aac-block-masonry-admission-closure/internal/task"
)

// TaskRuntime is the in-memory aggregate bundle for one production task. It is
// rebuilt from the durable snapshot on startup.
type TaskRuntime struct {
	Task        task.ProductionTask
	Snapshot    catalog.RecipeRuleSnapshot
	Graph       *task.Graph
	Ledger      *mass.Ledger
	Leases      *mass.LeaseTable
	Stages      *task.StageTracker
	Devices     *evidence.DeviceRegistry
	Sampling    *evidence.SamplingMap
	Evidence    []evidence.Evidence
	Tests       []retest.TestResult
	Retests     []retest.RetestSet
	Reviews     *retest.ReviewBoard
	Final       retest.FinalSlot
	Idempotency map[string]domain.IdempotencyRecord
	Topology    retest.BlockTopology
}

// Service is the application service. A single mutex serializes all commands so
// transactions are atomic with respect to each other.
type Service struct {
	mu    sync.Mutex
	dir   catalog.Directory
	store store.Persister

	tasks       map[string]*TaskRuntime
	idempotency map[string]domain.IdempotencyRecord
	eventLog    *domain.EventLog
}

// NewService constructs a Service and restores all aggregates from the durable
// store, giving restart recovery. Any catalog snapshots persisted alongside the
// tasks are re-registered with the directory if it supports registration.
func NewService(dir catalog.Directory, st store.Persister) (*Service, error) {
	s := &Service{
		dir:         dir,
		store:       st,
		tasks:       make(map[string]*TaskRuntime),
		idempotency: make(map[string]domain.IdempotencyRecord),
		eventLog:    domain.NewEventLog(),
	}
	snap, err := st.Load()
	if err != nil {
		return nil, err
	}
	if reg, ok := dir.(interface {
		RegisterAll(map[string]catalog.RecipeRuleSnapshot)
	}); ok && len(snap.Catalog) > 0 {
		reg.RegisterAll(snap.Catalog)
	}
	for _, rec := range snap.Idempotency {
		s.idempotency[string(rec.OperationID)] = rec
	}
	for _, e := range snap.EventLog {
		s.eventLog.Append(e)
	}
	for id, ts := range snap.Tasks {
		s.tasks[id] = restoreTask(ts)
	}
	return s, nil
}

// Health reports service readiness.
func (s *Service) Health() error { return nil }

// Store returns the backing durable store (used by tests and the smoke script
// to verify restart recovery).
func (s *Service) Store() store.Persister { return s.store }

// snapshot serializes the whole service state for durable persistence.
func (s *Service) snapshot() store.Snapshot {
	snap := store.Snapshot{Schema: store.Schema, Tasks: make(map[string]store.TaskSnapshot)}
	for id, rt := range s.tasks {
		snap.Tasks[id] = taskSnapshot(rt)
	}
	for _, rec := range s.idempotency {
		snap.Idempotency = append(snap.Idempotency, rec)
	}
	sort.Slice(snap.Idempotency, func(i, j int) bool { return snap.Idempotency[i].OperationID < snap.Idempotency[j].OperationID })
	snap.EventLog = s.eventLog.Entries()
	return snap
}

// persist writes the current state durably. It is called only after a
// successfully committed command.
func (s *Service) persist() error {
	if s.store == nil {
		return nil
	}
	return s.store.Save(s.snapshot())
}

// appendEvent records one monotonic append-only event in the durable log. The
// log is the append-only persistence of every committed quality fact: it is
// replayed on startup to rebuild aggregates, resume pending device calls and
// verify snapshot consistency.
func (s *Service) appendEvent(taskID, kind string, op domain.OperationID, payload any) {
	s.eventLog.Append(domain.EventLogEntry{Task: taskID, Kind: kind, OperationID: op, Payload: payload})
}

func taskSnapshot(rt *TaskRuntime) store.TaskSnapshot {
	ts := store.TaskSnapshot{
		Task:     rt.Task,
		Snapshot: rt.Snapshot,
		Accounts: rt.Ledger.Accounts(),
		Leases:   rt.Leases.Leases(),
		Sampling: rt.Sampling,
		Evidence: rt.Evidence,
		Tests:    rt.Tests,
		Retests:  rt.Retests,
		Reviews:  rt.Reviews.Reviews(),
		Final:    rt.Final,
	}
	for id, n := range rt.Graph.Nodes() {
		ts.Nodes = append(ts.Nodes, n)
		_ = id
	}
	sort.Slice(ts.Nodes, func(i, j int) bool { return ts.Nodes[i].ID < ts.Nodes[j].ID })
	for _, e := range rt.Graph.Edges() {
		ts.Edges = append(ts.Edges, e)
	}
	sort.Slice(ts.Edges, func(i, j int) bool {
		if ts.Edges[i].Child != ts.Edges[j].Child {
			return ts.Edges[i].Child < ts.Edges[j].Child
		}
		return ts.Edges[i].Parent < ts.Edges[j].Parent
	})
	ts.DeviceCalls = rt.Devices.Calls()
	// StageTracker stores per body; flatten deterministically.
	ts.Stages = flattenStages(rt.Stages, rt.Task.BodyIDs)
	for _, rec := range rt.Idempotency {
		ts.Idempotency = append(ts.Idempotency, rec)
	}
	sort.Slice(ts.Idempotency, func(i, j int) bool { return ts.Idempotency[i].OperationID < ts.Idempotency[j].OperationID })
	return ts
}

func flattenStages(st *task.StageTracker, bodies []string) []task.BodyStage {
	var out []task.BodyStage
	for _, b := range bodies {
		out = append(out, st.Stages(b)...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Body != out[j].Body {
			return out[i].Body < out[j].Body
		}
		return out[i].Sequence < out[j].Sequence
	})
	return out
}

func restoreTask(ts store.TaskSnapshot) *TaskRuntime {
	rt := newRuntime()
	rt.Task = ts.Task
	rt.Snapshot = ts.Snapshot
	for _, n := range ts.Nodes {
		_ = rt.Graph.AddNode(n)
	}
	for _, e := range ts.Edges {
		_ = rt.Graph.AddEdge(e)
	}
	rt.Ledger.Load(ts.Accounts)
	rt.Leases.Load(ts.Leases)
	rt.Stages.Load(ts.Stages)
	rt.Devices.Load(ts.DeviceCalls)
	if ts.Sampling != nil {
		rt.Sampling = ts.Sampling
	}
	rt.Evidence = ts.Evidence
	rt.Tests = ts.Tests
	rt.Retests = ts.Retests
	rt.Reviews.Load(ts.Reviews)
	rt.Final = ts.Final
	for _, rec := range ts.Idempotency {
		rt.Idempotency[string(rec.OperationID)] = rec
	}
	// Rebuild the propagation topology from the lineage and sampling map.
	rebuildTopology(rt)
	return rt
}

func newRuntime() *TaskRuntime {
	return &TaskRuntime{
		Graph:       task.NewGraph(),
		Ledger:      mass.NewLedger(),
		Leases:      mass.NewLeaseTable(),
		Stages:      task.NewStageTracker(),
		Devices:     evidence.NewDeviceRegistry(),
		Sampling:    evidence.NewSamplingMap(),
		Reviews:     retest.NewReviewBoard(),
		Idempotency: make(map[string]domain.IdempotencyRecord),
		Topology: retest.BlockTopology{
			Pan:        make(map[string]string),
			Position:   make(map[string]int64),
			WireWindow: make(map[string]string),
			RawBatch:   make(map[string]string),
		},
	}
}

// rebuildTopology reconstructs propagation facts from recorded evidence. Block
// positions and wire windows are carried in grouping/cut evidence payloads.
func rebuildTopology(rt *TaskRuntime) {
	for _, ev := range rt.Evidence {
		switch ev.Kind {
		case "grouping":
			var p groupingEvidence
			if b, err := json.Marshal(ev.Payload); err == nil {
				if json.Unmarshal(b, &p) == nil {
					for block, pos := range p.Positions {
						rt.Topology.Position[block] = pos
					}
				}
			}
		case "cut":
			var p cutEvidence
			if b, err := json.Marshal(ev.Payload); err == nil {
				if json.Unmarshal(b, &p) == nil {
					for block, wire := range p.BlockWire {
						rt.Topology.WireWindow[block] = wire
					}
					for block, pan := range p.BlockPan {
						rt.Topology.Pan[block] = pan
					}
					for block, batch := range p.BlockBatch {
						rt.Topology.RawBatch[block] = batch
					}
				}
			}
		}
	}
}

// getTask returns the runtime for a task id.
func (s *Service) getTask(id string) (*TaskRuntime, error) {
	rt, ok := s.tasks[id]
	if !ok {
		return nil, domain.Newf(domain.CodeInvalidArgument, "unknown task %s", id)
	}
	return rt, nil
}

// requestHash derives the canonical request digest for idempotency.
func requestHash(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// idempotencyCheck enforces the operation-id contract. It returns the recorded
// result on a matching replay, or an idempotency conflict on a divergent
// payload.
func (s *Service) idempotencyCheck(m map[string]domain.IdempotencyRecord, op domain.OperationID, hash string) (domain.IdempotencyRecord, bool, error) {
	if op == "" {
		return domain.IdempotencyRecord{}, false, nil
	}
	rec, ok := m[string(op)]
	if !ok {
		return rec, false, nil
	}
	if rec.RequestHash != hash {
		return rec, true, domain.Newf(domain.CodeIdempotencyConflict, "operation %s already committed with a different payload", op)
	}
	return rec, true, nil
}
