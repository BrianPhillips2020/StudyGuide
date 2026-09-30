// Package store owns the SQLite database: schema, question bank upserts,
// attempt logging, and the stats queries computed from attempt history.
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS questions (
	id          INTEGER PRIMARY KEY,
	key         TEXT NOT NULL UNIQUE,
	module      TEXT NOT NULL,
	topic       TEXT NOT NULL,
	kind        TEXT NOT NULL,
	prompt      TEXT NOT NULL,
	answer      TEXT NOT NULL,
	explanation TEXT NOT NULL,
	figure      TEXT NOT NULL DEFAULT '',
	source      TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS choices (
	question_id INTEGER NOT NULL REFERENCES questions(id) ON DELETE CASCADE,
	position    INTEGER NOT NULL,
	label       TEXT NOT NULL,
	text        TEXT NOT NULL,
	PRIMARY KEY (question_id, label)
);

CREATE TABLE IF NOT EXISTS attempts (
	id          INTEGER PRIMARY KEY,
	question_id INTEGER NOT NULL REFERENCES questions(id) ON DELETE CASCADE,
	answer      TEXT NOT NULL,
	correct     INTEGER NOT NULL,
	answered_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS attempts_question ON attempts(question_id, id);
`

// Store wraps the SQLite connection.
type Store struct {
	db *sql.DB
}

// DefaultPath returns the database location inside the OS app-data folder.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "StudyGuide", "studyguide.db"), nil
}

// Open opens (creating if needed) the database at path and applies the schema.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	dsn := "file:" + filepath.ToSlash(path) +
		"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// One connection keeps SQLite writes serialized and pragmas consistent.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Choice is one answer option.
type Choice struct {
	Label string `json:"label"`
	Text  string `json:"text"`
}

// Question is a bank item. Key is a stable identifier (e.g. "M1-Q9") so a
// reimport updates questions in place and keeps their attempt history.
type Question struct {
	ID          int64    `json:"id"`
	Key         string   `json:"key"`
	Module      string   `json:"module"`
	Topic       string   `json:"topic"`
	Kind        string   `json:"kind"` // "MCQ" or "TF"
	Prompt      string   `json:"prompt"`
	Choices     []Choice `json:"choices"`
	Answer      string   `json:"answer"`
	Explanation string   `json:"explanation"`
	Figure      string   `json:"figure"`
	Source      string   `json:"source"`
}

// UpsertQuestions inserts or updates questions by Key in one transaction.
// It returns how many were newly added and how many already existed.
func (s *Store) UpsertQuestions(qs []Question) (added, updated int, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	for _, q := range qs {
		var id int64
		err := tx.QueryRow(`SELECT id FROM questions WHERE key = ?`, q.Key).Scan(&id)
		switch {
		case err == sql.ErrNoRows:
			res, err := tx.Exec(`INSERT INTO questions
				(key, module, topic, kind, prompt, answer, explanation, figure, source)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				q.Key, q.Module, q.Topic, q.Kind, q.Prompt, q.Answer, q.Explanation, q.Figure, q.Source)
			if err != nil {
				return 0, 0, err
			}
			if id, err = res.LastInsertId(); err != nil {
				return 0, 0, err
			}
			added++
		case err != nil:
			return 0, 0, err
		default:
			if _, err := tx.Exec(`UPDATE questions SET
				module = ?, topic = ?, kind = ?, prompt = ?, answer = ?, explanation = ?, figure = ?, source = ?
				WHERE id = ?`,
				q.Module, q.Topic, q.Kind, q.Prompt, q.Answer, q.Explanation, q.Figure, q.Source, id); err != nil {
				return 0, 0, err
			}
			if _, err := tx.Exec(`DELETE FROM choices WHERE question_id = ?`, id); err != nil {
				return 0, 0, err
			}
			updated++
		}
		for i, c := range q.Choices {
			if _, err := tx.Exec(`INSERT INTO choices (question_id, position, label, text) VALUES (?, ?, ?, ?)`,
				id, i, c.Label, c.Text); err != nil {
				return 0, 0, err
			}
		}
	}
	return added, updated, tx.Commit()
}

// Question loads one question with its choices.
func (s *Store) Question(id int64) (Question, error) {
	var q Question
	err := s.db.QueryRow(`SELECT id, key, module, topic, kind, prompt, answer, explanation, figure, source
		FROM questions WHERE id = ?`, id).
		Scan(&q.ID, &q.Key, &q.Module, &q.Topic, &q.Kind, &q.Prompt, &q.Answer, &q.Explanation, &q.Figure, &q.Source)
	if err != nil {
		return q, err
	}
	rows, err := s.db.Query(`SELECT label, text FROM choices WHERE question_id = ? ORDER BY position`, id)
	if err != nil {
		return q, err
	}
	defer rows.Close()
	for rows.Next() {
		var c Choice
		if err := rows.Scan(&c.Label, &c.Text); err != nil {
			return q, err
		}
		q.Choices = append(q.Choices, c)
	}
	return q, rows.Err()
}

// Candidate is a question id with how often it has been attempted, used by
// the quiz package to pick the next question.
type Candidate struct {
	ID       int64
	Attempts int
}

// Candidates returns questions matching the filter. mode is "all", "missed"
// (most recent attempt was wrong) or "topic". module and topic narrow the set
// when non-empty.
func (s *Store) Candidates(mode, module, topic string) ([]Candidate, error) {
	query := `
		SELECT q.id, COUNT(a.id)
		FROM questions q
		LEFT JOIN attempts a ON a.question_id = q.id
		WHERE (? = '' OR q.module = ?) AND (? = '' OR q.topic = ?)`
	if mode == "missed" {
		query += `
		AND (SELECT correct FROM attempts WHERE question_id = q.id ORDER BY id DESC LIMIT 1) = 0`
	}
	query += ` GROUP BY q.id`

	rows, err := s.db.Query(query, module, module, topic, topic)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Candidate
	for rows.Next() {
		var c Candidate
		if err := rows.Scan(&c.ID, &c.Attempts); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// RecordAttempt logs one answer.
func (s *Store) RecordAttempt(questionID int64, answer string, correct bool, at time.Time) error {
	_, err := s.db.Exec(`INSERT INTO attempts (question_id, answer, correct, answered_at) VALUES (?, ?, ?, ?)`,
		questionID, answer, correct, at.UTC().Format(time.RFC3339))
	return err
}

// Topic is one module/topic pair, for building filters.
type Topic struct {
	Module string `json:"module"`
	Topic  string `json:"topic"`
	Count  int    `json:"count"`
}

// Topics lists every topic in bank order.
func (s *Store) Topics() ([]Topic, error) {
	rows, err := s.db.Query(`SELECT module, topic, COUNT(*) FROM questions
		GROUP BY module, topic ORDER BY module, MIN(id)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Topic
	for rows.Next() {
		var t Topic
		if err := rows.Scan(&t.Module, &t.Topic, &t.Count); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
