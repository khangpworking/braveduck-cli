// --- Ask your AI (rate / tailor CV / draft email) ------------------------------
let askState = null;

async function askAI(kind, id, model) {
  const notesEl = document.getElementById(`draftNotes-${id}`);
  let req;
  try {
    if (kind === "rate") showToast("Rating…", "info");
    req = await APIClient.askRequest(kind, id, kind === "draft" ? { notes: notesEl ? notesEl.value : "" } : kind === "rate" ? { model } : {});
  } catch (e) {
    showToast(e.message, "error");
    return;
  }
  if (req.rated) {
    // A Jev challenger leaves the canonical badge unchanged; report the
    // actual model verdict returned by this request.
    const verdict = req.job.ratings?.[req.model];
    showToast(`Rated ${verdict?.letter ?? req.job.score_letter} ${Number(verdict?.score ?? req.job.score).toFixed(1)} (${req.model})`, "success");
    refreshAppState();
    return;
  }
  askState = { kind, id, req };
  const titles = { rate: "Rate this job with your AI", cv: "Tailor your CV with your AI", draft: "Draft the email with your AI" };
  document.getElementById("askTitle").textContent = titles[kind];
  document.getElementById("askPrompt").textContent = req.prompt;
  const paste = document.getElementById("askPasteBlock");
  paste.style.display = kind === "rate" ? "none" : "block";
  document.getElementById("askPaste").value = "";
  document.getElementById("askPaste").placeholder = kind === "cv"
    ? "…or paste the CV your AI wrote (Markdown) here"
    : "…or paste the email here: first line \"Subject: …\", then a blank line, then the body";
  document.getElementById("askModalOverlay").classList.add("open");
}

async function copyText(text) {
  try {
    await navigator.clipboard.writeText(text);
    showToast("Copied.", "success");
  } catch {
    showToast("Couldn't copy — select the text and copy it yourself.", "error");
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
    showToast(askState.kind === "cv" ? "CV saved." : "Draft saved for review.", "success");
    refreshAppState();
  } catch (e) {
    showToast(`Couldn't save: ${e.message}`, "error");
  }
}

// --- Get started + profile -------------------------------------------------------
// The steps ratings and crawls need. Each opens the window that does it.
const SETUP_STEPS = [
  { key: "cv", title: "Upload your CV", hint: "Its text becomes the \"About me\" part of your profile.", action: "Upload CV", open: () => document.getElementById("myCVBtn").click() },
  { key: "wants", title: "Say what you're looking for", hint: "Roles, minimum pay, where you can work, deal-breakers.", action: "Edit profile", open: () => openProfile() },
  { key: "search_queries", title: "Add search terms", hint: "What the crawlers search job sites for, e.g. \"n8n\".", action: "Add terms", open: () => document.getElementById("keywordsBtn").click() },
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
  document.getElementById("setupSteps").innerHTML = SETUP_STEPS.map((step, i) => `
    <li class="setup-step${done[step.key] ? " done" : ""}">
      <span class="setup-step-title">${done[step.key] ? "✓" : `${i + 1}.`} ${esc(step.title)}</span>
      <span class="setup-step-hint">${esc(step.hint)}</span>
      ${done[step.key] ? "" : `<button class="btn-secondary btn-sm" data-setup="${esc(step.key)}">${esc(step.action)}</button>`}
    </li>`).join("");
  card.querySelectorAll("button[data-setup]").forEach(btn => {
    btn.addEventListener("click", () => SETUP_STEPS.find(s => s.key === btn.dataset.setup).open());
  });
}

// On a shared server: who is logged in, and the login service's log-out page.
function renderHostedUser(hosted) {
  const el = document.getElementById("hostedUser");
  el.hidden = !hosted;
  if (!hosted) return;
  el.innerHTML = `${esc(hosted.user)}${hosted.logout_url ? `<a href="${safeUrl(hosted.logout_url)}">Log out</a>` : ""}`;
}

async function openProfile() {
  const status = document.getElementById("profileStatus");
  const text = document.getElementById("profileText");
  document.getElementById("profileModalOverlay").classList.add("open");
  const res = await apiFetch(`${API_BASE}/profile`);
  const p = await res.json().catch(() => ({}));
  if (!res.ok) {
    status.innerText = `Couldn't load your profile: ${p.error || res.status}`;
    return;
  }
  text.value = p.text || "";
  renderProfileStatus(p);
}

function renderProfileStatus(p) {
  document.getElementById("profileStatus").innerHTML = p.filled
    ? `✓ Filled in, so jobs can be rated. Saved at <code>${esc(p.path)}</code>.`
    : `Still empty, so no job can be rated yet. Saved at <code>${esc(p.path)}</code>.`;
}

async function saveProfile() {
  const btn = document.getElementById("saveProfileBtn");
  btn.disabled = true;
  btn.textContent = "Saving…";
  try {
    const res = await apiFetch(`${API_BASE}/profile`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ text: document.getElementById("profileText").value })
    });
    const p = await res.json().catch(() => ({}));
    if (!res.ok) {
      showToast(`Couldn't save: ${p.error || res.status}`, "error");
      return;
    }
    renderProfileStatus(p);
    showToast("Profile saved", "success");
    renderSetup();
  } finally {
    btn.disabled = false;
    btn.textContent = "Save";
  }
}

// --- My CV (the base CV) -------------------------------------------------------
async function renderMyCV() {
  const status = document.getElementById("cvStatus");
  const res = await apiFetch(`${API_BASE}/cv`);
  const out = await res.json().catch(() => ({}));
  if (!res.ok) {
    status.innerText = `Couldn't load your CV: ${out.error || res.status}`;
    return;
  }
  const profile = out.profile || {};
  const profileLine = profile.filled
    ? `Your profile (<code>${esc(profile.path)}</code>) is filled in, so jobs can be rated.`
    : `Your profile (<code>${esc(profile.path)}</code>) is still empty, so no job can be rated yet.`;
  if (!out.cv) {
    status.innerHTML = `No CV uploaded yet. ${profileLine}`;
    return;
  }
  const cv = out.cv;
  status.innerHTML = `
    <div style="display: flex; justify-content: space-between; align-items: center; gap: 8px; padding: 8px 0; border-bottom: 1px solid var(--hairline);">
      <div><strong>${esc(cv.name)}</strong>
        <div style="font-size: 12px; color: var(--muted);">${esc(cv.kind.toUpperCase())} · ${Math.ceil(cv.size / 1024)} KB · uploaded ${esc(new Date(cv.uploaded_at).toLocaleString())}</div>
      </div>
      <button class="btn-secondary btn-sm" onclick="downloadMyCV()">Download</button>
    </div>
    <p style="margin: 8px 0 0;">${profileLine} Upload a new file to replace this one.</p>`;
}

async function uploadMyCV(replace) {
  const input = document.getElementById("cvFileInput");
  const btn = document.getElementById("cvUploadBtn");
  const result = document.getElementById("cvResult");
  const file = input.files[0];
  if (!file) {
    showToast("Choose your CV file first", "info");
    return;
  }
  const form = new FormData();
  form.append("file", file);
  if (replace) form.append("replace", "true");
  btn.disabled = true;
  btn.textContent = "Uploading…";
  try {
    const res = await apiFetch(`${API_BASE}/cv`, { method: "POST", body: form });
    const out = await res.json().catch(() => ({}));
    if (res.status === 409 && !replace) {
      if (confirm(`${out.error}\n\nOK replaces it with the text of ${file.name}.`)) return uploadMyCV(true);
      return;
    }
    result.style.display = "block";
    const tone = (bg, fg, border) => Object.assign(result.style, { backgroundColor: bg, color: fg, border: `1px solid ${border}` });
    if (!res.ok) {
      tone("#fee2e2", "#991b1b", "#fecaca");
      result.innerText = `❌ ${out.error || `HTTP ${res.status}`}`;
      return;
    }
    tone("#dcfce7", "#166534", "#bbf7d0");
    result.innerHTML = `✅ <strong>${esc(out.name)}</strong> uploaded, and its text is now in your profile's About me.<br>` +
      `Add what you're looking for (pay, location, deal-breakers) to the same file so ratings fit you.`;
    input.value = "";
    showToast("CV uploaded", "success");
    renderMyCV();
    renderSetup();
  } finally {
    btn.disabled = false;
    btn.textContent = "Upload";
  }
}

async function downloadMyCV() {
  const res = await apiFetch(`${API_BASE}/cv/file`);
  if (!res.ok) {
    const err = await res.json().catch(() => ({}));
    showToast(err.error || `HTTP ${res.status}`, "error");
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
    showToast(err.error || `HTTP ${res.status}`, "error");
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
    showToast(`Fix the draft first: ${e.message}`, "error");
    return;
  }
  window.location.href = mailto(to, subject, body);
  setTimeout(async () => {
    if (!confirm("Did you send the email from your mail app? (Mark it sent and move the job to Applied)")) return;
    try {
      await APIClient.post(`jobs/${id}/draft/sent`);
      showToast("Marked as sent.", "success");
    } catch (e) {
      showToast(e.message, "error");
    }
    refreshAppState();
  }, 1500);
}

async function rejectEmailDraft(id) {
  if (!confirm("Discard this draft?")) return;
  try {
    await APIClient.post(`jobs/${id}/draft/reject`);
  } catch (e) {
    showToast(e.message, "error");
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
  return `<button class="btn-primary btn-sm" onclick="applyOnSite('${job.id}')" title="Opens the job in a window with your ${esc(site.name)} login. You submit it there.">Apply on ${esc(site.name)}</button>`;
}

// Assisted apply: you approve by clicking, and you submit on the site yourself.
async function applyOnSite(id) {
  let prep;
  try {
    prep = await APIClient.post(`jobs/${id}/apply`);
  } catch (e) {
    showToast(e.message, "error");
    return;
  }
  if (prep.cover_letter) {
    try { await navigator.clipboard.writeText(prep.cover_letter); } catch {}
  }
  const topcv = prep.site_id === "topcv";
  alert(
    `A ${prep.site} window is opening with your login. In it:\n\n` +
    "1. Click Apply" + (topcv ? " (Ứng tuyển)" : "") + "\n" +
    "2. Upload this CV:\n   " + prep.cv_pdf + "\n" +
    (prep.cover_letter ? "3. Paste the cover letter (already copied to your clipboard)\n4. Submit, then close the window\n" : "3. Submit, then close the window\n") +
    (topcv ? "\nbraveduck checks TopCV afterwards and moves the job to Applied if it's there."
           : "\nWhen you close the window, the card asks whether you submitted it.")
  );
  refreshAppState();
}

// confirmApplied answers the card's "did you submit it?".
async function confirmApplied(id, submitted) {
  try {
    await APIClient.post(`jobs/${id}/apply/confirm`, { submitted });
  } catch (e) {
    showToast(e.message, "error");
    return;
  }
  if (submitted) showToast("Moved to Applied", "success");
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
    showToast("Details saved.", "success");
  } catch (e) {
    showToast(`Couldn't save: ${e.message}`, "error");
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
      showToast(`Couldn't save notes: ${e.message}`, "error");
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
    showToast(`Job moved to ${newStatus}`, "success");
    if (committedEl && document.activeElement === committedEl) committedEl.blur();
  } catch (e) {
    showToast(`Couldn't move job: ${e.message}`, "error");
    if (committedEl && committedEl.value === newStatus) committedEl.value = j.status;
  }
  refreshAppState();
}

// downloadJobsCSV saves every job as a spreadsheet (⚙ Settings).
async function downloadJobsCSV() {
  const res = await apiFetch(`${API_BASE}/export.csv`);
  if (!res.ok) {
    const err = await res.json().catch(() => ({}));
    showToast(err.error || `HTTP ${res.status}`, "error");
    return;
  }
  const url = URL.createObjectURL(await res.blob());
  const a = Object.assign(document.createElement("a"), { href: url, download: "braveduck-jobs.csv" });
  a.click();
  setTimeout(() => URL.revokeObjectURL(url), 10000);
}
