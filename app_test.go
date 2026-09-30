package main

import (
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"studyguide/internal/bank"
	"studyguide/internal/store"
)

// TestEndToEnd imports the real question pools into a temp database, answers
// some questions through the bound methods, and reimports.
func TestEndToEnd(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("questionPools", "*.pdf"))
	if len(files) == 0 {
		t.Skip("no question pool PDFs found")
	}
	s, err := store.Open(filepath.Join(t.TempDir(), "e2e.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := &App{store: s}

	importAll := func() (int, int) {
		var all []store.Question
		for _, f := range files {
			qs, _, err := bank.ParsePDF(f)
			if err != nil {
				t.Fatal(err)
			}
			all = append(all, qs...)
		}
		added, updated, err := s.UpsertQuestions(all)
		if err != nil {
			t.Fatal(err)
		}
		return added, updated
	}

	added, _ := importAll()
	t.Logf("imported %d questions", added)

	// Answer three questions: one right, two wrong.
	for i := range 3 {
		q, err := a.NextQuestion(Filter{Mode: "all"})
		if err != nil || q == nil {
			t.Fatalf("NextQuestion: %v %v", q, err)
		}
		full, _ := s.Question(q.ID)
		answer := full.Answer
		if i > 0 {
			answer = "wrong"
		}
		res, err := a.SubmitAnswer(q.ID, answer)
		if err != nil || res.Correct != (i == 0) {
			t.Fatalf("SubmitAnswer %d: %+v %v", i, res, err)
		}
	}

	missed, err := a.NextQuestion(Filter{Mode: "missed"})
	if err != nil || missed == nil || missed.Pool != 2 {
		t.Fatalf("missed filter: %+v %v", missed, err)
	}

	again, updated := importAll()
	if again != 0 || updated != added {
		t.Errorf("reimport added %d updated %d, want 0 and %d", again, updated, added)
	}
	st, err := a.GetStats()
	if err != nil || st.Attempts != 3 || st.MissedNow != 2 || st.TotalQuestions != added {
		t.Errorf("stats after reimport: %+v %v", st, err)
	}

	// Every figure question should serve its image; others should 404.
	h := figureHandler{a}
	withImage := 0
	for id := int64(1); id <= int64(added); id++ {
		q, err := s.Question(id)
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", fmt.Sprintf("/figures/%d", id), nil))
		if q.HasImage {
			withImage++
			ct := rec.Header().Get("Content-Type")
			if rec.Code != 200 || !strings.HasPrefix(ct, "image/") || rec.Body.Len() == 0 {
				t.Errorf("%s: figure served %d %q (%d bytes)", q.Key, rec.Code, ct, rec.Body.Len())
			}
		} else if rec.Code != 404 {
			t.Errorf("%s: no figure but got %d", q.Key, rec.Code)
		}
		if (q.Figure != "") != q.HasImage {
			t.Errorf("%s: caption %q but HasImage=%v", q.Key, q.Figure, q.HasImage)
		}
	}
	t.Logf("%d questions serve a figure image", withImage)
}
