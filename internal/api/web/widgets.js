let refreshGeneration = 0;

async function refreshAppState() {
  const generation = ++refreshGeneration;
  let location = AppState.location;
  try {
    const res = await apiFetch(`${API_BASE}/location`);
    if (res.ok) location = await res.json();
  } catch {}
  try {
    const jobs = await APIClient.getJobs();
    const ratingConfig = await APIClient.getRatingConfig();
    const crawlStatus = await APIClient.getCrawlStatus();
    const activity = await APIClient.getActivity();
    const stats = await APIClient.getStats();
    let platforms = AppState.platforms || [];
    try { platforms = await APIClient.getPlatforms(); } catch {}
    // Requests can finish out of order, or the user can begin editing after
    // one starts. Commit a complete snapshot only while it is still current
    // and safe to replace the card DOM. Focusout retries a deferred refresh.
    if (generation !== refreshGeneration) return;
    if (isEditingCard()) {
      refreshPending = true;
      return;
    }
    refreshPending = false;
    Object.assign(AppState, { location, jobs, ratingConfig, crawlStatus, activity, platforms, stats });
    renderAll();
    renderSetup();
    notifyNewMatches(AppState.jobs);
    // A login finishing writes the session, which triggers a live update.
    if (document.getElementById("platformsModalOverlay").classList.contains("open")) {
      renderPlatforms();
    }
  } catch (e) {
    if (generation === refreshGeneration) {
      showToast(t("Couldn't refresh board: {message}", { message: t(e.message) }), "error");
    }
  }
}

function renderAll() {
  renderStatPills();
  renderToday();
  renderMoraleWidget();
  renderCrawlStatus();
  renderBoard();
  renderActivityLog();
}

function renderStatPills() {
  const counts = {
    Saved: 0,
    Applied: 0,
    Interview: 0,
    Offer: 0
  };
  AppState.jobs.forEach(j => {
    if (counts[j.status] !== undefined) {
      counts[j.status]++;
    }
  });

  const container = document.getElementById("statPills");
  container.innerHTML = `
    <span class="stat-pill">${esc(t("Saved"))} <strong>${counts.Saved.toLocaleString(I18N.lang)}</strong></span>
    <span class="stat-pill">${esc(t("Applied"))} <strong>${counts.Applied.toLocaleString(I18N.lang)}</strong></span>
    <span class="stat-pill">${esc(t("Interview"))} <strong>${counts.Interview.toLocaleString(I18N.lang)}</strong></span>
    <span class="stat-pill">${esc(t("Offer"))} <strong>${counts.Offer.toLocaleString(I18N.lang)}</strong></span>
  `;
}

// --- Great-match notifications --------------------------------------------------
// While the board is open, a job newly rated A or B pops a desktop
// notification; clicking it opens the card. Opt-in (⚙ Settings), remembered
// per browser. Jobs already rated when it's switched on don't notify.
const NOTIFY_KEY = "notify_matches";
const NOTIFIED_KEY = "notified_match_ids";

function greatMatches(jobs) {
  return jobs.filter(j => j.score_status === "scored" && (j.score_letter === "A" || j.score_letter === "B")
    && (j.status === "Saved" || j.status === "Waiting to apply"));
}

function notifyOn() {
  return localStorage.getItem(NOTIFY_KEY) === "on" && "Notification" in window && Notification.permission === "granted";
}

function renderNotifyButton() {
  document.getElementById("notifyBtn").textContent = notifyOn() ? t("🔔 Notify me of great matches: on") : t("🔔 Notify me of great matches: off");
}

function markMatchesSeen(jobs) {
  localStorage.setItem(NOTIFIED_KEY, JSON.stringify(greatMatches(jobs).map(j => j.id)));
}

async function toggleNotify() {
  if (notifyOn()) {
    localStorage.setItem(NOTIFY_KEY, "off");
    showToast(t("Match notifications off"), "info");
  } else if (!("Notification" in window)) {
    showToast(t("This browser can't show notifications"), "error");
  } else {
    const perm = Notification.permission === "granted" ? "granted" : await Notification.requestPermission();
    if (perm !== "granted") {
      showToast(t("Notifications are blocked for this page in your browser settings"), "error");
    } else {
      localStorage.setItem(NOTIFY_KEY, "on");
      markMatchesSeen(AppState.jobs); // only matches rated from now on
      showToast(t("You'll be notified of new A and B matches while the board is open"), "success");
    }
  }
  renderNotifyButton();
}

function notifyNewMatches(jobs) {
  if (!notifyOn()) return;
  let seen;
  try { seen = new Set(JSON.parse(localStorage.getItem(NOTIFIED_KEY) || "[]")); } catch { seen = new Set(); }
  const fresh = greatMatches(jobs).filter(j => !seen.has(j.id)).sort((a, b) => b.score - a.score);
  // A few at most: rated while the board was closed shouldn't mean a flood.
  for (const j of fresh.slice(0, 3)) {
    const n = new Notification(t("Great match: {title}", { title: j.title }), {
      body: `${j.score_letter} · ${Number(j.score).toLocaleString(I18N.lang, { minimumFractionDigits: 1, maximumFractionDigits: 1 })}${j.company ? ` — ${j.company}` : ""}`,
      tag: `braveduck-job-${j.id}`
    });
    n.onclick = () => { window.focus(); showJobCard(j.id); n.close(); };
  }
  if (fresh.length > 3) {
    const n = new Notification(t("{n} more great matches", { n: (fresh.length - 3).toLocaleString(I18N.lang) }), { body: t("See Today on the board."), tag: "braveduck-more" });
    n.onclick = () => { window.focus(); n.close(); };
  }
  markMatchesSeen(jobs);
}

// --- Today -------------------------------------------------------------------
// One short line; what to act on now, from GET /api/today (the same list
// `braveduck today` and MCP get_today give), opens under it.
const plural = (n, word) => `${n} ${word}${n === 1 ? "" : "s"}`;

// Job sources by their proper names. Anything else shows as it was written.
const SOURCE_NAMES = {
  vietnamworks: "VietnamWorks", careerlink: "CareerLink", topcv: "TopCV", joboko: "JobOKO", facebook: "Facebook",
  paste: "Added by you", "topcv-sync": "TopCV updates", "jev-rate": "Rating"
};

function sourceName(source) {
  const key = String(source).toLowerCase();
  return Object.hasOwn(SOURCE_NAMES, key) ? t(SOURCE_NAMES[key]) : source;
}

function todayReason(message) {
  const deadline = /^closes in (\d+) days$/.exec(message);
  if (deadline) return t("closes in {n} days", { n: Number(deadline[1]).toLocaleString(I18N.lang) });
  const applied = /^applied (\d+) days ago$/.exec(message);
  if (applied) return t("applied {n} days ago", { n: Number(applied[1]).toLocaleString(I18N.lang) });
  const score = /^([AB]) · (\d+\.\d+)$/.exec(message);
  if (score) return `${score[1]} · ${Number(score[2]).toLocaleString(I18N.lang, { minimumFractionDigits: 1, maximumFractionDigits: 1 })}`;
  return t(message);
}

let todayCount = 0;

async function renderToday() {
  const list = document.getElementById("todayList");
  let groups = [];
  try {
    const res = await apiFetch(`${API_BASE}/today`);
    if (res.ok) groups = await res.json();
  } catch {}
  todayCount = groups.reduce((n, g) => n + g.items.length, 0);
  renderTodaySummary();
  if (!groups.length) {
    list.innerHTML = `<div class="today-empty">${t("Nothing needs you today.")}</div>`;
    return;
  }
  list.innerHTML = groups.map(g => `
    <div class="today-group">
      <div class="today-group-title">${esc(t(g.title))}</div>
      <ul>${g.items.map(it => `
        <li><button class="today-item" data-job="${esc(String(it.job_id))}">${esc(it.title)}<span class="today-why">${esc(todayReason(it.why))}</span></button></li>`).join("")}
      </ul>
    </div>`).join("");
  list.querySelectorAll("button[data-job]").forEach(btn => btn.addEventListener("click", () => showJobCard(btn.dataset.job)));
}

function renderTodaySummary() {
  const fresh = AppState.crawlStatus ? Number(AppState.crawlStatus.newJobsToday) : 0;
  const parts = [];
  if (fresh > 0) parts.push(t(fresh === 1 ? "{n} new job" : "{count} new jobs", { n: fresh.toLocaleString(I18N.lang), count: fresh.toLocaleString(I18N.lang) }));
  if (todayCount > 0) parts.push(t("{n} to look at", { n: todayCount.toLocaleString(I18N.lang) }));
  document.getElementById("todaySummary").textContent = parts.join(" · ") || t("Nothing needs you");
}

function renderMoraleWidget() {
  // The interview rate and its message come from the server (service.Stats:
  // jobs.SuccessRateOf / MoraleMessage), the same ones `get_stats` gives an
  // agent. Resolved = an interview or offer, a closed job, or an application
  // past the response window; the rate shows once 3 have resolved. Before
  // anything has resolved there is nothing to say.
  const st = AppState.stats;
  if (!st) return;
  document.getElementById("moraleWidget").hidden = st.resolved < 1;
  document.getElementById("moraleSubline").innerText =
    t("{interviews} from {applications} · {pending} waiting", {
      interviews: plural(st.interviews, "interview"),
      applications: plural(st.resolved, "answered application"),
      n: st.interviews.toLocaleString(I18N.lang),
      resolved: st.resolved.toLocaleString(I18N.lang),
      pending: st.pending.toLocaleString(I18N.lang)
    });
  const rate = document.getElementById("moraleRate");
  rate.innerText = st.resolved >= 3 ? `${Math.round(st.interview_rate * 100).toLocaleString(I18N.lang)}%` : "—";
  rate.hidden = st.resolved < 3;
  document.getElementById("moraleMessage").innerText = t(st.morale || "");
}

function renderCrawlStatus() {
  const status = AppState.crawlStatus;
  if (!status) return;
  renderTodaySummary();

  // Render per-source chips
  const chipsContainer = document.getElementById("crawlChips");
  // Background tasks (rating, status sync) find no jobs: no chip, but a
  // failing one still shows in the warnings.
  chipsContainer.innerHTML = status.sources.filter(s => !s.task)
    .map(s => `<span class="chip">${esc(sourceName(s.name))} <strong style="color:var(--ink)">${Number(s.count).toLocaleString(I18N.lang)}</strong></span>`)
    .join("");

  // Render unhealthy sources: the one thing here that shouldn't wait for a click.
  const unhealthyContainer = document.getElementById("unhealthyContainer");
  const unhealthySources = status.sources.filter(s => !s.healthy);
  unhealthyContainer.hidden = unhealthySources.length === 0;
  unhealthyContainer.innerHTML = unhealthySources
    .map(s => {
      const key = s.task ? "⚠️ {source} isn't working (last try: {when})"
        : s.lastError ? "⚠️ {source} couldn't check for jobs (last try: {when})"
        : "⚠️ {source} hasn't checked for jobs lately (last try: {when})";
      const problem = t(key, { source: sourceName(s.name), when: timeAgo(s.lastRun) || t("never") });
      return `
        <div class="chip unhealthy" ${s.lastError ? `title="${esc(t(s.lastError))}"` : ""}>
          <span>${esc(problem)}</span>
          <button class="btn-text" onclick="triggerCrawlerRetry(${jsArg(s.name)})">${t("Try again")}</button>
        </div>
      `;
    })
    .join("");
}

async function triggerCrawlerRetry(source) {
  const flash = document.getElementById("retryFlash");
  flash.hidden = false;
  flash.innerText = t("⟳ Checking {source} again…", { source: sourceName(source) });

  let result;
  try {
    result = await APIClient.retryCrawl(source);
  } catch (e) {
    result = { ok: false, message: String(e) };
  }

  flash.hidden = true;
  // The real backend returns {ok:false, message} until crawlers are wired in;
  // surface that truth rather than a canned success.
  if (result && result.ok === false) {
    showToast(result.message ? t(result.message) : t("Couldn't check {source}", { source: sourceName(source) }), "error");
  } else {
    showToast(result.message ? t(result.message) : t("{source} checked", { source: sourceName(source) }), "success");
  }
  refreshAppState();
}
