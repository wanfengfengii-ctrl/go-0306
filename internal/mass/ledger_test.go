package mass

import "testing"

func TestLedgerOverdraw(t *testing.T) {
	l := NewLedger()
	if err := l.Apply(MassLedgerEntry{Account: "fly_ash", Direction: Credit, Grams: 10}); err == nil {
		t.Fatal("expected overdraw error")
	}
}

func TestLedgerBalancedTransaction(t *testing.T) {
	l := NewLedger()
	if err := l.Apply(MassLedgerEntry{Account: "raw", Direction: Debit, Grams: 100}); err != nil {
		t.Fatal(err)
	}
	entries := []MassLedgerEntry{
		{Account: "raw", Direction: Credit, Grams: 100},
		{Account: "product", Direction: Debit, Grams: 90},
		{Account: "waste", Direction: Debit, Grams: 10},
	}
	if err := l.ApplyTransaction(entries); err != nil {
		t.Fatal(err)
	}
	if l.Balance("raw") != 0 {
		t.Fatalf("raw balance %d, want 0", l.Balance("raw"))
	}
	if l.Balance("product") != 90 || l.Balance("waste") != 10 {
		t.Fatalf("unexpected output balances: product=%d waste=%d", l.Balance("product"), l.Balance("waste"))
	}
}

func TestLedgerUnbalancedRollsBack(t *testing.T) {
	l := NewLedger()
	if err := l.Apply(MassLedgerEntry{Account: "raw", Direction: Debit, Grams: 100}); err != nil {
		t.Fatal(err)
	}
	entries := []MassLedgerEntry{
		{Account: "raw", Direction: Credit, Grams: 30},
		{Account: "product", Direction: Debit, Grams: 20},
	}
	if err := l.ApplyTransaction(entries); err == nil {
		t.Fatal("expected unbalanced error")
	}
	if l.Balance("raw") != 100 {
		t.Fatalf("raw balance %d, want 100 (rollback)", l.Balance("raw"))
	}
}

func TestLedgerOverdrawRollsBack(t *testing.T) {
	l := NewLedger()
	if err := l.Apply(MassLedgerEntry{Account: "raw", Direction: Debit, Grams: 5}); err != nil {
		t.Fatal(err)
	}
	entries := []MassLedgerEntry{
		{Account: "raw", Direction: Credit, Grams: 10},
		{Account: "product", Direction: Debit, Grams: 10},
	}
	if err := l.ApplyTransaction(entries); err == nil {
		t.Fatal("expected overdraw error")
	}
	if l.Balance("raw") != 5 {
		t.Fatalf("raw balance %d, want 5 (rollback)", l.Balance("raw"))
	}
}
