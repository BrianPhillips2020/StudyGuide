package store

// Stats is everything the Stats view shows. All of it is derived from the
// attempts table; nothing is stored as a running counter.
type Stats struct {
	TotalQuestions int          `json:"totalQuestions"`
	SeenQuestions  int          `json:"seenQuestions"`
	MissedNow      int          `json:"missedNow"` // most recent attempt wrong
	Attempts       int          `json:"attempts"`
	Correct        int          `json:"correct"`
	CurrentStreak  int          `json:"currentStreak"`
	BestStreak     int          `json:"bestStreak"`
	Topics         []TopicStat  `json:"topics"`
	MostMissed     []MissedStat `json:"mostMissed"`
	Daily          []DayStat    `json:"daily"`
}

type TopicStat struct {
	Module    string `json:"module"`
	Topic     string `json:"topic"`
	Questions int    `json:"questions"`
	Seen      int    `json:"seen"`
	Attempts  int    `json:"attempts"`
	Correct   int    `json:"correct"`
}

type MissedStat struct {
	ID       int64  `json:"id"`
	Key      string `json:"key"`
	Topic    string `json:"topic"`
	Prompt   string `json:"prompt"`
	Attempts int    `json:"attempts"`
	Wrong    int    `json:"wrong"`
	LastOK   bool   `json:"lastOk"`
}

type DayStat struct {
	Day      string `json:"day"`
	Attempts int    `json:"attempts"`
	Correct  int    `json:"correct"`
}

// Attempt is one row of a question's history.
type Attempt struct {
	Answer     string `json:"answer"`
	Correct    bool   `json:"correct"`
	AnsweredAt string `json:"answeredAt"`
}

func (s *Store) Stats() (Stats, error) {
	var st Stats
	err := s.db.QueryRow(`SELECT
			(SELECT COUNT(*) FROM questions),
			(SELECT COUNT(DISTINCT question_id) FROM attempts),
			(SELECT COUNT(*) FROM attempts),
			(SELECT COALESCE(SUM(correct), 0) FROM attempts),
			(SELECT COUNT(*) FROM questions q WHERE
				(SELECT correct FROM attempts WHERE question_id = q.id ORDER BY id DESC LIMIT 1) = 0)`).
		Scan(&st.TotalQuestions, &st.SeenQuestions, &st.Attempts, &st.Correct, &st.MissedNow)
	if err != nil {
		return st, err
	}

	if st.CurrentStreak, st.BestStreak, err = s.streaks(); err != nil {
		return st, err
	}

	rows, err := s.db.Query(`SELECT q.module, q.topic, COUNT(DISTINCT q.id),
			COUNT(DISTINCT a.question_id), COUNT(a.id), COALESCE(SUM(a.correct), 0)
		FROM questions q LEFT JOIN attempts a ON a.question_id = q.id
		GROUP BY q.module, q.topic ORDER BY q.module, MIN(q.id)`)
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var t TopicStat
		if err := rows.Scan(&t.Module, &t.Topic, &t.Questions, &t.Seen, &t.Attempts, &t.Correct); err != nil {
			rows.Close()
			return st, err
		}
		st.Topics = append(st.Topics, t)
	}
	rows.Close()

	rows, err = s.db.Query(`SELECT q.id, q.key, q.topic, q.prompt, COUNT(a.id), COUNT(a.id) - SUM(a.correct),
			(SELECT correct FROM attempts WHERE question_id = q.id ORDER BY id DESC LIMIT 1)
		FROM questions q JOIN attempts a ON a.question_id = q.id
		GROUP BY q.id HAVING COUNT(a.id) - SUM(a.correct) > 0
		ORDER BY 6 DESC, CAST(SUM(a.correct) AS REAL) / COUNT(a.id) ASC LIMIT 20`)
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var m MissedStat
		if err := rows.Scan(&m.ID, &m.Key, &m.Topic, &m.Prompt, &m.Attempts, &m.Wrong, &m.LastOK); err != nil {
			rows.Close()
			return st, err
		}
		st.MostMissed = append(st.MostMissed, m)
	}
	rows.Close()

	rows, err = s.db.Query(`SELECT date(answered_at, 'localtime') AS day, COUNT(*), SUM(correct)
		FROM attempts GROUP BY day ORDER BY day DESC LIMIT 30`)
	if err != nil {
		return st, err
	}
	defer rows.Close()
	for rows.Next() {
		var d DayStat
		if err := rows.Scan(&d.Day, &d.Attempts, &d.Correct); err != nil {
			return st, err
		}
		st.Daily = append(st.Daily, d)
	}
	return st, rows.Err()
}

// streaks walks attempts in order to find the current and best runs of
// consecutive correct answers.
func (s *Store) streaks() (current, best int, err error) {
	rows, err := s.db.Query(`SELECT correct FROM attempts ORDER BY id`)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var ok bool
		if err := rows.Scan(&ok); err != nil {
			return 0, 0, err
		}
		if ok {
			current++
			best = max(best, current)
		} else {
			current = 0
		}
	}
	return current, best, rows.Err()
}

// History returns a question's attempts, newest first.
func (s *Store) History(questionID int64) ([]Attempt, error) {
	rows, err := s.db.Query(`SELECT answer, correct, answered_at FROM attempts
		WHERE question_id = ? ORDER BY id DESC`, questionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Attempt{}
	for rows.Next() {
		var a Attempt
		if err := rows.Scan(&a.Answer, &a.Correct, &a.AnsweredAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
