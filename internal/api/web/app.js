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

function parseDraft(raw) {
  if (!raw) return null;
  if (typeof raw === "object") return raw;
  try { return JSON.parse(raw); } catch { return null; }
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
    // The store keeps the draft as JSON {to, subject, body}; "" = none.
    draft_email: parseDraft(j.draft_email),
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
      // 409 gets its own banner naming the jobs it matches; anything else (a
      // too-short paste, a link from another site, ...) surfaces the server's
      // message verbatim.
      const body = await res.json().catch(() => ({}));
      const err = new Error(body.error || `HTTP ${res.status}`);
      if (res.status === 409) err.duplicates = body.duplicates || [];
      throw err;
    }
    return res.json();
  },

  // rate / cv / draft return {prompt, brief}: the request for the user's AI.
  async askRequest(kind, id, body) {
    const res = await apiFetch(`${API_BASE}/jobs/${id}/${kind}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body || {})
    });
    const out = await res.json();
    if (!res.ok) throw new Error(out.error || `HTTP ${res.status}`);
    return out;
  },

  async put(path, body) {
    const res = await apiFetch(`${API_BASE}/${path}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body)
    });
    const out = await res.json();
    if (!res.ok) throw new Error(out.error || `HTTP ${res.status}`);
    return out;
  },

  async post(path) {
    const res = await apiFetch(`${API_BASE}/${path}`, { method: "POST" });
    const out = await res.json();
    if (!res.ok) throw new Error(out.error || `HTTP ${res.status}`);
    return out;
  },




  async keepJob(id) {
    const res = await apiFetch(`${API_BASE}/jobs/${id}/keep`, { method: "POST" });
    return res.json();
  },

  async pinJob(id) {
    const res = await apiFetch(`${API_BASE}/jobs/${id}/pin`, { method: "POST" });
    return res.json();
  },

  async getPlatforms() {
    const res = await apiFetch(`${API_BASE}/platforms`);
    return res.ok ? res.json() : [];
  },

  async connectPlatform(id) {
    const res = await apiFetch(`${API_BASE}/platforms/${encodeURIComponent(id)}/login`, { method: "POST" });
    const body = await res.json();
    if (!res.ok) throw new Error(body.error || `HTTP ${res.status}`);
    return body;
  },

  async syncPlatform(id) {
    const res = await apiFetch(`${API_BASE}/platforms/${encodeURIComponent(id)}/sync`, { method: "POST" });
    const body = await res.json();
    if (!res.ok) throw new Error(body.error || `HTTP ${res.status}`);
    return body;
  },

  async disconnectPlatform(id) {
    const res = await apiFetch(`${API_BASE}/platforms/${encodeURIComponent(id)}/login`, { method: "DELETE" });
    const body = await res.json();
    if (!res.ok) throw new Error(body.error || `HTTP ${res.status}`);
    return body;
  },

  async getCrawlSchedule() {
    const res = await apiFetch(`${API_BASE}/crawl-schedule`);
    return res.ok ? res.json() : [];
  },

  async setCrawlSchedule(source, everyMinutes) {
    const res = await apiFetch(`${API_BASE}/crawl-schedule/${encodeURIComponent(source)}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ every_minutes: everyMinutes })
    });
    const body = await res.json();
    if (!res.ok) throw new Error(body.error || `HTTP ${res.status}`);
    return body;
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
    const body = await res.json();
    if (!res.ok) throw new Error(body.error || `HTTP ${res.status}`);
    return body;
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
    const body = await res.json();
    if (!res.ok) throw new Error(body.error || `HTTP ${res.status}`);
    return body;
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
  }
};

// UI Rendering Logic
// --- Job-site logins panel -----------------------------------------------------
const PLATFORM_STATUS = {
  not_connected: ["Not connected", "var(--muted)"],
  connecting: ["Waiting for you to log in in the browser window…", "#b45309"],
  connected: ["Logged in ✓", "var(--success-border)"],
  expired: ["Logged out — connect again", "var(--error)"],
  unknown: ["Saved (login check not verified yet)", "#b45309"]
};

async function renderPlatforms() {
  const list = document.getElementById("platformsList");
  const platforms = await APIClient.getPlatforms();
  list.innerHTML = platforms.map(p => {
    const [label, color] = PLATFORM_STATUS[p.status] || [p.status, "var(--muted)"];
    const saved = p.status !== "not_connected" && p.status !== "connecting";
    const sync = p.id === "topcv" && p.status === "connected"
      ? `<button class="btn-secondary btn-sm" onclick="syncPlatform(${jsArg(p.id)}, this)">Sync applications</button>` : "";
    const action = p.status === "connecting" ? ""
      : saved
        ? `${sync}<button class="btn-secondary btn-sm" onclick="disconnectPlatform(${jsArg(p.id)})">Disconnect</button>
           <button class="btn-secondary btn-sm" onclick="connectPlatform(${jsArg(p.id)})">Log in again</button>`
        : `<button class="btn-primary btn-sm" onclick="connectPlatform(${jsArg(p.id)})">Connect</button>`;
    return `
      <div style="display: flex; justify-content: space-between; align-items: center; gap: 8px; padding: 8px 0; border-bottom: 1px solid var(--hairline);">
        <div>
          <strong>${esc(p.name)}</strong>${p.account ? ` <span style="font-size: 12px; color: var(--muted);">${esc(p.account)}</span>` : ""}
          <div style="font-size: 12px; color: ${color};">${esc(label)}</div>
        </div>
        <div style="display: flex; gap: 6px;">${action}</div>
      </div>`;
  }).join("");
}

// --- Location window (browser location, address search, Leaflet map) ------------
const LEAFLET = {
  css: ["https://unpkg.com/leaflet@1.9.4/dist/leaflet.css", "sha256-p4NxAoJBhIIN+hmNHrzRCf9tD/miZyoHS5obTRR9BMY="],
  js: ["https://unpkg.com/leaflet@1.9.4/dist/leaflet.js", "sha256-20nQCchB9co0qIjJZRGuk2/Z9VM+kNiyxNV1lvTlZBo="]
};
let leafletReady = null, locMap = null, locMarker = null, picked = null;

// Leaflet loads only when the window opens (pinned version + integrity hash),
// so the board contacts nobody unless the user wants the map.
function loadLeaflet() {
  if (!leafletReady) {
    leafletReady = new Promise((resolve, reject) => {
      const css = Object.assign(document.createElement("link"), { rel: "stylesheet", href: LEAFLET.css[0], integrity: LEAFLET.css[1], crossOrigin: "" });
      const js = Object.assign(document.createElement("script"), { src: LEAFLET.js[0], integrity: LEAFLET.js[1], crossOrigin: "" });
      js.onload = resolve;
      js.onerror = () => { leafletReady = null; reject(new Error("map library didn't load (offline?)")); };
      document.head.append(css, js);
    });
  }
  return leafletReady;
}

function pickPoint(lat, lng, label) {
  picked = { lat, lng, label: label || "" };
  document.getElementById("locationPicked").textContent =
    `Pin: ${label ? label + " — " : ""}${lat.toFixed(5)}, ${lng.toFixed(5)}`;
  if (locMap) {
    if (!locMarker) locMarker = L.marker([lat, lng]).addTo(locMap);
    locMarker.setLatLng([lat, lng]);
    locMap.setView([lat, lng], Math.max(locMap.getZoom(), 13));
  }
}

async function openLocationWindow() {
  document.getElementById("locationModalOverlay").classList.add("open");
  const cur = AppState.location || {};
  document.getElementById("maxKmInput").value = cur.max_km || 10;
  document.getElementById("placeResults").innerHTML = "";
  picked = cur.set ? { lat: cur.lat, lng: cur.lng, label: cur.label } : null;
  document.getElementById("locationPicked").textContent = cur.set
    ? `Saved: ${cur.label || ""} (${cur.lat.toFixed(5)}, ${cur.lng.toFixed(5)})` : "Click the map to place your pin.";
  try {
    await loadLeaflet();
  } catch (e) {
    document.getElementById("locationMap").textContent = `Map unavailable: ${e.message}. You can still search or use your current location.`;
    return;
  }
  if (!locMap) {
    locMap = L.map("locationMap").setView(cur.set ? [cur.lat, cur.lng] : [16, 106], cur.set ? 13 : 5);
    L.tileLayer("https://tile.openstreetmap.org/{z}/{x}/{y}.png", {
      maxZoom: 19, attribution: "&copy; OpenStreetMap contributors"
    }).addTo(locMap);
    locMap.on("click", e => pickPoint(e.latlng.lat, e.latlng.lng, ""));
  }
  setTimeout(() => locMap.invalidateSize(), 50);
  if (cur.set) pickPoint(cur.lat, cur.lng, cur.label);
}

function useMyLocation() {
  if (!navigator.geolocation) {
    showToast("This browser can't share its location — search or click the map instead.", "error");
    return;
  }
  navigator.geolocation.getCurrentPosition(
    pos => pickPoint(pos.coords.latitude, pos.coords.longitude, "My current location"),
    err => showToast(err.code === 1 ? "Location permission denied — search or click the map instead." : `Couldn't get your location: ${err.message}`, "error"),
    { enableHighAccuracy: false, timeout: 15000 }
  );
}

async function searchPlace() {
  const q = document.getElementById("placeSearch").value.trim();
  const box = document.getElementById("placeResults");
  box.innerHTML = `<span style="font-size:12px; color:var(--muted)">Searching…</span>`;
  try {
    const res = await apiFetch(`${API_BASE}/places?q=${encodeURIComponent(q)}`);
    const places = await res.json();
    if (!res.ok) throw new Error(places.error || `HTTP ${res.status}`);
    box.innerHTML = places.length ? places.map((p, i) =>
      `<button class="btn-secondary btn-sm" style="text-align:left;" data-i="${i}">${esc(p.label)}</button>`).join("")
      : `<span style="font-size:12px; color:var(--muted)">No match. Try adding the city.</span>`;
    box.querySelectorAll("button").forEach(b => b.addEventListener("click", () => {
      const p = places[Number(b.dataset.i)];
      pickPoint(p.lat, p.lng, p.label);
    }));
  } catch (e) {
    box.innerHTML = `<span style="font-size:12px; color:var(--error)">${esc(e.message)}</span>`;
  }
}

async function saveLocation() {
  if (!picked) {
    showToast("Pick a place first: your current location, a search result, or a click on the map.", "error");
    return;
  }
  try {
    await APIClient.put("location", { ...picked, max_km: Number(document.getElementById("maxKmInput").value) || 0 });
    document.getElementById("locationModalOverlay").classList.remove("open");
    showToast("Location saved — job distances update shortly.", "success");
  } catch (e) {
    showToast(`Couldn't save: ${e.message}`, "error");
  }
  refreshAppState();
}

async function clearLocation() {
  if (!confirm("Forget your location and all job distances?")) return;
  await apiFetch(`${API_BASE}/location`, { method: "DELETE" });
  picked = null;
  if (locMarker) { locMarker.remove(); locMarker = null; }
  document.getElementById("locationModalOverlay").classList.remove("open");
  refreshAppState();
}

// Readable names for keyword lists (keys mirror internal/core/keyword.Lists).
const KEYWORD_LABELS = {
  search_queries: "Search queries — what crawlers search for (comma-separated)",
  positive_title: "Keep only titles containing one of these",
  negative_title: "Drop if the title contains",
  negative_content: "Drop if the title or description contains",
  negative_company: "Drop these companies"
};

// --- Job sources (schedule) panel -----------------------------------------------
const EVERY_CHOICES = [[0, "Off"], [5, "Every 5 min"], [15, "Every 15 min"], [30, "Every 30 min"], [60, "Every hour"],
  [120, "Every 2 hours"], [360, "Every 6 hours"], [720, "Every 12 hours"], [1440, "Once a day"]];

async function renderSources() {
  const list = document.getElementById("sourcesList");
  const [scheds, status] = await Promise.all([APIClient.getCrawlSchedule(), APIClient.getCrawlStatus()]);
  const health = {};
  ((status && status.sources) || []).forEach(s => { health[s.name] = s; });
  list.innerHTML = scheds.map(sc => {
    const h = health[sc.source];
    const state = !h ? "never run" : h.healthy ? `ok · ${h.count} new today` : `⚠️ ${h.lastError || "no recent run"}`;
    // Built-ins hit someone else's server: no 5-minute option.
    const choices = EVERY_CHOICES.filter(([m]) => !(sc.builtin && m > 0 && m < 15));
    if (!choices.some(([m]) => m === sc.every_minutes)) choices.push([sc.every_minutes, `Every ${sc.every_minutes} min`]);
    const options = choices.map(([m, label]) =>
      `<option value="${m}" ${m === sc.every_minutes ? "selected" : ""}>${esc(label)}</option>`).join("");
    const unset = !sc.builtin && !sc.configured ? `<option value="" selected disabled>Not set (tool's own default)</option>` : "";
    return `
      <div style="display: flex; justify-content: space-between; align-items: center; gap: 8px; padding: 8px 0; border-bottom: 1px solid var(--hairline);">
        <div>
          <strong>${esc(sc.source)}</strong>
          <div style="font-size: 12px; color: var(--muted);">${sc.builtin ? "built-in" : "own tool"} · ${esc(state)}</div>
        </div>
        <div style="display: flex; gap: 6px; align-items: center;">
          <select class="text-input" style="width: auto; padding: 4px 8px; font-size: 12px;"
                  onchange="changeSchedule(${jsArg(sc.source)}, this)">${unset}${options}</select>
          ${sc.builtin ? `<button class="btn-secondary btn-sm" onclick="crawlNow(${jsArg(sc.source)}, this)">Crawl now</button>` : ""}
        </div>
      </div>`;
  }).join("");
}

async function changeSchedule(source, select) {
  try {
    await APIClient.setCrawlSchedule(source, Number(select.value));
    showToast(`${source}: ${select.options[select.selectedIndex].text.toLowerCase()}`, "success");
  } catch (e) {
    showToast(`Couldn't change the schedule: ${e.message}`, "error");
  }
  renderSources();
}

async function crawlNow(source, btn) {
  btn.disabled = true;
  btn.textContent = "Crawling…";
  try {
    const r = await APIClient.retryCrawl(source);
    showToast(r.message || "Done", r.ok === false ? "error" : "success");
  } catch (e) {
    showToast(`Crawl failed: ${e.message}`, "error");
  }
  renderSources();
  refreshAppState();
}

async function connectPlatform(id) {
  try {
    await APIClient.connectPlatform(id);
    showToast("A browser window opened — log in there.", "info");
  } catch (e) {
    showToast(`Couldn't start login: ${e.message}`, "error");
  }
  renderPlatforms();
}

async function syncPlatform(id, btn) {
  btn.disabled = true;
  btn.textContent = "Syncing…";
  try {
    const r = await APIClient.syncPlatform(id);
    showToast(`TopCV: ${r.found} applications — ${r.updated} updated, ${r.moved} moved to Applied, ${r.added} added`, "success");
  } catch (e) {
    showToast(`Sync failed: ${e.message}`, "error");
  }
  renderPlatforms();
  refreshAppState();
}

async function disconnectPlatform(id) {
  if (!confirm("Delete this saved login?")) return;
  try {
    await APIClient.disconnectPlatform(id);
    showToast("Login deleted.", "success");
  } catch (e) {
    showToast(`Couldn't disconnect: ${e.message}`, "error");
  }
  renderPlatforms();
}

async function refreshAppState() {
  try {
    const res = await apiFetch(`${API_BASE}/location`);
    if (res.ok) AppState.location = await res.json();
  } catch {}
  // A login finishing writes the session, which triggers a live update; keep
  // an open logins panel current too.
  if (document.getElementById("platformsModalOverlay").classList.contains("open")) {
    renderPlatforms();
  }
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
          <span>⚠️ ${esc(s.name)} — ${s.lastError ? esc(s.lastError) : "no successful run in the last hour"} (last report: ${esc(timeAgo(s.lastRun) || "never")})</span>
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
  const maxKm = (AppState.location && AppState.location.max_km) || 10;
  if (job.company_geo_status === "ok" && job.company_distance_km > maxKm) {
    dealbreakers.push(`⛔ ${Math.round(job.company_distance_km)} km away`);
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
  } else if (job.score_status === "scored") {
    scoreHtml = `<span class="score-badge ${scoreClass}">${esc(job.score_letter)} ${esc(job.score)}</span>`;
  } else {
    // Not rated yet: no number to show (a bare "0" read as a terrible score).
    scoreHtml = `<span class="score-badge score-pending" title="Not rated yet">—</span>`;
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

  // Distance badge: ✅ within your maximum commute, 🔴 beyond it.
  let distanceHtml = "";
  const locSet = AppState.location && AppState.location.set;
  if (job.company_geo_status === "ok" && typeof job.company_distance_km === "number") {
    const km = job.company_distance_km;
    const icon = km <= ((AppState.location && AppState.location.max_km) || 10) ? "✅" : "🔴";
    distanceHtml = `<span class="intel-badge">${icon} ${km < 10 ? km.toFixed(1) : Math.round(km)} km</span>`;
  } else if (!locSet) {
    distanceHtml = `<span class="intel-badge" title="Set your location (📍 Location) to see distances.">📍 ? km</span>`;
  } else if (job.company_geo_status === "geocode_failed") {
    distanceHtml = `<span class="intel-badge" title="This job's address couldn't be found on the map.">📍 unknown</span>`;
  } else if (job.company_geo_status === "no_address") {
    distanceHtml = `<span class="intel-badge" title="This job has no address. Add one under Edit details.">📍 no address</span>`;
  } else {
    distanceHtml = `<span class="intel-badge" title="Looking up this address…">📍 …</span>`;
  }

  // Platform badges
  let platformStatusHtml = "";
  if (job.topcv_status) {
    // Codes written by the TopCV status sync (internal/core/topcv).
    const labels = {
      sent: "📨 TopCV: sent",
      seen: "👁 TopCV: CV viewed",
      suitable: "✅ TopCV: suitable",
      rejected: "❌ TopCV: not suitable"
    };
    platformStatusHtml = `<span class="apply-badge" title="${esc(job.apply_detail)}">${esc(labels[job.topcv_status] || job.topcv_status)}</span>`;
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
    // Raters answer "point • point • point" (see the rubric); older entries
    // used one point per line. Accept both.
    const listItems = job.score_reasoning
      .split(/\n|\s+•\s+/)
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

  // 5b. Edit details (fix a scraped title, a group-id "company", …)
  const field = (key, label, value) => `
      <label style="font-size:11px; font-weight:600; color:var(--muted)">${label}</label>
      <input type="text" class="text-input" id="edit-${key}-${job.id}" value="${esc(value)}" style="font-size:12px; padding: 4px 8px; margin-bottom: 4px;">`;
  const editHtml = `
    <details class="detail-section" style="border-top: 1px solid var(--hairline); padding-top: 8px;">
      <summary class="detail-title" style="cursor:pointer;">Edit details</summary>
      <div style="display:flex; flex-direction:column; margin-top:6px;">
        ${field("title", "Title", job.title)}
        ${field("company", "Company", job.company)}
        ${field("salary", "Salary", job.salary)}
        ${field("location", "Location", job.location)}
        <button class="btn-secondary btn-sm" style="align-self:flex-start; margin-top:4px;" onclick="saveJobDetails('${job.id}')">Save details</button>
      </div>
    </details>
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
      // Email application: your AI writes the draft (Ask AI), you review and
      // edit it here, and "Send" opens it in your own mail app — braveduck
      // never sends email and holds no mail password.
      const draft = job.draft_email;
      const isSent = job.draft_status === "sent";
      let emailEditorHtml;
      if (!draft) {
        emailEditorHtml = `
          <p style="font-size:12px; color:var(--muted); margin: 4px 0;">No draft yet.</p>
          <textarea class="textarea-input" id="draftNotes-${job.id}" style="height:40px; font-size:12px;" placeholder="Optional notes for your AI (tone, what to stress)…">${esc(job.draft_notes)}</textarea>
          <button class="btn-secondary btn-sm" style="margin-top:6px;" onclick="askAI('draft', '${job.id}')">Ask your AI to draft it</button>`;
      } else {
        const ro = isSent ? "readonly" : "";
        emailEditorHtml = `
          <div style="margin-top: 6px; display: flex; flex-direction: column; gap: 6px;">
            <div>
              <label style="font-size:11px; font-weight:600; color:var(--muted)">Recipient</label>
              <input type="text" id="draftTo-${job.id}" class="text-input" value="${esc(draft.to)}" ${ro} style="font-size:12px; padding: 4px 8px;" placeholder="recruiter@company.com">
            </div>
            <div>
              <label style="font-size:11px; font-weight:600; color:var(--muted)">Subject</label>
              <input type="text" id="draftSubject-${job.id}" class="text-input" value="${esc(draft.subject)}" ${ro} style="font-size:12px; padding: 4px 8px;">
            </div>
            <div>
              <label style="font-size:11px; font-weight:600; color:var(--muted)">Email Body</label>
              <textarea id="draftBody-${job.id}" class="textarea-input" style="height: 140px; font-size: 12px;" ${ro}>${esc(draft.body)}</textarea>
            </div>
            ${isSent
              ? `<p style="font-size:12px; color: var(--success-border); font-weight: 500; margin-top: 6px;">✓ Sent (you confirmed sending it).</p>`
              : `<div style="display: flex; gap: var(--spacing-xs); flex-wrap: wrap;">
                   <button class="btn-primary btn-sm" onclick="sendEmailDraft('${job.id}')">Send from my mail app</button>
                   <button class="btn-secondary btn-sm" onclick="askAI('draft', '${job.id}')">Re-draft</button>
                   <button class="btn-danger btn-sm" style="padding: 6px 12px; font-size:12px;" onclick="rejectEmailDraft('${job.id}')">Discard</button>
                 </div>
                 <p style="font-size:11px; color:var(--muted); margin:0;">Attach the tailored CV PDF in your mail app before sending.</p>`}
          </div>`;
      }

      applyBlockHtml = `
        <div class="detail-section" style="border-top: 1px dashed var(--border-strong); padding-top: 12px; margin-top: 4px;">
          <div class="detail-title">✉ Email Application</div>
          ${emailEditorHtml}
        </div>
      `;
    }
  } else if (job.status === "Interview") {
    // Interview reply: written by you, sent from your own mail app.
    applyBlockHtml = `
      <div class="detail-section" style="border-top: 1px dashed var(--border-strong); padding-top: 12px; margin-top: 4px;">
        <div class="detail-title">📨 Reply to the recruiter</div>
        <div style="margin-top: 6px; display: flex; flex-direction: column; gap: 6px;">
          <input type="text" id="replyTo-${job.id}" class="text-input" value="${esc((job.draft_email && job.draft_email.to) || job.contact_email)}" style="font-size:12px; padding: 4px 8px;" placeholder="recruiter@company.com">
          <textarea id="replyBody-${job.id}" class="textarea-input" style="height: 80px; font-size: 12px;" placeholder="Your reply…"></textarea>
          <button class="btn-primary btn-sm" style="align-self:flex-start;" onclick="sendInterviewReply('${job.id}')">Open in my mail app</button>
        </div>
      </div>
    `;
  }

  // 7. Tailored CV (your AI writes it; braveduck renders the PDF)
  let cvControlsHtml = "";
  if (job.status === "Saved" || job.status === "Waiting to apply" || job.status === "Applied") {
    const ready = job.cv_status === "ready";
    const when = ready && job.cv_generated_at ? new Date(job.cv_generated_at).toLocaleString([], { dateStyle: "short", timeStyle: "short" }) : "";
    cvControlsHtml = `
      <div class="detail-section" style="border-top: 1px solid var(--hairline); padding-top: 8px;">
        <div class="detail-title">Tailored CV</div>
        <p style="font-size:12px; margin: 2px 0 6px 0; color: var(--body);">${ready ? `Version ${esc(job.cv_version)} · ${esc(when)}` : "Not written yet."}</p>
        <div style="display: flex; gap: var(--spacing-xs); flex-wrap: wrap;">
          ${ready ? `<button class="btn-secondary btn-sm" onclick="downloadCV('${job.id}')">Download CV.pdf</button>` : ""}
          ${ready && /topcv\.vn/.test(job.url) && !job.topcv_status && job.status !== "Applied"
            ? `<button class="btn-primary btn-sm" onclick="applyOnTopCV('${job.id}')" title="Opens the job in a window with your TopCV login. You submit it there.">Apply on TopCV</button>` : ""}
          <button class="btn-secondary btn-sm" onclick="askAI('cv', '${job.id}')">${ready ? "Ask your AI to improve it" : "Ask your AI to tailor it"}</button>
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
    ${editHtml}
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

// showJobCard closes the Add drawer and opens, scrolls to and flashes a card
// (the job a rejected paste duplicates).
function showJobCard(id) {
  document.getElementById("closeDrawerBtn").click();
  AppState.expandedCards.add(id);
  renderBoard();
  const card = document.querySelector(`.card[data-id="${CSS.escape(id)}"]`);
  if (!card) {
    showToast(`Job #${id} is hidden by the current filter`, "info");
    return;
  }
  card.scrollIntoView({ behavior: "smooth", block: "center" });
  card.classList.add("flash");
  setTimeout(() => card.classList.remove("flash"), 2000);
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
  askAI("rate", id);
}

async function askAnotherModel(id) {
  // A challenger rating comes from the user's AI too: same flow as Rate.
  askAI("rate", id);
}

// --- Ask your AI (rate / tailor CV / draft email) ------------------------------
let askState = null;

async function askAI(kind, id) {
  const notesEl = document.getElementById(`draftNotes-${id}`);
  let req;
  try {
    if (kind === "rate") showToast("Rating…", "info");
    req = await APIClient.askRequest(kind, id, kind === "draft" ? { notes: notesEl ? notesEl.value : "" } : {});
  } catch (e) {
    showToast(e.message, "error");
    return;
  }
  if (req.rated) {
    // Jev is the default rater: the score is already on the card.
    showToast(`Rated ${req.job.score_letter} ${req.job.score.toFixed(1)}`, "success");
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
  applyOnTopCV(id);
}

// Assisted apply: you approve by clicking, and you submit on TopCV yourself.
async function applyOnTopCV(id) {
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
  alert(
    "A TopCV window is opening with your login. In it:\n\n" +
    "1. Click Ứng tuyển (Apply)\n" +
    "2. Upload this CV:\n   " + prep.cv_pdf + "\n" +
    (prep.cover_letter ? "3. Paste the cover letter (already copied to your clipboard)\n4. Submit, then close the window\n" : "3. Submit, then close the window\n") +
    "\nbraveduck checks TopCV afterwards and moves the job to Applied if it's there."
  );
  refreshAppState();
}

async function undoOnSiteApply(id) {
  await transitionCardStatus(id, "Waiting to apply");
}

async function retryApply(id) {
  applyOnTopCV(id);
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

// --- Live updates -------------------------------------------------------------
// One "changed" event can arrive per second; refetch after a short debounce.
// Never re-render under the user's cursor: while they're typing in a card,
// hold the refresh until they leave the field.
let refreshTimer = null;
let refreshPending = false;

function isEditingCard() {
  const el = document.activeElement;
  return !!(el && el.closest && el.closest("#kanbanBoard") && /^(INPUT|TEXTAREA|SELECT)$/.test(el.tagName));
}

function scheduleRefresh() {
  clearTimeout(refreshTimer);
  refreshTimer = setTimeout(() => {
    if (isEditingCard()) {
      refreshPending = true;
      return;
    }
    refreshAppState();
  }, 250);
}

document.addEventListener("focusout", () => {
  if (refreshPending) {
    refreshPending = false;
    scheduleRefresh();
  }
});

function connectLiveUpdates() {
  const es = new EventSource(`${API_BASE}/events?token=${encodeURIComponent(API_TOKEN)}`);
  es.addEventListener("changed", scheduleRefresh);
  es.onerror = () => {
    // EventSource retries dropped connections itself and gives up (CLOSED)
    // when the server answers with an error. Only a 401 means the server
    // restarted with a new token (reload to pick it up); any other refusal —
    // e.g. 503 when live updates aren't available — falls back to polling.
    if (es.readyState === EventSource.CLOSED) onLiveUpdatesClosed();
  };
}

let pollTimer = null;

async function onLiveUpdatesClosed() {
  let status = 0;
  try {
    status = (await fetch(`${API_BASE}/jobs`, { headers: { Authorization: `Bearer ${API_TOKEN}` }, cache: "no-store" })).status;
  } catch {
    // server down: wait for it, then decide again
  }
  if (status === 401) {
    showToast("The board server restarted — reloading…", "info");
    setTimeout(() => location.reload(), 1000);
  } else if (status === 0) {
    showToast("Lost connection to the board server — reconnecting…", "error");
    setTimeout(onLiveUpdatesClosed, 3000);
  } else if (!pollTimer) {
    // Server is fine, live updates aren't: refresh now and then instead.
    pollTimer = setInterval(scheduleRefresh, 30000);
  }
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
      
      if (err.duplicates) {
        const matches = err.duplicates.map(d =>
          `<li>#${esc(String(d.id))} ${esc(d.title)} <em>(${esc(d.status)})</em> ` +
          `<button class="btn-secondary btn-sm" onclick="showJobCard('${esc(String(d.id))}')">Show</button></li>`).join("");
        extractResultBanner.innerHTML = `⚠️ <strong>Not added: this job is already on the board.</strong>` +
          `<ul style="margin: 6px 0 0; padding-left: 18px;">${matches}</ul>`;
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
        <label for="kw-${esc(source)}">${esc(KEYWORD_LABELS[source] || source)}</label>
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
    
    try {
      await APIClient.updateKeywords(kw);
    } catch (e) {
      showToast(`Couldn't save keywords: ${e.message}`, "error");
      return;
    }
    keywordsModal.classList.remove("open");
    showToast("Keywords saved.", "success");
    refreshAppState();
  });

  // Rating Config Modal
  const ratingModal = document.getElementById("ratingModalOverlay");
  const ratingBtn = document.getElementById("ratingBtn");
  const saveRatingBtn = document.getElementById("saveRatingConfigBtn");

  ratingBtn.addEventListener("click", async () => {
    const config = await APIClient.getRatingConfig();
    document.getElementById("abModeCheckbox").checked = config.ab_mode;
    document.getElementById("defaultModelInput").value = config.default_model;
    document.getElementById("challengerModelsInput").value =
      config.models.filter(m => m !== config.default_model).join(", ");
    document.getElementById("ratingConfigError").style.display = "none";
    ratingModal.classList.add("open");
  });

  saveRatingBtn.addEventListener("click", async () => {
    const abMode = document.getElementById("abModeCheckbox").checked;
    const defaultModel = document.getElementById("defaultModelInput").value.trim();
    const challengers = document.getElementById("challengerModelsInput").value
      .split(",").map(s => s.trim()).filter(s => s && s !== defaultModel);
    const errorBox = document.getElementById("ratingConfigError");

    try {
      await APIClient.updateRatingConfig({
        default_model: defaultModel,
        ab_mode: abMode,
        models: [defaultModel, ...new Set(challengers)]
      });
    } catch (e) {
      // The server validates (1-3 models, no blanks or duplicates); show why.
      errorBox.textContent = e.message;
      errorBox.style.display = "block";
      return;
    }
    ratingModal.classList.remove("open");
    showToast("Rating config updated!", "success");
    refreshAppState();
  });


  document.getElementById("locationBtn").addEventListener("click", openLocationWindow);
  document.getElementById("useMyLocationBtn").addEventListener("click", useMyLocation);
  document.getElementById("placeSearchBtn").addEventListener("click", searchPlace);
  document.getElementById("placeSearch").addEventListener("keydown", e => { if (e.key === "Enter") searchPlace(); });
  document.getElementById("saveLocationBtn").addEventListener("click", saveLocation);
  document.getElementById("clearLocationBtn").addEventListener("click", clearLocation);

  const sourcesModal = document.getElementById("sourcesModalOverlay");
  document.getElementById("sourcesBtn").addEventListener("click", async () => {
    sourcesModal.classList.add("open");
    await renderSources();
  });

  const cvModal = document.getElementById("cvModalOverlay");
  document.getElementById("myCVBtn").addEventListener("click", async () => {
    document.getElementById("cvResult").style.display = "none";
    cvModal.classList.add("open");
    await renderMyCV();
  });
  document.getElementById("cvUploadBtn").addEventListener("click", () => uploadMyCV(false));

  const platformsModal = document.getElementById("platformsModalOverlay");
  document.getElementById("platformsBtn").addEventListener("click", async () => {
    platformsModal.classList.add("open");
    await renderPlatforms();
  });

  // General Modal close logic
  document.querySelectorAll(".modal-close-btn").forEach(btn => {
    btn.addEventListener("click", () => {
      keywordsModal.classList.remove("open");
      ratingModal.classList.remove("open");
      platformsModal.classList.remove("open");
      sourcesModal.classList.remove("open");
      cvModal.classList.remove("open");
      document.getElementById("askModalOverlay").classList.remove("open");
      document.getElementById("locationModalOverlay").classList.remove("open");
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

  // 2. Live updates: the server pushes "changed" whenever the data changes,
  // whoever changed it (this tab, another tab, the CLI, an agent over MCP).
  connectLiveUpdates();

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
