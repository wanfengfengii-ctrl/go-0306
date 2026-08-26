package app

import (
	"sort"

	"github.com/example/aac-block-masonry-admission-closure/internal/domain"
	"github.com/example/aac-block-masonry-admission-closure/internal/retest"
	"github.com/example/aac-block-masonry-admission-closure/internal/task"
)

// RetestRequest creates or idempotently retrieves a retest supersession.
type RetestRequest struct {
	TaskID      string             `json:"-"`
	OperationID domain.OperationID `json:"operation_id"`
	Anomaly     retest.Anomaly     `json:"anomaly"`
	Source      string             `json:"source"`
	LogicalTime domain.LogicalTime `json:"logical_time"`
}

// CreateRetest expands an anomaly into its deterministic member set, computes
// the canonical key, and — if not already present — creates a new generation.
// Concurrent creators of the same key receive the winning generation and do not
// create a second one.
func (s *Service) CreateRetest(req RetestRequest) (retest.RetestSet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rt, err := s.getTask(req.TaskID)
	if err != nil {
		return retest.RetestSet{}, err
	}
	members := retest.Propagate(req.Source, rt.Topology, rt.blockIDs())
	key := retest.RetestKey(req.Anomaly, req.Source, members)
	for _, rs := range rt.Retests {
		if rs.Key == key {
			return rs, nil
		}
	}
	gen := rt.Task.Generation + 1
	rs := retest.RetestSet{Key: key, Generation: gen, Anomaly: req.Anomaly, Source: req.Source, Members: members}
	rt.Retests = append(rt.Retests, rs)
	rt.Task.Generation = gen
	// Mark prior generations non-current and record the new coverage.
	for i := range rt.Task.Generations {
		rt.Task.Generations[i].Current = false
	}
	rt.Task.Generations = append(rt.Task.Generations, task.TaskGeneration{Generation: gen, Current: true, Coverage: members})
	for _, review := range rt.Reviews.Reviews() {
		review.Generation = gen
		if err := rt.Reviews.Submit(review); err != nil {
			return retest.RetestSet{}, err
		}
	}
	s.appendEvent(req.TaskID, "retest-create", req.OperationID, rs)
	if err := s.persist(); err != nil {
		return retest.RetestSet{}, err
	}
	return rs, nil
}

// blockIDs returns the bare block ids in the lineage, ascending.
func (rt *TaskRuntime) blockIDs() []string {
	var out []string
	for id, n := range rt.Graph.Nodes() {
		if n.Kind == task.NodeBlock {
			out = append(out, trimBlockPrefix(id))
		}
	}
	sort.Strings(out)
	return out
}

func trimBlockPrefix(id string) string {
	if len(id) > len("block:") && id[:len("block:")] == "block:" {
		return id[len("block:"):]
	}
	return id
}

// ReviewRequest submits an independent review.
type ReviewRequest struct {
	TaskID     string             `json:"-"`
	Person     string             `json:"person"`
	Qualified  bool               `json:"qualified"`
	Generation domain.Generation  `json:"generation"`
	Summary    string             `json:"summary"`
	SignedAt   domain.LogicalTime `json:"signed_at"`
}

// SubmitReview records one independent review.
func (s *Service) SubmitReview(req ReviewRequest) (retest.Review, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rt, err := s.getTask(req.TaskID)
	if err != nil {
		return retest.Review{}, err
	}
	r := retest.Review{Person: req.Person, Qualified: req.Qualified, Generation: req.Generation, Summary: req.Summary, SignedAt: req.SignedAt}
	if err := rt.Reviews.Submit(r); err != nil {
		return retest.Review{}, err
	}
	s.appendEvent(req.TaskID, "review", "", r)
	if err := s.persist(); err != nil {
		return retest.Review{}, err
	}
	return r, nil
}

// VerdictRequest submits a competing terminal decision.
type VerdictRequest struct {
	TaskID          string             `json:"-"`
	Kind            retest.VerdictKind `json:"kind"`
	ExpectedVersion domain.Version     `json:"expected_version"`
	Reason          string             `json:"reason"`
	Credential      string             `json:"credential"`
	LogicalTime     domain.LogicalTime `json:"logical_time"`
}

// VerdictResponse is the current unique terminal decision after a submission.
type VerdictResponse struct {
	Verdict *retest.FinalVerdict `json:"verdict"`
	Already bool                 `json:"already_set"`
}

// SubmitVerdict competes for the single-writer final slot. Admission requires
// every body to have closed all stages and two distinct qualified reviewers;
// isolate and cancel may be written without those conditions. Exactly one
// verdict commits; concurrent and late writers receive FINAL_ALREADY_SET.
func (s *Service) SubmitVerdict(req VerdictRequest) (VerdictResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rt, err := s.getTask(req.TaskID)
	if err != nil {
		return VerdictResponse{}, err
	}
	if rt.Final.Verdict != nil {
		return VerdictResponse{Verdict: rt.Final.Verdict, Already: true}, nil
	}
	if req.ExpectedVersion != rt.Task.Version {
		return VerdictResponse{}, domain.Newf(domain.CodeGenerationConflict, "expected version %d != current %d", req.ExpectedVersion, rt.Task.Version)
	}
	if req.Kind == retest.VerdictAdmit {
		if err := rt.allBodiesClosed(); err != nil {
			return VerdictResponse{}, err
		}
		if err := rt.Reviews.AdmitEligible(rt.Task.Generation); err != nil {
			return VerdictResponse{}, err
		}
	}
	v := retest.FinalVerdict{Task: req.TaskID, Kind: req.Kind, Credential: req.Credential, Reason: req.Reason, Version: req.ExpectedVersion}
	if err := rt.Final.Set(v, req.ExpectedVersion); err != nil {
		return VerdictResponse{}, err
	}
	rt.Task.Version = rt.Final.Version
	rt.Task.Status = task.TaskClosed
	rt.Task.FinalRef = req.Credential
	s.appendEvent(req.TaskID, "verdict", "", v)
	if err := s.persist(); err != nil {
		return VerdictResponse{}, err
	}
	return VerdictResponse{Verdict: rt.Final.Verdict, Already: false}, nil
}

// allBodiesClosed verifies every locked body reached the terminal cooling stage.
func (rt *TaskRuntime) allBodiesClosed() error {
	for _, body := range rt.Task.BodyIDs {
		if !rt.Stages.Completed(body) {
			return domain.Newf(domain.CodeStageOutOfOrder, "body %s has not completed all stages", body)
		}
	}
	if len(rt.Task.BodyIDs) == 0 {
		return domain.New(domain.CodeStageOutOfOrder, "task has no bodies")
	}
	return nil
}
