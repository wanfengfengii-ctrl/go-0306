// Package mass implements the 质量守恒与资源租约管理器 component: integer-gram
// inventory reservation, deduction, reclaim, output, sample consumption,
// offcut and loss balancing, plus mutually-exclusive resource leases with
// logical expiry, all committed in a single transaction with lineage and
// idempotency records.
package mass

import (
	"github.com/example/aac-block-masonry-admission-closure/internal/domain"
)

// Direction is the debit/credit direction of a ledger entry.
type Direction int

const (
	Debit  Direction = 1  // increases an account
	Credit Direction = -1 // decreases an account
)

// MassLedgerEntry is one double-entry line in the integer-gram ledger. After
// commit the net of all lines in a conversion transaction must be zero.
type MassLedgerEntry struct {
	Account     string             `json:"account"`
	Direction   Direction          `json:"direction"`
	Grams       int64              `json:"grams"`
	Transaction string             `json:"transaction"`
	LineageNode string             `json:"lineage_node"`
	OperationID domain.OperationID `json:"operation_id"`
}

// Ledger tracks integer-gram accounts with overdraw protection.
type Ledger struct {
	accounts map[string]int64
}

// NewLedger returns an empty ledger.
func NewLedger() *Ledger {
	return &Ledger{accounts: make(map[string]int64)}
}

// Balance returns the current integer-gram balance of an account.
func (l *Ledger) Balance(account string) int64 { return l.accounts[account] }

// Accounts returns a copy of all account balances, used for restart recovery
// snapshots.
func (l *Ledger) Accounts() map[string]int64 {
	out := make(map[string]int64, len(l.accounts))
	for k, v := range l.accounts {
		out[k] = v
	}
	return out
}

// Load replaces the account balances with the supplied snapshot. Balances are
// never negative; callers supply only committed, non-negative accounts.
func (l *Ledger) Load(accounts map[string]int64) {
	l.accounts = make(map[string]int64, len(accounts))
	for k, v := range accounts {
		if v >= 0 {
			l.accounts[k] = v
		}
	}
}

// Apply posts a single entry, rejecting material overdraw on credit.
func (l *Ledger) Apply(e MassLedgerEntry) error {
	if e.Grams < 0 {
		return domain.New(domain.CodeInvalidArgument, "mass must be non-negative grams")
	}
	if e.Direction == Credit {
		cur := l.accounts[e.Account]
		if e.Grams > cur {
			return domain.Newf(domain.CodeMaterialOverdraw, "account %s would go negative", e.Account)
		}
		l.accounts[e.Account] = cur - e.Grams
		return nil
	}
	l.accounts[e.Account] += e.Grams
	return nil
}

// ApplyTransaction posts a balanced set of entries and verifies the net is
// zero before returning. On any failure the ledger is left unchanged.
func (l *Ledger) ApplyTransaction(entries []MassLedgerEntry) error {
	snapshot := make(map[string]int64, len(l.accounts))
	for k, v := range l.accounts {
		snapshot[k] = v
	}
	var net int64
	for _, e := range entries {
		if err := l.Apply(e); err != nil {
			l.accounts = snapshot
			return err
		}
		net += int64(e.Direction) * e.Grams
	}
	if net != 0 {
		l.accounts = snapshot
		return domain.New(domain.CodeInvalidArgument, "conversion transaction is not balanced")
	}
	return nil
}
