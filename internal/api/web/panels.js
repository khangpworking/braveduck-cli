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
  const note = hosted ? `<p class="hint" style="font-size: 13px; color: var(--muted);">${esc(t("Logging in to a job site opens a browser window on your own computer, so it's off on this shared server. Run braveduck on your computer to apply through your accounts."))}</p>` : "";
  list.innerHTML = note + platforms.map(p => {
    const [label, color] = PLATFORM_STATUS[p.status] || [p.status, "var(--muted)"];
    const saved = p.status !== "not_connected" && p.status !== "connecting";
    const sync = p.id === "topcv" && p.status === "connected"
      ? `<button class="btn-secondary btn-sm" onclick="syncPlatform(${jsArg(p.id)}, this)">${esc(t("Sync applications"))}</button>` : "";
    const action = p.status === "connecting" ? ""
      : hosted ? (saved ? `<button class="btn-secondary btn-sm" onclick="disconnectPlatform(${jsArg(p.id)})">${esc(t("Disconnect"))}</button>` : "")
      : saved
        ? `${sync}<button class="btn-secondary btn-sm" onclick="disconnectPlatform(${jsArg(p.id)})">${esc(t("Disconnect"))}</button>
           <button class="btn-secondary btn-sm" onclick="connectPlatform(${jsArg(p.id)})">${esc(t("Log in again"))}</button>`
        : `<button class="btn-primary btn-sm" onclick="connectPlatform(${jsArg(p.id)})">${esc(t("Connect"))}</button>`;
    return `
      <div style="display: flex; justify-content: space-between; align-items: center; gap: 8px; padding: 8px 0; border-bottom: 1px solid var(--hairline);">
        <div>
          <strong>${esc(p.name)}</strong>${p.account ? ` <span style="font-size: 12px; color: var(--muted);">${esc(p.account)}</span>` : ""}
          <div style="font-size: 12px; color: ${color};">${esc(t(label))}</div>
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
      js.onerror = () => { leafletReady = null; reject(new Error(t("map library didn't load (offline?)"))); };
      document.head.append(css, js);
    });
  }
  return leafletReady;
}

function pickPoint(lat, lng, label) {
  picked = { lat, lng, label: label || "" };
  document.getElementById("locationPicked").textContent =
    t("Pin: {place}{lat}, {lng}", {place: label ? label + " — " : "", lat: lat.toLocaleString(I18N.lang, {minimumFractionDigits: 5, maximumFractionDigits: 5}), lng: lng.toLocaleString(I18N.lang, {minimumFractionDigits: 5, maximumFractionDigits: 5})});
  if (locMap) {
    if (!locMarker) locMarker = L.marker([lat, lng], {title: t("Your location"), alt: t("Your location")}).addTo(locMap);
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
    ? t("Saved: {place} ({lat}, {lng})", {place: cur.label || "", lat: cur.lat.toLocaleString(I18N.lang, {minimumFractionDigits: 5, maximumFractionDigits: 5}), lng: cur.lng.toLocaleString(I18N.lang, {minimumFractionDigits: 5, maximumFractionDigits: 5})}) : t("Click the map to place your pin.");
  try {
    await loadLeaflet();
  } catch (e) {
    document.getElementById("locationMap").textContent = t("Map unavailable: {error}. You can still search or use your current location.", {error: t(e.message)});
    return;
  }
  if (!locMap) {
    locMap = L.map("locationMap", {zoomControl: false}).setView(cur.set ? [cur.lat, cur.lng] : [16, 106], cur.set ? 13 : 5);
    L.control.zoom({zoomInTitle: t("Zoom in"), zoomOutTitle: t("Zoom out")}).addTo(locMap);
    L.tileLayer("https://tile.openstreetmap.org/{z}/{x}/{y}.png", {
      maxZoom: 19, attribution: "&copy; " + esc(t("OpenStreetMap contributors"))
    }).addTo(locMap);
    locMap.getContainer().querySelector(".leaflet-control-attribution a[title]").title = t("A JavaScript library for interactive maps");
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
    showToast(t("This browser can't share its location — search or click the map instead."), "error");
    return;
  }
  navigator.geolocation.getCurrentPosition(
    pos => pickPoint(pos.coords.latitude, pos.coords.longitude, t("My current location")),
    err => showToast(err.code === 1 ? t("Location permission denied — search or click the map instead.") : t("Couldn't get your location: {error}", {error: t(err.message)}), "error"),
    { enableHighAccuracy: false, timeout: 15000 }
  );
}

async function searchPlace() {
  const q = document.getElementById("placeSearch").value.trim();
  const box = document.getElementById("placeResults");
  box.innerHTML = `<span style="font-size:12px; color:var(--muted)">${esc(t("Searching…"))}</span>`;
  try {
    const res = await apiFetch(`${API_BASE}/places?q=${encodeURIComponent(q)}`);
    const places = await res.json();
    if (!res.ok) throw new Error(places.error || `HTTP ${res.status}`);
    box.innerHTML = places.length ? places.map((p, i) =>
      `<button class="btn-secondary btn-sm" style="text-align:left;" data-i="${i}">${esc(p.label)}</button>`).join("")
      : `<span style="font-size:12px; color:var(--muted)">${esc(t("No match. Try adding the city."))}</span>`;
    box.querySelectorAll("button").forEach(b => b.addEventListener("click", () => {
      const p = places[Number(b.dataset.i)];
      pickPoint(p.lat, p.lng, p.label);
    }));
  } catch (e) {
    box.innerHTML = `<span style="font-size:12px; color:var(--error)">${esc(t(e.message))}</span>`;
  }
}

async function saveLocation() {
  if (!picked) {
    showToast(t("Pick a place first: your current location, a search result, or a click on the map."), "error");
    return;
  }
  try {
    await APIClient.put("location", { ...picked, max_km: Number(document.getElementById("maxKmInput").value) || 0 });
    document.getElementById("locationModalOverlay").classList.remove("open");
    showToast(t("Location saved — job distances update shortly."), "success");
  } catch (e) {
    showToast(t("Couldn't save: {error}", {error: t(e.message)}), "error");
  }
  refreshAppState();
}

async function clearLocation() {
  if (!confirm(t("Forget your location and all job distances?"))) return;
  await apiFetch(`${API_BASE}/location`, { method: "DELETE" });
  picked = null;
  if (locMarker) { locMarker.remove(); locMarker = null; }
  document.getElementById("locationModalOverlay").classList.remove("open");
  refreshAppState();
}

// --- Search terms window ---------------------------------------------------------
// One question; the filter lists wait under More (keys mirror
// internal/core/keyword.Lists). Every list goes back to the server on Save.
const KEYWORD_FILTERS = [
  ["negative_content", "Hide jobs that mention…", "In the title or the description."],
  ["negative_title", "Hide jobs with these words in the title", ""],
  ["negative_company", "Hide jobs from these companies", ""],
  ["positive_title", "Only keep jobs with these words in the title", ""]
];
let keywordLists = null;

// foldText lets "ke toan" find "Kế toán", as the server's filters do.
function foldText(s) {
  return s.normalize("NFD").replace(/\p{M}/gu, "").toLowerCase().replace(/đ/g, "d");
}

function keywordCountLine(terms) {
  if (!terms.length) return "";
  const folded = terms.map(foldText);
  const n = AppState.jobs.filter(j => {
    const text = foldText(`${j.title} ${j.jd_full || ""}`);
    return folded.some(t => text.includes(t));
  }).length;
  if (n === 0) return t("No jobs on your board mention these yet.");
  return n === 1 ? t("1 job on your board mentions these.") : t("{n} jobs on your board mention these.", {n: n.toLocaleString(I18N.lang)});
}

async function openKeywords() {
  try {
    keywordLists = await APIClient.getKeywords();
    if (keywordLists.error) throw new Error(keywordLists.error);
  } catch (e) {
    showToast(t("Couldn't load your search terms: {error}", {error: t(e.message)}), "error");
    return;
  }
  const queries = keywordLists.search_queries = keywordLists.search_queries || [];
  const count = el("p", { class: "pf-hint", id: "keywordsCount" });
  const updateCount = () => { count.textContent = keywordCountLine(queries); };
  KEYWORD_FILTERS.forEach(([key]) => { keywordLists[key] = keywordLists[key] || []; });
  const filters = KEYWORD_FILTERS.map(([key, label, hint]) => tagsField(t(label), t(hint), keywordLists[key], []));
  const inUse = KEYWORD_FILTERS.reduce((n, [key]) => n + keywordLists[key].length, 0);
  document.getElementById("keywordsForm").replaceChildren(
    tagsField(t("What jobs should we look for?"), t("Job titles or skills, e.g. Kế toán, n8n."), queries, [], updateCount),
    count,
    el("details", { class: "pf-more" }, el("summary", {}, inUse ? t("More filters ({n} in use)", {n: inUse.toLocaleString(I18N.lang)}) : t("More filters")), ...filters));
  updateCount();
  document.getElementById("keywordsModalOverlay").classList.add("open");
}

async function saveKeywords() {
  // Keep a term the user typed without pressing Enter.
  document.querySelectorAll("#keywordsForm .pf-tag-input").forEach(i => i.dispatchEvent(new Event("blur")));
  try {
    await APIClient.updateKeywords(keywordLists);
  } catch (e) {
    showToast(t("Couldn't save: {error}", {error: t(e.message)}), "error");
    return;
  }
  document.getElementById("keywordsModalOverlay").classList.remove("open");
  showToast(t("Search terms saved."), "success");
  refreshAppState();
}

// --- Job sources window ----------------------------------------------------------
// Job sites up front with one "Check now"; background tasks and external tools
// wait under Advanced. The server says which is which and the shortest interval
// each may use (an hour on a shared server).
const EVERY_CHOICES = [[0, "off"], [5, "every 5 minutes"], [15, "every 15 minutes"], [30, "every 30 minutes"], [60, "every hour"],
  [120, "every 2 hours"], [360, "every 6 hours"], [720, "every 12 hours"], [1440, "once a day"]];
let sourceSites = [], sourcesNeedTerms = false, checkingSources = false;

function everyLabel(minutes) {
  const choice = EVERY_CHOICES.find(([m]) => m === minutes);
  return choice ? t(choice[1]) : t("every {n} minutes", {n: minutes.toLocaleString(I18N.lang)});
}

// agoWords says how long ago in words: "5 minutes ago".
function agoWords(dateString) {
  const count = (n, unit) => {
    const keys = {minute: ["{n} minute ago", "{n} minutes ago"], hour: ["{n} hour ago", "{n} hours ago"], day: ["{n} day ago", "{n} days ago"]};
    return t(keys[unit][n === 1 ? 0 : 1], {n: n.toLocaleString(I18N.lang)});
  };
  const min = Math.floor((Date.now() - new Date(dateString)) / 60000);
  if (!(min >= 1)) return t("just now");
  if (min < 60) return count(min, "minute");
  if (min < 1440) return count(Math.floor(min / 60), "hour");
  return count(Math.floor(min / 1440), "day");
}

function sourceRow(sc, health, labelText) {
  const site = sc.builtin && !sc.task;
  const failed = health && health.lastError;
  const state = !health || !health.lastRun ? (site ? t("Not checked yet") : t("Hasn't run yet"))
    : failed ? (site ? t("The last check didn't work") : `⚠️ ${t(health.lastError)}`)
    : t(site ? "Last checked {time}" : "Last ran {time}", {time: agoWords(health.lastRun)});
  const choices = EVERY_CHOICES.filter(([m]) => m === 0 || m >= sc.min_every_minutes);
  if (!choices.some(([m]) => m === sc.every_minutes)) choices.push([sc.every_minutes, everyLabel(sc.every_minutes)]);
  const unset = !sc.builtin && !sc.configured;
  const select = el("select", { class: "text-input source-every", "aria-label": t("{site}: {label}", {site: t(sc.name), label: labelText}), onchange: () => changeSchedule(sc, select) },
    unset ? el("option", { value: "", selected: true, disabled: true }, t("not set")) : null,
    choices.map(([m, label]) => el("option", { value: m, selected: !unset && m === sc.every_minutes }, t(label))));
  return el("div", { class: "source-row" },
    el("div", {}, el("strong", {}, t(sc.name)), el("div", { class: "source-state" + (failed ? " bad" : "") }, state)),
    el("label", { class: "source-every-label" }, labelText + " ", select));
}

async function renderSources() {
  const [scheds, status, kw] = await Promise.all([APIClient.getCrawlSchedule(), APIClient.getCrawlStatus(), APIClient.getKeywords()]);
  const health = {};
  ((status && status.sources) || []).forEach(s => { health[s.name] = s; });
  sourceSites = scheds.filter(sc => sc.builtin && !sc.task);
  sourcesNeedTerms = !(kw.search_queries && kw.search_queries.length);
  const others = scheds.filter(sc => !sc.builtin || sc.task);
  document.getElementById("sourcesList").replaceChildren(
    ...sourceSites.map(sc => sourceRow(sc, health[sc.source], t("Check for new jobs:"))),
    others.length ? el("details", { class: "pf-more" }, el("summary", {}, t("Advanced")),
      ...others.map(sc => sourceRow(sc, health[sc.source], t("Runs:")))) : null);
  renderCheckNow();
}

// renderCheckNow shows the one action, or the one reason it can't run yet.
function renderCheckNow() {
  const on = sourceSites.some(sc => sc.every_minutes > 0);
  const btn = document.getElementById("checkSourcesBtn");
  btn.disabled = checkingSources || sourcesNeedTerms || !on;
  btn.textContent = checkingSources ? t("Checking…") : t("Check now");
  const addTerms = el("button", { type: "button", class: "btn-link", onclick: () => {
    document.getElementById("sourcesModalOverlay").classList.remove("open");
    openKeywords();
  } }, t("Add them"));
  document.getElementById("sourcesHint").replaceChildren(...(sourcesNeedTerms ? [t("First tell us what jobs to look for. "), addTerms]
    : on ? [] : [t("All sites are off. Turn one on to check it.")]));
}

async function changeSchedule(sc, select) {
  try {
    await APIClient.setCrawlSchedule(sc.source, Number(select.value));
  } catch (e) {
    showToast(t("Couldn't change it: {error}", {error: t(e.message)}), "error");
    select.value = sc.configured || sc.builtin ? String(sc.every_minutes) : "";
    return;
  }
  // No redraw: arrow keys on this list must keep their place.
  sc.every_minutes = Number(select.value);
  sc.configured = true;
  showToast(t("{site}: {schedule}", {site: t(sc.name), schedule: everyLabel(sc.every_minutes)}), "success");
  renderCheckNow();
}

// checkSources checks every site that's on. One crawl can take minutes, so
// the window may close meanwhile.
async function checkSources() {
  const sites = sourceSites.filter(sc => sc.every_minutes > 0);
  checkingSources = true;
  renderCheckNow();
  const runs = await Promise.allSettled(sites.map(sc => APIClient.retryCrawl(sc.source)));
  checkingSources = false;
  const added = runs.reduce((n, r) => n + ((r.value && r.value.result && r.value.result.added) || 0), 0);
  // A site already being crawled (its schedule came up) is being checked.
  const failed = sites.filter((sc, i) => {
    const r = runs[i].value;
    return !(r && (r.ok || /already being crawled/.test(r.error || "")));
  }).map(sc => sc.name);
  if (failed.length) showToast(t("Couldn't check {sites}. Found {n} new.", {sites: new Intl.ListFormat(I18N.lang, {style: "long", type: "conjunction"}).format(failed), n: added.toLocaleString(I18N.lang)}), "error");
  else if (added) showToast(added === 1 ? t("Found 1 new job.") : t("Found {n} new jobs.", {n: added.toLocaleString(I18N.lang)}), "success");
  else showToast(t("No new jobs right now."), "info");
  await renderSources();
  refreshAppState();
}

async function connectPlatform(id) {
  try {
    await APIClient.connectPlatform(id);
    showToast(t("A browser window opened — log in there."), "info");
  } catch (e) {
    showToast(t("Couldn't start login: {error}", {error: t(e.message)}), "error");
  }
  renderPlatforms();
}

async function syncPlatform(id, btn) {
  btn.disabled = true;
  btn.textContent = t("Syncing…");
  try {
    const r = await APIClient.syncPlatform(id);
    showToast(t("TopCV: {found} applications — {updated} updated, {moved} moved to Applied, {added} added", {found: r.found.toLocaleString(I18N.lang), updated: r.updated.toLocaleString(I18N.lang), moved: r.moved.toLocaleString(I18N.lang), added: r.added.toLocaleString(I18N.lang)}), "success");
  } catch (e) {
    showToast(t("Sync failed: {error}", {error: t(e.message)}), "error");
  }
  renderPlatforms();
  refreshAppState();
}

async function disconnectPlatform(id) {
  if (!confirm(t("Delete this saved login?"))) return;
  try {
    await APIClient.disconnectPlatform(id);
    showToast(t("Login deleted."), "success");
  } catch (e) {
    showToast(t("Couldn't disconnect: {error}", {error: t(e.message)}), "error");
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
      <span style="min-width:0; overflow-wrap:anywhere;"><a href="${safeUrl(g.url)}" target="_blank" rel="noopener noreferrer">${esc(g.name || g.url)}</a>
        <span style="display:block; font-size:12px; color:${color};">${esc(t(label))}${g.note ? " — " + esc(g.note) : ""}</span></span>
      <button class="btn-secondary btn-sm" onclick="unfollowFBGroup(${jsArg(g.key)})">${esc(t("Unfollow"))}</button></li>`;
  }).join("");
}

async function unfollowFBGroup(key) {
  try {
    await APIClient.del(`facebook-groups/${encodeURIComponent(key)}`);
  } catch (e) {
    showToast(t(e.message), "error");
    return;
  }
  renderFBGroups();
}
