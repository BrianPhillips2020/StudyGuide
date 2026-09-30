// Package bank parses question banks (the course PDFs, or JSON) into
// store.Questions, flagging items it cannot parse cleanly instead of guessing.
package bank

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ledongthuc/pdf"

	"studyguide/internal/store"
)

// Issue describes a problem found during parsing. Skipped items are not
// imported; the rest are imported with a warning.
type Issue struct {
	Source  string `json:"source"`
	Item    string `json:"item"`
	Message string `json:"message"`
	Skipped bool   `json:"skipped"`
}

// line is one row of PDF text with the page it came from, or, when image is
// set, an image drawn at that point in reading order.
type line struct {
	text  string
	page  int
	y     float64
	image string // imageRef of an image draw; text is empty
}

var (
	reModule   = regexp.MustCompile(`^Module\s+(\d+)\s+Question Pool`)
	reLesson   = regexp.MustCompile(`^Lesson\s+\d+:\s*(.+)$`)
	reQuestion = regexp.MustCompile(`^Q(\d+)\.\s*\[(\w+)\]`)
	reMCQ      = regexp.MustCompile(`^•\s*([A-Z])\.\s+(.*)$`)
	reTF       = regexp.MustCompile(`^•\s*(True|False)\s*$`)
	reAnswer   = regexp.MustCompile(`^Correct answer:\s*(.*)$`)
	reWhy      = regexp.MustCompile(`^Why:\s*(.*)$`)
	reFigure   = regexp.MustCompile(`^Figure:\s*(.*)$`)

	reModuleSuffix = regexp.MustCompile(`\s*\(Module \d+\)\s*$`)
)

// ParsePDF reads a "Module N Question Pool" PDF, including the figure images.
// A figure whose image can't be found is imported with its caption only and
// a warning.
func ParsePDF(path string) ([]store.Question, []Issue, error) {
	source := filepath.Base(path)
	lines, issues, err := readPDFLines(path)
	if err != nil {
		return nil, nil, err
	}
	qs, more := parseLines(lines, source)
	issues = append(issues, more...)

	images, err := extractImages(path)
	if err != nil {
		issues = append(issues, Issue{Source: source, Message: fmt.Sprintf("figures not imported: %v", err)})
		images = nil
	}
	for i := range qs {
		q := &qs[i]
		if q.Figure == "" {
			continue
		}
		if img := images[q.ImageRef]; img != nil {
			q.Image = img
		} else if err == nil {
			issues = append(issues, Issue{Source: source, Item: q.Key,
				Message: "figure image not found in the PDF (caption imported only)"})
		}
	}
	return qs, issues, nil
}

func readPDFLines(path string) ([]line, []Issue, error) {
	source := filepath.Base(path)
	f, r, err := pdf.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open %s: %w", source, err)
	}
	defer f.Close()

	var out []line
	var issues []Issue
	for p := 1; p <= r.NumPage(); p++ {
		page := r.Page(p)
		if page.V.IsNull() {
			continue
		}
		rows, err := page.GetTextByRow()
		if err != nil {
			return nil, nil, fmt.Errorf("%s page %d: %w", source, p, err)
		}
		var pageLines []line
		for _, row := range rows {
			var b strings.Builder
			for _, w := range row.Content {
				b.WriteString(w.S)
			}
			pageLines = append(pageLines, line{text: strings.TrimSpace(b.String()), page: p, y: float64(row.Position)})
		}
		imgs, err := pageImages(page, p)
		if err != nil {
			issues = append(issues, Issue{Source: source, Message: err.Error()})
		}
		out = append(out, withImages(pageLines, imgs)...)
	}
	return out, issues, nil
}

// draft is a question being assembled line by line.
type draft struct {
	num, kind      string
	page           int
	prompt, why    []string
	choices        []store.Choice
	answer, figure string
	imageRef       string
	sawAnswer      bool
	topic          string
}

// parseLines runs a small state machine over the text rows. The PDFs have no
// font information, so topic headings are recognized by position: a line
// after a blank line, not ending in a period or comma, directly followed
// by a "Qn. [TYPE]" marker.
//
// Each figure image is drawn just above its "Figure:" caption, sometimes at
// the bottom of the previous page, so a caption takes the most recent image
// that no caption has claimed yet.
func parseLines(lines []line, source string) ([]store.Question, []Issue) {
	var (
		drafts  []*draft
		issues  []Issue
		module  string
		lesson  string
		topic   string
		cur     *draft
		section string // which part of cur the next wrapped line continues
		image   string // most recent unclaimed image draw
	)

	finish := func() {
		if cur != nil {
			drafts = append(drafts, cur)
		}
		cur = nil
	}

	for i, l := range lines {
		if l.image != "" {
			image = l.image
			continue
		}
		t := l.text
		if t == "" {
			continue
		}
		if m := reModule.FindStringSubmatch(t); m != nil && cur == nil {
			module = m[1]
			continue
		}
		if m := reLesson.FindStringSubmatch(t); m != nil && cur == nil {
			lesson = strings.TrimSpace(m[1])
			continue
		}
		if m := reQuestion.FindStringSubmatch(t); m != nil {
			finish()
			cur = &draft{num: m[1], kind: m[2], page: l.page, topic: topic}
			section = "prompt"
			continue
		}
		if isHeading(lines, i) {
			finish()
			topic = t
			continue
		}
		if cur == nil {
			continue // title lines such as "OMSCS 6250 Computer Networks"
		}

		switch m := matchAny(t); {
		case m.kind == "figure":
			caption := reModuleSuffix.ReplaceAllString(m.val, "")
			cur.figure = fmt.Sprintf("%s (%s, page %d)", caption, source, l.page)
			cur.imageRef, image = image, ""
		case m.kind == "answer":
			cur.answer, cur.sawAnswer = m.val, true
			section = ""
		case m.kind == "why":
			cur.why = append(cur.why, m.val)
			section = "why"
		case m.kind == "choice" && section != "why":
			cur.choices = append(cur.choices, store.Choice{Label: m.label, Text: m.val})
			section = "choice"
		default:
			switch section {
			case "prompt":
				cur.prompt = append(cur.prompt, t)
			case "choice":
				c := &cur.choices[len(cur.choices)-1]
				c.Text = joinWrapped(c.Text, t)
			case "why":
				cur.why = append(cur.why, t)
			default:
				issues = append(issues, Issue{Source: source, Item: "Q" + cur.num,
					Message: fmt.Sprintf("unexpected text on page %d: %q", l.page, t)})
			}
		}
	}
	finish()

	if module == "" {
		issues = append(issues, Issue{Source: source, Message: `no "Module N Question Pool" title found; is this a question pool PDF?`, Skipped: true})
		return nil, issues
	}

	// Prompts and explanations are set in different font sizes, so each wraps
	// at its own width.
	var promptWrap, whyWrap int
	for _, d := range drafts {
		promptWrap = max(promptWrap, longest(d.prompt))
		whyWrap = max(whyWrap, longest(d.why))
	}
	var qs []store.Question
	for _, d := range drafts {
		q, errs, skip := d.build(module, lesson, source, promptWrap, whyWrap)
		for _, e := range errs {
			issues = append(issues, Issue{Source: source, Item: "Q" + d.num, Message: e, Skipped: skip})
		}
		if !skip {
			qs = append(qs, q)
		}
	}
	return qs, issues
}

type match struct{ kind, label, val string }

func matchAny(t string) match {
	if m := reFigure.FindStringSubmatch(t); m != nil {
		return match{kind: "figure", val: m[1]}
	}
	if m := reAnswer.FindStringSubmatch(t); m != nil {
		return match{kind: "answer", val: strings.TrimSpace(m[1])}
	}
	if m := reWhy.FindStringSubmatch(t); m != nil {
		return match{kind: "why", val: m[1]}
	}
	if m := reTF.FindStringSubmatch(t); m != nil {
		return match{kind: "choice", label: m[1], val: m[1]}
	}
	if m := reMCQ.FindStringSubmatch(t); m != nil {
		return match{kind: "choice", label: m[1], val: m[2]}
	}
	return match{}
}

func isHeading(lines []line, i int) bool {
	t := lines[i].text
	// Headings can end in "?" ("What's Inside a Router?"), so only other
	// sentence punctuation rules a line out.
	if strings.ContainsAny(t[len(t)-1:], ".!,;") {
		return false
	}
	if i == 0 || lines[i-1].text != "" {
		return false
	}
	for _, next := range lines[i+1:] {
		if next.text != "" {
			return reQuestion.MatchString(next.text)
		}
	}
	return false
}

// longest returns the length of the longest line, which approximates the
// width at which the PDF wrapped that kind of text.
func longest(ls []string) int {
	w := 0
	for _, l := range ls {
		w = max(w, len(l))
	}
	return w
}

func (d *draft) build(module, lesson, source string, promptWrap, whyWrap int) (store.Question, []string, bool) {
	var errs []string
	q := store.Question{
		Key:         fmt.Sprintf("M%s-Q%s", module, d.num),
		Module:      fmt.Sprintf("Module %s: %s", module, lesson),
		Topic:       d.topic,
		Kind:        d.kind,
		Prompt:      paragraphs(d.prompt, promptWrap),
		Choices:     d.choices,
		Answer:      d.answer,
		Explanation: paragraphs(d.why, whyWrap),
		Figure:      d.figure,
		ImageRef:    d.imageRef,
		Source:      fmt.Sprintf("%s, page %d", source, d.page),
	}
	if q.Topic == "" {
		q.Topic = "General"
	}
	skip := false
	if q.Kind != "MCQ" && q.Kind != "TF" {
		errs, skip = append(errs, fmt.Sprintf("unknown question type [%s]", q.Kind)), true
	}
	if q.Prompt == "" {
		errs, skip = append(errs, "no question text"), true
	}
	if len(q.Choices) < 2 {
		errs, skip = append(errs, fmt.Sprintf("found %d answer choices, need at least 2", len(q.Choices))), true
	}
	if !d.sawAnswer || q.Answer == "" {
		errs, skip = append(errs, "no correct answer given"), true
	} else if !hasLabel(q.Choices, q.Answer) {
		errs, skip = append(errs, fmt.Sprintf("correct answer %q is not one of the choices", q.Answer)), true
	}
	if q.Explanation == "" {
		errs = append(errs, "no explanation (imported anyway)")
	}
	return q, errs, skip
}

func hasLabel(cs []store.Choice, label string) bool {
	for _, c := range cs {
		if c.Label == label {
			return true
		}
	}
	return false
}

// paragraphs rejoins wrapped PDF lines. A line that ends in sentence
// punctuation well short of the wrap width ends a paragraph.
func paragraphs(ls []string, wrap int) string {
	var b strings.Builder
	for i, l := range ls {
		if i > 0 {
			prev := ls[i-1]
			if strings.ContainsAny(prev[len(prev)-1:], ".?!:") && len(prev) < wrap*85/100 {
				b.WriteString("\n")
			} else if !strings.HasSuffix(prev, "-") {
				b.WriteString(" ")
			}
		}
		b.WriteString(l)
	}
	return strings.TrimSpace(b.String())
}

func joinWrapped(a, b string) string {
	if strings.HasSuffix(a, "-") {
		return a + b
	}
	return a + " " + b
}
