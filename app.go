package main

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"studyguide/internal/bank"
	"studyguide/internal/quiz"
	"studyguide/internal/store"
)

// App is bound to the frontend; Wails exposes its exported methods to JS
// as window.go.main.App.<Method>.
type App struct {
	ctx     context.Context
	store   *store.Store
	dbPath  string
	openErr error
	lastID  int64
}

func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.dbPath, a.openErr = store.DefaultPath()
	if a.openErr == nil {
		a.store, a.openErr = store.Open(a.dbPath)
	}
}

func (a *App) shutdown(ctx context.Context) {
	if a.store != nil {
		a.store.Close()
	}
}

func (a *App) db() (*store.Store, error) {
	if a.openErr != nil {
		return nil, fmt.Errorf("database unavailable (%s): %w", a.dbPath, a.openErr)
	}
	return a.store, nil
}

// Filter selects which questions NextQuestion draws from.
type Filter struct {
	Mode   string `json:"mode"` // "all" or "missed"
	Module string `json:"module"`
	Topic  string `json:"topic"`
}

// QuestionView is a question as shown before answering; it deliberately
// leaves out the answer and explanation.
type QuestionView struct {
	ID       int64          `json:"id"`
	Key      string         `json:"key"`
	Module   string         `json:"module"`
	Topic    string         `json:"topic"`
	Kind     string         `json:"kind"`
	Prompt   string         `json:"prompt"`
	Choices  []store.Choice `json:"choices"`
	Figure   string         `json:"figure"`
	Source   string         `json:"source"`
	Attempts int            `json:"attempts"`
	Pool     int            `json:"pool"` // questions matching the filter
}

// NextQuestion returns the next question for the filter, or null when no
// question matches.
func (a *App) NextQuestion(f Filter) (*QuestionView, error) {
	s, err := a.db()
	if err != nil {
		return nil, err
	}
	cands, err := s.Candidates(f.Mode, f.Module, f.Topic)
	if err != nil {
		return nil, err
	}
	id, ok := quiz.Pick(cands, a.lastID)
	if !ok {
		return nil, nil
	}
	q, err := s.Question(id)
	if err != nil {
		return nil, err
	}
	a.lastID = id
	v := &QuestionView{ID: q.ID, Key: q.Key, Module: q.Module, Topic: q.Topic, Kind: q.Kind,
		Prompt: q.Prompt, Choices: q.Choices, Figure: q.Figure, Source: q.Source, Pool: len(cands)}
	for _, c := range cands {
		if c.ID == id {
			v.Attempts = c.Attempts
		}
	}
	return v, nil
}

// Result is the feedback after an answer.
type Result struct {
	Correct     bool            `json:"correct"`
	Answer      string          `json:"answer"`
	Explanation string          `json:"explanation"`
	History     []store.Attempt `json:"history"`
}

// SubmitAnswer grades and records an attempt.
func (a *App) SubmitAnswer(id int64, answer string) (Result, error) {
	s, err := a.db()
	if err != nil {
		return Result{}, err
	}
	q, err := s.Question(id)
	if err != nil {
		return Result{}, err
	}
	ok := quiz.Grade(answer, q.Answer)
	if err := s.RecordAttempt(id, answer, ok, time.Now()); err != nil {
		return Result{}, err
	}
	hist, err := s.History(id)
	if err != nil {
		return Result{}, err
	}
	return Result{Correct: ok, Answer: q.Answer, Explanation: q.Explanation, History: hist}, nil
}

// GetStats returns overall, per-topic, most-missed and daily stats.
func (a *App) GetStats() (store.Stats, error) {
	s, err := a.db()
	if err != nil {
		return store.Stats{}, err
	}
	return s.Stats()
}

// GetTopics lists modules and topics for the Study filter.
func (a *App) GetTopics() ([]store.Topic, error) {
	s, err := a.db()
	if err != nil {
		return nil, err
	}
	return s.Topics()
}

// QuestionHistory is one question in full plus every attempt at it.
type QuestionHistory struct {
	Question store.Question  `json:"question"`
	Attempts []store.Attempt `json:"attempts"`
}

// GetQuestionHistory returns a question (with answer) and its attempts.
func (a *App) GetQuestionHistory(id int64) (QuestionHistory, error) {
	s, err := a.db()
	if err != nil {
		return QuestionHistory{}, err
	}
	q, err := s.Question(id)
	if err != nil {
		return QuestionHistory{}, err
	}
	h, err := s.History(id)
	return QuestionHistory{Question: q, Attempts: h}, err
}

// ImportResult summarizes an import.
type ImportResult struct {
	Files   []string     `json:"files"`
	Added   int          `json:"added"`
	Updated int          `json:"updated"`
	Issues  []bank.Issue `json:"issues"`
	DBPath  string       `json:"dbPath"`
}

// ImportBank opens a native file picker and loads the chosen PDFs or JSON
// files. Questions are matched by key, so reimporting updates them in place
// and keeps their history. Returns null if the picker is cancelled.
func (a *App) ImportBank() (*ImportResult, error) {
	s, err := a.db()
	if err != nil {
		return nil, err
	}
	paths, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Import question bank",
		Filters: []runtime.FileFilter{{DisplayName: "Question banks (*.pdf, *.json)", Pattern: "*.pdf;*.json"}},
	})
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, nil
	}

	// A file that fails to parse is reported as an issue rather than
	// aborting the other files in the same import.
	res := &ImportResult{DBPath: a.dbPath, Issues: []bank.Issue{}}
	var all []store.Question
	for _, p := range paths {
		qs, issues, err := bank.ParseFile(p)
		if err != nil {
			res.Issues = append(res.Issues, bank.Issue{Source: filepath.Base(p), Message: err.Error(), Skipped: true})
			continue
		}
		res.Files = append(res.Files, filepath.Base(p))
		res.Issues = append(res.Issues, issues...)
		all = append(all, qs...)
	}
	if len(all) > 0 {
		if res.Added, res.Updated, err = s.UpsertQuestions(all); err != nil {
			return nil, err
		}
	}
	return res, nil
}

// Info returns where the database lives, for the Import view.
func (a *App) Info() map[string]string {
	return map[string]string{"dbPath": a.dbPath}
}
