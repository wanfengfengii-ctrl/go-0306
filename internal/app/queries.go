package app

import (
	"sort"

	"github.com/example/aac-block-masonry-admission-closure/internal/domain"
	"github.com/example/aac-block-masonry-admission-closure/internal/retest"
	"github.com/example/aac-block-masonry-admission-closure/internal/task"
)

// GetTask returns the current task aggregate.
func (s *Service) GetTask(id string) (task.ProductionTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rt, err := s.getTask(id)
	if err != nil {
		return task.ProductionTask{}, err
	}
	return rt.Task, nil
}

// LineageView is the full material lineage of a task, sorted deterministically.
type LineageView struct {
	Nodes []task.MaterialNode `json:"nodes"`
	Edges []task.LineageEdge  `json:"edges"`
}

// GetLineage returns the append-only material lineage in canonical order.
func (s *Service) GetLineage(id string) (LineageView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rt, err := s.getTask(id)
	if err != nil {
		return LineageView{}, err
	}
	v := LineageView{}
	for _, n := range rt.Graph.Nodes() {
		v.Nodes = append(v.Nodes, n)
	}
	sort.Slice(v.Nodes, func(i, j int) bool { return v.Nodes[i].ID < v.Nodes[j].ID })
	for _, e := range rt.Graph.Edges() {
		v.Edges = append(v.Edges, e)
	}
	sort.Slice(v.Edges, func(i, j int) bool {
		if v.Edges[i].Child != v.Edges[j].Child {
			return v.Edges[i].Child < v.Edges[j].Child
		}
		return v.Edges[i].Parent < v.Edges[j].Parent
	})
	return v, nil
}

// GetMassBalance returns the integer-gram account balances in canonical order.
func (s *Service) GetMassBalance(id string) (map[string]int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rt, err := s.getTask(id)
	if err != nil {
		return nil, err
	}
	return rt.Ledger.Accounts(), nil
}

// GetEvidence returns the recorded evidence in canonical order.
func (s *Service) GetEvidence(id string) ([]EvidenceView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rt, err := s.getTask(id)
	if err != nil {
		return nil, err
	}
	out := make([]EvidenceView, 0, len(rt.Evidence))
	for _, e := range rt.Evidence {
		out = append(out, EvidenceView{ID: e.ID, Body: e.Body, Kind: e.Kind, Generation: e.Generation, Payload: e.Payload})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// EvidenceView is a serializable evidence record.
type EvidenceView struct {
	ID         string            `json:"id"`
	Body       string            `json:"body"`
	Kind       string            `json:"kind"`
	Generation domain.Generation `json:"generation"`
	Payload    any               `json:"payload"`
}

// GetVerdict returns the current terminal verdict, if any.
func (s *Service) GetVerdict(id string) (*retest.FinalVerdict, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rt, err := s.getTask(id)
	if err != nil {
		return nil, err
	}
	return rt.Final.Verdict, nil
}

// GetRetest returns the retest set for a generation.
func (s *Service) GetRetest(id string, gen domain.Generation) (retest.RetestSet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rt, err := s.getTask(id)
	if err != nil {
		return retest.RetestSet{}, err
	}
	for _, rs := range rt.Retests {
		if rs.Generation == gen {
			return rs, nil
		}
	}
	return retest.RetestSet{}, domain.Newf(domain.CodeInvalidArgument, "no retest for generation %d", gen)
}
