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
      showToast(`Couldn't refresh board: ${e.message}`, "error");
    }
  }
}

function renderAll() {
  renderStatPills();
  renderToday();
  renderMoraleWidget();
  renderCrawlStatus();
  renderPlatformFilters();
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
    <span class="stat-pill">Saved <strong>${counts.Saved}</strong></span>
    <span class="stat-pill">Applied <strong>${counts.Applied}</strong></span>
    <span class="stat-pill">Interview <strong>${counts.Interview}</strong></span>
    <span class="stat-pill">Offer <strong>${counts.Offer}</strong></span>
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
  document.getElementById("notifyBtn").textContent = `🔔 Notify me of great matches: ${notifyOn() ? "on" : "off"}`;
}

function markMatchesSeen(jobs) {
  localStorage.setItem(NOTIFIED_KEY, JSON.stringify(greatMatches(jobs).map(j => j.id)));
}

async function toggleNotify() {
  if (notifyOn()) {
    localStorage.setItem(NOTIFY_KEY, "off");
    showToast("Match notifications off", "info");
  } else if (!("Notification" in window)) {
    showToast("This browser can't show notifications", "error");
  } else {
    const perm = Notification.permission === "granted" ? "granted" : await Notification.requestPermission();
    if (perm !== "granted") {
      showToast("Notifications are blocked for this page in your browser settings", "error");
    } else {
      localStorage.setItem(NOTIFY_KEY, "on");
      markMatchesSeen(AppState.jobs); // only matches rated from now on
      showToast("You'll be notified of new A and B matches while the board is open", "success");
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
    const n = new Notification(`Great match: ${j.title}`, {
      body: `${j.score_letter} · ${Number(j.score).toFixed(1)}${j.company ? ` — ${j.company}` : ""}`,
      tag: `braveduck-job-${j.id}`
    });
    n.onclick = () => { window.focus(); showJobCard(j.id); n.close(); };
  }
  if (fresh.length > 3) {
    const n = new Notification(`${fresh.length - 3} more great matches`, { body: "See Today on the board.", tag: "braveduck-more" });
    n.onclick = () => { window.focus(); n.close(); };
  }
  markMatchesSeen(jobs);
}

// --- Today -------------------------------------------------------------------
// What to act on now, from GET /api/today (the same list `braveduck today`
// and MCP get_today give).
async function renderToday() {
  const list = document.getElementById("todayList");
  let groups = [];
  try {
    const res = await apiFetch(`${API_BASE}/today`);
    if (res.ok) groups = await res.json();
  } catch {}
  if (!groups.length) {
    list.innerHTML = `<div class="today-empty">Nothing needs you today. New jobs are found and rated in the background.</div>`;
    return;
  }
  list.innerHTML = groups.map(g => `
    <div class="today-group">
      <div class="today-group-title">${esc(g.title)}</div>
      <ul>${g.items.map(it => `
        <li><button class="today-item" data-job="${esc(String(it.job_id))}">${esc(it.title)}<span class="today-why">${esc(it.why)}</span></button></li>`).join("")}
      </ul>
    </div>`).join("");
  list.querySelectorAll("button[data-job]").forEach(btn => btn.addEventListener("click", () => showJobCard(btn.dataset.job)));
}

function renderMoraleWidget() {
  // The interview rate and its message come from the server (service.Stats:
  // jobs.SuccessRateOf / MoraleMessage), the same ones `get_stats` gives an
  // agent. Resolved = an interview or offer, a closed job, or an application
  // past the response window; the rate shows once 3 have resolved.
  const st = AppState.stats;
  if (!st) return;
  const plural = (n, word) => `${n} ${word}${n === 1 ? "" : "s"}`;
  document.getElementById("moraleSubline").innerText =
    `${plural(st.interviews, "interview")} from ${plural(st.resolved, "answered application")} · ${st.pending} waiting`;
  document.getElementById("moraleRate").innerText = st.resolved >= 3 ? `${Math.round(st.interview_rate * 100)}%` : "—";
  document.getElementById("moraleMessage").innerText = st.morale || "";
}

function renderCrawlStatus() {
  const status = AppState.crawlStatus;
  if (!status) return;

  const headline = document.getElementById("crawlHeadline");
  headline.innerHTML = `📥 <strong>${esc(status.newJobsToday)} new jobs</strong> found · ${esc(status.date)}`;

  // Render per-source chips
  const chipsContainer = document.getElementById("crawlChips");
  // Background tasks (rating, status sync) find no jobs: no chip, and no
  // filter below, but a failing one still shows in the warnings.
  chipsContainer.innerHTML = status.sources.filter(s => !s.task)
    .map(s => `<span class="chip">${esc(s.name)} <strong style="color:var(--ink)">${esc(s.count)}</strong></span>`)
    .join("");

  // Render unhealthy sources
  const unhealthyContainer = document.getElementById("unhealthyContainer");
  const unhealthySources = status.sources.filter(s => !s.healthy);

  if (unhealthySources.length === 0) {
    unhealthyContainer.innerHTML = "";
    unhealthyContainer.style.display = "none";
  } else {
    unhealthyContainer.style.display = "flex";
    unhealthyContainer.innerHTML = unhealthySources
      .map(s => `
        <div class="chip unhealthy" style="justify-content: space-between; display: flex; width: 100%;">
          <span>⚠️ ${esc(s.name)} — ${s.lastError ? esc(s.lastError) : "no recent successful run"} (last report: ${esc(timeAgo(s.lastRun) || "never")})</span>
          <button class="btn-text" onclick="triggerCrawlerRetry(${jsArg(s.name)})" style="color: #991b1b; text-decoration: underline;">Retry ⟳</button>
        </div>
      `)
      .join("");
  }
}

async function triggerCrawlerRetry(sourceName) {
  const flash = document.getElementById("retryFlash");
  flash.style.display = "block";
  flash.innerText = `⟳ Restarting ${sourceName} crawler...`;

  showToast(`Restarting crawler ${sourceName}...`, "info");

  let result;
  try {
    result = await APIClient.retryCrawl(sourceName);
  } catch (e) {
    result = { ok: false, message: String(e) };
  }

  flash.style.display = "none";
  // The real backend returns {ok:false, message} until crawlers are wired in;
  // surface that truth rather than a canned success.
  if (result && result.ok === false) {
    showToast(result.message || `Couldn't retry ${sourceName}`, "error");
  } else {
    showToast(result.message || `${sourceName} crawled`, "success");
  }
  refreshAppState();
}

function renderPlatformFilters() {
  // Collect all unique discovery platforms + "All"
  const platforms = new Set(["All"]);
  // Crawl sources always appear
  if (AppState.crawlStatus && AppState.crawlStatus.sources) {
    AppState.crawlStatus.sources.filter(s => !s.task).forEach(s => platforms.add(s.name));
  }
  // Other platforms from jobs
  AppState.jobs.forEach(j => {
    if (j.discovery_source) platforms.add(j.discovery_source);
  });

  const container = document.getElementById("platformFilters");
  container.innerHTML = Array.from(platforms)
    .map(p => `
      <label class="filter-cb">
        <input type="checkbox" name="platform-filter" value="${esc(p)}" ${AppState.filters.platform === p ? "checked" : ""} onchange="updatePlatformFilter(this)">
        ${esc(p)}
      </label>
    `)
    .join("");
}

function updatePlatformFilter(checkbox) {
  const name = checkbox.value;
  if (checkbox.checked) {
    AppState.filters.platform = name;
    // Uncheck other checkboxes
    document.querySelectorAll('input[name="platform-filter"]').forEach(cb => {
      if (cb.value !== name) cb.checked = false;
    });
  } else {
    // If unchecked, default back to All
    AppState.filters.platform = "All";
    document.querySelectorAll('input[name="platform-filter"]').forEach(cb => {
      if (cb.value === "All") cb.checked = true;
    });
  }
  renderBoard();
}
