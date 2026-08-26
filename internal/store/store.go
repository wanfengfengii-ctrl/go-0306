// Package store implements durable, atomic JSON persistence for the whole
// application aggregate set. It is the single restart-recovery boundary: the
// application service snapshots its state into a Snapshot, the store writes it
// to a temporary file and atomically renames it into place, and on startup the
// service rebuilds every aggregate from the loaded snapshot.
package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/example/aac-block-masonry-admission-closure/internal/catalog"
	"github.com/example/aac-block-masonry-admission-closure/internal/domain"
	"github.com/example/aac-block-masonry-admission-closure/internal/evidence"
	"github.com/example/aac-block-masonry-admission-closure/internal/mass"
	"github.com/example/aac-block-masonry-admission-closure/internal/retest"
	"github.com/example/aac-block-masonry-admission-closure/internal/task"
)

// Schema is the current on-disk snapshot schema version.
const Schema = 1

// Snapshot is the complete, serializable application state. It is plain data:
// every field is exported and JSON round-trips deterministically.
type Snapshot struct {
	Schema      int                                   `json:"schema"`
	Catalog     map[string]catalog.RecipeRuleSnapshot `json:"catalog,omitempty"`
	Tasks       map[string]TaskSnapshot               `json:"tasks"`
	Idempotency []domain.IdempotencyRecord            `json:"idempotency,omitempty"`
	EventLog    []domain.EventLogEntry                `json:"event_log,omitempty"`
}

// TaskSnapshot is the per-task bundle of aggregate state.
type TaskSnapshot struct {
	Task        task.ProductionTask        `json:"task"`
	Snapshot    catalog.RecipeRuleSnapshot `json:"snapshot,omitempty"`
	Nodes       []task.MaterialNode        `json:"nodes,omitempty"`
	Edges       []task.LineageEdge         `json:"edges,omitempty"`
	Accounts    map[string]int64           `json:"accounts,omitempty"`
	Leases      []mass.ResourceLease       `json:"leases,omitempty"`
	Stages      []task.BodyStage           `json:"stages,omitempty"`
	DeviceCalls []evidence.DeviceCall      `json:"device_calls,omitempty"`
	Evidence    []evidence.Evidence        `json:"evidence,omitempty"`
	Sampling    *evidence.SamplingMap      `json:"sampling,omitempty"`
	Tests       []retest.TestResult        `json:"tests,omitempty"`
	Retests     []retest.RetestSet         `json:"retests,omitempty"`
	Reviews     []retest.Review            `json:"reviews,omitempty"`
	Final       retest.FinalSlot           `json:"final,omitempty"`
	Idempotency []domain.IdempotencyRecord `json:"idempotency,omitempty"`
}

// Persister is the durable persistence contract implemented by Store. It is the
// seam through which the application service snapshots and restores its whole
// aggregate set, and it is what makes persistence independently verifiable in
// tests and replaceable with an in-memory or fault-injecting implementation.
type Persister interface {
	Save(Snapshot) error
	Load() (Snapshot, error)
}

// Store provides atomic, durable file persistence for a Snapshot. It implements
// Persister and is the production persistence backend: on Save it marshals the
// snapshot to JSON, writes it to a temporary file in the same directory and
// atomically renames it into place, and on Load it reads the snapshot back,
// returning an empty schema-versioned snapshot when the file does not yet exist.
type Store struct {
	mu   sync.RWMutex
	path string
}

// New returns a Store persisting to path. If path is empty the store keeps no
// file (Save is a no-op and Load returns an empty snapshot), which is useful
// for deterministic in-memory testing.
func New(path string) *Store {
	return &Store{path: path}
}

// Path returns the backing file path (may be empty).
func (s *Store) Path() string { return s.path }

// Save atomically writes the snapshot. It marshals to a temporary file in the
// same directory, writes it with os.WriteFile (the explicit durable-write
// boundary), then renames it into place so a crash mid-write never leaves a
// partially-written snapshot.
func (s *Store) Save(snap Snapshot) error {
	if s.path == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".snapshot-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.WriteFile(tmpName, data, 0o644); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// Load reads the snapshot from disk. A missing file yields an empty snapshot
// with the current schema, which is the normal first-boot state.
func (s *Store) Load() (Snapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap := Snapshot{Schema: Schema}
	if s.path == "" {
		return snap, nil
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return snap, nil
		}
		return snap, err
	}
	if len(data) == 0 {
		return snap, nil
	}
	if err := json.Unmarshal(data, &snap); err != nil {
		return snap, err
	}
	return snap, nil
}
