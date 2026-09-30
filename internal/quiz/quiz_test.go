package quiz

import (
	"testing"

	"studyguide/internal/store"
)

func TestPickPrefersLeastAttempted(t *testing.T) {
	cands := []store.Candidate{{ID: 1, Attempts: 3}, {ID: 2, Attempts: 0}, {ID: 3, Attempts: 1}}
	for range 20 {
		if id, ok := Pick(cands, 0); !ok || id != 2 {
			t.Fatalf("Pick = %d, %v; want 2", id, ok)
		}
	}
}

func TestPickAvoidsRepeat(t *testing.T) {
	cands := []store.Candidate{{ID: 1, Attempts: 0}, {ID: 2, Attempts: 5}}
	if id, _ := Pick(cands, 1); id != 2 {
		t.Errorf("Pick repeated the last question")
	}
	if id, ok := Pick(cands[:1], 1); !ok || id != 1 {
		t.Errorf("Pick with a single candidate = %d, %v; want 1", id, ok)
	}
	if _, ok := Pick(nil, 0); ok {
		t.Errorf("Pick with no candidates reported ok")
	}
}

func TestGrade(t *testing.T) {
	if !Grade(" b", "B") || !Grade("true", "True") || Grade("A", "B") {
		t.Error("Grade mismatch")
	}
}
