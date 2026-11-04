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
