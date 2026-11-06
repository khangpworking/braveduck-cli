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

async function readAPIJSON(path) {
  const res = await apiFetch(`${API_BASE}${path}`);
  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    throw new Error(body.error || `HTTP ${res.status}`);
  }
  return res.json();
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
    query: "" // the search box
  },
  sorting: {
    Saved: null // the user's choice; null follows savedSort() in board.js
  },
  expandedCards: new Set() // stores expanded card IDs
};

// API Client Wrapper (mirrors REST-API-CONTRACT.md)
const APIClient = {
  async getJobs() {
    const data = await readAPIJSON('/jobs');
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
    if (!res.ok) {
      const err = new Error(out.error || `HTTP ${res.status}`);
      err.code = out.code;
      throw err;
    }
    return out;
  },

  async get(path) {
    const res = await apiFetch(`${API_BASE}/${path}`);
    const out = await res.json();
    if (!res.ok) throw new Error(out.error || `HTTP ${res.status}`);
    return out;
  },

  async del(path) {
    const res = await apiFetch(`${API_BASE}/${path}`, { method: "DELETE" });
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

  async post(path, body) {
    const res = await apiFetch(`${API_BASE}/${path}`, body === undefined ? { method: "POST" } : {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body)
    });
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
    return readAPIJSON('/rating-config');
  },

  async getStats() {
    return readAPIJSON('/stats');
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
    return readAPIJSON('/crawl-status');
  },

  async getActivity() {
    const events = await readAPIJSON('/activity');
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
