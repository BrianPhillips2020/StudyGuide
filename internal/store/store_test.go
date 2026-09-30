package store

import (
	"path/filepath"
	"testing"
	"time"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func sample(key, topic, prompt string) Question {
	return Question{
		Key: key, Module: "Module 1: Test", Topic: topic, Kind: "MCQ", Prompt: prompt,
		Choices: []Choice{{"A", "a"}, {"B", "b"}}, Answer: "A", Explanation: "because",
	}
}

func TestReimportKeepsHistory(t *testing.T) {
	s := openTest(t)
	added, updated, err := s.UpsertQuestions([]Question{sample("M1-Q1", "T1", "old"), sample("M1-Q2", "T2", "two")})
	if err != nil || added != 2 || updated != 0 {
		t.Fatalf("first import: added=%d updated=%d err=%v", added, updated, err)
	}
	cands, _ := s.Candidates("all", "", "")
	id := cands[0].ID
	if err := s.RecordAttempt(id, "B", false, time.Now()); err != nil {
		t.Fatal(err)
	}

	q := sample("M1-Q1", "T1", "new text")
	q.Choices = []Choice{{"A", "x"}, {"B", "y"}, {"C", "z"}}
	added, updated, err = s.UpsertQuestions([]Question{q})
	if err != nil || added != 0 || updated != 1 {
		t.Fatalf("reimport: added=%d updated=%d err=%v", added, updated, err)
	}
	got, err := s.Question(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Prompt != "new text" || len(got.Choices) != 3 {
		t.Errorf("reimport did not update question: %+v", got)
	}
	h, _ := s.History(id)
	if len(h) != 1 {
		t.Errorf("history lost on reimport: %+v", h)
	}
}

func TestMissedAndStats(t *testing.T) {
	s := openTest(t)
	s.UpsertQuestions([]Question{sample("a", "T1", "a"), sample("b", "T1", "b"), sample("c", "T2", "c")})
	all, _ := s.Candidates("all", "", "")
	a, b := all[0].ID, all[1].ID
	now := time.Now()

	s.RecordAttempt(a, "B", false, now) // a: wrong
	s.RecordAttempt(b, "B", false, now) // b: wrong, then right
	s.RecordAttempt(b, "A", true, now)

	missed, _ := s.Candidates("missed", "", "")
	if len(missed) != 1 || missed[0].ID != a {
		t.Errorf("missed = %+v, want only question a", missed)
	}
	topic, _ := s.Candidates("all", "", "T2")
	if len(topic) != 1 {
		t.Errorf("topic filter returned %d, want 1", len(topic))
	}

	st, err := s.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if st.TotalQuestions != 3 || st.SeenQuestions != 2 || st.Attempts != 3 || st.Correct != 1 || st.MissedNow != 1 {
		t.Errorf("stats = %+v", st)
	}
	if st.CurrentStreak != 1 || st.BestStreak != 1 {
		t.Errorf("streaks = %d/%d, want 1/1", st.CurrentStreak, st.BestStreak)
	}
	if len(st.MostMissed) != 2 || len(st.Topics) != 2 || len(st.Daily) != 1 {
		t.Errorf("mostMissed=%d topics=%d daily=%d", len(st.MostMissed), len(st.Topics), len(st.Daily))
	}
}
