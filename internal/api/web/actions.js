// --- Ask your AI (rate / tailor CV / draft email) ------------------------------
let askState = null;

async function askAI(kind, id, model) {
  const notesEl = document.getElementById(`draftNotes-${id}`);
  let req;
  try {
    if (kind === "rate") showToast(t("Rating…"), "info");
    req = await APIClient.askRequest(kind, id, kind === "draft" ? { notes: notesEl ? notesEl.value : "" } : kind === "rate" ? { model } : {});
  } catch (e) {
    if (e.code === "profile_empty") {
      showToast(t(e.message), "info");
      openProfile();
      return;
    }
    showToast(t(e.message), "error");
    return;
  }
  if (req.rated) {
    // A Jev challenger leaves the canonical badge unchanged; report the
    // actual model verdict returned by this request.
    const verdict = req.job.ratings?.[req.model];
    const score = Number(verdict?.score ?? req.job.score).toLocaleString(I18N.lang, { minimumFractionDigits: 1, maximumFractionDigits: 1 });
    showToast(t("Rated {letter} {score}", { letter: verdict?.letter ?? req.job.score_letter, score }), "success");
    refreshAppState();
    return;
  }
  askState = { kind, id, req };
  renderAskConnect();
  const titles = { rate: "Rate this job with your AI", cv: "Tailor your CV with your AI", draft: "Draft the email with your AI" };
  document.getElementById("askTitle").textContent = t(titles[kind]);
  document.getElementById("askIntro").innerHTML = t("braveduck doesn't run an AI itself. In <strong>Claude Code</strong> (with braveduck connected), paste this:");
  document.getElementById("askPrompt").textContent = req.prompt;
  const paste = document.getElementById("askPasteBlock");
  paste.style.display = kind === "rate" ? "none" : "block";
  document.getElementById("askPaste").value = "";
  document.getElementById("askPaste").placeholder = kind === "cv"
    ? t("…or paste the CV your AI wrote (Markdown) here")
    : t("…or paste the email here: first line \"Subject: …\", then a blank line, then the body");
  document.getElementById("askModalOverlay").classList.add("open");
}

async function copyText(text) {
  try {
    await navigator.clipboard.writeText(text);
    showToast(t("Copied."), "success");
  } catch {
    showToast(t("Couldn't copy — select the text and copy it yourself."), "error");
  }
}

async function saveAskPaste() {
  const text = document.getElementById("askPaste").value.trim();
  if (!text || !askState) return;
  try {
    if (askState.kind === "cv") {
      await APIClient.put(`jobs/${askState.id}/cv`, { markdown: text });
    } else {
      const [first, ...rest] = text.split("\n");
      const m = first.match(/^\s*Subject:\s*(.*)$/i);
      await APIClient.put(`jobs/${askState.id}/draft`, m
        ? { subject: m[1].trim(), body: rest.join("\n").trim() }
        : { subject: "", body: text });
    }
    document.getElementById("askModalOverlay").classList.remove("open");
    showToast(t(askState.kind === "cv" ? "CV saved." : "Draft saved for review."), "success");
    refreshAppState();
  } catch (e) {
    showToast(t("Couldn't save: {error}", { error: t(e.message) }), "error");
  }
}

// --- Get started + profile -------------------------------------------------------
// The steps ratings and crawls need. Each opens the window that does it.
const SETUP_STEPS = [
  { key: "cv", title: "Upload your CV", hint: "Its text becomes the \"About me\" part of your profile.", action: "Upload CV", open: () => document.getElementById("myCVBtn").click() },
  { key: "wants", title: "Say what you're looking for", hint: "The jobs you want, your minimum pay, where you can work, your deal-breakers.", action: "Answer a few questions", open: () => openProfile() },
  { key: "search_queries", title: "Add search terms", hint: "What to look for on job sites, e.g. \"n8n\".", action: "Add terms", open: () => document.getElementById("keywordsBtn").click() },
];

async function renderSetup() {
  const card = document.getElementById("setupCard");
  const res = await apiFetch(`${API_BASE}/setup`);
  if (!res.ok) return;
  const st = await res.json();
  const hadCV = AppState.setup && AppState.setup.cv;
  AppState.setup = st;
  renderHostedUser(st.hosted);
  // The Apply buttons depend on having a CV: re-render only when that changes
  // (not on every first load, which raced tests holding card elements).
  if (Boolean(st.cv) !== Boolean(hadCV)) renderBoard();
  // "About me" written by hand counts as the CV step too.
  const done = { cv: st.cv || st.about_me, wants: st.wants, search_queries: st.search_queries > 0 };
  card.hidden = st.done;
  if (st.done) return;
  // Only what's left: a finished step has nothing to say.
  const left = SETUP_STEPS.filter(step => !done[step.key]);
  document.getElementById("setupLeft").textContent = t(left.length === 1 ? "{n} step left" : "{n} steps left", { n: left.length });
  document.getElementById("setupSteps").innerHTML = left.map((step, i) => `
    <li class="setup-step">
      <span class="setup-step-title">${i + 1}. ${esc(t(step.title))}</span>
      <button class="${i === 0 ? "btn-primary" : "btn-secondary"} btn-sm" data-setup="${esc(step.key)}" title="${esc(t(step.hint))}">${esc(t(step.action))}</button>
    </li>`).join("");
  card.querySelectorAll("button[data-setup]").forEach(btn => {
    btn.addEventListener("click", () => SETUP_STEPS.find(s => s.key === btn.dataset.setup).open());
  });
}

// On a shared server the agent reaches this board's MCP over HTTPS with a
// key made in the "Connect your AI agent" window (shown once; the server
// keeps only its hash). Ask your AI points there; so does ⚙ Settings, so a
// new user with an empty board can connect too.
function renderAskConnect() {
  document.getElementById("askConnect").hidden = !(AppState.setup && AppState.setup.hosted);
}

function openAgentModal() {
  document.getElementById("askModalOverlay").classList.remove("open");
  document.getElementById("agentModalOverlay").classList.add("open");
  renderAgentConnect();
}

async function renderAgentConnect() {
  document.getElementById("agentConnectCmd").hidden = true;
  try {
    const st = await APIClient.get("agent-key");
    document.getElementById("agentKeyRevoke").hidden = !st.exists;
    document.getElementById("agentKeyNew").textContent = st.exists ? t("Make a new key (the old one stops working)") : t("Make a key for your agent");
    document.getElementById("agentConnectText").textContent = st.exists
      ? t("Your agent has a key. Make a new one if you lost it.")
      : t("Make a key, then run the command it shows once in a terminal.");
  } catch {}
}

async function newAgentKey() {
  let res;
  try {
    res = await APIClient.post("agent-key");
  } catch (e) {
    showToast(t(e.message), "error");
    return;
  }
  const cmd = document.getElementById("agentConnectCmd");
  cmd.textContent = `claude mcp add --transport http braveduck ${location.origin}/mcp \\\n  --header "Authorization: Bearer ${res.key}"`;
  cmd.hidden = false;
  document.getElementById("agentConnectText").textContent = t("Run this once in a terminal. The key is shown only now — copy it:");
  document.getElementById("agentKeyRevoke").hidden = false;
}

async function revokeAgentKey() {
  try {
    await APIClient.del("agent-key");
  } catch (e) {
    showToast(t(e.message), "error");
    return;
  }
  showToast(t("The agent key is removed"), "success");
  renderAgentConnect();
}

// On a shared server: who is logged in, and the login service's log-out page.
function renderHostedUser(hosted) {
  const el = document.getElementById("hostedUser");
  el.hidden = !hosted;
  document.getElementById("agentBtn").hidden = !hosted;
  if (!hosted) return;
  el.innerHTML = `${esc(hosted.user)}${hosted.logout_url ? `<a href="${safeUrl(hosted.logout_url)}">${esc(t("Log out"))}</a>` : ""}`;
}


// --- My CV (the base CV) -------------------------------------------------------
// Picking or dropping a file uploads it: there is no second button.
async function renderMyCV() {
  const status = document.getElementById("cvStatus");
  const choose = document.getElementById("cvChooseBtn");
  const res = await apiFetch(`${API_BASE}/cv`);
  const out = await res.json().catch(() => ({}));
  if (!res.ok) {
    status.textContent = t("Couldn't load your CV: {error}", { error: t(out.error || String(res.status)) });
    return;
  }
  choose.textContent = t(out.cv ? "Replace it" : "Choose file");
  if (!out.cv) {
    status.textContent = t("Add your CV and we'll fill in your profile from it.");
    return;
  }
  status.replaceChildren(
    el("span", { class: "cv-line" }, t("✓ {name}, added {time}", { name: out.cv.name, time: agoWords(out.cv.uploaded_at) })),
    el("button", { type: "button", class: "btn-link", onclick: downloadMyCV }, t("Download")));
}

function wireCVUpload(overlay) {
  const input = document.getElementById("cvFileInput");
  const drop = document.getElementById("cvDrop");
  const hasFile = e => [...e.dataTransfer.types].includes("Files");
  document.getElementById("cvChooseBtn").addEventListener("click", () => input.click());
  input.addEventListener("change", () => {
    const file = input.files[0];
    input.value = ""; // so picking the same file again still counts
    if (file) uploadMyCV(file);
  });
  // The whole window takes the drop: a near miss must not open the file in this tab.
  overlay.addEventListener("dragover", e => {
    if (!hasFile(e)) return;
    e.preventDefault();
    drop.classList.add("over");
  });
  overlay.addEventListener("dragleave", e => {
    if (!overlay.contains(e.relatedTarget)) drop.classList.remove("over");
  });
  overlay.addEventListener("drop", e => {
    if (!hasFile(e)) return;
    e.preventDefault();
    drop.classList.remove("over");
    if (e.dataTransfer.files[0]) uploadMyCV(e.dataTransfer.files[0]);
  });
}

async function uploadMyCV(file) {
  const btn = document.getElementById("cvChooseBtn");
  const result = document.getElementById("cvResult");
  if (btn.disabled) return;
  const send = async replace => {
    const form = new FormData();
    form.append("file", file);
    if (replace) form.append("replace", "true");
    const res = await apiFetch(`${API_BASE}/cv`, { method: "POST", body: form });
    return [res, await res.json().catch(() => ({}))];
  };
  const fail = message => {
    result.textContent = `❌ ${message}`;
    result.hidden = false;
  };
  const label = btn.textContent;
  let uploaded = false;
  result.hidden = true;
  btn.disabled = true;
  btn.textContent = t("Uploading…");
  try {
    let [res, out] = await send(false);
    if (res.status === 409) {
      if (!confirm(`${t(out.error)}\n\n${t("OK replaces it with the text of {file}.", { file: file.name })}`)) return;
      [res, out] = await send(true);
    }
    if (!res.ok) {
      fail(t((out.error || `HTTP ${res.status}`).replace(/^invalid input: /, "")));
      return;
    }
    uploaded = true;
    showToast(t("CV added. Your profile is updated."), "success");
  } catch (e) {
    fail(e.message);
  } finally {
    btn.disabled = false;
    btn.textContent = label;
  }
  if (uploaded) {
    renderMyCV();
    renderSetup();
  }
}

async function downloadMyCV() {
  const res = await apiFetch(`${API_BASE}/cv/file`);
  if (!res.ok) {
    const err = await res.json().catch(() => ({}));
    showToast(t(err.error || `HTTP ${res.status}`), "error");
    return;
  }
  const name = (res.headers.get("Content-Disposition") || "").match(/filename\*?=(?:UTF-8'')?"?([^";]+)/i);
  const url = URL.createObjectURL(await res.blob());
  const a = Object.assign(document.createElement("a"), { href: url, download: name ? decodeURIComponent(name[1]) : "cv" });
  a.click();
  setTimeout(() => URL.revokeObjectURL(url), 10000);
}

async function downloadCV(id) {
  const res = await apiFetch(`${API_BASE}/jobs/${id}/cv.pdf`);
  if (!res.ok) {
    const err = await res.json().catch(() => ({}));
    showToast(t(err.error || `HTTP ${res.status}`), "error");
    return;
  }
  const url = URL.createObjectURL(await res.blob());
  const a = Object.assign(document.createElement("a"), { href: url, download: `cv-job-${id}.pdf` });
  a.click();
  setTimeout(() => URL.revokeObjectURL(url), 10000);
}

function mailto(to, subject, body) {
  const q = new URLSearchParams();
  if (subject) q.set("subject", subject);
  if (body) q.set("body", body);
  return `mailto:${encodeURIComponent(to || "")}?${q.toString().replace(/\+/g, "%20")}`;
}

async function sendEmailDraft(id) {
  const to = document.getElementById(`draftTo-${id}`).value.trim();
  const subject = document.getElementById(`draftSubject-${id}`).value;
  const body = document.getElementById(`draftBody-${id}`).value;
  try {
    await APIClient.put(`jobs/${id}/draft`, { to, subject, body }); // keep the user's edits
  } catch (e) {
    showToast(t(e.message), "error");
    return;
  }
  window.location.href = mailto(to, subject, body);
  setTimeout(async () => {
    if (!confirm(t("Did you send the email from your mail app? (Mark it sent and move the job to Applied)"))) return;
    try {
      await APIClient.post(`jobs/${id}/draft/sent`);
      showToast(t("Marked as sent."), "success");
    } catch (e) {
      showToast(t(e.message), "error");
    }
    refreshAppState();
  }, 1500);
}

async function rejectEmailDraft(id) {
  if (!confirm(t("Discard this draft?"))) return;
  try {
    await APIClient.post(`jobs/${id}/draft/reject`);
  } catch (e) {
    showToast(t(e.message), "error");
  }
  refreshAppState();
}

async function sendInterviewReply(id) {
  const to = document.getElementById(`replyTo-${id}`).value.trim();
  const body = document.getElementById(`replyBody-${id}`).value;
  const job = AppState.jobs.find(j => j.id === id);
  window.location.href = mailto(to, job ? `Re: ${job.title}` : "", body);
}

async function approveOnSiteApply(id) {
  applyOnSite(id);
}

// siteFor is the job site a job's link is on, if you're logged in there.
function siteFor(job) {
  let host = "";
  try { host = new URL(job.url).hostname.toLowerCase(); } catch { return null; }
  return (AppState.platforms || []).find(p => p.status === "connected" &&
    (p.domains || []).some(d => host === d || host.endsWith("." + d))) || null;
}

// applyButton: "Apply on <site>" when you're logged in to the job's site,
// have a CV to send (tailored, or the one you uploaded) and haven't applied.
function applyButton(job, tailored) {
  const site = siteFor(job);
  const open = job.status === "Saved" || job.status === "Waiting to apply";
  if (!site || !open || job.topcv_status || job.apply_state === "filling" || job.apply_state === "confirm") return "";
  if (!tailored && !(AppState.setup && AppState.setup.cv)) return "";
  return `<button class="btn-primary card-primary" onclick="applyOnSite('${job.id}')" title="${esc(t("Opens the job in a window with your {site} login. You submit it there.", { site: site.name }))}">${esc(t("Apply on {site}", { site: site.name }))}</button>`;
}

// Assisted apply: you approve by clicking, and you submit on the site yourself.
async function applyOnSite(id) {
  let prep;
  try {
    prep = await APIClient.post(`jobs/${id}/apply`);
  } catch (e) {
    showToast(t(e.message), "error");
    return;
  }
  if (prep.cover_letter) {
    try { await navigator.clipboard.writeText(prep.cover_letter); } catch {}
  }
  const topcv = prep.site_id === "topcv";
  alert(
    t("A {site} window is opening with your login. In it:", { site: prep.site }) + "\n\n" +
    t(topcv ? "1. Click Apply (Ứng tuyển)" : "1. Click Apply") + "\n" +
    t("2. Upload this CV:") + "\n   " + prep.cv_pdf + "\n" +
    (prep.cover_letter
      ? t("3. Paste the cover letter (already copied to your clipboard)") + "\n" + t("4. Submit, then close the window") + "\n"
      : t("3. Submit, then close the window") + "\n") +
    "\n" + t(topcv ? "braveduck checks TopCV afterwards and moves the job to Applied if it's there."
                   : "When you close the window, the card asks whether you submitted it.")
  );
  refreshAppState();
}

// confirmApplied answers the card's "did you submit it?".
async function confirmApplied(id, submitted) {
  try {
    await APIClient.post(`jobs/${id}/apply/confirm`, { submitted });
  } catch (e) {
    showToast(t(e.message), "error");
    return;
  }
  if (submitted) showToast(t("Moved to Applied"), "success");
  refreshAppState();
}

async function undoOnSiteApply(id) {
  await transitionCardStatus(id, "Waiting to apply");
}

async function retryApply(id) {
  applyOnSite(id);
}

async function saveJobDetails(id) {
  const val = key => document.getElementById(`edit-${key}-${id}`).value;
  try {
    await APIClient.updateJob(id, { title: val("title"), company: val("company"), salary: val("salary"), location: val("location") });
    showToast(t("Details saved."), "success");
  } catch (e) {
    showToast(t("Couldn't save: {error}", { error: t(e.message) }), "error");
  }
  refreshAppState();
}

// Save job notes inside detail panels (auto-saved on input)
function saveJobNotes(id, textarea) {
  const text = textarea.value;
  // Debounce: save 500ms after the last keystroke.
  clearTimeout(notesTimers[id]);
  notesTimers[id] = setTimeout(async () => {
    try {
      await APIClient.updateJob(id, { notes: text });
      const j = AppState.jobs.find(item => item.id === id);
      if (j) j.notes = text;
    } catch (e) {
      showToast(t("Couldn't save notes: {error}", { error: t(e.message) }), "error");
    }
  }, 500);
}

const notesTimers = {};

// Drag & Drop event bindings for column element
function setupColumnDragEvents(colEl) {
  colEl.addEventListener("dragover", (e) => {
    e.preventDefault();
    colEl.classList.add("drag-over");
  });
  
  colEl.addEventListener("dragenter", (e) => {
    e.preventDefault();
  });
  
  colEl.addEventListener("dragleave", () => {
    colEl.classList.remove("drag-over");
  });
  
  colEl.addEventListener("drop", async (e) => {
    e.preventDefault();
    colEl.classList.remove("drag-over");
    
    const cardId = e.dataTransfer.getData("text/plain");
    const newStatus = colEl.getAttribute("data-status");
    
    await transitionCardStatus(cardId, newStatus);
  });
}

// Quick stage jump dropdown logic
async function moveStageDropdown(id, selectEl) {
  const status = selectEl.value;
  await transitionCardStatus(id, status, selectEl);
}

async function transitionCardStatus(id, newStatus, committedEl) {
  const j = AppState.jobs.find(item => item.id === id);
  if (!j || j.status === newStatus) return;
  try {
    await APIClient.updateJob(id, { status: newStatus });
    showToast(t("Job moved to {status}", { status: t(newStatus) }), "success");
    if (committedEl && document.activeElement === committedEl) committedEl.blur();
  } catch (e) {
    showToast(t("Couldn't move job: {error}", { error: t(e.message) }), "error");
    if (committedEl && committedEl.value === newStatus) committedEl.value = j.status;
  }
  refreshAppState();
}

// downloadJobsCSV saves every job as a spreadsheet (⚙ Settings).
async function downloadJobsCSV() {
  const res = await apiFetch(`${API_BASE}/export.csv`);
  if (!res.ok) {
    const err = await res.json().catch(() => ({}));
    showToast(t(err.error || `HTTP ${res.status}`), "error");
    return;
  }
  const url = URL.createObjectURL(await res.blob());
  const a = Object.assign(document.createElement("a"), { href: url, download: "braveduck-jobs.csv" });
  a.click();
  setTimeout(() => URL.revokeObjectURL(url), 10000);
}
