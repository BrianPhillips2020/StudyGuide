// Package quiz holds question selection and grading, independent of storage.
package quiz

import (
	"math/rand/v2"
	"strings"

	"studyguide/internal/store"
)

// Pick chooses the next question id from candidates. It favors the questions
// attempted least often, so a pass covers the whole set before repeating, and
// avoids serving lastID twice in a row when there is any alternative.
// It returns false when there are no candidates.
func Pick(cands []store.Candidate, lastID int64) (int64, bool) {
	if len(cands) > 1 {
		filtered := cands[:0:0]
		for _, c := range cands {
			if c.ID != lastID {
				filtered = append(filtered, c)
			}
		}
		cands = filtered
	}
	if len(cands) == 0 {
		return 0, false
	}

	least := cands[0].Attempts
	for _, c := range cands[1:] {
		least = min(least, c.Attempts)
	}
	var pool []int64
	for _, c := range cands {
		if c.Attempts == least {
			pool = append(pool, c.ID)
		}
	}
	return pool[rand.IntN(len(pool))], true
}

// Grade reports whether answer matches the correct answer, ignoring case and
// surrounding whitespace ("true" matches "True", " b" matches "B").
func Grade(answer, correct string) bool {
	return strings.EqualFold(strings.TrimSpace(answer), strings.TrimSpace(correct))
}
