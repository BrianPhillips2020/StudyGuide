# StudyGuide

A native desktop app for drilling the OMSCS 6250 question pools and tracking how you do. Built with Go and Wails v2, and stores its data in a local SQLite database. It runs without a server, an account, or a network connection. See [Studyguideplan.md.md](Studyguideplan.md.md) for the design.

## Prerequisites

- **Go 1.26+**. `modernc.org/sqlite` requires it. An older Go still works because it downloads the newer toolchain automatically, but it's cleaner to install it.
- **Wails v2 CLI**: `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- **WebView2**, which ships with Windows 11.

Node.js is **not** needed. The frontend is plain HTML, CSS, and JS with no build step, and it calls Go through `window.go.main.App`.

## Run

```bash
wails dev      # run with live reload
wails build    # produces build/bin/studyguide.exe
go test ./...  # parser, store, quiz, and end-to-end tests
```

If `wails build` complains about npm, add `-s` to skip the (empty) frontend build step.

## Using it

1. Open the **Import** tab and select the `questionPools/Module N Question Pool.pdf` files (you can select several at once).
2. Answer questions in the **Study** tab. You can filter by All / Missed only, module, and topic. Keyboard shortcuts: `A`–`D` or `T`/`F` choose an answer, and `Enter` submits or moves to the next question.
3. The **Stats** tab shows overall accuracy, accuracy by module and topic, the most-missed questions (click one to see its full history), and accuracy by day.

"Missed" means your **most recent** answer to that question was wrong. Answering it correctly takes it off the missed list.

## How import works

- The PDFs are parsed directly. Topics come from the section headings, and each question is keyed by module and number (`M1-Q9`), so importing again updates the text but keeps your history.
- Items with a missing answer, too few choices, or an answer that isn't one of the choices are listed and skipped, not guessed at. The current pools (226 questions) import with no issues.
- JSON banks in the format from the plan are also accepted. Optional `id`, `module`, and `kind` fields are supported.
- Figures are not extracted. A question that refers to a figure shows its caption along with the PDF file and page to look at.

## Layout

```
main.go              Wails entry point, embeds frontend/src
app.go               App struct: NextQuestion, SubmitAnswer, GetStats, ImportBank, …
internal/store/      SQLite schema, queries, stats
internal/quiz/       question selection and grading
internal/bank/       PDF and JSON parsers
frontend/src/        index.html, style.css, app.js
```

The database is at `%AppData%\StudyGuide\studyguide.db` (it's under `os.UserConfigDir()` on each OS).

### AI USE DISCLAIMER
This entire project was generated with Claude Opus 5.5. No data is collected from this apps usage.
