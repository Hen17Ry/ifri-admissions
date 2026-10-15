package selection

import (
	"math/big"
	"sort"
	"time"

	"ifri-admissions/services/api/internal/modules/submissions"
)

// CalculationVersion identifies the ranking rule below; stored with every freeze.
const CalculationVersion = 1

const (
	CriterionAverage = "AVERAGE"
	CriterionMath    = "MATH"
	CriterionDate    = "SUBMITTED_AT"
	CriterionNumber  = "APPLICATION_NUMBER"

	MathNotNeeded = "NOT_NEEDED" // unique average: no tie to break
	MathCompared  = "COMPARED"   // every member of the tie group has a mathematics subject
	MathSkipped   = "SKIPPED"    // a member has none: the criterion is skipped for the whole group

	Admitted   = "ADMITTED"
	Waitlisted = "WAITLISTED"
	NotRanked  = "NOT_RANKED"
	Withdrawn  = "WITHDRAWN"
)

// Candidate is a validated dossier with the values the ranking rule reads. Weighted and
// Coefficients are the exact effective sums (after approved corrections) from the review.
type Candidate struct {
	ApplicationID string
	Number        string
	SubmittedAt   time.Time
	Weighted      *big.Rat
	Coefficients  *big.Rat
	// MathGrade is the effective grade of the single subject listed in ranking_math_subjects;
	// nil when the candidate's grid has none. MathSubjects counts such subjects in the grid.
	MathSubject  string
	MathGrade    *big.Rat
	MathSubjects int
}

// Ranked is a candidate at its place: Position orders the whole list (ranked first, then not
// ranked); Rank is 0 for a candidate below the minimum average ("Non classé").
type Ranked struct {
	Candidate
	Position     int
	Rank         int
	MathTiebreak string
	// Decisive is the criterion that separated this candidate from the previous one ("" first).
	Decisive string
	// Decision is empty while the seats are not set.
	Decision string
}

// compareAverages compares two exact fractions by cross product: w1/c1 against w2/c2.
func compareAverages(a, b Candidate) int {
	left := new(big.Rat).Mul(a.Weighted, b.Coefficients)
	right := new(big.Rat).Mul(b.Weighted, a.Coefficients)
	return left.Cmp(right)
}

// Rank orders candidates, strongest first:
//  1. effective average, compared exactly (cross product of the fractions);
//  2. within a group of exactly equal averages, the effective mathematics grade, highest first,
//     only if every member of the group has a mathematics subject — otherwise skipped for the
//     whole group (a pairwise skip is not transitive: see docs/RANKING_SELECTION.md);
//  3. earliest submission;
//  4. application number, for a total and deterministic order.
//
// Below the minimum average (if any) a candidate is not ranked. With seats set, the first
// ranked candidates up to the seats are admitted and the other ranked ones wait.
func Rank(candidates []Candidate, seats *int, minimum *big.Rat) []Ranked {
	list := make([]Ranked, len(candidates))
	for i, c := range candidates {
		list[i] = Ranked{Candidate: c}
	}
	sort.SliceStable(list, func(i, j int) bool {
		if c := compareAverages(list[i].Candidate, list[j].Candidate); c != 0 {
			return c > 0
		}
		return list[i].Number < list[j].Number
	})
	for start := 0; start < len(list); {
		end := start + 1
		for end < len(list) && compareAverages(list[start].Candidate, list[end].Candidate) == 0 {
			end++
		}
		group := list[start:end]
		tiebreak := MathNotNeeded
		if len(group) > 1 {
			tiebreak = MathCompared
			for _, r := range group {
				if r.MathGrade == nil {
					tiebreak = MathSkipped
				}
			}
		}
		sort.SliceStable(group, func(i, j int) bool {
			a, b := group[i], group[j]
			if tiebreak == MathCompared {
				if c := a.MathGrade.Cmp(b.MathGrade); c != 0 {
					return c > 0
				}
			}
			if !a.SubmittedAt.Equal(b.SubmittedAt) {
				return a.SubmittedAt.Before(b.SubmittedAt)
			}
			return a.Number < b.Number
		})
		for i := range group {
			group[i].MathTiebreak = tiebreak
		}
		start = end
	}
	rank := 0
	for i := range list {
		r := &list[i]
		r.Position = i + 1
		if i > 0 {
			r.Decisive = decisive(list[i-1], *r)
		}
		if minimum != nil && new(big.Rat).Mul(minimum, r.Coefficients).Cmp(r.Weighted) > 0 {
			if seats != nil {
				r.Decision = NotRanked
			}
			continue
		}
		rank++
		r.Rank = rank
		if seats != nil {
			r.Decision = Waitlisted
			if rank <= *seats {
				r.Decision = Admitted
			}
		}
	}
	return list
}

// decisive names the first criterion that orders two adjacent candidates.
func decisive(previous, current Ranked) string {
	if compareAverages(previous.Candidate, current.Candidate) != 0 {
		return CriterionAverage
	}
	if current.MathTiebreak == MathCompared && previous.MathGrade.Cmp(current.MathGrade) != 0 {
		return CriterionMath
	}
	if !previous.SubmittedAt.Equal(current.SubmittedAt) {
		return CriterionDate
	}
	return CriterionNumber
}

// AverageDisplay truncates an exact fraction to 2 decimals for reading, never rounding.
func AverageDisplay(weighted, coefficients *big.Rat) string {
	display, _ := submissions.Truncate(new(big.Rat).Quo(weighted, coefficients), 2)
	return display
}

// Event is a withdrawal or a promotion after the freeze; version 1 is the freeze itself.
type Event struct {
	Version       int
	Type          string // WITHDRAWN or PROMOTED
	ApplicationID string
}

// States derives every candidate's current decision from the frozen snapshot and the events up
// to a list version; the snapshot itself never changes.
func States(entries []Ranked, events []Event, upTo int) map[string]string {
	states := make(map[string]string, len(entries))
	for _, e := range entries {
		states[e.ApplicationID] = e.Decision
	}
	for _, ev := range events {
		if ev.Version > upTo {
			continue
		}
		switch ev.Type {
		case "WITHDRAWN":
			states[ev.ApplicationID] = Withdrawn
		case "PROMOTED":
			if states[ev.ApplicationID] != Withdrawn {
				states[ev.ApplicationID] = Admitted
			}
		}
	}
	return states
}

// FreeSeats is the number of seats left by withdrawals and not yet given by a promotion.
func FreeSeats(seats int, states map[string]string) int {
	admitted := 0
	for _, s := range states {
		if s == Admitted {
			admitted++
		}
	}
	return max(seats-admitted, 0)
}

// FirstWaiting is the next candidate to promote: the best-ranked one still waiting.
func FirstWaiting(entries []Ranked, states map[string]string) (Ranked, bool) {
	best := Ranked{}
	for _, e := range entries {
		if states[e.ApplicationID] == Waitlisted && e.Rank > 0 && (best.Rank == 0 || e.Rank < best.Rank) {
			best = e
		}
	}
	return best, best.Rank > 0
}
