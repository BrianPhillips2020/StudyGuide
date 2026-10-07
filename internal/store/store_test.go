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

func TestResetAndRemove(t *testing.T) {
	s := openTest(t)
	a, b := sample("a", "T1", "a"), sample("b", "T2", "b")
	b.Module = "Module 2: Other"
	a.Image = &Figure{Mime: "image/png", Data: []byte{1, 2, 3}}
	s.UpsertQuestions([]Question{a, b})
	all, _ := s.Candidates("all", "", "")
	for _, c := range all {
		s.RecordAttempt(c.ID, "A", true, time.Now())
	}
	count := func(table string) (n int) {
		s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n)
		return n
	}

	if err := s.ResetStats(); err != nil || count("attempts") != 0 || count("questions") != 2 {
		t.Fatalf("ResetStats: err=%v attempts=%d questions=%d", err, count("attempts"), count("questions"))
	}

	for _, c := range all {
		s.RecordAttempt(c.ID, "A", true, time.Now())
	}
	n, err := s.RemoveModule("Module 1: Test")
	if err != nil || n != 1 {
		t.Fatalf("RemoveModule: n=%d err=%v", n, err)
	}
	if count("questions") != 1 || count("attempts") != 1 || count("figures") != 0 || count("choices") != 2 {
		t.Errorf("RemoveModule left questions=%d attempts=%d figures=%d choices=%d",
			count("questions"), count("attempts"), count("figures"), count("choices"))
	}

	if err := s.HardReset(); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"questions", "choices", "attempts", "figures"} {
		if n := count(table); n != 0 {
			t.Errorf("HardReset left %d rows in %s", n, table)
		}
	}
}

func TestMissedAndStats(t *testing.T) {
	s := openTest(t)
	s.UpsertQuestions([]Question{sample("a", "T1", "a"), sample("b", "T1", "b"), sample("c", "T2", "c")})
	all, _ := s.Candidates("all", "", "")
	a, b, c := all[0].ID, all[1].ID, all[2].ID
	now := time.Now()
	missedIDs := func() map[int64]bool {
		m := map[int64]bool{}
		cands, _ := s.Candidates("missed", "", "")
		for _, x := range cands {
			m[x.ID] = true
		}
		return m
	}

	s.RecordAttempt(a, "B", false, now) // a: wrong
	s.RecordAttempt(b, "B", false, now) // b: wrong, then right: still missed
	s.RecordAttempt(b, "A", true, now)
	s.RecordAttempt(c, "B", false, now) // c: wrong, then right MissedWindow-1 times
	for range MissedWindow - 1 {
		s.RecordAttempt(c, "A", true, now)
	}
	if m := missedIDs(); len(m) != 3 {
		t.Errorf("missed = %v, want a, b and c (c's miss is still inside the window)", m)
	}

	s.RecordAttempt(c, "A", true, now) // c's miss now falls outside the window
	if m := missedIDs(); len(m) != 2 || !m[a] || !m[b] {
		t.Errorf("missed = %v, want only a and b", m)
	}

	topic, _ := s.Candidates("all", "", "T2")
	if len(topic) != 1 {
		t.Errorf("topic filter returned %d, want 1", len(topic))
	}

	st, err := s.Stats()
	if err != nil {
		t.Fatal(err)
	}
	wantAttempts := 3 + 1 + MissedWindow
	if st.TotalQuestions != 3 || st.SeenQuestions != 3 || st.Attempts != wantAttempts ||
		st.Correct != 1+MissedWindow || st.MissedNow != 2 {
		t.Errorf("stats = %+v", st)
	}
	if st.CurrentStreak != MissedWindow || st.BestStreak != MissedWindow {
		t.Errorf("streaks = %d/%d, want %d/%d", st.CurrentStreak, st.BestStreak, MissedWindow, MissedWindow)
	}
	if len(st.MostMissed) != 3 || len(st.Topics) != 2 || len(st.Daily) != 1 {
		t.Errorf("mostMissed=%d topics=%d daily=%d", len(st.MostMissed), len(st.Topics), len(st.Daily))
	}
}
