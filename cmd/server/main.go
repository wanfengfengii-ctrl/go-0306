// Command server is the executable entry point for the AAC block masonry
// admission quality-closure backend. It registers the seed recipe directory,
// restores any persisted aggregate state, constructs the application service and
// serves the HTTP API.
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/example/aac-block-masonry-admission-closure/internal/app"
	"github.com/example/aac-block-masonry-admission-closure/internal/catalog"
	"github.com/example/aac-block-masonry-admission-closure/internal/domain"
	"github.com/example/aac-block-masonry-admission-closure/internal/httpapi"
	"github.com/example/aac-block-masonry-admission-closure/internal/store"
)

func main() {
	dir := catalog.NewMemoryDirectory()
	seedDirectory(dir)

	path := os.Getenv("STATE_PATH")
	if path == "" {
		path = "./aac-state.json"
	}
	st := store.New(path)

	svc, err := app.NewService(dir, st)
	if err != nil {
		log.Fatalf("restore state: %v", err)
	}

	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	srv := httpapi.NewServer(svc)
	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
		log.Fatal(err)
	}
}

// seedDirectory registers a single deterministic recipe snapshot so the service
// is usable immediately on first boot.
func seedDirectory(dir *catalog.MemoryDirectory) {
	min, _ := domain.NewFixed(3_500_000, 3) // 3.5 MPa compressive strength
	dir.Register(catalog.RecipeRuleSnapshot{
		Version:        "v1",
		RecipeSummary:  "aac-basic",
		ReclaimMaxPPM:  300_000, // 30%
		WireLifeWindow: 1000,
		Materials: []catalog.MaterialRange{
			{Class: catalog.MaterialCement, MinG: 100, MaxG: 1_000_000},
			{Class: catalog.MaterialFlyAsh, MinG: 100, MaxG: 1_000_000},
			{Class: catalog.MaterialLime, MinG: 100, MaxG: 1_000_000},
			{Class: catalog.MaterialWater, MinG: 100, MaxG: 1_000_000},
			{Class: catalog.MaterialAluminum, MinG: 1, MaxG: 10_000},
			{Class: catalog.MaterialReclaim, MinG: 0, MaxG: 1_000_000},
		},
		AllowedBatches: []catalog.BatchRef{
			{Class: catalog.MaterialCement, Batch: "cem-1"},
			{Class: catalog.MaterialFlyAsh, Batch: "fa-1"},
			{Class: catalog.MaterialLime, Batch: "lime-1"},
			{Class: catalog.MaterialWater, Batch: "water-1"},
			{Class: catalog.MaterialAluminum, Batch: "alu-1"},
			{Class: catalog.MaterialReclaim, Batch: "reclaim-1"},
		},
		Thresholds: map[string]catalog.ThresholdRule{
			"compressive_strength": {Metric: "compressive_strength", Min: min},
		},
	})
}
