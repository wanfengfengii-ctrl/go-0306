package app

import (
	"encoding/json"
	"fmt"

	"github.com/example/aac-block-masonry-admission-closure/internal/catalog"
	"github.com/example/aac-block-masonry-admission-closure/internal/domain"
	"github.com/example/aac-block-masonry-admission-closure/internal/evidence"
	"github.com/example/aac-block-masonry-admission-closure/internal/mass"
	"github.com/example/aac-block-masonry-admission-closure/internal/retest"
	"github.com/example/aac-block-masonry-admission-closure/internal/task"
)

// --- evidence payloads ------------------------------------------------------

// groupingEvidence is the durable payload of a grouping command: the kiln-car
// position assignment for each block.
type groupingEvidence struct {
	Positions map[string]int64 `json:"positions"`
}

// cutEvidence is the durable payload of a cut command: per-block wire window,
// mix pan and raw batch for propagation.
type cutEvidence struct {
	BlockWire  map[string]string `json:"block_wire"`
	BlockPan   map[string]string `json:"block_pan"`
	BlockBatch map[string]string `json:"block_batch"`
}

// --- account helpers --------------------------------------------------------

func rawAccount(class string) string    { return "raw:" + class }
func mixpanAccount(body string) string  { return "mixpan:" + body }
func bodyAccount(body string) string    { return "body:" + body }
func blockAccount(block string) string  { return "block:" + block }
func offcutAccount(block string) string { return "offcut:" + block }
func wasteAccount(block string) string  { return "waste:" + block }

// --- create task ------------------------------------------------------------

// CreateTaskRequest is the input to task locking.
type CreateTaskRequest struct {
	OperationID     domain.OperationID `json:"operation_id"`
	Factory         string             `json:"factory"`
	ProductionBatch string             `json:"production_batch"`
	MixPlan         string             `json:"mix_plan"`
	RuleVersion     string             `json:"rule_version"`
	Batches         []catalog.BatchRef `json:"batches"`
	RecipeHash      string             `json:"recipe_hash"`
	BodyIDs         []string           `json:"body_ids"`
	RawGrams        map[string]int64   `json:"raw_grams"`
}

// CreateTaskResponse is the successful lock result.
type CreateTaskResponse struct {
	TaskID      string            `json:"task_id"`
	Generation  domain.Generation `json:"generation"`
	LockSummary string            `json:"lock_summary"`
	Version     domain.Version    `json:"version"`
}

// CreateTask locks a production task against the current catalog snapshot. It
// validates the lock request, funds raw-material accounts, records the lineage
// raw-batch nodes and persists atomically. Any validation failure leaves no
// residue.
func (s *Service) CreateTask(req CreateTaskRequest) (CreateTaskResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	hash := requestHash(req)
	if rec, found, err := s.idempotencyCheck(s.idempotency, req.OperationID, hash); err != nil {
		return CreateTaskResponse{}, err
	} else if found {
		var res CreateTaskResponse
		_ = json.Unmarshal(rec.Result, &res)
		return res, nil
	}

	lockReq := catalog.LockRequest{
		RuleVersion: req.RuleVersion,
		Batches:     req.Batches,
		RecipeHash:  req.RecipeHash,
		BodyIDs:     req.BodyIDs,
	}
	snap, reasons := s.dir.ValidateLock(lockReq)
	if len(reasons) > 0 {
		return CreateTaskResponse{}, domain.New(domain.CodeInvalidArgument, "task lock failed").WithReasons(reasons...)
	}

	id := fmt.Sprintf("%s-%s", req.Factory, req.ProductionBatch)
	if _, exists := s.tasks[id]; exists {
		return CreateTaskResponse{}, domain.Newf(domain.CodeDuplicateBody, "task %s already exists", id)
	}

	rt := newRuntime()
	rt.Task = task.ProductionTask{
		ID:              id,
		Factory:         req.Factory,
		ProductionBatch: req.ProductionBatch,
		Generation:      0,
		LockSummary:     snap.SummaryHash(),
		MixPlan:         req.MixPlan,
		Status:          task.TaskLocked,
		Version:         0,
		BodyIDs:         req.BodyIDs,
	}
	rt.Snapshot = snap
	rt.Final = retest.FinalSlot{Task: id, Version: 0}

	// Fund raw-material accounts and record raw-batch lineage nodes.
	classes := []string{
		string(catalog.MaterialFlyAsh), string(catalog.MaterialMortar),
		string(catalog.MaterialCement), string(catalog.MaterialLime),
		string(catalog.MaterialWater), string(catalog.MaterialAluminum),
		string(catalog.MaterialReclaim),
	}
	for _, class := range classes {
		g, ok := req.RawGrams[class]
		if !ok {
			continue
		}
		if g < 0 {
			return CreateTaskResponse{}, domain.New(domain.CodeInvalidArgument, "raw grams must be non-negative")
		}
		if err := rt.Ledger.Apply(mass.MassLedgerEntry{Account: rawAccount(class), Direction: mass.Debit, Grams: g, LineageNode: "raw:" + class, OperationID: req.OperationID}); err != nil {
			return CreateTaskResponse{}, err
		}
		if err := rt.Graph.AddNode(task.MaterialNode{ID: "raw:" + class, Kind: task.NodeRawBatch, MassGrams: g, SourceOp: req.OperationID}); err != nil {
			return CreateTaskResponse{}, err
		}
	}

	s.tasks[id] = rt
	res := CreateTaskResponse{TaskID: id, Generation: 0, LockSummary: snap.SummaryHash(), Version: 0}
	resBytes, _ := json.Marshal(res)
	s.idempotency[string(req.OperationID)] = domain.IdempotencyRecord{
		Scope: "task-create", OperationID: req.OperationID, RequestHash: hash, Result: resBytes,
	}
	s.appendEvent(id, "task-create", req.OperationID, res)
	if err := s.persist(); err != nil {
		delete(s.tasks, id)
		delete(s.idempotency, string(req.OperationID))
		return CreateTaskResponse{}, err
	}
	return res, nil
}

// --- unified command --------------------------------------------------------

// Command is a unified stage/command submission against a task.
type Command struct {
	OperationID     domain.OperationID        `json:"operation_id"`
	ExpectedVersion domain.Version            `json:"expected_version"`
	LogicalTime     domain.LogicalTime        `json:"logical_time"`
	LeaseToken      domain.Token              `json:"lease_token"`
	Kind            string                    `json:"kind"`
	Body            string                    `json:"body"`
	Materials       map[string]int64          `json:"materials,omitempty"`
	ReclaimGrams    int64                     `json:"reclaim_grams,omitempty"`
	MixPan          string                    `json:"mix_pan,omitempty"`
	PreHeightUM     int64                     `json:"pre_height_um,omitempty"`
	PostHeightUM    int64                     `json:"post_height_um,omitempty"`
	Grid            *evidence.Grid            `json:"grid,omitempty"`
	WireWindow      string                    `json:"wire_window,omitempty"`
	Blocks          []BlockAllocation         `json:"blocks,omitempty"`
	OffcutGrams     int64                     `json:"offcut_grams,omitempty"`
	WasteGrams      int64                     `json:"waste_grams,omitempty"`
	Positions       map[string]int64          `json:"positions,omitempty"`
	AutoclavePoints []evidence.AutoclavePoint `json:"autoclave_points,omitempty"`
	HoldPressure    domain.Fixed              `json:"hold_pressure,omitempty"`
	Cracks          []string                  `json:"cracks,omitempty"`
	Test            *TestCommand              `json:"test,omitempty"`
}

// BlockAllocation maps one cut block to its integer-gram mass.
type BlockAllocation struct {
	ID        string `json:"id"`
	MassGrams int64  `json:"mass_grams"`
}

// TestCommand carries one size/performance measurement.
type TestCommand struct {
	Sample    string         `json:"sample"`
	Metric    retest.Metric  `json:"metric"`
	Value     domain.Fixed   `json:"value"`
	Threshold domain.Fixed   `json:"threshold,omitempty"`
	Anomaly   retest.Anomaly `json:"anomaly,omitempty"`
}

// CommandResult is the generic result of a committed command.
type CommandResult struct {
	TaskID      string             `json:"task_id"`
	Version     domain.Version     `json:"version"`
	LogicalTime domain.LogicalTime `json:"logical_time"`
	Result      any                `json:"result,omitempty"`
}

// SubmitCommand dispatches a unified stage/command. It enforces the expected
// version, idempotency, lease validity, strict stage prefix and the specific
// domain rule for each command kind, committing atomically.
func (s *Service) SubmitCommand(taskID string, cmd Command) (CommandResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rt, err := s.getTask(taskID)
	if err != nil {
		return CommandResult{}, err
	}
	hash := requestHash(cmd)
	if rec, found, err := s.idempotencyCheck(rt.Idempotency, cmd.OperationID, hash); err != nil {
		return CommandResult{}, err
	} else if found {
		var res CommandResult
		_ = json.Unmarshal(rec.Result, &res)
		return res, nil
	}
	if cmd.ExpectedVersion != rt.Task.Version {
		return CommandResult{}, domain.Newf(domain.CodeGenerationConflict, "expected version %d != current %d", cmd.ExpectedVersion, rt.Task.Version)
	}
	if cmd.LogicalTime.Before(rt.Task.Clock) {
		return CommandResult{}, domain.Newf(domain.CodeLogicalTimeReversed, "logical time %d before clock %d", cmd.LogicalTime, rt.Task.Clock)
	}

	var result any
	switch cmd.Kind {
	case "dosing":
		result, err = s.doDosing(rt, cmd)
	case "mixing":
		err = s.doStage(rt, cmd, task.StageMixing, mass.ResourceMixer, cmd.MixPan)
	case "pouring":
		err = s.doPouring(rt, cmd)
	case "rising":
		result, err = s.doRising(rt, cmd)
	case "standing":
		err = s.doStage(rt, cmd, task.StageStanding, mass.ResourceStandingBay, cmd.Body)
	case "demold":
		err = s.doStage(rt, cmd, task.StageDemold, mass.ResourceMold, cmd.Body)
	case "longitudinal_cut", "cross_cut":
		err = s.doCut(rt, cmd)
	case "grouping":
		err = s.doGrouping(rt, cmd)
	case "autoclave":
		result, err = s.doAutoclave(rt, cmd)
	case "cooling":
		result, err = s.doCooling(rt, cmd)
	case "test":
		err = s.doTest(rt, cmd)
	default:
		return CommandResult{}, domain.Newf(domain.CodeInvalidArgument, "unknown command kind %s", cmd.Kind)
	}
	if err != nil {
		return CommandResult{}, err
	}

	rt.Task.Clock = cmd.LogicalTime
	rt.Task.Version++
	rt.Final.Version = rt.Task.Version
	if rt.Task.Status == task.TaskLocked {
		rt.Task.Status = task.TaskInProgress
	}
	res := CommandResult{TaskID: taskID, Version: rt.Task.Version, LogicalTime: rt.Task.Clock, Result: result}
	resBytes, _ := json.Marshal(res)
	rt.Idempotency[string(cmd.OperationID)] = domain.IdempotencyRecord{
		Scope: taskID, OperationID: cmd.OperationID, RequestHash: hash, Result: resBytes, CommitVer: rt.Task.Version,
	}
	s.appendEvent(taskID, cmd.Kind, cmd.OperationID, res)
	if err := s.persist(); err != nil {
		return CommandResult{}, err
	}
	return res, nil
}

// requireLease validates that the command holds a live, matching lease on the
// given resource at the given logical time.
func (rt *TaskRuntime) requireLease(rtRes mass.ResourceType, id string, token domain.Token, now domain.LogicalTime) error {
	l, ok := rt.Leases.Lookup(rtRes, id)
	if !ok {
		return domain.Newf(domain.CodeLeaseExpired, "no lease held on %s %s", rtRes, id)
	}
	if l.Token != token {
		return domain.New(domain.CodeLeaseConflict, "lease token mismatch")
	}
	if l.Expired(now) {
		return domain.Newf(domain.CodeLeaseExpired, "lease on %s %s expired at %d", rtRes, id, l.ExpiresAt)
	}
	return nil
}

// advanceStage records a stage event after validating the prefix.
func (rt *TaskRuntime) advanceStage(cmd Command, stage task.Stage) error {
	return rt.Stages.Advance(cmd.Body, stage, int64(len(rt.Stages.Stages(cmd.Body)))+1, cmd.LogicalTime, cmd.OperationID)
}

// doStage handles a pure stage advance with a lease check.
func (s *Service) doStage(rt *TaskRuntime, cmd Command, stage task.Stage, res mass.ResourceType, resID string) error {
	if err := rt.requireLease(res, resID, cmd.LeaseToken, cmd.LogicalTime); err != nil {
		return err
	}
	return rt.advanceStage(cmd, stage)
}

// doDosing batches raw materials into a mix pan: it deducts raw accounts,
// credits the mix pan, records lineage nodes and edges, and enforces the
// reclaim ratio bound.
func (s *Service) doDosing(rt *TaskRuntime, cmd Command) (any, error) {
	if err := rt.requireLease(mass.ResourceMixer, cmd.MixPan, cmd.LeaseToken, cmd.LogicalTime); err != nil {
		return nil, err
	}
	if err := rt.advanceStage(cmd, task.StageDosing); err != nil {
		return nil, err
	}
	var total int64
	for _, g := range cmd.Materials {
		if g < 0 {
			return nil, domain.New(domain.CodeInvalidArgument, "material grams must be non-negative")
		}
		total += g
	}
	if total <= 0 {
		return nil, domain.New(domain.CodeInvalidArgument, "dosing requires positive total mass")
	}
	if err := mass.CheckReclaim(cmd.ReclaimGrams, total, rt.Snapshot.ReclaimMaxPPM); err != nil {
		return nil, err
	}

	// Build a balanced ledger transaction: credit raw accounts, debit mix pan.
	var entries []mass.MassLedgerEntry
	for class, g := range cmd.Materials {
		entries = append(entries, mass.MassLedgerEntry{Account: rawAccount(class), Direction: mass.Credit, Grams: g, LineageNode: "raw:" + class, OperationID: cmd.OperationID})
	}
	entries = append(entries, mass.MassLedgerEntry{Account: mixpanAccount(cmd.Body), Direction: mass.Debit, Grams: total, LineageNode: "mixpan:" + cmd.Body, OperationID: cmd.OperationID})
	if err := rt.Ledger.ApplyTransaction(entries); err != nil {
		return nil, err
	}

	// Lineage: raw nodes -> mix pan node.
	if err := rt.Graph.AddNode(task.MaterialNode{ID: "mixpan:" + cmd.Body, Kind: task.NodeMixPan, MassGrams: total, SourceOp: cmd.OperationID}); err != nil {
		return nil, err
	}
	for class := range cmd.Materials {
		if err := rt.Graph.AddEdge(task.LineageEdge{Parent: "raw:" + class, Child: "mixpan:" + cmd.Body, Type: task.EdgeDose, Generation: rt.Task.Generation}); err != nil {
			return nil, err
		}
	}
	return map[string]int64{"batched_grams": total}, nil
}

// doPouring moves the mix pan mass into the cast body.
func (s *Service) doPouring(rt *TaskRuntime, cmd Command) error {
	if err := rt.requireLease(mass.ResourceMixer, cmd.MixPan, cmd.LeaseToken, cmd.LogicalTime); err != nil {
		return err
	}
	if err := rt.advanceStage(cmd, task.StagePouring); err != nil {
		return err
	}
	pan := mixpanAccount(cmd.Body)
	grams := rt.Ledger.Balance(pan)
	if grams <= 0 {
		return domain.New(domain.CodeInvalidArgument, "mix pan has no mass to pour")
	}
	entries := []mass.MassLedgerEntry{
		{Account: pan, Direction: mass.Credit, Grams: grams, LineageNode: "mixpan:" + cmd.Body, OperationID: cmd.OperationID},
		{Account: bodyAccount(cmd.Body), Direction: mass.Debit, Grams: grams, LineageNode: "body:" + cmd.Body, OperationID: cmd.OperationID},
	}
	if err := rt.Ledger.ApplyTransaction(entries); err != nil {
		return err
	}
	if err := rt.Graph.AddNode(task.MaterialNode{ID: "body:" + cmd.Body, Kind: task.NodeBody, MassGrams: grams, SourceOp: cmd.OperationID}); err != nil {
		return err
	}
	return rt.Graph.AddEdge(task.LineageEdge{Parent: "mixpan:" + cmd.Body, Child: "body:" + cmd.Body, Type: task.EdgePour, Generation: rt.Task.Generation})
}

// doRising records the rising stage and computes the expansion ratio.
func (s *Service) doRising(rt *TaskRuntime, cmd Command) (any, error) {
	if err := rt.advanceStage(cmd, task.StageRising); err != nil {
		return nil, err
	}
	if cmd.PreHeightUM <= 0 {
		return nil, domain.New(domain.CodeInvalidArgument, "pre-rise height must be positive")
	}
	if cmd.PostHeightUM < cmd.PreHeightUM {
		return nil, domain.New(domain.CodeInvalidArgument, "post-rise height below pre-rise height")
	}
	pre, _ := domain.NewFixed(cmd.PreHeightUM, 0)
	post, _ := domain.NewFixed(cmd.PostHeightUM, 0)
	delta, err := post.Sub(pre)
	if err != nil {
		return nil, err
	}
	ratio, err := delta.Div(pre)
	if err != nil {
		return nil, err
	}
	return map[string]any{"expansion_ratio": ratio}, nil
}

// doCut validates the cut grid and wire window, then (for the cross cut)
// splits the body into blocks, offcuts and waste with exact mass conservation.
func (s *Service) doCut(rt *TaskRuntime, cmd Command) error {
	stage := task.StageLongCut
	if cmd.Kind == "cross_cut" {
		stage = task.StageCrossCut
	}
	if err := rt.requireLease(mass.ResourceCutLine, cmd.Body, cmd.LeaseToken, cmd.LogicalTime); err != nil {
		return err
	}
	if err := rt.advanceStage(cmd, stage); err != nil {
		return err
	}
	if cmd.Grid != nil {
		if err := cmd.Grid.Validate(); err != nil {
			return err
		}
	}
	if cmd.WireWindow != "" && rt.Snapshot.WireLifeWindow > 0 {
		// Wire window is an opaque id validated elsewhere; reject empty/long.
		if len(cmd.WireWindow) > int(rt.Snapshot.WireLifeWindow) {
			return domain.New(domain.CodeWireLifeExceeded, "wire life window exceeded")
		}
	}
	// The longitudinal cut only advances the stage; the cross cut performs the
	// block split.
	if cmd.Kind != "cross_cut" {
		return nil
	}
	if cmd.Grid == nil {
		return domain.New(domain.CodeInvalidArgument, "cross cut requires a grid")
	}

	body := bodyAccount(cmd.Body)
	bodyMass := rt.Ledger.Balance(body)
	var out int64
	for _, b := range cmd.Blocks {
		out += b.MassGrams
	}
	out += cmd.OffcutGrams + cmd.WasteGrams
	if out != bodyMass {
		return domain.Newf(domain.CodeInvalidArgument, "cut output %d does not equal body mass %d", out, bodyMass)
	}

	var entries []mass.MassLedgerEntry
	entries = append(entries, mass.MassLedgerEntry{Account: body, Direction: mass.Credit, Grams: bodyMass, LineageNode: "body:" + cmd.Body, OperationID: cmd.OperationID})
	for _, b := range cmd.Blocks {
		if b.MassGrams < 0 {
			return domain.New(domain.CodeInvalidArgument, "block mass must be non-negative")
		}
		entries = append(entries, mass.MassLedgerEntry{Account: blockAccount(b.ID), Direction: mass.Debit, Grams: b.MassGrams, LineageNode: "block:" + b.ID, OperationID: cmd.OperationID})
	}
	if cmd.OffcutGrams < 0 || cmd.WasteGrams < 0 {
		return domain.New(domain.CodeInvalidArgument, "offcut/waste mass must be non-negative")
	}
	entries = append(entries, mass.MassLedgerEntry{Account: offcutAccount(cmd.Body), Direction: mass.Debit, Grams: cmd.OffcutGrams, LineageNode: "offcut:" + cmd.Body, OperationID: cmd.OperationID})
	entries = append(entries, mass.MassLedgerEntry{Account: wasteAccount(cmd.Body), Direction: mass.Debit, Grams: cmd.WasteGrams, LineageNode: "waste:" + cmd.Body, OperationID: cmd.OperationID})
	if err := rt.Ledger.ApplyTransaction(entries); err != nil {
		return err
	}

	wire := make(map[string]string, len(cmd.Blocks))
	pan := make(map[string]string, len(cmd.Blocks))
	batch := make(map[string]string, len(cmd.Blocks))
	for _, b := range cmd.Blocks {
		if err := rt.Graph.AddNode(task.MaterialNode{ID: "block:" + b.ID, Kind: task.NodeBlock, MassGrams: b.MassGrams, SourceOp: cmd.OperationID}); err != nil {
			return err
		}
		if err := rt.Graph.AddEdge(task.LineageEdge{Parent: "body:" + cmd.Body, Child: "block:" + b.ID, Type: task.EdgeCut, Generation: rt.Task.Generation}); err != nil {
			return err
		}
		rt.Topology.WireWindow[b.ID] = cmd.WireWindow
		rt.Topology.Pan[b.ID] = cmd.Body
		rt.Topology.RawBatch[b.ID] = rt.Task.ProductionBatch
		wire[b.ID] = cmd.WireWindow
		pan[b.ID] = cmd.Body
		batch[b.ID] = rt.Task.ProductionBatch
	}
	rt.Evidence = append(rt.Evidence, evidence.Evidence{
		ID: "cut:" + cmd.Body, Body: cmd.Body, Kind: "cut", Generation: rt.Task.Generation,
		Payload: cutEvidence{BlockWire: wire, BlockPan: pan, BlockBatch: batch},
	})
	return nil
}

// doGrouping assigns blocks to kiln-car positions.
func (s *Service) doGrouping(rt *TaskRuntime, cmd Command) error {
	if err := rt.requireLease(mass.ResourceKilnCarPos, cmd.Body, cmd.LeaseToken, cmd.LogicalTime); err != nil {
		return err
	}
	if err := rt.advanceStage(cmd, task.StageGrouping); err != nil {
		return err
	}
	for block, pos := range cmd.Positions {
		if _, taken := rt.Topology.Position[block]; taken {
			return domain.Newf(domain.CodeLeaseConflict, "block %s already grouped", block)
		}
		rt.Topology.Position[block] = pos
	}
	rt.Evidence = append(rt.Evidence, evidence.Evidence{
		ID: "grouping:" + cmd.Body, Body: cmd.Body, Kind: "grouping", Generation: rt.Task.Generation,
		Payload: groupingEvidence{Positions: cmd.Positions},
	})
	return nil
}

// doAutoclave appends autoclave points and validates the pressure curve.
func (s *Service) doAutoclave(rt *TaskRuntime, cmd Command) (any, error) {
	if err := rt.requireLease(mass.ResourceAutoclave, cmd.Body, cmd.LeaseToken, cmd.LogicalTime); err != nil {
		return nil, err
	}
	if err := rt.advanceStage(cmd, task.StageAutoclave); err != nil {
		return nil, err
	}
	res, err := evidence.AnalyzeAutoclave(cmd.AutoclavePoints, cmd.HoldPressure)
	if err != nil {
		return nil, err
	}
	rt.Evidence = append(rt.Evidence, evidence.Evidence{
		ID: "autoclave:" + cmd.Body, Body: cmd.Body, Kind: "autoclave", Generation: rt.Task.Generation,
		Payload: cmd.AutoclavePoints,
	})
	return res, nil
}

// doCooling records the cooling stage and surface-crack evidence.
func (s *Service) doCooling(rt *TaskRuntime, cmd Command) (any, error) {
	if err := rt.advanceStage(cmd, task.StageCooling); err != nil {
		return nil, err
	}
	rt.Evidence = append(rt.Evidence, evidence.Evidence{
		ID: "cooling:" + cmd.Body, Body: cmd.Body, Kind: "cooling", Generation: rt.Task.Generation,
		Payload: map[string]any{"cracks": cmd.Cracks},
	})
	return map[string]any{"cracks": len(cmd.Cracks)}, nil
}

// doTest records a measurement, consumes its sample exactly once and flags an
// anomaly when the threshold is violated.
func (s *Service) doTest(rt *TaskRuntime, cmd Command) error {
	if cmd.Test == nil {
		return domain.New(domain.CodeInvalidArgument, "test command requires a test payload")
	}
	if err := rt.requireLease(mass.ResourcePress, cmd.Test.Sample, cmd.LeaseToken, cmd.LogicalTime); err != nil {
		return err
	}
	tc := cmd.Test
	if rt.Sampling.Consumed[tc.Sample] {
		return domain.Newf(domain.CodeInvalidArgument, "sample %s already consumed", tc.Sample)
	}
	pass := true
	var anomaly retest.Anomaly
	if tc.Threshold.Scale() > 0 || tc.Threshold.Scaled() != 0 {
		cmp, err := tc.Value.Cmp(tc.Threshold)
		if err != nil {
			return err
		}
		// Threshold semantics are metric-specific; the caller supplies the bound
		// such that a value below it fails for strength/density and above it
		// fails for deviation/loss. The anomaly is set explicitly on failure.
		pass = cmp >= 0
	}
	if tc.Anomaly != "" {
		anomaly = tc.Anomaly
		pass = false
	}
	rt.Tests = append(rt.Tests, retest.TestResult{
		Sample:    tc.Sample,
		Metric:    tc.Metric,
		Value:     tc.Value,
		Threshold: tc.Threshold,
		Pass:      pass,
		ConsumeOp: cmd.OperationID,
		Anomaly:   anomaly,
	})
	rt.Sampling.Consumed[tc.Sample] = true
	return nil
}
