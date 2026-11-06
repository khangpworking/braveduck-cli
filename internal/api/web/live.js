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
    showToast(t("The board server restarted — reloading…"), "info");
    setTimeout(() => location.reload(), 1000);
  } else if (status === 0) {
    showToast(t("Lost connection to the board server — reconnecting…"), "error");
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

// Known server event templates; preserve unknown messages, especially errors.
function activityMessage(message) {
  if (I18N.lang === "en") return message;
  const stageNames = { saved: "Saved", waiting: "Waiting to apply", applied: "Applied", interview: "Interview", offer: "Offer", closed: "Closed" };
  const patterns = [
    [/^Added (".*")$/, "Added {job}", ([, job]) => ({ job })],
    [/^Removed (".*")$/, "Removed {job}", ([, job]) => ({ job })],
    [/^Moved (".*") to (saved|waiting|applied|interview|offer|closed)$/, "Moved {job} to {stage}", ([, job, stage]) => ({ job, stage: t(stageNames[stage]) })],
    [/^Rated (".*") ([A-F]) ([\d.]+) \((.*)\)$/, "Rated {job} {letter} {score} ({rater})", ([, job, letter, score, rater]) => ({ job, letter, score: Number(score).toLocaleString(I18N.lang, { minimumFractionDigits: 1, maximumFractionDigits: 1 }), rater })],
    [/^Tailored CV v(\d+) for (".*")$/, "Tailored CV v{n} for {job}", ([, n, job]) => ({ n, job })],
    [/^Email draft ready for (".*")$/, "Email draft ready for {job}", ([, job]) => ({ job })],
    [/^Application email sent for (".*")$/, "Application email sent for {job}", ([, job]) => ({ job })],
    [/^Applied on (.*): (".*")$/, "Applied on {site}: {job}", ([, site, job]) => ({ site, job })],
    [/^Connected (.+)$/, "Connected {site}", ([, site]) => ({ site })],
    [/^Disconnected (.+)$/, "Disconnected {site}", ([, site]) => ({ site })],
    [/^Following the Facebook group (.+)$/, "Following the Facebook group {url}", ([, url]) => ({ url })],
    [/^(.+): (\d+) new job\(s\)$/, "{source}: {n} new jobs", ([, source, n]) => ({ source: sourceName(source), n: Number(n).toLocaleString(I18N.lang) })],
    [/^(.+): (\d+) new job post\(s\)$/, "{source}: {n} new job posts", ([, source, n]) => ({ source, n: Number(n).toLocaleString(I18N.lang) })],
    [/^Moved (\d+) expired job\(s\) to Closed \(drag one back to keep it\)$/, "Moved {n} expired jobs to Closed (drag one back to keep it)", ([, n]) => ({ n: Number(n).toLocaleString(I18N.lang) })],
    [/^Uploaded your CV (".*") — its text is in your profile$/, "Uploaded your CV {file} — its text is in your profile", ([, file]) => ({ file })],
    [/^TopCV sync: (\d+) updated, (\d+) moved to Applied, (\d+) added$/, "TopCV sync: {updated} updated, {moved} moved to Applied, {added} added", ([, updated, moved, added]) => ({ updated, moved, added })],
    [/^(.+) is a private group: only its members can see its posts, so paste the ones you want$/, "{group} is a private group: only its members can see its posts, so paste the ones you want", ([, group]) => ({ group })]
  ];
  for (const [pattern, key, vars] of patterns) {
    const match = pattern.exec(message);
    if (match) return t(key, vars(match));
  }
  return t(message);
}

// Recent activity: hidden until the 🔔 button opens it.
function renderActivityLog() {
  const feed = document.getElementById("logFeed");
  const activity = AppState.activity;

  if (activity.length === 0) {
    feed.innerHTML = `<li class="log-item">${t("Nothing yet.")}</li>`;
    return;
  }

  feed.innerHTML = activity.slice(0, 20).map(act => {
    let icon = "✅";
    if (act.type === "error") icon = "❌";
    if (act.type === "info") icon = "🔄";

    return `
      <li class="log-item">
        <span class="log-icon">${icon}</span>
        <span class="log-message">${esc(activityMessage(act.message))}</span>
        <span class="log-time">${esc(act.timestamp)}</span>
      </li>
    `;
  }).join("");
}

function setActivityOpen(open) {
  document.getElementById("activityPanel").hidden = !open;
  document.getElementById("activityBtn").setAttribute("aria-expanded", String(open));
}

document.addEventListener("DOMContentLoaded", () => {
  const btn = document.getElementById("activityBtn");
  const panel = document.getElementById("activityPanel");
  const closeBtn = document.getElementById("activityCloseBtn");
  btn.addEventListener("click", () => {
    setActivityOpen(panel.hidden);
    if (!panel.hidden) closeBtn.focus();
  });
  closeBtn.addEventListener("click", () => {
    setActivityOpen(false);
    btn.focus();
  });
  document.addEventListener("keydown", e => {
    if (e.key === "Escape" && !panel.hidden) {
      setActivityOpen(false);
      btn.focus();
    }
  });
  document.addEventListener("click", e => {
    if (!panel.hidden && !panel.contains(e.target) && !btn.contains(e.target)) setActivityOpen(false);
  });
});

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

  if (diffSec < 60) return t("just now");
  if (diffMin < 60) return t("{n}m", { n: diffMin.toLocaleString(I18N.lang) });
  if (diffHr < 24) return t("{n}h", { n: diffHr.toLocaleString(I18N.lang) });
  return t("{n}d", { n: diffDays.toLocaleString(I18N.lang) });
}

function formatDuration(ms) {
  if (ms < 0) ms = 0;
  const totalSecs = Math.floor(ms / 1000);
  const m = Math.floor(totalSecs / 60).toString().padStart(2, '0');
  const s = (totalSecs % 60).toString().padStart(2, '0');
  return `${m}:${s}`;
}

// Setup Event Listeners on DOM Load
