package retest

import (
	"github.com/example/aac-block-masonry-admission-closure/internal/domain"
)

// ReviewBoard owns the independent dual-person review gate for a generation and
// the closure conditions that gate the single-writer final verdict.
type ReviewBoard struct {
	reviews []Review
}

// NewReviewBoard returns an empty review board.
func NewReviewBoard() *ReviewBoard { return &ReviewBoard{} }

// Reviews returns the reviews in submission order.
func (b *ReviewBoard) Reviews() []Review { return b.reviews }

// Load replaces the board with a recovered snapshot.
func (b *ReviewBoard) Load(reviews []Review) { b.reviews = append([]Review(nil), reviews...) }

// Submit records an independent review. A person may only review a generation
// once; a repeat by the same person is an idempotent no-op only when the
// summary matches, otherwise it is a generation conflict.
func (b *ReviewBoard) Submit(r Review) error {
	for _, existing := range b.reviews {
		if existing.Person == r.Person && existing.Generation == r.Generation {
			if existing.Summary != r.Summary {
				return domain.Newf(domain.CodeGenerationConflict, "person %s already reviewed generation %d", r.Person, r.Generation)
			}
			return nil
		}
	}
	b.reviews = append(b.reviews, r)
	return nil
}

// DistinctQualified returns the distinct qualified reviewers who independently
// passed the given generation, in submission order.
func (b *ReviewBoard) DistinctQualified(gen domain.Generation) []string {
	seen := make(map[string]bool)
	var out []string
	for _, r := range b.reviews {
		if r.Generation == gen && r.Qualified && !seen[r.Person] {
			seen[r.Person] = true
			out = append(out, r.Person)
		}
	}
	return out
}

// AdmitEligible reports whether a generation may be admitted: at least two
// distinct qualified reviewers must have independently passed it.
func (b *ReviewBoard) AdmitEligible(gen domain.Generation) error {
	persons := b.DistinctQualified(gen)
	if len(persons) < 2 {
		return domain.Newf(domain.CodeGenerationConflict, "generation %d requires two distinct qualified reviewers, have %d", gen, len(persons))
	}
	return nil
}
