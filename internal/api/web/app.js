// Career Tracker - Core Application Logic

const API_BASE = "/api";

// The server injects a per-run token into index.html and requires it on every
// /api/ call (see guard in api.go). On 401 the page is stale — the server was
// restarted with a new token — so reloading picks the new one up.
const API_TOKEN = document.querySelector('meta[name="braveduck-token"]')?.content || "";

async function apiFetch(url, opts = {}) {
  const res = await fetch(url, {
    ...opts,
    headers: { ...(opts.headers || {}), Authorization: `Bearer ${API_TOKEN}` }
  });
  if (res.status === 401) {
    showToast("Session expired — reloading the board…", "error");
    setTimeout(() => location.reload(), 1500);
  }
  return res;
}

// --- HTML safety -------------------------------------------------------------
// Job fields come from scraped third-party pages, so treat every one as hostile
// text. Anything interpolated into an innerHTML template goes through esc();
// a value inside an inline onclick="f(...)" goes through jsArg(); a value used
// as an href goes through safeUrl(), which only lets http(s) links through.
function esc(v) {
  return String(v ?? "")
    .replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;").replace(/'/g, "&#39;");
}

function jsArg(v) {
  return esc(JSON.stringify(String(v ?? "")));
}

function safeUrl(v) {
  if (!v) return "#";
  try {
    const u = new URL(String(v ?? ""), location.href);
    return u.protocol === "http:" || u.protocol === "https:" ? esc(u.href) : "#";
  } catch {
    return "#";
  }
}

// --- Field-shape adapter -----------------------------------------------------
// The board's views use a slightly different Job shape from the Go store's JSON
// in three ways: status is TitleCase ("Waiting to apply") vs the store's
// lowercase ("waiting"), skills is an array vs a comma-separated string, and id
// is a string vs a number. We reconcile at the fetch boundary so the rest of the
// app stays untouched.
const STATUS_API_TO_UI = {
  saved: "Saved", waiting: "Waiting to apply", applied: "Applied",
  interview: "Interview", offer: "Offer", closed: "Closed"
};
const STATUS_UI_TO_API = {
  "Saved": "saved", "Waiting to apply": "waiting", "Applied": "applied",
  "Interview": "interview", "Offer": "offer", "Closed": "closed"
};

// The store's expires_at is a tagged grammar ("expire:YYYY-MM-DD",
// "applied:YYYY-MM-DD", "unknown", ""); the UI just wants a plain deadline it
// can new Date(). Keep only the real scraped deadline; blank the rest so the
// card badge never renders "Invalid Date". (The applied: anchor is Go's
// apply-silence clock, not a display deadline.)
function decodeExpiresAt(raw) {
  if (!raw || raw === "unknown") return "";
  if (raw.startsWith("expire:")) return raw.slice("expire:".length);
  if (raw.startsWith("applied:")) return "";
  return raw; // legacy bare ISO
}

function normalizeJob(j) {
  return {
    ...j,
    id: String(j.id),
    status: STATUS_API_TO_UI[j.status] || j.status,
    skills: Array.isArray(j.skills)
      ? j.skills
      : (typeof j.skills === "string" && j.skills
          ? j.skills.split(",").map(s => s.trim()).filter(Boolean)
          : []),
    expires_at: decodeExpiresAt(j.expires_at),
    ratings: j.ratings || {}
  };
}

function denormalizeFields(fields) {
  const out = { ...fields };
  if (out.status && STATUS_UI_TO_API[out.status]) out.status = STATUS_UI_TO_API[out.status];
  if (Array.isArray(out.skills)) out.skills = out.skills.join(",");
  return out;
}



// Global App State
const AppState = {
  jobs: [],
  ratingConfig: {},
  keywords: {},
  crawlStatus: {},
  platforms: [],
  activity: [],
  filters: {
    platform: "All"
  },
  sorting: {
    Saved: "Most recent" // or "Highest score"
  },
  expandedCards: new Set() // stores expanded card IDs
};

// API Client Wrapper (mirrors REST-API-CONTRACT.md)
const APIClient = {
  async getJobs() {
    const res = await apiFetch(`${API_BASE}/jobs`);
    const data = await res.json();
    return Array.isArray(data) ? data.map(normalizeJob) : [];
  },

  async updateJob(id, fields) {
    const res = await apiFetch(`${API_BASE}/jobs/${id}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(denormalizeFields(fields))
    });
    const body = await res.json();
    if (!res.ok) throw new Error(body.error || `HTTP ${res.status}`);
    return normalizeJob(body);
  },

  async deleteJob(id) {
    const res = await apiFetch(`${API_BASE}/jobs/${id}`, { method: "DELETE" });
    return res.json();
  },

  async addJob(pasteContent) {
    const res = await apiFetch(`${API_BASE}/jobs`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ content: pasteContent })
    });
    if (!res.ok) {
      // 409 gets its own banner; anything else (a too-short paste, a link to
      // localhost/LAN, ...) surfaces the server's message verbatim.
      if (res.status === 409) throw new Error("duplicate");
      const body = await res.json().catch(() => ({}));
      throw new Error(body.error || `HTTP ${res.status}`);
    }
    return res.json();
  },

  async rescore(id) {
    const res = await apiFetch(`${API_BASE}/jobs/${id}/rate`, { method: "POST" });
    return res.json();
  },

  async askModel(id, modelName) {
    const res = await apiFetch(`${API_BASE}/jobs/${id}/rate/ask`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ model: modelName })
    });
    return res.json();
  },

  async generateCV(id) {
    const res = await apiFetch(`${API_BASE}/jobs/${id}/cv`, { method: "POST" });
    return res.json();
  },

  async draftEmail(id, options = {}) {
    const res = await apiFetch(`${API_BASE}/jobs/${id}/draft`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(options)
    });
    return res.json();
  },

  async approveDraft(id, draftEmail) {
    const res = await apiFetch(`${API_BASE}/jobs/${id}/draft/approve`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ draft_email: draftEmail })
    });
    return res.json();
  },

  async rejectDraft(id) {
    const res = await apiFetch(`${API_BASE}/jobs/${id}/draft/reject`, { method: "POST" });
    return res.json();
  },

  async applyJob(id) {
    // Triggers platform apply engine (on-site apply)
    const res = await apiFetch(`${API_BASE}/jobs/${id}/apply`, { method: "POST" });
    return res.json();
  },

  async keepJob(id) {
    const res = await apiFetch(`${API_BASE}/jobs/${id}/keep`, { method: "POST" });
    return res.json();
  },

  async pinJob(id) {
    const res = await apiFetch(`${API_BASE}/jobs/${id}/pin`, { method: "POST" });
    return res.json();
  },

  async getRatingConfig() {
    const res = await apiFetch(`${API_BASE}/rating-config`);
    return res.json();
  },

  async updateRatingConfig(config) {
    const res = await apiFetch(`${API_BASE}/rating-config`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(config)
    });
    return res.json();
  },

  async getKeywords() {
    const res = await apiFetch(`${API_BASE}/keywords`);
    return res.json();
  },

  async updateKeywords(keywords) {
    const res = await apiFetch(`${API_BASE}/keywords`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(keywords)
    });
    return res.json();
  },

  async getCrawlStatus() {
    const res = await apiFetch(`${API_BASE}/crawl-status`);
    return res.json();
  },

  async getActivity() {
    const res = await apiFetch(`${API_BASE}/activity`);
    if (!res.ok) return [];
    const events = await res.json();
    // Server timestamps are ISO; the feed renders a plain clock.
    return (events || []).map(e => ({
      ...e,
      timestamp: new Date(e.timestamp).toLocaleTimeString('vi-VN', { hour12: false })
    }));
  },

  async retryCrawl(sourceName) {
    const res = await apiFetch(`${API_BASE}/crawl/${sourceName}/retry`, { method: "POST" });
    return res.json();
  },

  async backfillRatings() {
    const res = await apiFetch(`${API_BASE}/jobs/rate-backfill`, { method: "POST" });
    return res.json();
  }
};

// UI Rendering Logic
async function refreshAppState() {
  AppState.jobs = await APIClient.getJobs();
  AppState.ratingConfig = await APIClient.getRatingConfig();
  AppState.crawlStatus = await APIClient.getCrawlStatus();
  AppState.activity = await APIClient.getActivity();

  renderAll();
}

function renderAll() {
  renderStatPills();
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

function renderMoraleWidget() {
  // Moral Widget Logic:
  // Resolved = Interview / Offer (wins), or Applied / Closed that is aged past 14 days (misses).
  // Freshly-applied or Saved are not counted in morale calculation.
  let wins = 0;
  let misses = 0;
  let pending = 0;
  
  const now = new Date();

  AppState.jobs.forEach(j => {
    if (j.status === "Interview" || j.status === "Offer") {
      wins++;
    } else if (j.status === "Applied" || j.status === "Closed") {
      const savedDate = new Date(j.saved_at);
      const diffDays = (now - savedDate) / (1000 * 60 * 60 * 24);
      if (diffDays > 14 && j.apply_state === "sent") {
        misses++;
      } else {
        pending++;
      }
    } else if (j.status === "Waiting to apply") {
      pending++;
    }
  });

  const totalResolved = wins + misses;
  const rateEl = document.getElementById("moraleRate");
  const sublineEl = document.getElementById("moraleSubline");
  const msgEl = document.getElementById("moraleMessage");

  sublineEl.innerText = `${wins} interviews / ${totalResolved} resolved answered · ${pending} pending`;

  if (totalResolved < 3) {
    rateEl.innerText = "—";
    msgEl.innerText = "Too early to tell. Keep applying to collect traction stats. (Còn quá sớm để đánh giá. Hãy tiếp tục ứng tuyển.)";
  } else {
    const rate = Math.round((wins / totalResolved) * 100);
    rateEl.innerText = `${rate}%`;
    
    if (rate < 20) {
      msgEl.innerText = "Keep going, consistency is key! (Hãy kiên trì, sự đều đặn là chìa khóa!)";
    } else if (rate <= 40) {
      msgEl.innerText = "You're getting solid traction. Keep pushing. (Bạn đang có đà tiến triển tốt. Hãy tiếp tục cố gắng.)";
    } else {
      msgEl.innerText = "You're a superstar! Excellent interview conversion. (Bạn là một ngôi sao! Tỷ lệ chuyển đổi phỏng vấn xuất sắc.)";
    }
  }
}

function renderCrawlStatus() {
  const status = AppState.crawlStatus;
  if (!status) return;

  const headline = document.getElementById("crawlHeadline");
  headline.innerHTML = `📥 <strong>${esc(status.newJobsToday)} new jobs</strong> found · ${esc(status.date)}`;

  // Render per-source chips
  const chipsContainer = document.getElementById("crawlChips");
  chipsContainer.innerHTML = status.sources
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
          <span>⚠️ ${esc(s.name)} (Crawl stalled: ${esc(timeAgo(s.lastRun) || "never run")})</span>
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
    showToast(`Crawler ${sourceName} recovered successfully!`, "success");
  }
  refreshAppState();
}

function renderPlatformFilters() {
  // Collect all unique discovery platforms + "All"
  const platforms = new Set(["All"]);
  // Crawl sources always appear
  if (AppState.crawlStatus && AppState.crawlStatus.sources) {
    AppState.crawlStatus.sources.forEach(s => platforms.add(s.name));
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

function renderBoard() {
  const columns = ["Saved", "Waiting to apply", "Applied", "Interview", "Offer", "Closed"];
  const container = document.getElementById("kanbanBoard");
  container.innerHTML = "";

  // Apply filters
  let filteredJobs = AppState.jobs;
  if (AppState.filters.platform !== "All") {
    filteredJobs = filteredJobs.filter(j => j.discovery_source === AppState.filters.platform);
  }

  columns.forEach(colName => {
    let colJobs = filteredJobs.filter(j => j.status === colName);

    // Apply sorting for Saved lane
    if (colName === "Saved") {
      const sortMethod = AppState.sorting.Saved;
      colJobs.sort((a, b) => {
        // Pin stays at the top always
        if (a.pinned && !b.pinned) return -1;
        if (!a.pinned && b.pinned) return 1;

        // Sub sorting within pinned and unpinned groups
        if (sortMethod === "Highest score") {
          return b.score - a.score;
        } else {
          // Most recent
          return new Date(b.saved_at) - new Date(a.saved_at);
        }
      });
    } else {
      // General sorting: Pinned first, then by date saved
      colJobs.sort((a, b) => {
        if (a.pinned && !b.pinned) return -1;
        if (!a.pinned && b.pinned) return 1;
        return new Date(b.saved_at) - new Date(a.saved_at);
      });
    }

    const isCollapsed = localStorage.getItem(`col_collapsed_${colName}`) === "true";

    const colEl = document.createElement("div");
    colEl.className = `column ${isCollapsed ? "" : "expanded"}`;
    colEl.id = `col-${colName.replace(/\s+/g, "-")}`;
    colEl.setAttribute("data-status", colName);

    // Column Header
    let sortSelectorHtml = "";
    if (colName === "Saved") {
      sortSelectorHtml = `
        <div class="column-sort" onclick="toggleSavedSort(event)">
          ${AppState.sorting.Saved} ▾
        </div>
      `;
    }

    colEl.innerHTML = `
      <div class="column-header" onclick="toggleColumnCollapse('${colName}')">
        <h2 class="column-title">${colName}</h2>
        <span class="column-count">${colJobs.length}</span>
      </div>
      ${sortSelectorHtml}
      <div class="column-cards" id="cards-${colName.replace(/\s+/g, "-")}"></div>
    `;

    const cardsContainer = colEl.querySelector(".column-cards");

    // Implement Divider for Saved Column
    if (colName === "Saved") {
      const pinnedJobs = colJobs.filter(j => j.pinned);
      const unpinnedJobs = colJobs.filter(j => !j.pinned);

      // Render Pinned
      pinnedJobs.forEach(job => renderJobCard(job, cardsContainer));

      // Render Divider if both exist
      if (pinnedJobs.length > 0 && unpinnedJobs.length > 0) {
        const divider = document.createElement("div");
        divider.className = "divider";
        divider.innerHTML = `<span>${unpinnedJobs.length} more unpinned</span>`;
        cardsContainer.appendChild(divider);
      }

      // Render Unpinned
      unpinnedJobs.forEach(job => renderJobCard(job, cardsContainer));
    } else {
      colJobs.forEach(job => renderJobCard(job, cardsContainer));
    }

    // Set up column drag events
    setupColumnDragEvents(colEl);

    container.appendChild(colEl);
  });
}

function toggleSavedSort(e) {
  e.stopPropagation();
  AppState.sorting.Saved = AppState.sorting.Saved === "Most recent" ? "Highest score" : "Most recent";
  renderBoard();
}

function toggleColumnCollapse(colName) {
  if (window.innerWidth > 768) return; // Only accordion on mobile
  const key = `col_collapsed_${colName}`;
  const isCollapsed = localStorage.getItem(key) === "true";
  localStorage.setItem(key, !isCollapsed ? "false" : "true");
  renderBoard();
}

// Render Job Card Component
function renderJobCard(job, container) {
  const cardEl = document.createElement("div");
  // Visual border accents
  let extraClass = "";
  if (job.pinned) extraClass += " pinned";
  if (job.apply_state === "sent") extraClass += " has-sent";
  
  cardEl.className = `card${extraClass}`;
  cardEl.setAttribute("draggable", "true");
  cardEl.setAttribute("data-id", job.id);

  // Setup drag start / end on card
  cardEl.addEventListener("dragstart", (e) => {
    cardEl.classList.add("dragging");
    e.dataTransfer.setData("text/plain", job.id);
  });
  cardEl.addEventListener("dragend", () => {
    cardEl.classList.remove("dragging");
  });

  // Calculate Dealbreakers
  const dealbreakers = [];
  if (job.jd_full && (job.jd_full.toLowerCase().includes("thứ 7") || job.jd_full.toLowerCase().includes("t7") || job.jd_full.toLowerCase().includes("saturday"))) {
    dealbreakers.push("⛔ Làm T7");
  }
  if (!job.salary || job.salary.includes("thỏa thuận") || job.salary.toLowerCase().includes("negotiable") || job.salary.toLowerCase().includes("cạnh tranh")) {
    dealbreakers.push("⛔ Lương thỏa thuận");
  }
  if (job.company_distance_km && parseFloat(job.company_distance_km) > 10) {
    dealbreakers.push(`⛔ Xa ${esc(job.company_distance_km)}km`);
  }
  if (job.company_active_date && job.company_active_date !== "🏢 …" && job.company_active_date !== "🏢 Unknown") {
    const year = new Date(job.company_active_date).getFullYear();
    if (2026 - year < 2) {
      dealbreakers.push(`⛔ Cty <2 năm (${year})`);
    }
  }

  const dealbreakersHtml = dealbreakers
    .map(db => `<span class="badge dealbreaker">${db}</span>`)
    .join("");

  // Score Badge
  let scoreClass = "score-pending";
  if (job.score_status === "scored") {
    if (job.score_letter === "A") scoreClass = "score-a";
    else if (job.score_letter === "B") scoreClass = "score-b";
    else if (job.score_letter === "C") scoreClass = "score-c";
    else scoreClass = "score-f";
  }

  let scoreHtml = "";
  if (job.score_status === "scoring") {
    scoreHtml = `<span class="score-badge score-pending" style="font-size:11px">scoring…</span>`;
  } else if (job.score_status === "failed") {
    scoreHtml = `<span class="score-badge score-f" style="font-size:11px; cursor:pointer;" onclick="retryScoring('${job.id}')" title="Scoring failed. Click to retry.">✕ failed</span>`;
  } else if (job.score_status === "stuck") {
    scoreHtml = `<span class="score-badge score-f" style="font-size:11px; cursor:pointer;" onclick="retryScoring('${job.id}')" title="Scoring hung. Click to force retry.">stuck·retry</span>`;
  } else {
    scoreHtml = `<span class="score-badge ${scoreClass}">${esc(job.score_letter)} ${esc(job.score)}</span>`;
  }

  // Compare Badges (A/B mode on Saved/Waiting cards)
  let compareBadgesHtml = "";
  if (AppState.ratingConfig.ab_mode && (job.status === "Saved" || job.status === "Waiting to apply")) {
    AppState.ratingConfig.models.forEach(model => {
      if (model !== AppState.ratingConfig.default_model) {
        const rating = job.ratings && job.ratings[model];
        if (rating) {
          if (rating.letter === "...") {
            compareBadgesHtml += `<span class="score-badge score-pending" style="font-size:10px; opacity:0.8;" title="${esc(model)}">${esc(model.substring(0,3))}:…</span>`;
          } else if (rating.letter === "✕") {
            compareBadgesHtml += `<span class="score-badge score-f" style="font-size:10px; opacity:0.8;" title="${esc(model)} failed">${esc(model.substring(0,3))}:✕</span>`;
          } else {
            let modelClass = rating.letter === "A" ? "score-a" : (rating.letter === "B" ? "score-b" : "score-c");
            compareBadgesHtml += `<span class="score-badge ${modelClass}" style="font-size:10px; opacity:0.8;" title="${esc(model)}">${esc(model.substring(0,3))}: ${esc(rating.letter)} ${esc(rating.score)}</span>`;
          }
        }
      }
    });
  }

  // Salary chip
  const salaryHtml = job.salary && !job.salary.includes("thỏa thuận") ? `<span class="salary-chip">💰 ${esc(job.salary)}</span>` : "";

  // Company active date (masothue link)
  let masothueHtml = "";
  if (job.company_active_date) {
    if (job.company_active_date === "🏢 …") {
      masothueHtml = `<span class="intel-badge">🏢 …</span>`;
    } else if (job.company_active_date === "🏢 Unknown") {
      masothueHtml = `<span class="intel-badge">🏢 Unknown</span>`;
    } else {
      const year = new Date(job.company_active_date).getFullYear();
      masothueHtml = `<a href="https://masothue.com/Search/?q=${encodeURIComponent(job.company)}" target="_blank" class="intel-badge" onclick="event.stopPropagation()">📅 ${year}</a>`;
    }
  }

  // Distance Intel Badge
  let distanceHtml = "";
  if (job.company_distance_km) {
    if (job.company_distance_km === "🚗 …") {
      distanceHtml = `<span class="intel-badge">🚗 …</span>`;
    } else {
      let icon = "✅";
      if (job.company_geo_status === "ok") icon = "🟡";
      if (job.company_geo_status === "far") icon = "🔴";
      distanceHtml = `<span class="intel-badge">${icon} ${esc(job.company_distance_km)}km</span>`;
    }
  } else {
    distanceHtml = `<span class="intel-badge" title="Unknown distance. Pin card to trigger lookup.">📍 ? km</span>`;
  }

  // Platform badges
  let platformStatusHtml = "";
  if (job.discovery_source === "TopCV" && job.topcv_status) {
    const labels = {
      seen: "👁 seen",
      reviewing: "📋 reviewing",
      considering: "🤔 considering",
      suitable: "✅ suitable",
      rejected: "❌ rejected"
    };
    platformStatusHtml = `<span class="apply-badge">${esc(labels[job.topcv_status] || job.topcv_status)}</span>`;
  } else if (job.discovery_source === "VietnamWorks" && job.vnw_status) {
    platformStatusHtml = `<span class="apply-badge">${job.vnw_status === "seen" ? "👁 seen" : esc(job.vnw_status)}</span>`;
  }

  // Age and expiry badge calculation
  const ageStr = timeAgo(job.saved_at);
  let expiryHtml = "";
  if (job.expires_at) {
    const deadline = new Date(job.expires_at);
    const now = new Date();
    const daysLeft = Math.ceil((deadline - now) / (1000 * 60 * 60 * 24));
    
    if (job.keep) {
      expiryHtml = `<span class="expiry-badge grey">🛟 kept</span>`;
    } else if (job.pinned) {
      expiryHtml = daysLeft < 0 ? `<span class="expiry-badge red">⛔ closed</span>` : `<span class="expiry-badge amber">📅 deadline</span>`;
    } else {
      if (daysLeft < 0) {
        const deleteIn = 7 + daysLeft; // Auto-deletes 7 days after expired
        expiryHtml = `<span class="expiry-badge red" title="Exempt this card by keeping or pinning it!">⛔ delete in ${deleteIn > 0 ? deleteIn : 0}d</span>`;
      } else if (daysLeft <= 10) {
        expiryHtml = `<span class="expiry-badge amber">⚠ ${daysLeft}d left</span>`;
      } else {
        expiryHtml = `<span class="expiry-badge grey">${deadline.toLocaleDateString('vi-VN', { day: 'numeric', month: 'short' })}</span>`;
      }
    }
  } else if (job.status === "Applied" || job.status === "Interview") {
    // Silence clock
    const saved = new Date(job.saved_at);
    const now = new Date();
    const daysApplied = Math.floor((now - saved) / (1000 * 60 * 60 * 24));
    
    if (job.keep) {
      expiryHtml = `<span class="expiry-badge grey">🛟 kept</span>`;
    } else if (job.pinned) {
      expiryHtml = `<span class="expiry-badge grey">⛔ no reply</span>`;
    } else if (daysApplied > 20) {
      const deleteIn = 7 - (daysApplied - 20);
      expiryHtml = `<span class="expiry-badge red">⛔ delete in ${deleteIn > 0 ? deleteIn : 0}d</span>`;
    } else {
      expiryHtml = `<span class="expiry-badge grey">📮 ${daysApplied}d</span>`;
    }
  }

  // Tickers/Automation state badge
  let automationHtml = "";
  if (job.status === "Waiting to apply") {
    if (job.cv_status === "tailoring") {
      const elapsed = formatDuration(Date.now() - new Date(job.saved_at).getTime() / 2); // simulated duration
      automationHtml = `<span class="apply-badge tailoring">✍️ Tailoring CV (${elapsed})</span>`;
    } else if (job.draft_status === "drafting") {
      automationHtml = `<span class="apply-badge tailoring">✍️ Drafting email...</span>`;
    }
  } else if (job.status === "Applied") {
    if (job.apply_state === "queued") {
      automationHtml = `<span class="apply-badge queued">🕒 queued</span>`;
    } else if (job.apply_state === "filling") {
      automationHtml = `<span class="apply-badge queued">✍️ filling…</span>`;
    }
  }

  if (job.apply_state === "failed") {
    automationHtml = `<span class="apply-badge failed" onclick="retryApply('${job.id}')" title="Platform apply failed. Click to retry.">✕ failed·retry</span>`;
  }

  // Sent markers
  let sentMarkersHtml = "";
  if (job.apply_state === "sent") {
    sentMarkersHtml = `<span class="apply-badge sent">✓ sent</span>`;
  } else if (job.apply_state === "replied") {
    sentMarkersHtml = `<span class="apply-badge sent">✓ replied</span>`;
  }
  if (job.followup_sent_at) {
    sentMarkersHtml += `<span class="apply-badge sent">📨 followup</span>`;
  }

  // Quick action buttons
  const isPinned = job.pinned;
  const pinTitle = isPinned ? "Unpin" : "Pin";
  const pinIcon = isPinned ? "📌" : "📍";
  
  let pinBtnHtml = "";
  if (job.status === "Saved") {
    pinBtnHtml = `<button class="icon-btn-sm" title="${pinTitle}" onclick="togglePin('${job.id}')">${pinIcon}</button>`;
  }

  // Move-to dropdown items
  const stages = ["Saved", "Waiting to apply", "Applied", "Interview", "Offer", "Closed"];
  const selectOptions = stages
    .map(st => `<option value="${st}" ${job.status === st ? "selected" : ""}>${st}</option>`)
    .join("");

  const isOpen = AppState.expandedCards.has(job.id);
  
  cardEl.innerHTML = `
    <div class="card-header">
      <div class="card-top-row">
        <div class="card-title-group">
          <a href="${safeUrl(job.url)}" target="_blank" rel="noopener noreferrer" class="card-title" onclick="event.stopPropagation()">${esc(job.title)}</a>
          <div class="card-company">${esc(job.company)}</div>
        </div>
        <div class="card-actions-group">
          <div style="display: flex; gap: 4px; align-items: center;">
            ${compareBadgesHtml}
            ${scoreHtml}
          </div>
          <div class="quick-actions">
            <a href="${safeUrl(job.url)}" target="_blank" rel="noopener noreferrer" class="icon-btn-sm" title="Open original link" onclick="event.stopPropagation()">↗</a>
            ${pinBtnHtml}
            <button class="icon-btn-sm" title="Delete job" style="color:var(--error);" onclick="deleteJobCard('${job.id}')">✕</button>
          </div>
        </div>
      </div>
    </div>
    
    <div class="card-meta">
      ${salaryHtml}
      ${masothueHtml}
      ${distanceHtml}
    </div>
    
    <div class="card-footer">
      <div class="footer-left">
        <span class="age-label">${esc(ageStr)} · ${esc(job.discovery_source)}</span>
        ${expiryHtml}
        ${automationHtml}
        ${sentMarkersHtml}
        ${platformStatusHtml}
        
        <!-- Quick stage jump dropdown (accessible alternative to drag) -->
        <select class="move-select" onchange="moveStageDropdown('${job.id}', this)" onclick="event.stopPropagation()">
          ${selectOptions}
        </select>
      </div>
      <button class="icon-btn expand-btn" title="Toggle Details" onclick="toggleCardDetail('${job.id}')">${isOpen ? "▴" : "▾"}</button>
    </div>
    
    <!-- Expanded Detail Panel -->
    <div class="card-detail ${isOpen ? "open" : ""}" id="detail-${job.id}">
      <!-- Details panel elements injected here -->
    </div>
  `;

  // Render detail panel contents if it's expanded
  if (isOpen) {
    renderCardDetailContents(job, cardEl.querySelector(".card-detail"));
  }

  container.appendChild(cardEl);
}

// Render expanded card detail panel contents
function renderCardDetailContents(job, detailContainer) {
  // 1. AI Assessment
  let assessmentHtml = "";
  if (job.score_reasoning) {
    const listItems = job.score_reasoning
      .split("\n")
      .map(line => line.trim().replace(/^•\s*/, ""))
      .filter(Boolean)
      .map(item => `<li>${esc(item)}</li>`)
      .join("");
    
    assessmentHtml = `
      <div class="detail-section">
        <div class="detail-title">AI Assessment (${esc(AppState.ratingConfig.default_model)})</div>
        <ul class="assessment-list">${listItems}</ul>
      </div>
    `;
  }

  // 2. Challenger Models list
  let otherModelsHtml = "";
  if (AppState.ratingConfig.ab_mode) {
    let list = "";
    AppState.ratingConfig.models.forEach(model => {
      if (model !== AppState.ratingConfig.default_model) {
        const rating = job.ratings && job.ratings[model];
        if (rating && rating.letter !== "...") {
          list += `
            <div style="margin-bottom: 6px;">
              <span class="score-badge ${rating.letter === "A" ? "score-a" : (rating.letter === "B" ? "score-b" : "score-c")}" style="font-size: 11px;">
                ${esc(model)}: ${esc(rating.letter)} ${esc(rating.score)}
              </span>
              <p style="font-size:12px; margin: 2px 0 0 0; color: var(--body); font-style: italic;">
                "${esc(rating.reasoning)}"
              </p>
            </div>
          `;
        }
      }
    });

    // Option to ask another model
    const availableOthers = AppState.ratingConfig.models.filter(m => !job.ratings || !job.ratings[m] || job.ratings[m].letter === "✕");
    let askSelectHtml = "";
    if (availableOthers.length > 0) {
      const options = availableOthers.map(m => `<option value="${esc(m)}">${esc(m)}</option>`).join("");
      askSelectHtml = `
        <div style="margin-top: 8px; display: flex; gap: 6px; align-items: center;">
          <select class="text-input" style="padding: 4px 8px; font-size:12px; width: auto;" id="askModelSelect-${job.id}">
            ${options}
          </select>
          <button class="btn-secondary btn-sm" style="padding: 4px 10px; font-size:12px;" onclick="askAnotherModel('${job.id}')">Ask Model</button>
        </div>
      `;
    }

    otherModelsHtml = `
      <div class="detail-section" style="border-top: 1px solid var(--hairline); padding-top: 8px;">
        <div class="detail-title">Challenger Ratings</div>
        ${list || '<div style="font-size:12px; color:var(--muted)">No challenger models have rated this job yet.</div>'}
        ${askSelectHtml}
      </div>
    `;
  }

  // 3. Skills
  let skillsHtml = "";
  if (job.skills && job.skills.length > 0) {
    const pills = job.skills.map(s => `<span class="skill-pill">${esc(s)}</span>`).join("");
    skillsHtml = `
      <div class="detail-section" style="border-top: 1px solid var(--hairline); padding-top: 8px;">
        <div class="detail-title">Skills Extracted</div>
        <div class="skills-list">${pills}</div>
      </div>
    `;
  }

  // 4. Job Summary / JD summary
  let summaryHtml = "";
  if (job.jd_ai_summary || job.jd_summary) {
    const content = job.jd_ai_summary
      ? job.jd_ai_summary.split("\n").map(l => `<li>${esc(l.trim().replace(/^•\s*/, ""))}</li>`).join("")
      : `<li>${esc(job.jd_summary)}</li>`;
    
    summaryHtml = `
      <div class="detail-section" style="border-top: 1px solid var(--hairline); padding-top: 8px;">
        <div class="detail-title">AI Summary</div>
        <ul class="assessment-list">${content}</ul>
      </div>
    `;
  }

  // 5. Notes block
  const notesHtml = `
    <div class="detail-section" style="border-top: 1px solid var(--hairline); padding-top: 8px;">
      <div class="detail-title">My Notes (Auto-saves)</div>
      <textarea class="textarea-input" style="height: 60px; font-size: 12px;" placeholder="Add custom notes..." oninput="saveJobNotes('${job.id}', this)">${esc(job.notes)}</textarea>
    </div>
  `;

  // 6. Application Method Block
  let applyBlockHtml = "";
  
  // Apply block only displays in Saved, Waiting, Applied lanes (in closed/interview we hide it except interview reply draft editor)
  const showApply = ["Saved", "Waiting to apply", "Applied"].includes(job.status);
  
  if (showApply) {
    if (job.apply_method === "on-site") {
      // Platform apply UI
      let btnLabel = "Approve CV — mark applied";
      let btnAction = `approveOnSiteApply('${job.id}')`;
      let undoBtn = "";
      
      if (job.status === "Applied") {
        btnLabel = "Application Submitted (Undo)";
        btnAction = `undoOnSiteApply('${job.id}')`;
        undoBtn = `<p style="font-size:12px; margin: 4px 0 0 0; color: var(--success-border)">✓ CV Approved and forwarded to the apply engine.</p>`;
      }
      
      applyBlockHtml = `
        <div class="detail-section" style="border-top: 1px dashed var(--border-strong); padding-top: 12px; margin-top: 4px;">
          <div class="detail-title">🌐 On-Site Application (${esc(job.discovery_source)})</div>
          <div style="margin-top: 6px;">
            <button class="btn-primary btn-sm" onclick="${btnAction}">${btnLabel}</button>
            ${undoBtn}
          </div>
        </div>
      `;
    } else {
      // Email apply UI (recipient field, editor body)
      let emailEditorHtml = "";
      
      if (job.cv_status === "tailoring") {
        emailEditorHtml = `<p style="font-size:12px; color:var(--muted)">Tailoring CV and drafting email... Please wait.</p>`;
      } else {
        const draft = job.draft_email || { to: job.contact_email || "", subject: "", body: "" };
        const isSent = job.draft_status === "sent";
        const readOnlyAttr = isSent ? "readonly" : "";
        
        let draftControls = `
          <div style="display: flex; gap: var(--spacing-xs); margin-top: 8px;">
            <button class="btn-primary btn-sm" onclick="approveEmailDraft('${job.id}')">Approve & Send</button>
            <button class="btn-secondary btn-sm" onclick="reDraftEmailTrigger('${job.id}')">Re-draft</button>
            <button class="btn-danger btn-sm" style="padding: 6px 12px; font-size:12px;" onclick="rejectEmailDraft('${job.id}')">Reject Draft</button>
          </div>
          <div style="margin-top: 10px; border-top: 1px dashed var(--hairline); padding-top: 8px;">
            <label style="font-size:11px; font-weight:600; color:var(--muted); text-transform:uppercase;">Draft Guide Notes (Tone/Availability)</label>
            <textarea class="textarea-input" id="draftNotes-${job.id}" style="height:40px; font-size:12px; margin-top:4px;" placeholder="Hints to steer re-draft (e.g. emphasize SQL, polite tone)..."></textarea>
          </div>
        `;
        
        if (isSent) {
          draftControls = `<p style="font-size:12px; color: var(--success-border); font-weight: 500; margin-top: 6px;">✓ Email successfully approved & sent.</p>`;
        }
        
        emailEditorHtml = `
          <div style="margin-top: 6px; display: flex; flex-direction: column; gap: 6px;">
            <div>
              <label style="font-size:11px; font-weight:600; color:var(--muted)">Recipient</label>
              <input type="text" id="draftTo-${job.id}" class="text-input" value="${esc(draft.to)}" ${readOnlyAttr} style="font-size:12px; padding: 4px 8px;">
            </div>
            <div>
              <label style="font-size:11px; font-weight:600; color:var(--muted)">Subject</label>
              <input type="text" id="draftSubject-${job.id}" class="text-input" value="${esc(draft.subject || `Ứng tuyển vị trí ${job.title} - CV tailored`)}" ${readOnlyAttr} style="font-size:12px; padding: 4px 8px;">
            </div>
            <div>
              <label style="font-size:11px; font-weight:600; color:var(--muted)">Email Body</label>
              <textarea id="draftBody-${job.id}" class="textarea-input" style="height: 120px; font-size: 12px;" ${readOnlyAttr}>${esc(draft.body)}</textarea>
            </div>
            ${draftControls}
          </div>
        `;
      }

      applyBlockHtml = `
        <div class="detail-section" style="border-top: 1px dashed var(--border-strong); padding-top: 12px; margin-top: 4px;">
          <div class="detail-title">✉ Email Application</div>
          ${emailEditorHtml}
        </div>
      `;
    }
  } else if (job.status === "Interview") {
    // Shows interview-reply draft editor
    let emailEditorHtml = "";
    const draft = job.draft_email || { to: job.contact_email || "", subject: `Trả lời lịch phỏng vấn - ${job.company}`, body: `Kính gửi ${job.company} HR,\n\nTôi đã nhận được thư mời phỏng vấn. Tôi xin xác nhận lịch hẹn vào lúc...` };
    const isSent = job.draft_status === "replied-sent";
    const readOnlyAttr = isSent ? "readonly" : "";

    let draftControls = `
      <div style="display: flex; gap: var(--spacing-xs); margin-top: 8px;">
        <button class="btn-primary btn-sm" onclick="sendInterviewReply('${job.id}')">Approve & Send Reply</button>
      </div>
    `;
    if (isSent) {
      draftControls = `<p style="font-size:12px; color: var(--success-border); font-weight: 500; margin-top: 6px;">✓ Reply sent successfully.</p>`;
    }

    emailEditorHtml = `
      <div style="margin-top: 6px; display: flex; flex-direction: column; gap: 6px;">
        <div>
          <label style="font-size:11px; font-weight:600; color:var(--muted)">Recipient</label>
          <input type="text" id="replyTo-${job.id}" class="text-input" value="${esc(draft.to)}" ${readOnlyAttr} style="font-size:12px; padding: 4px 8px;">
        </div>
        <div>
          <label style="font-size:11px; font-weight:600; color:var(--muted)">Email Body</label>
          <textarea id="replyBody-${job.id}" class="textarea-input" style="height: 80px; font-size: 12px;" ${readOnlyAttr}>${esc(draft.body)}</textarea>
        </div>
        ${draftControls}
      </div>
    `;

    applyBlockHtml = `
      <div class="detail-section" style="border-top: 1px dashed var(--border-strong); padding-top: 12px; margin-top: 4px;">
        <div class="detail-title">📨 Recruiter Reply Draft</div>
        ${emailEditorHtml}
      </div>
    `;
  }

  // 7. CV controls & PDF download (Waiting/Applied only)
  let cvControlsHtml = "";
  if (job.status === "Waiting to apply" || job.status === "Applied") {
    let cvStatusText = "Tailored CV: Not generated yet";
    let cvButtons = `<button class="btn-secondary btn-sm" onclick="triggerCvTailor('${job.id}')">Generate Tailored CV</button>`;

    if (job.cv_status === "tailoring") {
      cvStatusText = "Tailored CV: ✍️ Tailoring in progress...";
      cvButtons = "";
    } else if (job.cv_status === "ready") {
      cvStatusText = `Tailored CV: Ready (${esc(job.cv_version)}) · ${new Date(job.cv_generated_at).toLocaleTimeString('vi-VN', { hour: '2-digit', minute: '2-digit' })}`;
      cvButtons = `
        <a href="${safeUrl(job.cv_path)}" download class="btn-secondary btn-sm" onclick="event.stopPropagation(); showToast('Downloading CV.pdf...', 'info');">Download CV.pdf</a>
        <button class="btn-secondary btn-sm" onclick="triggerCvTailor('${job.id}')">Re-draft CV</button>
      `;
    }

    cvControlsHtml = `
      <div class="detail-section" style="border-top: 1px solid var(--hairline); padding-top: 8px;">
        <div class="detail-title">CV Controls</div>
        <p style="font-size:12px; margin: 2px 0 6px 0; color: var(--body);">${cvStatusText}</p>
        <div style="display: flex; gap: var(--spacing-xs);">
          ${cvButtons}
        </div>
      </div>
    `;
  }

  // 8. Keep exemption button (exposes the escape hatch to auto-clear)
  let keepButtonHtml = "";
  if (job.expires_at || job.status === "Applied" || job.status === "Interview") {
    keepButtonHtml = `
      <div class="detail-section" style="border-top: 1px solid var(--hairline); padding-top: 8px;">
        <div style="display:flex; justify-content: space-between; align-items: center;">
          <span style="font-size:12px; color:var(--muted)">Auto-clear exemption</span>
          <button class="btn-secondary btn-sm" onclick="toggleKeepExemption('${job.id}')">
            ${job.keep ? "🛟 Kept (Exempt)" : "Exempt Card from Auto-delete"}
          </button>
        </div>
      </div>
    `;
  }

  detailContainer.innerHTML = `
    ${assessmentHtml}
    ${otherModelsHtml}
    ${skillsHtml}
    ${summaryHtml}
    ${notesHtml}
    ${cvControlsHtml}
    ${applyBlockHtml}
    ${keepButtonHtml}
  `;
}

// Actions & Event Triggers
function toggleCardDetail(id) {
  if (AppState.expandedCards.has(id)) {
    AppState.expandedCards.delete(id);
  } else {
    AppState.expandedCards.add(id);
  }
  renderBoard();
}

async function togglePin(id) {
  await APIClient.pinJob(id);
  refreshAppState();
}

async function deleteJobCard(id) {
  if (confirm("Are you sure you want to delete this job card?")) {
    await APIClient.deleteJob(id);
    AppState.expandedCards.delete(id);
    refreshAppState();
  }
}

async function toggleKeepExemption(id) {
  await APIClient.keepJob(id);
  refreshAppState();
}

async function retryScoring(id) {
  await APIClient.rescore(id);
  refreshAppState();
}

async function askAnotherModel(id) {
  const select = document.getElementById(`askModelSelect-${id}`);
  if (select) {
    const model = select.value;
    await APIClient.askModel(id, model);
    refreshAppState();
  }
}

async function triggerCvTailor(id) {
  await APIClient.generateCV(id);
  refreshAppState();
}

async function reDraftEmailTrigger(id) {
  const textNotes = document.getElementById(`draftNotes-${id}`).value;
  await APIClient.draftEmail(id, { notes: textNotes });
  refreshAppState();
}

async function rejectEmailDraft(id) {
  await APIClient.rejectDraft(id);
  refreshAppState();
}

async function approveEmailDraft(id) {
  const to = document.getElementById(`draftTo-${id}`).value;
  const subject = document.getElementById(`draftSubject-${id}`).value;
  const body = document.getElementById(`draftBody-${id}`).value;
  
  await APIClient.approveDraft(id, { to, subject, body });
  showToast("Application email approved and sent!", "success");
  refreshAppState();
}

async function sendInterviewReply(id) {
  // Email sending isn't wired into braveduck-cli yet; say so rather than
  // pretending the reply went out.
  showToast("Sending email isn't available in this build yet.", "error");
}

async function approveOnSiteApply(id) {
  await APIClient.applyJob(id);
  refreshAppState();
}

async function undoOnSiteApply(id) {
  await transitionCardStatus(id, "Waiting to apply");
}

async function retryApply(id) {
  await APIClient.applyJob(id);
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
  await transitionCardStatus(id, status);
}

async function transitionCardStatus(id, newStatus) {
  const j = AppState.jobs.find(item => item.id === id);
  if (!j || j.status === newStatus) return;
  try {
    await APIClient.updateJob(id, { status: newStatus });
    showToast(`Job moved to ${newStatus}`, "success");
  } catch (e) {
    showToast(`Couldn't move job: ${e.message}`, "error");
  }
  refreshAppState();
}

// Toast alerts utility
function showToast(message, type = "success") {
  const container = document.getElementById("toastContainer");
  const toast = document.createElement("div");
  toast.className = `toast ${type}`;
  toast.innerHTML = `<span>${type === 'success' ? '✅' : (type === 'error' ? '❌' : 'ℹ️')}</span> ${esc(message)}`;
  container.appendChild(toast);
  
  // Show animation
  setTimeout(() => {
    toast.classList.add("show");
  }, 10);
  
  // Dismiss after 3s
  setTimeout(() => {
    toast.classList.remove("show");
    setTimeout(() => {
      toast.remove();
    }, 300);
  }, 3000);
}

// Activity Logs Feed Render
function renderActivityLog() {
  const feed = document.getElementById("logFeed");
  const activity = AppState.activity;

  if (activity.length === 0) {
    feed.innerHTML = `<div class="log-item"><span class="log-time">—</span> <span>No system activity logged yet.</span></div>`;
    return;
  }

  feed.innerHTML = activity.slice(0, 5).map(act => {
    let icon = "✅";
    if (act.type === "error") icon = "❌";
    if (act.type === "info") icon = "🔄";
    
    return `
      <div class="log-item">
        <span class="log-time">${esc(act.timestamp)}</span>
        <span class="log-icon">${icon}</span>
        <span>${esc(act.message)}</span>
      </div>
    `;
  }).join("");
}

// Helpers
function timeAgo(dateString) {
  if (!dateString) return "";
  const date = new Date(dateString);
  const now = new Date();
  const diffMs = now - date;
  const diffSec = Math.floor(diffMs / 1000);
  const diffMin = Math.floor(diffSec / 60);
  const diffHr = Math.floor(diffMin / 60);
  const diffDays = Math.floor(diffHr / 24);

  if (diffSec < 60) return "just now";
  if (diffMin < 60) return `${diffMin}m`;
  if (diffHr < 24) return `${diffHr}h`;
  return `${diffDays}d`;
}

function formatDuration(ms) {
  if (ms < 0) ms = 0;
  const totalSecs = Math.floor(ms / 1000);
  const m = Math.floor(totalSecs / 60).toString().padStart(2, '0');
  const s = (totalSecs % 60).toString().padStart(2, '0');
  return `${m}:${s}`;
}

// Setup Event Listeners on DOM Load
document.addEventListener("DOMContentLoaded", () => {
  // Toggle Drawer
  const drawer = document.getElementById("addJobDrawer");
  const overlay = document.getElementById("drawerOverlay");
  const openBtn = document.getElementById("openDrawerBtn");
  const closeBtn = document.getElementById("closeDrawerBtn");

  function openDrawer() {
    drawer.classList.add("open");
    overlay.classList.add("open");
  }

  function closeDrawer() {
    drawer.classList.remove("open");
    overlay.classList.remove("open");
    // Clear extract results banner
    const banner = document.getElementById("extractResultBanner");
    banner.style.display = "none";
  }

  openBtn.addEventListener("click", openDrawer);
  closeBtn.addEventListener("click", closeDrawer);
  overlay.addEventListener("click", closeDrawer);

  // Extract & Add Job Button Event
  const extractBtn = document.getElementById("extractAddBtn");
  const pasteTextarea = document.getElementById("pasteJDTextarea");
  const extractResultBanner = document.getElementById("extractResultBanner");

  extractBtn.addEventListener("click", async () => {
    const content = pasteTextarea.value;
    try {
      extractBtn.disabled = true;
      extractBtn.innerText = "Extracting details...";
      
      const newJob = await APIClient.addJob(content);
      
      extractResultBanner.className = "success";
      extractResultBanner.style.backgroundColor = "#dcfce7";
      extractResultBanner.style.color = "#166534";
      extractResultBanner.style.border = "1px solid #bbf7d0";
      extractResultBanner.innerHTML = `✨ <strong>Job Added Successfully (#${newJob.id})</strong><br>Extracted: ${esc(newJob.title)} at ${esc(newJob.company)}.`;
      extractResultBanner.style.display = "block";
      pasteTextarea.value = "";
      
      showToast("Job added to Saved queue!", "success");
      refreshAppState();
    } catch (err) {
      extractResultBanner.className = "error";
      extractResultBanner.style.backgroundColor = "#fee2e2";
      extractResultBanner.style.color = "#991b1b";
      extractResultBanner.style.border = "1px solid #fecaca";
      
      if (err.message === "duplicate") {
        extractResultBanner.innerText = "⚠️ Already in tracker: This job listing already exists in your Kanban pipeline.";
      } else {
        extractResultBanner.innerText = `❌ Couldn't add this job: ${err.message}`;
      }
      extractResultBanner.style.display = "block";
    } finally {
      extractBtn.disabled = false;
      extractBtn.innerText = "Extract & Add";
    }
  });

  // Keywords Config Modal
  const keywordsModal = document.getElementById("keywordsModalOverlay");
  const keywordsBtn = document.getElementById("keywordsBtn");
  const saveKeywordsBtn = document.getElementById("saveKeywordsBtn");

  keywordsBtn.addEventListener("click", async () => {
    const kw = await APIClient.getKeywords();
    const container = document.getElementById("keywordsForm");
    
    container.innerHTML = Object.keys(kw).map(source => `
      <div class="form-group">
        <label for="kw-${esc(source)}">${esc(source)} Crawler Keywords</label>
        <textarea id="kw-${esc(source)}" class="textarea-input" style="height: 60px;" oninput="previewKeywordMatches()">${esc(kw[source].join(", "))}</textarea>
      </div>
    `).join("");

    previewKeywordMatches();
    keywordsModal.classList.add("open");
  });

  saveKeywordsBtn.addEventListener("click", async () => {
    const kw = {};
    document.querySelectorAll("#keywordsForm textarea").forEach(ta => {
      const source = ta.id.replace("kw-", "");
      kw[source] = ta.value.split(",").map(w => w.trim()).filter(Boolean);
    });
    
    await APIClient.updateKeywords(kw);
    keywordsModal.classList.remove("open");
    showToast("Keyword configurations saved!", "success");
    refreshAppState();
  });

  // Rating Config Modal
  const ratingModal = document.getElementById("ratingModalOverlay");
  const ratingBtn = document.getElementById("ratingBtn");
  const saveRatingBtn = document.getElementById("saveRatingConfigBtn");

  ratingBtn.addEventListener("click", async () => {
    const config = await APIClient.getRatingConfig();
    document.getElementById("abModeCheckbox").checked = config.ab_mode;
    document.getElementById("defaultModelSelect").value = config.default_model;

    const challengerContainer = document.getElementById("challengerModelsList");
    const allModels = ["GPT-4o", "Claude 3.5", "Gemini 1.5 Pro"];
    
    challengerContainer.innerHTML = allModels.map(m => `
      <label class="filter-cb">
        <input type="checkbox" name="challengerModel" value="${esc(m)}" ${config.models.includes(m) ? "checked" : ""}>
        ${esc(m)}
      </label>
    `).join("");

    ratingModal.classList.add("open");
  });

  saveRatingBtn.addEventListener("click", async () => {
    const abMode = document.getElementById("abModeCheckbox").checked;
    const defaultModel = document.getElementById("defaultModelSelect").value;
    
    const models = [];
    document.querySelectorAll('input[name="challengerModel"]:checked').forEach(cb => {
      models.push(cb.value);
    });
    // Ensure default model is always inside the list
    if (!models.includes(defaultModel)) {
      models.push(defaultModel);
    }

    const config = {
      default_model: defaultModel,
      ab_mode: abMode,
      models: models
    };
    
    const prevConfig = AppState.ratingConfig;
    await APIClient.updateRatingConfig(config);
    ratingModal.classList.remove("open");
    showToast("Rating config updated!", "success");
    
    // Check if backfill banner needs to be displayed (A/B toggled on)
    if (abMode && !prevConfig.ab_mode) {
      document.getElementById("backfillBanner").style.display = "flex";
    }
    
    refreshAppState();
  });

  // Backfill Banner handlers
  document.getElementById("backfillConfirmBtn").addEventListener("click", async () => {
    document.getElementById("backfillBanner").style.display = "none";
    showToast("Initiating A/B backfill rating process...", "info");
    await APIClient.backfillRatings();
  });
  
  document.getElementById("backfillDismissBtn").addEventListener("click", () => {
    document.getElementById("backfillBanner").style.display = "none";
  });

  // General Modal close logic
  document.querySelectorAll(".modal-close-btn").forEach(btn => {
    btn.addEventListener("click", () => {
      keywordsModal.classList.remove("open");
      ratingModal.classList.remove("open");
    });
  });

  // Periodic Intervals
  // 1. Tickers and timers refresh every 1 second
  setInterval(() => {
    const waitingCards = document.querySelectorAll("#col-Waiting-to-apply .card");
    waitingCards.forEach(card => {
      const id = card.getAttribute("data-id");
      const job = AppState.jobs.find(j => j.id === id);
      if (job) {
        if (job.cv_status === "tailoring") {
          // Live render elapsed ticker in place without full refresh to preserve scroll/input state
          const badge = card.querySelector(".apply-badge.tailoring");
          if (badge) {
            const elapsed = formatDuration(Date.now() - new Date(job.saved_at).getTime() / 2);
            badge.innerText = `✍️ Tailoring CV (${elapsed})`;
          }
        }
      }
    });
  }, 1000);

  // 2. Activity feed: poll the server's real event log. Only the feed is
  // re-rendered, so a half-typed note or open card is never disturbed.
  setInterval(async () => {
    if (document.hidden) return;
    AppState.activity = await APIClient.getActivity();
    renderActivityLog();
  }, 15000);

  // Initial load
  refreshAppState();
});

// Keywords Change Match Preview
function previewKeywordMatches() {
  const previewBox = document.getElementById("keywordsPreviewList");
  const jobs = AppState.jobs;
  
  // Compile active keywords from textareas
  const kwList = [];
  document.querySelectorAll("#keywordsForm textarea").forEach(ta => {
    const vals = ta.value.split(",").map(v => v.trim()).filter(Boolean);
    kwList.push(...vals);
  });

  if (kwList.length === 0) {
    previewBox.innerText = "No keywords entered. Match count: 0 jobs.";
    return;
  }

  const matches = jobs.filter(j => {
    const text = `${j.title} ${j.company} ${j.jd_full || ""}`.toLowerCase();
    return kwList.some(kw => text.includes(kw.toLowerCase()));
  });

  if (matches.length === 0) {
    previewBox.innerText = "0 matches found across current jobs database.";
  } else {
    previewBox.innerHTML = `
      <strong>${matches.length} matching jobs found:</strong>
      <ul style="margin: 4px 0 0 0; padding-left: 16px;">
        ${matches.map(j => `<li>${esc(j.title)} at ${esc(j.company)} (${esc(j.discovery_source)})</li>`).join("")}
      </ul>
    `;
  }
}
