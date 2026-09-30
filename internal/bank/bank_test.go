package bank

import (
	"os"
	"path/filepath"
	"testing"
)

// TestParseQuestionPools parses the real course PDFs when they are present.
func TestParseQuestionPools(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("..", "..", "questionPools", "*.pdf"))
	if len(files) == 0 {
		t.Skip("no question pool PDFs found")
	}
	total, figures := 0, 0
	for _, f := range files {
		qs, issues, err := ParsePDF(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, is := range issues {
			t.Errorf("%s %s: %s", is.Source, is.Item, is.Message)
		}
		seen := map[string]bool{}
		for _, q := range qs {
			if seen[q.Key] {
				t.Errorf("duplicate key %s", q.Key)
			}
			seen[q.Key] = true
			if q.Topic == "General" {
				t.Errorf("%s has no topic heading", q.Key)
			}
			if q.Figure != "" {
				figures++
				if q.Image == nil || len(q.Image.Data) == 0 {
					t.Errorf("%s has a figure caption but no image", q.Key)
				} else if dir := os.Getenv("DUMP_FIGURES"); dir != "" {
					ext := map[string]string{"image/jpeg": ".jpg", "image/png": ".png"}[q.Image.Mime]
					os.WriteFile(filepath.Join(dir, q.Key+ext), q.Image.Data, 0o644)
					t.Logf("%s <- %s: %s", q.Key, q.ImageRef, q.Figure)
				}
			}
		}
		total += len(qs)
		if testing.Verbose() && len(qs) > 1 {
			q := qs[1]
			t.Logf("sample %s [%s] figure=%q\n%s\n%+v\nanswer=%s\n%s", q.Key, q.Kind, q.Figure, q.Prompt, q.Choices, q.Answer, q.Explanation)
			last := ""
			for _, q := range qs {
				if q.Topic != last {
					t.Logf("%s | %s", q.Module, q.Topic)
					last = q.Topic
				}
			}
		}
	}
	t.Logf("parsed %d questions (%d with figures) from %d files", total, figures, len(files))
}

func TestParseJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bank.json")
	os.WriteFile(path, []byte(`[
		{"topic": "T", "question": "Q1?", "choices": {"B": "b", "A": "a"}, "answer": "B", "explanation": "because"},
		{"topic": "T", "question": "Is it?", "answer": "true", "explanation": "yes"},
		{"topic": "T", "question": "No answer?", "choices": {"A": "a", "B": "b"}},
		{"topic": "T", "question": "Bad answer?", "choices": {"A": "a", "B": "b"}, "answer": "C"}
	]`), 0o644)

	qs, issues, err := ParseJSON(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(qs) != 2 {
		t.Fatalf("got %d questions, want 2", len(qs))
	}
	if qs[0].Choices[0].Label != "A" {
		t.Errorf("choices not sorted by label: %+v", qs[0].Choices)
	}
	if qs[1].Kind != "TF" || qs[1].Answer != "True" {
		t.Errorf("true/false item parsed as %s %q", qs[1].Kind, qs[1].Answer)
	}
	if len(issues) != 2 {
		t.Errorf("got %d issues, want 2: %+v", len(issues), issues)
	}
}
