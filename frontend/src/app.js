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
  for (const c of current.choices) {
    const b = document.createElement("button");
    b.className = "choice" + (current.kind === "TF" ? " tf" : "");
    b.dataset.label = c.label;
    b.innerHTML = `<span class="label">${esc(c.label)}</span><span>${esc(c.text)}</span>`;
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
  v.textContent = res.correct ? "Correct" : `Incorrect. The answer is ${res.answer}.`;
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
  else if (current.choices.some((c) => c.label === k)) choose(k);
});

/* ---------- Stats ---------- */

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

  // Topics grouped under a module row with module totals.
  const rows = [];
  let mod = null;
  for (const t of s.topics ?? []) {
    if (t.module !== mod) {
      mod = t.module;
      const all = s.topics.filter((x) => x.module === mod);
      const sum = (f) => all.reduce((n, x) => n + x[f], 0);
      rows.push(`<tr class="module"><td>${esc(mod)}</td><td class="num">${sum("seen")}/${sum("questions")}</td>` +
        `<td class="num">${sum("attempts")}</td><td>${bar(sum("correct"), sum("attempts"))}</td></tr>`);
    }
    rows.push(`<tr><td>${esc(t.topic)}</td><td class="num">${t.seen}/${t.questions}</td>` +
      `<td class="num">${t.attempts}</td><td>${bar(t.correct, t.attempts)}</td></tr>`);
  }
  $("#topic-table tbody").innerHTML = rows.join("") ||
    `<tr><td colspan="4" class="muted">No questions imported yet.</td></tr>`;

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
  const choices = (q.choices ?? []).map((c) =>
    `<div class="choice ${c.label === q.answer ? "correct" : ""} ${q.kind === "TF" ? "tf" : ""}">` +
    `<span class="label">${esc(c.label)}</span><span>${esc(c.text)}</span></div>`).join("");
  const attempts = (h.attempts ?? []).map((a) =>
    `<tr><td>${esc(when(a.answeredAt))}</td><td>${esc(a.answer)}</td>` +
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
