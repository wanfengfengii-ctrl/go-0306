package app

import (
	"sync"
	"testing"

	"github.com/example/aac-block-masonry-admission-closure/internal/catalog"
	"github.com/example/aac-block-masonry-admission-closure/internal/domain"
	"github.com/example/aac-block-masonry-admission-closure/internal/evidence"
	"github.com/example/aac-block-masonry-admission-closure/internal/mass"
	"github.com/example/aac-block-masonry-admission-closure/internal/retest"
	"github.com/example/aac-block-masonry-admission-closure/internal/store"
)

func testSnapshot() catalog.RecipeRuleSnapshot {
	return catalog.RecipeRuleSnapshot{
		Version:        "v1",
		RecipeSummary:  "aac-basic",
		ReclaimMaxPPM:  300_000,
		WireLifeWindow: 1000,
		AllowedBatches: []catalog.BatchRef{
			{Class: catalog.MaterialCement, Batch: "cem-1"},
		},
		Materials: []catalog.MaterialRange{
			{Class: catalog.MaterialCement, MinG: 1, MaxG: 1_000_000},
		},
	}
}

func newTestService(t *testing.T, path string) *Service {
	t.Helper()
	dir := catalog.NewMemoryDirectory()
	dir.Register(testSnapshot())
	svc, err := NewService(dir, store.New(path))
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func createTask(t *testing.T, svc *Service, id string) CreateTaskResponse {
	t.Helper()
	res, err := svc.CreateTask(CreateTaskRequest{
		OperationID:     domain.OperationID("create-" + id),
		Factory:         "f",
		ProductionBatch: id,
		RuleVersion:     "v1",
		Batches:         []catalog.BatchRef{{Class: catalog.MaterialCement, Batch: "cem-1"}},
		RecipeHash:      testSnapshot().SummaryHash(),
		BodyIDs:         []string{"body-" + id},
		RawGrams:        map[string]int64{"cement": 1000, "water": 500, "aluminum_suspension": 50},
	})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func acquire(t *testing.T, svc *Service, taskID string, rt mass.ResourceType, id string, now domain.LogicalTime) LeaseResponse {
	t.Helper()
	res, err := svc.AcquireLease(LeaseRequest{TaskID: taskID, ResourceType: rt, ResourceID: id, Holder: "op", LogicalTime: now, Duration: 1000})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestFullLifecycle(t *testing.T) {
	svc := newTestService(t, "")
	res := createTask(t, svc, "p1")
	taskID := res.TaskID
	body := "body-p1"

	mixer := acquire(t, svc, taskID, mass.ResourceMixer, "pan-1", 1)

	// dosing
	if _, err := svc.SubmitCommand(taskID, Command{
		OperationID: "dose-1", ExpectedVersion: 0, LogicalTime: 2, LeaseToken: mixer.Token,
		Kind: "dosing", Body: body, MixPan: "pan-1",
		Materials:    map[string]int64{"cement": 900, "water": 450, "aluminum_suspension": 45},
		ReclaimGrams: 0,
	}); err != nil {
		t.Fatal(err)
	}
	// mixing
	if _, err := svc.SubmitCommand(taskID, Command{OperationID: "mix-1", ExpectedVersion: 1, LogicalTime: 3, LeaseToken: mixer.Token, Kind: "mixing", Body: body, MixPan: "pan-1"}); err != nil {
		t.Fatal(err)
	}
	// pouring
	if _, err := svc.SubmitCommand(taskID, Command{OperationID: "pour-1", ExpectedVersion: 2, LogicalTime: 4, LeaseToken: mixer.Token, Kind: "pouring", Body: body, MixPan: "pan-1"}); err != nil {
		t.Fatal(err)
	}
	// rising
	if _, err := svc.SubmitCommand(taskID, Command{OperationID: "rise-1", ExpectedVersion: 3, LogicalTime: 5, Kind: "rising", Body: body, PreHeightUM: 500, PostHeightUM: 750}); err != nil {
		t.Fatal(err)
	}
	// standing
	stand := acquire(t, svc, taskID, mass.ResourceStandingBay, body, 6)
	if _, err := svc.SubmitCommand(taskID, Command{OperationID: "stand-1", ExpectedVersion: 4, LogicalTime: 6, LeaseToken: stand.Token, Kind: "standing", Body: body}); err != nil {
		t.Fatal(err)
	}
	// demold
	mold := acquire(t, svc, taskID, mass.ResourceMold, body, 7)
	if _, err := svc.SubmitCommand(taskID, Command{OperationID: "demold-1", ExpectedVersion: 5, LogicalTime: 7, LeaseToken: mold.Token, Kind: "demold", Body: body}); err != nil {
		t.Fatal(err)
	}
	// longitudinal cut
	cut := acquire(t, svc, taskID, mass.ResourceCutLine, body, 8)
	grid := &evidence.Grid{Body: evidence.BodyBounds{WidthUM: 100, HeightUM: 100, DepthUM: 100}, Cells: []evidence.CutCell{{Width: 100, Height: 100, Depth: 100}}}
	if _, err := svc.SubmitCommand(taskID, Command{OperationID: "lcut-1", ExpectedVersion: 6, LogicalTime: 8, LeaseToken: cut.Token, Kind: "longitudinal_cut", Body: body, Grid: grid}); err != nil {
		t.Fatal(err)
	}
	// cross cut (split)
	if _, err := svc.SubmitCommand(taskID, Command{
		OperationID: "ccut-1", ExpectedVersion: 7, LogicalTime: 9, LeaseToken: cut.Token,
		Kind: "cross_cut", Body: body, Grid: grid, WireWindow: "w1",
		Blocks:      []BlockAllocation{{ID: "b1", MassGrams: 1000}, {ID: "b2", MassGrams: 350}},
		OffcutGrams: 20, WasteGrams: 25,
	}); err != nil {
		t.Fatal(err)
	}
	// grouping
	group := acquire(t, svc, taskID, mass.ResourceKilnCarPos, body, 10)
	if _, err := svc.SubmitCommand(taskID, Command{OperationID: "group-1", ExpectedVersion: 8, LogicalTime: 10, LeaseToken: group.Token, Kind: "grouping", Body: body, Positions: map[string]int64{"b1": 1, "b2": 2}}); err != nil {
		t.Fatal(err)
	}
	// autoclave
	auto := acquire(t, svc, taskID, mass.ResourceAutoclave, body, 11)
	hold, _ := domain.NewFixed(1200, 0)
	pts := []evidence.AutoclavePoint{
		{LogicalTime: 11, Pressure: mustFixed(0, 0)},
		{LogicalTime: 12, Pressure: mustFixed(1200, 0)},
		{LogicalTime: 13, Pressure: mustFixed(1200, 0)},
		{LogicalTime: 14, Pressure: mustFixed(0, 0)},
	}
	if _, err := svc.SubmitCommand(taskID, Command{OperationID: "auto-1", ExpectedVersion: 9, LogicalTime: 14, LeaseToken: auto.Token, Kind: "autoclave", Body: body, AutoclavePoints: pts, HoldPressure: hold}); err != nil {
		t.Fatal(err)
	}
	// cooling
	if _, err := svc.SubmitCommand(taskID, Command{OperationID: "cool-1", ExpectedVersion: 10, LogicalTime: 15, Kind: "cooling", Body: body}); err != nil {
		t.Fatal(err)
	}
	// test
	press := acquire(t, svc, taskID, mass.ResourcePress, "s1", 16)
	if _, err := svc.SubmitCommand(taskID, Command{OperationID: "test-1", ExpectedVersion: 11, LogicalTime: 16, LeaseToken: press.Token, Kind: "test", Test: &TestCommand{Sample: "s1", Metric: retest.MetricCompressiveStrength, Value: mustFixed(4000, 0), Threshold: mustFixed(3500, 0)}}); err != nil {
		t.Fatal(err)
	}
	// reviews by two distinct qualified persons
	if _, err := svc.SubmitReview(ReviewRequest{TaskID: taskID, Person: "a", Qualified: true, Generation: 0, Summary: "ok", SignedAt: 17}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SubmitReview(ReviewRequest{TaskID: taskID, Person: "b", Qualified: true, Generation: 0, Summary: "ok", SignedAt: 18}); err != nil {
		t.Fatal(err)
	}
	// verdict admit
	vr, err := svc.SubmitVerdict(VerdictRequest{TaskID: taskID, Kind: retest.VerdictAdmit, ExpectedVersion: 12, Reason: "all pass", Credential: "cred-1", LogicalTime: 19})
	if err != nil {
		t.Fatal(err)
	}
	if vr.Verdict == nil || vr.Verdict.Kind != retest.VerdictAdmit {
		t.Fatalf("unexpected verdict %+v", vr.Verdict)
	}

	// second verdict must be FINAL_ALREADY_SET
	vr2, err := svc.SubmitVerdict(VerdictRequest{TaskID: taskID, Kind: retest.VerdictCancel, ExpectedVersion: 13, Reason: "late", Credential: "cred-2", LogicalTime: 20})
	if err != nil {
		t.Fatal(err)
	}
	if !vr2.Already {
		t.Fatal("expected already-set flag on second verdict")
	}
	if vr2.Verdict.Kind != retest.VerdictAdmit {
		t.Fatalf("verdict changed to %s", vr2.Verdict.Kind)
	}
}

func TestRestartRecovery(t *testing.T) {
	path := t.TempDir() + "/state.json"
	svc := newTestService(t, path)
	res := createTask(t, svc, "p1")

	// commit a dosing command so there is durable state.
	mixer := acquire(t, svc, res.TaskID, mass.ResourceMixer, "pan-1", 1)
	if _, err := svc.SubmitCommand(res.TaskID, Command{
		OperationID: "dose-1", ExpectedVersion: 0, LogicalTime: 2, LeaseToken: mixer.Token,
		Kind: "dosing", Body: "body-p1", MixPan: "pan-1",
		Materials: map[string]int64{"cement": 500, "water": 250, "aluminum_suspension": 25},
	}); err != nil {
		t.Fatal(err)
	}

	// Restart: build a fresh service over the same store file.
	dir := catalog.NewMemoryDirectory()
	dir.Register(testSnapshot())
	svc2, err := NewService(dir, store.New(path))
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc2.GetTask(res.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != res.TaskID || got.Version != 1 {
		t.Fatalf("recovered task %+v", got)
	}
	bal, err := svc2.GetMassBalance(res.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if bal[mixpanAccount("body-p1")] != 775 {
		t.Fatalf("recovered mix pan balance %d, want 775", bal[mixpanAccount("body-p1")])
	}
}

func TestEventLogPersists(t *testing.T) {
	path := t.TempDir() + "/state.json"
	svc := newTestService(t, path)
	res := createTask(t, svc, "p1")

	// Committing a command must append an event to the durable append-only log.
	mixer := acquire(t, svc, res.TaskID, mass.ResourceMixer, "pan-1", 1)
	if _, err := svc.SubmitCommand(res.TaskID, Command{
		OperationID: "dose-1", ExpectedVersion: 0, LogicalTime: 2, LeaseToken: mixer.Token,
		Kind: "dosing", Body: "body-p1", MixPan: "pan-1",
		Materials: map[string]int64{"cement": 500, "water": 250, "aluminum_suspension": 25},
	}); err != nil {
		t.Fatal(err)
	}

	// The on-disk snapshot carries the append-only event log with monotonic
	// sequence numbers, proving the append-only persistence is real.
	snap, err := svc.Store().Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.EventLog) < 2 {
		t.Fatalf("expected at least a task-create and command event, got %d", len(snap.EventLog))
	}
	for i, e := range snap.EventLog {
		if e.Sequence != int64(i+1) {
			t.Fatalf("event log sequence not monotonic: index %d has sequence %d", i, e.Sequence)
		}
	}
}

func TestIdempotentReplayAfterRestart(t *testing.T) {
	path := t.TempDir() + "/state.json"
	svc := newTestService(t, path)
	res := createTask(t, svc, "p1")
	mixer := acquire(t, svc, res.TaskID, mass.ResourceMixer, "pan-1", 1)
	cmd := Command{
		OperationID: "dose-1", ExpectedVersion: 0, LogicalTime: 2, LeaseToken: mixer.Token,
		Kind: "dosing", Body: "body-p1", MixPan: "pan-1",
		Materials: map[string]int64{"cement": 500, "water": 250, "aluminum_suspension": 25},
	}
	r1, err := svc.SubmitCommand(res.TaskID, cmd)
	if err != nil {
		t.Fatal(err)
	}

	// Restart over the same store file: the idempotency record's result must
	// survive the JSON round-trip and replay with the identical version.
	dir := catalog.NewMemoryDirectory()
	dir.Register(testSnapshot())
	svc2, err := NewService(dir, store.New(path))
	if err != nil {
		t.Fatal(err)
	}
	r2, err := svc2.SubmitCommand(res.TaskID, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if r1.Version != r2.Version {
		t.Fatalf("idempotent replay after restart changed version %d vs %d", r1.Version, r2.Version)
	}
}

func TestConcurrentLeaseOnlyOneWins(t *testing.T) {
	svc := newTestService(t, "")
	res := createTask(t, svc, "p1")
	var wg sync.WaitGroup
	results := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, results[i] = svc.AcquireLease(LeaseRequest{TaskID: res.TaskID, ResourceType: mass.ResourceMixer, ResourceID: "pan-1", Holder: "op", LogicalTime: 1, Duration: 10})
		}(i)
	}
	wg.Wait()
	successes := 0
	for _, err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("expected exactly one lease winner, got %d", successes)
	}
}

func TestMaterialOverdraw(t *testing.T) {
	svc := newTestService(t, "")
	res := createTask(t, svc, "p1")
	mixer := acquire(t, svc, res.TaskID, mass.ResourceMixer, "pan-1", 1)
	if _, err := svc.SubmitCommand(res.TaskID, Command{
		OperationID: "dose-1", ExpectedVersion: 0, LogicalTime: 2, LeaseToken: mixer.Token,
		Kind: "dosing", Body: "body-p1", MixPan: "pan-1",
		Materials: map[string]int64{"cement": 900, "water": 450, "aluminum_suspension": 45},
	}); err != nil {
		t.Fatal(err)
	}
	// Overdraw cement (only 100 remaining).
	if _, err := svc.SubmitCommand(res.TaskID, Command{
		OperationID: "dose-2", ExpectedVersion: 1, LogicalTime: 3, LeaseToken: mixer.Token,
		Kind: "dosing", Body: "body-p1", MixPan: "pan-1",
		Materials: map[string]int64{"cement": 200, "water": 50, "aluminum_suspension": 5},
	}); err == nil {
		t.Fatal("expected overdraw error")
	}
}

func TestIdempotentReplay(t *testing.T) {
	svc := newTestService(t, "")
	res := createTask(t, svc, "p1")
	mixer := acquire(t, svc, res.TaskID, mass.ResourceMixer, "pan-1", 1)
	cmd := Command{
		OperationID: "dose-1", ExpectedVersion: 0, LogicalTime: 2, LeaseToken: mixer.Token,
		Kind: "dosing", Body: "body-p1", MixPan: "pan-1",
		Materials: map[string]int64{"cement": 500, "water": 250, "aluminum_suspension": 25},
	}
	r1, err := svc.SubmitCommand(res.TaskID, cmd)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := svc.SubmitCommand(res.TaskID, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if r1.Version != r2.Version {
		t.Fatalf("replay version changed %d vs %d", r1.Version, r2.Version)
	}
	// Divergent payload must conflict.
	cmd.Materials = map[string]int64{"cement": 999, "water": 250, "aluminum_suspension": 25}
	if _, err := svc.SubmitCommand(res.TaskID, cmd); err == nil {
		t.Fatal("expected idempotency conflict")
	}
}

func mustFixed(s int64, scale int) domain.Fixed {
	f, err := domain.NewFixed(s, scale)
	if err != nil {
		panic(err)
	}
	return f
}
