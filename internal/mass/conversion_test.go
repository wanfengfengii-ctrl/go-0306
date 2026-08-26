package mass

import "testing"

func TestReclaimPPM(t *testing.T) {
	ppm, err := ReclaimPPM(300, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if ppm != 300_000 {
		t.Fatalf("ppm %d, want 300000", ppm)
	}
}

func TestCheckReclaimUnderLimit(t *testing.T) {
	if err := CheckReclaim(300, 1000, 300_000); err != nil {
		t.Fatalf("expected within limit: %v", err)
	}
}

func TestCheckReclaimExceedsLimit(t *testing.T) {
	if err := CheckReclaim(301, 1000, 300_000); err == nil {
		t.Fatal("expected reclaim-ratio-exceeded error")
	}
}

func TestReclaimPPMZeroTotal(t *testing.T) {
	if _, err := ReclaimPPM(0, 0); err == nil {
		t.Fatal("expected error for zero total")
	}
}
