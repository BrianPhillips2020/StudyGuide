package bank

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"studyguide/internal/store"
)

// jsonItem is the JSON import format from the plan. id, module and kind are
// optional; without an id the key is derived from the question text.
type jsonItem struct {
	ID          string            `json:"id"`
	Module      string            `json:"module"`
	Topic       string            `json:"topic"`
	Kind        string            `json:"kind"`
	Question    string            `json:"question"`
	Choices     map[string]string `json:"choices"`
	Answer      string            `json:"answer"`
	Explanation string            `json:"explanation"`
}

// ParseJSON reads a JSON array of questions.
func ParseJSON(path string) ([]store.Question, []Issue, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	source := filepath.Base(path)
	var items []jsonItem
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", source, err)
	}

	var qs []store.Question
	var issues []Issue
	for i, it := range items {
		name := fmt.Sprintf("item %d", i+1)
		q := store.Question{
			Key:         it.ID,
			Module:      it.Module,
			Topic:       strings.TrimSpace(it.Topic),
			Kind:        strings.ToUpper(it.Kind),
			Prompt:      strings.TrimSpace(it.Question),
			Answer:      strings.TrimSpace(it.Answer),
			Explanation: strings.TrimSpace(it.Explanation),
			Source:      fmt.Sprintf("%s, %s", source, name),
		}
		if q.Key == "" {
			sum := sha1.Sum([]byte(q.Prompt))
			q.Key = "json-" + hex.EncodeToString(sum[:6])
		}
		if q.Module == "" {
			q.Module = strings.TrimSuffix(source, filepath.Ext(source))
		}
		if q.Topic == "" {
			q.Topic = "General"
		}

		labels := make([]string, 0, len(it.Choices))
		for l := range it.Choices {
			labels = append(labels, l)
		}
		sort.Strings(labels)
		for _, l := range labels {
			q.Choices = append(q.Choices, store.Choice{Label: l, Text: it.Choices[l]})
		}
		isTF := strings.EqualFold(q.Answer, "true") || strings.EqualFold(q.Answer, "false")
		if len(q.Choices) == 0 && isTF {
			q.Kind = "TF"
			q.Answer = strings.ToUpper(q.Answer[:1]) + strings.ToLower(q.Answer[1:])
			q.Choices = []store.Choice{{Label: "True", Text: "True"}, {Label: "False", Text: "False"}}
		}
		if q.Kind == "" {
			q.Kind = "MCQ"
		}

		var errs []string
		if q.Prompt == "" {
			errs = append(errs, "no question text")
		}
		if len(q.Choices) < 2 {
			errs = append(errs, "fewer than 2 choices")
		}
		if q.Answer == "" {
			errs = append(errs, "no correct answer given")
		} else if !hasLabel(q.Choices, q.Answer) {
			errs = append(errs, fmt.Sprintf("correct answer %q is not one of the choices", q.Answer))
		}
		for _, e := range errs {
			issues = append(issues, Issue{Source: source, Item: name, Message: e, Skipped: true})
		}
		if len(errs) > 0 {
			continue
		}
		if q.Explanation == "" {
			issues = append(issues, Issue{Source: source, Item: name, Message: "no explanation (imported anyway)"})
		}
		qs = append(qs, q)
	}
	return qs, issues, nil
}

// ParseFile dispatches on file extension.
func ParseFile(path string) ([]store.Question, []Issue, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pdf":
		return ParsePDF(path)
	case ".json":
		return ParseJSON(path)
	default:
		return nil, nil, fmt.Errorf("%s: unsupported file type (use .pdf or .json)", filepath.Base(path))
	}
}
