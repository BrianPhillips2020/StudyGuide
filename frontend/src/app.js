// Go methods bound by Wails (see app.go). No bundler or generated bindings
// are needed; the runtime injects window.go before this script runs.
const api = () => window.go.main.App;

const $ = (sel) => document.querySelector(sel);
const esc = (s) => String(s ?? "").replace(/[&<>"']/g, (c) =>
  ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
const pct = (correct, total) => (total ? Math.round((100 * correct) / total) : 0);
const bar = (correct, total) => total
  ? `<div class="bar"><div class="track"><div class="fill" style="width:${pct(correct, total)}%"></div></div><span class="pct">${pct(correct, total)}%</span></div>`
  : `<span class="muted">not seen</span>`;
const when = (iso) => new Date(iso).toLocaleString();

// Figure images are served by the Go figureHandler at /figures/{questionID}.
// Without an image (e.g. a bank imported before figures were supported),
// fall back to pointing at the PDF page.
function figureHTML(id, hasImage, caption) {
  if (!hasImage) {
    return `<div class="figure">This question refers to a figure: ${esc(caption)}. Reimport the PDFs to show it here.</div>`;
  }
  return `<img src="/figures/${id}" alt="${esc(caption)}" title="Click to enlarge">` +
    `<figcaption>${esc(caption)}</figcaption>`;
}

// Click any figure to view it full size; click again or press Esc to close.
document.addEventListener("click", (e) => {
  if (e.target.matches(".figure-img img")) {
    $("#zoom-img").src = e.target.src;
    $("#zoom").showModal();
  }
});
$("#zoom").addEventListener("click", () => $("#zoom").close());

// Fisher–Yates shuffle, in place.
function shuffle(a) {
  for (let i = a.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1));
    [a[i], a[j]] = [a[j], a[i]];
  }
  return a;
}

function showError(err) {
  alert(String(err?.message ?? err));
}

/* ---------- Tabs ---------- */

document.querySelectorAll(".tab").forEach((tab) => {
  tab.addEventListener("click", () => showView(tab.dataset.view));
});

function showView(name) {
  document.querySelectorAll(".tab").forEach((t) => t.classList.toggle("active", t.dataset.view === name));
  document.querySelectorAll(".view").forEach((v) => v.classList.toggle("active", v.id === `view-${name}`));
  if (name === "stats") loadStats();
  if (name === "questions" && questionsStale) loadQuestionList();
  if (name === "settings") loadSettings();
}

/* ---------- Settings ---------- */

// confirmAction shows an in-app confirmation and resolves true if the user
// clicks the action button.
function confirmAction(message, action) {
  const dlg = $("#confirm");
  $("#confirm-msg").textContent = message;
  $("#confirm-ok").textContent = action;
  return new Promise((resolve) => {
    const done = (ok) => { dlg.close(); resolve(ok); };
    dlg.querySelector(".cancel").onclick = () => done(false);
    $("#confirm-ok").onclick = () => done(true);
    dlg.oncancel = () => resolve(false); // Esc
    dlg.showModal();
    dlg.querySelector(".cancel").focus(); // Enter shouldn't delete anything
  });
}

async function loadSettings() {
  const byModule = new Map();
  for (const t of topics) byModule.set(t.module, (byModule.get(t.module) ?? 0) + t.count);
  const tbody = $("#module-table tbody");
  tbody.innerHTML = [...byModule].map(([mod, n], i) =>
    `<tr><td>${esc(mod)}</td><td class="num">${n}</td>` +
    `<td class="num"><button class="danger small" data-idx="${i}">Remove</button></td></tr>`).join("") ||
    `<tr><td colspan="3" class="muted">No modules imported.</td></tr>`;
  const modules = [...byModule.keys()];
  tbody.querySelectorAll("button[data-idx]").forEach((b) => b.addEventListener("click", async () => {
    const mod = modules[b.dataset.idx];
    if (!(await confirmAction(`Remove "${mod}"? Its ${byModule.get(mod)} questions, figures and answer history will be deleted.`, "Remove module"))) return;
    try {
      await api().RemoveModule(mod);
    } catch (err) {
      return showError(err);
    }
    await afterDataChange();
  }));
}

// afterDataChange refreshes every view that depends on the bank or history.
async function afterDataChange() {
  questionsStale = true;
  await loadTopics();
  current = null;
  await nextQuestion();
  await loadSettings();
}

$("#btn-reset-stats").addEventListener("click", async () => {
  if (!(await confirmAction("Delete your entire answer history? Every attempt, streak and accuracy figure will be erased. Questions are kept.", "Reset all stats"))) return;
  try {
    await api().ResetStats();
  } catch (err) {
    return showError(err);
  }
  await afterDataChange();
});

$("#btn-hard-reset").addEventListener("click", async () => {
  if (!(await confirmAction("Delete everything? All modules, questions, figures and answer history will be erased. This cannot be undone.", "Delete everything"))) return;
  try {
    await api().HardReset();
  } catch (err) {
    return showError(err);
  }
  await afterDataChange();
});

/* ---------- Questions ---------- */

const PREVIEW_CHARS = 60;  // how much of each prompt the list shows
let questionsStale = true; // reload the list on next visit (set after an import)

async function loadQuestionList() {
  let qs;
  try {
    qs = (await api().ListQuestions()) ?? [];
  } catch (err) {
    return showError(err);
  }
  questionsStale = false;
  const box = $("#q-list");
  if (qs.length === 0) {
    box.innerHTML = `<div class="empty">No questions imported yet.</div>`;
    return;
  }

  const modules = [...new Set(qs.map((q) => q.module))];
  box.innerHTML = modules.map((mod) => {
    const list = qs.filter((q) => q.module === mod);
    const items = list.map((q) => {
      const num = q.key.match(/-(Q\d+)$/)?.[1] ?? q.key;
      const flat = q.prompt.replace(/\s+/g, " ");
      const preview = flat.length > PREVIEW_CHARS ? flat.slice(0, PREVIEW_CHARS).trimEnd() + "…" : flat;
      return `<details class="q-item" data-id="${q.id}">` +
        `<summary><span class="q-num">${esc(num)}</span><span class="q-preview">${esc(preview)}</span></summary>` +
        `<div class="q-detail muted">Loading…</div></details>`;
    }).join("");
    return `<details class="q-module"><summary>${esc(mod)} <span class="muted">(${list.length})</span></summary>${items}</details>`;
  }).join("");

  box.querySelectorAll("details.q-item").forEach((d) =>
    d.addEventListener("toggle", () => { if (d.open && !d.dataset.loaded) showQuestionDetail(d); }));
}

async function showQuestionDetail(d) {
  const body = d.querySelector(".q-detail");
  let h;
  try {
    h = await api().GetQuestionHistory(Number(d.dataset.id));
  } catch (err) {
    body.textContent = String(err?.message ?? err);
    return;
  }
  d.dataset.loaded = "1";
  const q = h.question;
  const tries = h.attempts ?? [];
  const right = tries.filter((a) => a.correct).length;
  const choices = (q.choices ?? []).map((c) =>
    `<div class="choice static ${q.kind === "TF" ? "tf" : ""}" data-label="${esc(c.label)}">` +
    `<span class="label">${esc(c.label)}</span><span>${esc(c.text)}</span></div>`).join("");

  body.className = "q-detail";
  body.innerHTML =
    `<div class="meta">${esc(q.topic)} · ${tries.length ? `answered ${tries.length}×, ${right} correct` : "not answered yet"}</div>` +
    (q.figure ? `<figure class="figure-img">${figureHTML(q.id, q.hasImage, q.figure)}</figure>` : "") +
    `<p class="prompt">${esc(q.prompt)}</p><div class="choices">${choices}</div>` +
    `<div class="reveal"><button class="show-answer">Show answer</button></div>` +
    `<div class="answer-box" hidden><p class="explanation">${esc(q.explanation)}</p></div>`;

  // One button toggles between showing and hiding the answer.
  const btn = body.querySelector(".show-answer");
  btn.addEventListener("click", () => {
    const show = body.querySelector(".answer-box").hidden;
    body.querySelectorAll(".choice").forEach((c) => c.classList.toggle("correct", show && c.dataset.label === q.answer));
    body.querySelector(".answer-box").hidden = !show;
    btn.textContent = show ? "Hide answer" : "Show answer";
  });
}

/* ---------- Study ---------- */

let topics = [];      // [{module, topic, count}]
let current = null;   // QuestionView being shown
let selected = null;  // chosen label
let answered = false;

async function loadTopics() {
  topics = (await api().GetTopics()) ?? [];
  const modSel = $("#f-module");
  const keep = modSel.value;
  const modules = [...new Set(topics.map((t) => t.module))];
  modSel.innerHTML = `<option value="">All modules</option>` +
    modules.map((m) => `<option value="${esc(m)}">${esc(m)}</option>`).join("");
  if (modules.includes(keep)) modSel.value = keep;
  fillTopics();
}

function fillTopics() {
  const mod = $("#f-module").value;
  const topSel = $("#f-topic");
  const keep = topSel.value;
  const list = topics.filter((t) => !mod || t.module === mod);
  topSel.innerHTML = `<option value="">All topics</option>` +
    list.map((t) => `<option value="${esc(t.topic)}">${esc(t.topic)} (${t.count})</option>`).join("");
  if (list.some((t) => t.topic === keep)) topSel.value = keep;
}

function filter() {
  return { mode: $("#f-mode").value, module: $("#f-module").value, topic: $("#f-topic").value };
}

async function nextQuestion() {
  try {
    current = await api().NextQuestion(filter());
  } catch (err) {
    return showError(err);
  }
  selected = null;
  answered = false;

  if (!current) {
    $("#card").hidden = true;
    $("#pool").textContent = "";
    const empty = $("#study-empty");
    empty.hidden = false;
    if (topics.length === 0) {
      empty.innerHTML = `No questions yet. <a href="#" id="go-import">Import the question bank</a> to get started.`;
      $("#go-import").onclick = (e) => { e.preventDefault(); showView("import"); };
    } else if (filter().mode === "missed") {
      empty.textContent = "Nothing missed in this selection. Every question here was answered correctly on its most recent attempt.";
    } else {
      empty.textContent = "No questions match this filter.";
    }
    return;
  }

  $("#study-empty").hidden = true;
  $("#card").hidden = false;
  $("#pool").textContent = `${current.pool} question${current.pool === 1 ? "" : "s"} in this selection`;

  const seen = current.attempts ? `answered ${current.attempts}×` : "new";
  $("#q-meta").textContent = `${current.module} · ${current.topic} · ${current.key} · ${seen}`;

  const fig = $("#q-figure");
  fig.hidden = !current.figure;
  fig.innerHTML = current.figure ? figureHTML(current.id, current.hasImage, current.figure) : "";

  $("#q-prompt").textContent = current.prompt;

  const box = $("#q-choices");
  box.innerHTML = "";
  // Multiple-choice options are shuffled every time and relabeled A, B, C…
  // in display order. c.label stays the bank's letter, which is what gets
  // graded and recorded; c.shown is the letter on screen.
  if (current.kind === "MCQ") shuffle(current.choices);
  current.choices.forEach((c, i) => {
    c.shown = current.kind === "MCQ" ? String.fromCharCode(65 + i) : c.label;
  });

  for (const c of current.choices) {
    const b = document.createElement("button");
    b.className = "choice" + (current.kind === "TF" ? " tf" : "");
    b.dataset.label = c.label;
    b.innerHTML = `<span class="label">${esc(c.shown)}</span><span>${esc(c.text)}</span>`;
    b.addEventListener("click", () => choose(c.label));
    box.appendChild(b);
  }

  $("#feedback").hidden = true;
  $("#btn-submit").hidden = false;
  $("#btn-submit").disabled = true;
  $("#btn-next").hidden = true;
}

function choose(label) {
  if (answered) return;
  selected = label;
  document.querySelectorAll(".choice").forEach((b) => b.classList.toggle("selected", b.dataset.label === label));
  $("#btn-submit").disabled = false;
}

async function submit() {
  if (!current || !selected || answered) return;
  let res;
  try {
    res = await api().SubmitAnswer(current.id, selected);
  } catch (err) {
    return showError(err);
  }
  answered = true;

  document.querySelectorAll(".choice").forEach((b) => {
    b.disabled = true;
    b.classList.remove("selected");
    if (b.dataset.label === res.answer) b.classList.add("correct");
    else if (b.dataset.label === selected) b.classList.add("wrong");
  });

  const v = $("#fb-verdict");
  v.className = "verdict " + (res.correct ? "good" : "bad");
  const correctChoice = current.choices.find((c) => c.label === res.answer);
  v.textContent = res.correct ? "Correct" : `Incorrect. The answer is ${correctChoice ? correctChoice.shown : res.answer}.`;
  $("#fb-explanation").textContent = res.explanation;

  const h = res.history ?? [];
  const right = h.filter((a) => a.correct).length;
  $("#fb-history").textContent = `You've answered this ${h.length} time${h.length === 1 ? "" : "s"}, ${right} correct.`;

  $("#feedback").hidden = false;
  $("#btn-submit").hidden = true;
  $("#btn-next").hidden = false;
  $("#btn-next").focus();
}

$("#btn-submit").addEventListener("click", submit);
$("#btn-next").addEventListener("click", nextQuestion);
$("#f-mode").addEventListener("change", nextQuestion);
$("#f-module").addEventListener("change", () => { fillTopics(); nextQuestion(); });
$("#f-topic").addEventListener("change", nextQuestion);

document.addEventListener("keydown", (e) => {
  if (!$("#view-study").classList.contains("active") || !current) return;
  if (e.target.tagName === "SELECT" || $("#history").open) return;
  const k = e.key.toUpperCase();
  if (k === "ENTER") {
    e.preventDefault();
    answered ? nextQuestion() : submit();
    return;
  }
  if (answered) return;
  if (current.kind === "TF" && (k === "T" || k === "F")) choose(k === "T" ? "True" : "False");
  else {
    const c = current.choices.find((c) => c.shown === k);
    if (c) choose(c.label);
  }
});

/* ---------- Stats ---------- */

const expandedModules = new Set();

async function loadStats() {
  let s;
  try {
    s = await api().GetStats();
  } catch (err) {
    return showError(err);
  }

  $("#tiles").innerHTML = [
    [s.attempts ? `${pct(s.correct, s.attempts)}%` : "–", "Overall accuracy"],
    [s.attempts, "Answers given"],
    [`${s.seenQuestions}/${s.totalQuestions}`, "Questions seen"],
    [s.missedNow, "Currently missed"],
    [`${s.currentStreak}`, `Current streak (best ${s.bestStreak})`],
  ].map(([v, l]) => `<div class="tile"><div class="value">${esc(v)}</div><div class="label">${esc(l)}</div></div>`).join("");

  // Each module is a collapsible row with module totals; its topics show
  // when it is expanded. Expanded modules stay open across reloads.
  const rows = [];
  const modules = [...new Set((s.topics ?? []).map((t) => t.module))];
  modules.forEach((mod, i) => {
    const all = s.topics.filter((x) => x.module === mod);
    const sum = (f) => all.reduce((n, x) => n + x[f], 0);
    const open = expandedModules.has(mod);
    rows.push(`<tr class="module${open ? " open" : ""}" data-idx="${i}">` +
      `<td><span class="caret">▸</span>${esc(mod)}</td><td class="num">${sum("seen")}/${sum("questions")}</td>` +
      `<td class="num">${sum("attempts")}</td><td>${bar(sum("correct"), sum("attempts"))}</td></tr>`);
    for (const t of all) {
      rows.push(`<tr class="topic-row" data-idx="${i}"${open ? "" : " hidden"}><td class="indent">${esc(t.topic)}</td>` +
        `<td class="num">${t.seen}/${t.questions}</td>` +
        `<td class="num">${t.attempts}</td><td>${bar(t.correct, t.attempts)}</td></tr>`);
    }
  });
  const tbody = $("#topic-table tbody");
  tbody.innerHTML = rows.join("") ||
    `<tr><td colspan="4" class="muted">No questions imported yet.</td></tr>`;
  tbody.querySelectorAll("tr.module").forEach((tr) => tr.addEventListener("click", () => {
    const mod = modules[tr.dataset.idx];
    const open = !expandedModules.has(mod);
    open ? expandedModules.add(mod) : expandedModules.delete(mod);
    tr.classList.toggle("open", open);
    tbody.querySelectorAll(`tr.topic-row[data-idx="${tr.dataset.idx}"]`).forEach((r) => (r.hidden = !open));
  }));

  const missed = s.mostMissed ?? [];
  $("#missed-table tbody").innerHTML = missed.map((m) =>
    `<tr data-id="${m.id}"><td class="prompt-cell"><strong>${esc(m.key)}</strong> ${esc(m.prompt.slice(0, 110))}${m.prompt.length > 110 ? "…" : ""}</td>` +
    `<td>${esc(m.topic)}</td><td class="num">${m.wrong}</td><td class="num">${m.attempts}</td>` +
    `<td>${m.lastOk ? '<span class="ok">right</span>' : '<span class="no">wrong</span>'}</td></tr>`).join("") ||
    `<tr><td colspan="5" class="muted">No missed questions yet.</td></tr>`;
  document.querySelectorAll("#missed-table tbody tr[data-id]").forEach((tr) =>
    tr.addEventListener("click", () => openHistory(Number(tr.dataset.id))));

  $("#daily-table tbody").innerHTML = (s.daily ?? []).map((d) =>
    `<tr><td>${esc(d.day)}</td><td class="num">${d.attempts}</td><td>${bar(d.correct, d.attempts)}</td></tr>`).join("") ||
    `<tr><td colspan="3" class="muted">No answers yet.</td></tr>`;
}

async function openHistory(id) {
  let h;
  try {
    h = await api().GetQuestionHistory(id);
  } catch (err) {
    return showError(err);
  }
  const q = h.question;
  $("#h-title").textContent = `${q.key} · ${q.topic}`;
  // Choices are shuffled and relabeled while studying, so the bank's letters
  // mean nothing to the user here; show answer text only.
  const choices = (q.choices ?? []).map((c) =>
    `<div class="choice tf ${c.label === q.answer ? "correct" : ""}"><span>${esc(c.text)}</span></div>`).join("");
  const answerText = (label) => (q.choices ?? []).find((c) => c.label === label)?.text ?? label;
  const attempts = (h.attempts ?? []).map((a) =>
    `<tr><td>${esc(when(a.answeredAt))}</td><td>${esc(answerText(a.answer))}</td>` +
    `<td>${a.correct ? '<span class="ok">right</span>' : '<span class="no">wrong</span>'}</td></tr>`).join("");
  $("#h-body").innerHTML =
    (q.figure ? `<figure class="figure-img">${figureHTML(q.id, q.hasImage, q.figure)}</figure>` : "") +
    `<p class="prompt">${esc(q.prompt)}</p><div class="choices">${choices}</div>` +
    `<p class="explanation" style="margin-top:14px">${esc(q.explanation)}</p>` +
    `<h2>Attempts</h2><table class="grid"><thead><tr><th>When</th><th>Answer</th><th>Result</th></tr></thead>` +
    `<tbody>${attempts || '<tr><td colspan="3" class="muted">Never answered.</td></tr>'}</tbody></table>`;
  $("#history").showModal();
}

$("#h-close").addEventListener("click", () => $("#history").close());

/* ---------- Import ---------- */

$("#btn-import").addEventListener("click", async () => {
  let r;
  try {
    r = await api().ImportBank();
  } catch (err) {
    return showError(err);
  }
  if (!r) return; // picker cancelled

  const skipped = r.issues.filter((i) => i.skipped).length;
  const issues = r.issues.map((i) =>
    `<tr><td>${esc(i.source)}</td><td>${esc(i.item)}</td><td>${esc(i.message)}</td><td>${i.skipped ? "skipped" : "warning"}</td></tr>`).join("");
  $("#import-result").innerHTML =
    `<div class="summary">Imported ${r.added} new and updated ${r.updated} existing question${r.updated === 1 ? "" : "s"}` +
    ` from ${(r.files ?? []).length} file${(r.files ?? []).length === 1 ? "" : "s"}.` +
    (skipped ? ` ${skipped} skipped.` : "") + `</div>` +
    (issues
      ? `<table class="grid"><thead><tr><th>File</th><th>Item</th><th>Problem</th><th></th></tr></thead><tbody>${issues}</tbody></table>`
      : `<p class="muted">No problems found.</p>`);

  questionsStale = true;
  await loadTopics();
  if (!current) nextQuestion();
});

/* ---------- Start ---------- */

(async () => {
  try {
    const info = await api().Info();
    $("#db-path").textContent = info.dbPath;
    await loadTopics();
  } catch (err) {
    showError(err);
  }
  nextQuestion();
})();
