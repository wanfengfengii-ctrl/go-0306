package store

import (
	"testing"

	"github.com/example/aac-block-masonry-admission-closure/internal/task"
)

func TestStoreRoundTrip(t *testing.T) {
	path := t.TempDir() + "/state.json"
	st := New(path)
	snap := Snapshot{
		Schema: Schema,
		Tasks: map[string]TaskSnapshot{
			"t1": {Task: task.ProductionTask{ID: "t1", Factory: "f", Version: 3}},
		},
	}
	if err := st.Save(snap); err != nil {
		t.Fatal(err)
	}
	loaded, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Tasks["t1"].Task.ID != "t1" || loaded.Tasks["t1"].Task.Version != 3 {
		t.Fatalf("loaded %+v", loaded.Tasks["t1"])
	}
}

func TestStoreMissingFileIsEmpty(t *testing.T) {
	st := New(t.TempDir() + "/nope.json")
	snap, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	if snap.Schema != Schema || len(snap.Tasks) != 0 {
		t.Fatalf("unexpected empty snapshot %+v", snap)
	}
}
