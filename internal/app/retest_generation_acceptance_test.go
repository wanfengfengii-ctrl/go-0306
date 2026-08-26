package app

import (
	"testing"

	"github.com/example/aac-block-masonry-admission-closure/internal/domain"
	"github.com/example/aac-block-masonry-admission-closure/internal/evidence"
	"github.com/example/aac-block-masonry-admission-closure/internal/mass"
	"github.com/example/aac-block-masonry-admission-closure/internal/retest"
)

func TestModel_RetestRequiresCurrentGenerationReviews(t *testing.T) {
	svc := newTestService(t, t.TempDir()+"/state.json")
	created := createTask(t, svc, "cold-school")
	taskID := created.TaskID
	body := "body-cold-school"

	mixer := acquire(t, svc, taskID, mass.ResourceMixer, "pan-1", 1)
	commands := []Command{
		{OperationID: "dose-cold", ExpectedVersion: 0, LogicalTime: 2, LeaseToken: mixer.Token, Kind: "dosing", Body: body, MixPan: "pan-1", Materials: map[string]int64{"cement": 900, "water": 450, "aluminum_suspension": 45}},
		{OperationID: "mix-cold", ExpectedVersion: 1, LogicalTime: 3, LeaseToken: mixer.Token, Kind: "mixing", Body: body, MixPan: "pan-1"},
		{OperationID: "pour-cold", ExpectedVersion: 2, LogicalTime: 4, LeaseToken: mixer.Token, Kind: "pouring", Body: body, MixPan: "pan-1"},
		{OperationID: "rise-cold", ExpectedVersion: 3, LogicalTime: 5, Kind: "rising", Body: body, PreHeightUM: 500, PostHeightUM: 750},
	}
	for _, cmd := range commands {
		if _, err := svc.SubmitCommand(taskID, cmd); err != nil {
			t.Fatalf("submit %s: %v", cmd.Kind, err)
		}
	}

	standing := acquire(t, svc, taskID, mass.ResourceStandingBay, body, 6)
	mold := acquire(t, svc, taskID, mass.ResourceMold, body, 7)
	cut := acquire(t, svc, taskID, mass.ResourceCutLine, body, 8)
	grid := &evidence.Grid{
		Body:  evidence.BodyBounds{WidthUM: 100, HeightUM: 100, DepthUM: 100},
		Cells: []evidence.CutCell{{Width: 100, Height: 100, Depth: 100}},
	}
	commands = []Command{
		{OperationID: "stand-cold", ExpectedVersion: 4, LogicalTime: 6, LeaseToken: standing.Token, Kind: "standing", Body: body},
		{OperationID: "demold-cold", ExpectedVersion: 5, LogicalTime: 7, LeaseToken: mold.Token, Kind: "demold", Body: body},
		{OperationID: "lcut-cold", ExpectedVersion: 6, LogicalTime: 8, LeaseToken: cut.Token, Kind: "longitudinal_cut", Body: body, Grid: grid},
		{OperationID: "ccut-cold", ExpectedVersion: 7, LogicalTime: 9, LeaseToken: cut.Token, Kind: "cross_cut", Body: body, Grid: grid, WireWindow: "winter-wire", Blocks: []BlockAllocation{{ID: "b1", MassGrams: 1000}, {ID: "b2", MassGrams: 350}}, OffcutGrams: 20, WasteGrams: 25},
	}
	for _, cmd := range commands {
		if _, err := svc.SubmitCommand(taskID, cmd); err != nil {
			t.Fatalf("submit %s: %v", cmd.Kind, err)
		}
	}

	grouping := acquire(t, svc, taskID, mass.ResourceKilnCarPos, body, 10)
	autoclave := acquire(t, svc, taskID, mass.ResourceAutoclave, body, 11)
	hold, err := domain.NewFixed(1200, 0)
	if err != nil {
		t.Fatal(err)
	}
	commands = []Command{
		{OperationID: "group-cold", ExpectedVersion: 8, LogicalTime: 10, LeaseToken: grouping.Token, Kind: "grouping", Body: body, Positions: map[string]int64{"b1": 1, "b2": 2}},
		{OperationID: "autoclave-cold", ExpectedVersion: 9, LogicalTime: 14, LeaseToken: autoclave.Token, Kind: "autoclave", Body: body, HoldPressure: hold, AutoclavePoints: []evidence.AutoclavePoint{{LogicalTime: 11, Pressure: mustFixed(0, 0)}, {LogicalTime: 12, Pressure: hold}, {LogicalTime: 13, Pressure: hold}, {LogicalTime: 14, Pressure: mustFixed(0, 0)}}},
		{OperationID: "cool-cold", ExpectedVersion: 10, LogicalTime: 15, Kind: "cooling", Body: body},
	}
	for _, cmd := range commands {
		if _, err := svc.SubmitCommand(taskID, cmd); err != nil {
			t.Fatalf("submit %s: %v", cmd.Kind, err)
		}
	}

	for i, person := range []string{"reviewer-a", "reviewer-b"} {
		if _, err := svc.SubmitReview(ReviewRequest{TaskID: taskID, Person: person, Qualified: true, Generation: 0, Summary: "generation zero checked", SignedAt: domain.LogicalTime(16 + i)}); err != nil {
			t.Fatalf("submit generation-zero review: %v", err)
		}
	}

	retestRequest := RetestRequest{TaskID: taskID, OperationID: "low-strength-retest", Anomaly: retest.AnomalyLowStrength, Source: "b1", LogicalTime: 18}
	var current retest.RetestSet

	cases := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "retest is idempotent and leaves historical reviews in generation zero",
			run: func(t *testing.T) {
				first, err := svc.CreateRetest(retestRequest)
				if err != nil {
					t.Fatal(err)
				}
				current = first
				second, err := svc.CreateRetest(retestRequest)
				if err != nil {
					t.Fatal(err)
				}
				queried, err := svc.GetRetest(taskID, 1)
				if err != nil {
					t.Fatal(err)
				}
				if first.Key != second.Key || first.Key != queried.Key || first.Generation != 1 || second.Generation != 1 {
					t.Fatalf("retest replay changed generation or key: first=%+v second=%+v queried=%+v", first, second, queried)
				}

				snapshot, err := svc.Store().Load()
				if err != nil {
					t.Fatal(err)
				}
				reviews := snapshot.Tasks[taskID].Reviews
				if len(reviews) != 2 {
					t.Fatalf("creating generation 1 changed review history: got %d reviews, want 2", len(reviews))
				}
				for _, review := range reviews {
					if review.Generation != 0 {
						t.Fatalf("historical review by %s moved or leaked into generation %d", review.Person, review.Generation)
					}
				}
			},
		},
		{
			name: "old generation reviews cannot admit the retest generation",
			run: func(t *testing.T) {
				if _, err := svc.SubmitVerdict(VerdictRequest{TaskID: taskID, Kind: retest.VerdictAdmit, ExpectedVersion: 11, Reason: "old reviews", Credential: "admit-too-early"}); err == nil {
					t.Fatal("admission succeeded before any generation-one review")
				}
				if verdict, err := svc.GetVerdict(taskID); err != nil || verdict != nil {
					t.Fatalf("failed admission wrote a verdict: verdict=%+v err=%v", verdict, err)
				}
			},
		},
		{
			name: "one current qualified reviewer is still insufficient",
			run: func(t *testing.T) {
				if _, err := svc.SubmitReview(ReviewRequest{TaskID: taskID, Person: "reviewer-c", Qualified: true, Generation: current.Generation, Summary: "retest checked", SignedAt: 19}); err != nil {
					t.Fatal(err)
				}
				if _, err := svc.SubmitVerdict(VerdictRequest{TaskID: taskID, Kind: retest.VerdictAdmit, ExpectedVersion: 11, Reason: "one new review", Credential: "admit-still-early"}); err == nil {
					t.Fatal("admission succeeded with only one generation-one reviewer")
				}
			},
		},
		{
			name: "two distinct current qualified reviewers allow admission",
			run: func(t *testing.T) {
				if _, err := svc.SubmitReview(ReviewRequest{TaskID: taskID, Person: "reviewer-d", Qualified: true, Generation: current.Generation, Summary: "independent retest check", SignedAt: 20}); err != nil {
					t.Fatal(err)
				}
				response, err := svc.SubmitVerdict(VerdictRequest{TaskID: taskID, Kind: retest.VerdictAdmit, ExpectedVersion: 11, Reason: "two new reviews", Credential: "admit-current"})
				if err != nil {
					t.Fatal(err)
				}
				if response.Already || response.Verdict == nil || response.Verdict.Kind != retest.VerdictAdmit {
					t.Fatalf("unexpected current-generation admission response: %+v", response)
				}
			},
		},
		{
			name: "isolate and cancel remain ungated by reviews",
			run: func(t *testing.T) {
				for _, kind := range []retest.VerdictKind{retest.VerdictIsolate, retest.VerdictCancel} {
					other := createTask(t, svc, string(kind))
					response, err := svc.SubmitVerdict(VerdictRequest{TaskID: other.TaskID, Kind: kind, ExpectedVersion: 0, Reason: "safety closure", Credential: string(kind) + "-credential"})
					if err != nil {
						t.Fatalf("%s unexpectedly gated by reviews: %v", kind, err)
					}
					if response.Verdict == nil || response.Verdict.Kind != kind {
						t.Fatalf("unexpected %s response: %+v", kind, response)
					}
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}
