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
  // A login window would open on the shared server, not in front of the user.
  const hosted = Boolean(AppState.setup && AppState.setup.hosted);
  const note = hosted ? `<p class="hint" style="font-size: 13px; color: var(--muted);">Logging in to a job site opens a browser window on your own computer, so it's off on this shared server. Run braveduck on your computer to apply through your accounts.</p>` : "";
  list.innerHTML = note + platforms.map(p => {
    const [label, color] = PLATFORM_STATUS[p.status] || [p.status, "var(--muted)"];
    const saved = p.status !== "not_connected" && p.status !== "connecting";
    const sync = p.id === "topcv" && p.status === "connected"
      ? `<button class="btn-secondary btn-sm" onclick="syncPlatform(${jsArg(p.id)}, this)">Sync applications</button>` : "";
    const action = p.status === "connecting" ? ""
      : hosted ? (saved ? `<button class="btn-secondary btn-sm" onclick="disconnectPlatform(${jsArg(p.id)})">Disconnect</button>` : "")
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
  const openedWith = picked;
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
  // A point picked while the map library loaded (your location, a search
  // result) wins over the saved one, and gets its pin now.
  if (picked && picked !== openedWith) pickPoint(picked.lat, picked.lng, picked.label);
  else if (cur.set) pickPoint(cur.lat, cur.lng, cur.label);
  else if (locMarker) { locMarker.remove(); locMarker = null; }
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

// --- Facebook groups (in the Add Job drawer) ----------------------------------
const FB_GROUP_STATUS = {
  checking: ["⏳ Checking whether it's public…", "var(--muted)"],
  public: ["🌐 Public — its job posts come to your board", "var(--success-border)"],
  private: ["🔒 Private — only members see its posts. Paste the posts you want (link + text).", "#b45309"],
  unavailable: ["⚠️ Can't be read right now", "var(--error)"],
};

async function renderFBGroups() {
  const block = document.getElementById("fbGroupsBlock");
  let groups = [];
  try {
    groups = await APIClient.get("facebook-groups");
  } catch {
    return;
  }
  block.hidden = groups.length === 0;
  document.getElementById("fbGroupsList").innerHTML = groups.map(g => {
    const [label, color] = FB_GROUP_STATUS[g.visibility] || [g.visibility, "var(--muted)"];
    return `<li style="display:flex; justify-content:space-between; gap:8px; padding:6px 0; border-bottom:1px solid var(--hairline);">
      <span><a href="${safeUrl(g.url)}" target="_blank" rel="noopener noreferrer">${esc(g.name || g.url)}</a>
        <span style="display:block; font-size:12px; color:${color};">${esc(label)}${g.note ? " — " + esc(g.note) : ""}</span></span>
      <button class="btn-secondary btn-sm" onclick="unfollowFBGroup(${jsArg(g.key)})">Unfollow</button></li>`;
  }).join("");
}

async function unfollowFBGroup(key) {
  try {
    await APIClient.del(`facebook-groups/${encodeURIComponent(key)}`);
  } catch (e) {
    showToast(e.message, "error");
    return;
  }
  renderFBGroups();
}
