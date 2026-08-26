package domain

import "testing"

func TestFixedAdd(t *testing.T) {
	a, _ := NewFixed(1500, 3) // 1.5
	b, _ := NewFixed(2500, 3) // 2.5
	got, err := a.Add(b)
	if err != nil {
		t.Fatalf("Add error: %v", err)
	}
	if got.Scaled() != 4000 || got.Scale() != 3 {
		t.Fatalf("got %d@%d, want 4000@3", got.Scaled(), got.Scale())
	}
}

func TestFixedSub(t *testing.T) {
	a, _ := NewFixed(5000, 3)
	b, _ := NewFixed(1250, 3)
	got, err := a.Sub(b)
	if err != nil {
		t.Fatalf("Sub error: %v", err)
	}
	if got.Scaled() != 3750 {
		t.Fatalf("got %d, want 3750", got.Scaled())
	}
}

func TestFixedMulHalfAwayFromZero(t *testing.T) {
	a, _ := NewFixed(1000, 3) // 1.000
	b, _ := NewFixed(1000, 3) // 1.000
	got, err := a.Mul(b)
	if err != nil {
		t.Fatalf("Mul error: %v", err)
	}
	if got.Scaled() != 1000 {
		t.Fatalf("got %d, want 1000", got.Scaled())
	}
}

func TestFixedDivByZero(t *testing.T) {
	a, _ := NewFixed(1, 0)
	b, _ := NewFixed(0, 0)
	if _, err := a.Div(b); err == nil {
		t.Fatal("expected division by zero error")
	}
}

func TestFixedAddOverflow(t *testing.T) {
	a, _ := NewFixed(1<<62, 0)
	b, _ := NewFixed(1<<62, 0)
	if _, err := a.Add(b); err == nil {
		t.Fatal("expected overflow error")
	}
}

func TestFixedRoundNegativeHalfAwayFromZero(t *testing.T) {
	// -1.50 at scale 2 is a tie and rounds away from zero to -2.
	got := roundHalfAwayFromZero(-150, 2)
	if got != -2 {
		t.Fatalf("got %d, want -2", got)
	}
	// -1.25 at scale 2 rounds to nearest integer -1.
	got = roundHalfAwayFromZero(-125, 2)
	if got != -1 {
		t.Fatalf("got %d, want -1", got)
	}
}
