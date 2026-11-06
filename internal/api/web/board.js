// foldText lowercases and drops Vietnamese accents, so "ky su" finds "Kỹ sư".
function foldText(v) {
  return String(v ?? "").normalize("NFD").replace(/[\u0300-\u036f]/g, "").replace(/đ/gi, "d").toLowerCase();
}

// formatSalary shortens what job sites write in đồng: "16000000 - 22000000
// VND/month" reads "16–22 triệu/tháng". Anything else is shown as written.
function formatSalary(s) {
  if (!/vnd/i.test(s) || !/\d{6,}/.test(s)) return s;
  const millions = n => {
    const m = Number(n) / 1e6;
    return Number(m.toFixed(1)).toLocaleString(I18N.lang);
  };
  let out = s.replace(/(\d{6,})\s*-\s*(\d{6,})/, (_, a, b) => `${millions(a)}–${millions(b)}`)
    .replace(/\d{6,}/g, n => millions(n));
  out = out.replace(/\s*VND\s*\/\s*month/i, " triệu/tháng").replace(/\s*VND\s*\/\s*year/i, " triệu/năm").replace(/\s*VND\b/i, " triệu");
  return out;
}

// payUndisclosed: no salary, or a word instead of a figure — thỏa thuận /
// thương lượng / cạnh tranh / negotiable / competitive, any case or accents.
// Decides the 💰 chip; the "Lương thỏa thuận" pill comes from the server.
function payUndisclosed(salary) {
  const s = foldText(salary).trim();
  return !s || /thoa thuan|thuong luong|canh tranh|negotiable|competitive/.test(s);
}

// How many cards a lane shows before "Show N more".
const LANE_PAGE = 10;
// Lanes opened with "Show N more", cards revealed from elsewhere (a Today item,
// a duplicate paste) that stay shown past the cut, and which <details> are
// open: a refresh rebuilds every card, and none of these should reset.
const fullLanes = new Set();
const revealedCards = new Set();
const openDetails = new Set();

const SORT_RATED = "Best rated first";
const SORT_NEWEST = "Newest first";

function isRated(job) {
  return job.score_status === "scored";
}

// savedSort: the user's choice, else best rated as soon as anything is rated.
function savedSort(jobs) {
  return AppState.sorting.Saved || (jobs.some(isRated) ? SORT_RATED : SORT_NEWEST);
}

function matchesSearch(job) {
  const hay = foldText([job.title, job.company, job.location, job.skills, job.salary, job.notes, job.discovery_source].join(" "));
  return foldText(AppState.filters.query).split(/\s+/).filter(Boolean).every(w => hay.includes(w));
}

function renderBoard() {
  const columns = ["Saved", "Waiting to apply", "Applied", "Interview", "Offer", "Closed"];
  const container = document.getElementById("kanbanBoard");
  const focus = boardFocus();
  container.innerHTML = "";

  const filteredJobs = AppState.jobs.filter(matchesSearch);

  columns.forEach(colName => {
    let colJobs = filteredJobs.filter(j => j.status === colName);
    const newest = (a, b) => new Date(b.saved_at) - new Date(a.saved_at);
    const sortMethod = colName === "Saved" ? savedSort(colJobs) : SORT_NEWEST;
    colJobs.sort((a, b) => {
      // Pin stays at the top always
      if (a.pinned && !b.pinned) return -1;
      if (!a.pinned && b.pinned) return 1;
      if (sortMethod === SORT_RATED) {
        // Unrated jobs go after every rated one, however low it scored.
        const rank = j => isRated(j) ? Number(j.score) : -1;
        return rank(b) - rank(a) || newest(a, b);
      }
      return newest(a, b);
    });

    const isCollapsed = localStorage.getItem(`col_collapsed_${colName}`) === "true";
    const folded = isLaneFolded(colName, colJobs.length);
    const isDesktop = window.innerWidth > 768;

    const colEl = document.createElement("div");
    colEl.className = `column ${isCollapsed ? "" : "expanded"}${folded ? " folded" : ""}${colJobs.length ? "" : " empty"}`;
    colEl.id = `col-${colName.replace(/\s+/g, "-")}`;
    colEl.setAttribute("data-status", colName);

    // Column Header
    let sortSelectorHtml = "";
    if (colName === "Saved" && colJobs.some(isRated) && colJobs.length > 1) {
      sortSelectorHtml = `
        <button class="column-sort" title="${esc(t("Change the order"))}" data-sort="${sortMethod}" onclick="toggleSavedSort(event)">
          ${t(sortMethod)} ▾
        </button>
      `;
    }

    colEl.innerHTML = `
      <div class="column-header" role="button" tabindex="0" aria-expanded="${isDesktop ? !folded : !isCollapsed}"
           onclick="toggleColumnCollapse('${colName}')" onkeydown="laneHeaderKey(event, '${colName}')"
           title="${isDesktop ? esc(folded ? t("Show {lane}", {lane: t(colName)}) : t("Fold {lane} away", {lane: t(colName)})) : ""}">
        <h2 class="column-title">${esc(t(colName))}</h2>
        <span class="column-count">${colJobs.length.toLocaleString(I18N.lang)}</span>
      </div>
      ${sortSelectorHtml}
      <div class="column-cards" id="cards-${colName.replace(/\s+/g, "-")}"></div>
    `;

    const cardsContainer = colEl.querySelector(".column-cards");

    // A long lane shows its first cards and a button for the rest.
    const showAll = fullLanes.has(colName);
    const shown = showAll ? colJobs : colJobs.filter((j, i) => i < LANE_PAGE || j.pinned || revealedCards.has(j.id));
    shown.forEach(job => renderJobCard(job, cardsContainer));
    const hidden = colJobs.length - shown.length;
    if (hidden > 0 || (showAll && colJobs.length > LANE_PAGE)) {
      const more = document.createElement("button");
      more.className = "btn-secondary btn-sm lane-more";
      more.textContent = hidden > 0 ? t("Show {n} more", { n: hidden.toLocaleString(I18N.lang) }) : t("Show fewer");
      more.addEventListener("click", () => {
        if (hidden > 0) fullLanes.add(colName); else fullLanes.delete(colName);
        renderBoard();
      });
      cardsContainer.appendChild(more);
    }

    // Set up column drag events
    setupColumnDragEvents(colEl);

    container.appendChild(colEl);
  });
  restoreBoardFocus(focus);
}

// Rebuilding the board drops keyboard focus: remember where it was, and put
// it back on the same control of the same card or lane.
const FOCUSABLE = "a, button, input, select, textarea, summary";

function boardFocus() {
  const el = document.activeElement;
  if (!el || !el.closest || !el.closest("#kanbanBoard")) return null;
  const card = el.closest(".card");
  if (card) return { card: card.dataset.id, index: [...card.querySelectorAll(FOCUSABLE)].indexOf(el) };
  const col = el.closest(".column");
  const part = ["column-header", "column-sort", "lane-more"].find(c => el.classList.contains(c));
  return col && part ? { selector: `#${col.id} .${part}` } : null;
}

function restoreBoardFocus(focus) {
  if (!focus) return;
  const el = focus.selector
    ? document.querySelector(focus.selector)
    : document.querySelector(`.card[data-id="${CSS.escape(focus.card)}"]`)?.querySelectorAll(FOCUSABLE)[focus.index];
  if (el) el.focus({ preventScroll: true });
}

function toggleSavedSort(e) {
  e.stopPropagation();
  AppState.sorting.Saved = e.currentTarget.dataset.sort === SORT_RATED ? SORT_NEWEST : SORT_RATED;
  renderBoard();
}

function laneHeaderKey(e, colName) {
  if (e.key !== "Enter" && e.key !== " ") return;
  e.preventDefault();
  toggleColumnCollapse(colName);
}

// Desktop lanes fold into a narrow strip (name and count) so six lanes fit:
// by default Closed and any empty lane are folded (a folded strip still takes
// dropped cards, and a lane unfolds once it has some). Clicking a header
// folds or unfolds it for good. Phones use the accordion below instead.
function isLaneFolded(colName, count) {
  if (window.innerWidth <= 768) return false;
  const v = localStorage.getItem(`lane_folded_${colName}`);
  if (v !== null) return v === "true";
  return colName === "Closed" || count === 0;
}

function toggleColumnCollapse(colName) {
  if (window.innerWidth > 768) {
    const count = AppState.jobs.filter(j => j.status === colName).length;
    localStorage.setItem(`lane_folded_${colName}`, String(!isLaneFolded(colName, count)));
    renderBoard();
    return;
  }
  const key = `col_collapsed_${colName}`;
  const isCollapsed = localStorage.getItem(key) === "true";
  localStorage.setItem(key, String(!isCollapsed));
  renderBoard();
}

function setCardDetailsOpen(key, open) {
  if (open) openDetails.add(key); else openDetails.delete(key);
}

// clockBadge shows the job's clock from the server (jobs.ClockOf, the same
// rules as the hourly sweep): an open job's deadline, or the silence since
// applying. Kept or pinned cards never move, so they get no countdown.
// Red and amber are warnings; grey is only information.
function clockBadge(job) {
  const badge = (level, text, why) => ({ level, html: `<span class="expiry-badge ${level}"${why ? ` title="${esc(why)}"` : ""}>${text}</span>` });
  const toClosed = (days, why) => badge("red", t("⛔ to Closed in {n}d", { n: Math.max(0, days).toLocaleString(I18N.lang) }), `${why} ${t("Turn on Keep or pin it to stop that.")}`);
  const clock = job.clock;
  if (clock && clock.kind === "deadline") {
    if (job.keep) return badge("grey", t("🛟 kept"));
    if (job.pinned) return clock.state === "expired" ? badge("red", t("⛔ closed")) : badge("amber", t("📅 deadline"));
    if (clock.state === "expired") return toClosed(clock.closes_in, t("The posting has closed: this card moves to Closed 7 days after its deadline."));
    if (clock.state === "warning") return badge("amber", t("⚠ {n}d left", { n: clock.days.toLocaleString(I18N.lang) }));
    return badge("grey", fmtDate(`${clock.date}T00:00:00`, { day: 'numeric', month: 'short' }), t("Closing date"));
  }
  if (clock && clock.kind === "applied") {
    if (job.keep) return badge("grey", t("🛟 kept"));
    const waiting = badge("grey", t("📮 {n}d", { n: clock.days.toLocaleString(I18N.lang) }), t("Days since you applied"));
    if (job.pinned) return clock.state === "silent" ? waiting : badge("grey", t("⛔ no reply"));
    if (clock.state !== "silent") return toClosed(clock.closes_in, t("No answer yet: this card moves to Closed 27 days after applying."));
    return waiting;
  }
  return null;
}

// Dealbreakers come with the job, computed once by the server
// (jobs.Dealbreakers: Saturday work, unstated pay, too far, a new company).
function dealbreakerPill(text, title) {
  return `<span class="badge dealbreaker"${title ? ` title="${esc(title)}"` : ""}>⛔ ${esc(text)}</span>`;
}

// cardWarning is the one warning a closed card has room for: a clock about to
// close it, then a dealbreaker (the others in its tooltip), then a near deadline.
function cardWarning(job) {
  const clock = clockBadge(job);
  if (clock && clock.level === "red") return clock.html;
  const dealbreakers = job.dealbreakers || [];
  if (dealbreakers.length) {
    const extra = dealbreakers.length > 1 ? ` +${dealbreakers.length - 1}` : "";
    return dealbreakerPill(dealbreakers[0] + extra, dealbreakers.length > 1 ? dealbreakers.join(", ") : "");
  }
  return clock && clock.level === "amber" ? clock.html : "";
}

// Render Job Card Component
function renderJobCard(job, container) {
  const cardEl = document.createElement("div");
  const isOpen = AppState.expandedCards.has(job.id);
  // Visual border accents
  let extraClass = "";
  if (job.pinned) extraClass += " pinned";
  if (job.apply_state === "sent") extraClass += " has-sent";
  if (isOpen) extraClass += " open";

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
  // Anywhere on the card that isn't a control opens it.
  cardEl.addEventListener("click", (e) => {
    if (!e.target.closest(`${FOCUSABLE}, label, .card-detail`)) toggleCardDetail(job.id);
  });

  // Score Badge: only once there is something to say (a bare "—" on every
  // unrated card said nothing).
  let scoreClass = "score-f";
  if (job.score_letter === "A") scoreClass = "score-a";
  else if (job.score_letter === "B") scoreClass = "score-b";
  else if (job.score_letter === "C") scoreClass = "score-c";

  let scoreHtml = "";
  if (job.score_status === "scoring") {
    scoreHtml = `<span class="score-badge score-pending" style="font-size:11px" title="${esc(t("Your AI is rating this job"))}">${t("rating…")}</span>`;
  } else if (job.score_status === "failed") {
    scoreHtml = `<button class="score-badge score-f score-retry" onclick="retryScoring('${job.id}')" title="${esc(t("Rating failed. Click to try again."))}">${t("Rating failed")}</button>`;
  } else if (job.score_status === "stuck") {
    scoreHtml = `<button class="score-badge score-f score-retry" onclick="retryScoring('${job.id}')" title="${esc(t("Rating got stuck. Click to try again."))}">${t("Rating stuck")}</button>`;
  } else if (isRated(job)) {
    scoreHtml = `<span class="score-badge ${scoreClass}" title="${esc(t("How well this job fits you"))}">${esc(job.score_letter)} ${Number(job.score).toLocaleString(I18N.lang)}</span>`;
  }

  // Salary chip
  const salaryHtml = job.salary && !payUndisclosed(job.salary) ? `<span class="salary-chip" title="${esc(job.salary)}">💰 ${esc(formatSalary(job.salary))}</span>` : "";

  // Work in progress, and the questions a closed card must still ask.
  let automationHtml = "";
  if (job.status === "Waiting to apply") {
    if (job.cv_status === "tailoring") {
      const elapsed = formatDuration(Date.now() - new Date(job.saved_at).getTime() / 2); // simulated duration
      automationHtml = `<span class="apply-badge tailoring">${t("✍️ Tailoring CV ({time})", { time: elapsed })}</span>`;
    } else if (job.draft_status === "drafting") {
      automationHtml = `<span class="apply-badge tailoring">${t("✍️ Drafting email...")}</span>`;
    }
  } else if (job.status === "Applied") {
    if (job.apply_state === "queued") {
      automationHtml = `<span class="apply-badge queued">${t("🕒 queued")}</span>`;
    } else if (job.apply_state === "filling") {
      automationHtml = `<span class="apply-badge queued">${t("✍️ filling…")}</span>`;
    }
  }

  if (job.apply_state === "confirm") {
    const site = siteFor(job);
    automationHtml = `<span class="apply-confirm">${site ? t("Did you submit it on {site}?", { site: esc(site.name) }) : t("Did you submit it?")}
      <button class="btn-primary btn-sm" onclick="confirmApplied('${job.id}', true)">${t("Yes, I applied")}</button>
      <button class="btn-secondary btn-sm" onclick="confirmApplied('${job.id}', false)">${t("Not yet")}</button></span>`;
  }

  if (job.apply_state === "failed") {
    automationHtml = `<button class="apply-badge failed" onclick="retryApply('${job.id}')" title="${esc(t("Applying on the site failed. Click to try again."))}">${t("✕ Apply failed: try again")}</button>`;
  }

  // Move-to dropdown items
  const stages = ["Saved", "Waiting to apply", "Applied", "Interview", "Offer", "Closed"];
  const selectOptions = stages
    .map(st => `<option value="${st}" ${job.status === st ? "selected" : ""}>${esc(t(st))}</option>`)
    .join("");

  // An open card lists every warning itself; a failed apply is warning enough.
  const warningHtml = isOpen || job.apply_state === "failed" ? "" : cardWarning(job);

  cardEl.innerHTML = `
    <div class="card-header">
      <div class="card-top-row">
        <div class="card-title-group">
          <a href="${safeUrl(job.url)}" target="_blank" rel="noopener noreferrer" class="card-title" onclick="event.stopPropagation()">${esc(job.title)}</a>
          <div class="card-company">${esc(job.company)}</div>
        </div>
        <div class="card-actions-group">
          ${job.pinned ? `<span class="card-pin" role="img" aria-label="${esc(t("Pinned"))}" title="${esc(t("Pinned: stays at the top"))}">📌</span>` : ""}
          ${scoreHtml}
        </div>
      </div>
    </div>

    ${salaryHtml || warningHtml ? `<div class="card-meta">${salaryHtml}${warningHtml}</div>` : ""}
    ${automationHtml ? `<div class="card-status">${automationHtml}</div>` : ""}

    <div class="card-footer">
      <!-- Quick stage jump dropdown (accessible alternative to drag) -->
      <select class="move-select" title="${esc(t("Move this job to another stage"))}" aria-label="${esc(t("Stage: {title}", { title: job.title }))}" onchange="moveStageDropdown('${job.id}', this)" onclick="event.stopPropagation()">
        ${selectOptions}
      </select>
      <div class="card-footer-actions">
        ${job.url ? `<a href="${safeUrl(job.url)}" target="_blank" rel="noopener noreferrer" class="card-link" title="${esc(t("Open the job on its site"))}" aria-label="${esc(t("Open the job: {title}", { title: job.title }))}" onclick="event.stopPropagation()">${t("Open ↗")}</a>` : ""}
        <button class="card-link expand-btn" title="${esc(isOpen ? t("Hide the details") : t("Show the details"))}" aria-label="${esc(t("Details: {title}", { title: job.title }))}" aria-expanded="${isOpen}" onclick="toggleCardDetail('${job.id}')">${isOpen ? t("Hide ▴") : t("Details ▾")}</button>
      </div>
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

// cardFacts: what a closed card leaves out to stay short.
function cardFacts(job) {
  const facts = [];
  const clock = clockBadge(job);
  if (clock) facts.push(clock.html);
  (job.dealbreakers || []).forEach(d => facts.push(dealbreakerPill(d)));

  // Distance badge: ✅ within your maximum commute, 🔴 beyond it.
  const locSet = AppState.location && AppState.location.set;
  if (job.company_geo_status === "ok" && typeof job.company_distance_km === "number") {
    const km = job.company_distance_km;
    const icon = km <= ((AppState.location && AppState.location.max_km) || 10) ? "✅" : "🔴";
    facts.push(`<span class="intel-badge" title="${esc(t("Distance from your location"))}">${icon} ${km.toLocaleString(I18N.lang, { minimumFractionDigits: km < 10 ? 1 : 0, maximumFractionDigits: km < 10 ? 1 : 0 })} km</span>`);
  } else if (!locSet) {
    // No location yet: nothing to measure from, so no badge (📍 Location sets it).
  } else if (job.company_geo_status === "geocode_failed") {
    facts.push(`<span class="intel-badge" title="${esc(t("This job's address couldn't be found on the map."))}">📍 ${t("unknown")}</span>`);
  } else if (job.company_geo_status === "no_address") {
    facts.push(`<span class="intel-badge" title="${esc(t("This job has no address. Add one under Edit details."))}">📍 ${t("no address")}</span>`);
  } else {
    facts.push(`<span class="intel-badge" title="${esc(t("Looking up this address…"))}">📍 …</span>`);
  }

  // Company active date (masothue link)
  if (job.company_active_date === "🏢 …" || job.company_active_date === "🏢 Unknown") {
    facts.push(`<span class="intel-badge" title="${esc(t("When the company started"))}">${esc(t(job.company_active_date))}</span>`);
  } else if (job.company_active_date) {
    const year = new Date(job.company_active_date).getFullYear();
    facts.push(`<a href="https://masothue.com/Search/?q=${encodeURIComponent(job.company)}" target="_blank" rel="noopener noreferrer" class="intel-badge" title="${esc(t("The year the company started"))}" onclick="event.stopPropagation()">📅 ${year}</a>`);
  }

  // Platform badges
  if (job.topcv_status) {
    // Codes written by the TopCV status sync (internal/core/topcv).
    const labels = {
      sent: "📨 TopCV: sent",
      seen: "👁 TopCV: CV viewed",
      suitable: "✅ TopCV: suitable",
      rejected: "❌ TopCV: not suitable"
    };
    facts.push(`<span class="apply-badge" title="${esc(t(job.apply_detail))}">${esc(t(labels[job.topcv_status] || job.topcv_status))}</span>`);
  } else if (job.discovery_source === "VietnamWorks" && job.vnw_status) {
    facts.push(`<span class="apply-badge">${job.vnw_status === "seen" ? t("👁 seen") : esc(job.vnw_status)}</span>`);
  }

  // Sent markers
  if (job.apply_state === "sent") {
    facts.push(`<span class="apply-badge sent">${t("✓ sent")}</span>`);
  } else if (job.apply_state === "replied") {
    facts.push(`<span class="apply-badge sent">${t("✓ replied")}</span>`);
  }
  if (job.followup_sent_at) {
    facts.push(`<span class="apply-badge sent">${t("📨 followup")}</span>`);
  }

  facts.push(`<span class="age-label" title="${esc(t("When it was found, and where"))}">${esc(timeAgo(job.saved_at))} · ${esc(sourceName(job.discovery_source))}</span>`);
  return facts.join("");
}

// primaryAction is the one obvious next step for the job's stage.
function primaryAction(job) {
  const move = (label, to) => `<button class="btn-primary card-primary" onclick="transitionCardStatus('${job.id}', '${to}')">${label}</button>`;
  switch (job.status) {
    case "Applied": return move(t("Got an interview"), "Interview");
    case "Interview": return move(t("Got an offer"), "Offer");
    case "Offer": return move(t("Close this job"), "Closed");
    case "Closed": return move(t("Back to Saved"), "Saved");
  }
  // The card is already asking "did you submit it?".
  if (job.apply_state === "confirm") return "";
  const apply = applyButton(job, job.cv_status === "ready");
  if (apply) return apply;
  if (draftIsPrimary(job)) {
    return `<button class="btn-primary card-primary" onclick="sendEmailDraft('${job.id}')">${t("Send from my mail app")}</button>`;
  }
  if (job.url) {
    return `<a class="btn-primary card-primary" href="${safeUrl(job.url)}" target="_blank" rel="noopener noreferrer">${t("Open the job ↗")}</a>`;
  }
  return move(t("I applied"), "Applied");
}

// draftIsPrimary: the card's main button sends the unsent email draft.
function draftIsPrimary(job) {
  return (job.status === "Saved" || job.status === "Waiting to apply") &&
    job.apply_state !== "confirm" && !applyButton(job, job.cv_status === "ready") &&
    job.apply_method !== "on-site" && !!job.draft_email && job.draft_status !== "sent";
}

// Render expanded card detail panel contents
function renderCardDetailContents(job, detailContainer) {
  // 1. Why this rating: the first points show; the rest wait under More.
  const reasons = (job.score_reasoning || "")
    // Raters answer "point • point • point" (see the rubric); older entries
    // used one point per line. Accept both.
    .split(/\n|\s+•\s+/)
    .map(line => line.trim().replace(/^•\s*/, ""))
    .filter(Boolean)
    .map(item => `<li>${esc(item)}</li>`);
  const whyHtml = reasons.length ? `
    <div class="detail-section">
      <div class="detail-title">${t("Why this rating")}</div>
      <ul class="assessment-list">${reasons.slice(0, 3).join("")}</ul>
    </div>` : "";
  const ratingHtml = `
    <div class="detail-section">
      <div class="detail-title">${t("Rating")}</div>
      ${reasons.length > 3 ? `<ul class="assessment-list">${reasons.slice(3).join("")}</ul>` : ""}
      <button class="btn-secondary btn-sm" style="align-self:flex-start;" onclick="askAI('rate', '${job.id}')">${isRated(job) ? t("Rate again") : t("Rate")}</button>
    </div>`;

  // 2. Other raters, for those who turned on comparing them in Settings.
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
                ${esc(model)}: ${esc(rating.letter)} ${Number(rating.score).toLocaleString(I18N.lang)}
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
          <button class="btn-secondary btn-sm" style="padding: 4px 10px; font-size:12px;" onclick="askAnotherModel('${job.id}')">${t("Ask Model")}</button>
        </div>
      `;
    }

    otherModelsHtml = `
      <div class="detail-section" style="border-top: 1px solid var(--hairline); padding-top: 8px;">
        <div class="detail-title">${t("Other AI ratings")}</div>
        ${list || `<div style="font-size:12px; color:var(--muted)">${t("No other AI has rated this job yet.")}</div>`}
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
        <div class="detail-title">${t("Skills")}</div>
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
        <div class="detail-title">${t("Summary")}</div>
        <ul class="assessment-list">${content}</ul>
      </div>
    `;
  }

  // 5. Notes block
  const notesHtml = `
    <div class="detail-section">
      <div class="detail-title">${t("My notes (saved as you type)")}</div>
      <textarea class="textarea-input" style="height: 60px; font-size: 12px;" aria-label="${esc(t("My notes"))}" placeholder="${esc(t("Add custom notes..."))}" oninput="saveJobNotes('${job.id}', this)">${esc(job.notes)}</textarea>
    </div>
  `;

  // 5b. Edit details (fix a scraped title, a group-id "company", …)
  const field = (key, label, value) => `
      <label style="font-size:11px; font-weight:600; color:var(--muted)">${esc(t(label))}</label>
      <input type="text" class="text-input" id="edit-${key}-${job.id}" value="${esc(value)}" style="font-size:12px; padding: 4px 8px; margin-bottom: 4px;">`;
  const editHtml = `
    <details class="detail-section edit-details" style="border-top: 1px solid var(--hairline); padding-top: 8px;" ${openDetails.has(`edit-${job.id}`) ? "open" : ""} ontoggle="setCardDetailsOpen('edit-${job.id}', this.open)">
      <summary class="detail-title" style="cursor:pointer;">${t("Edit details")}</summary>
      <div style="display:flex; flex-direction:column; margin-top:6px;">
        ${field("title", "Title", job.title)}
        ${field("company", "Company", job.company)}
        ${field("salary", "Salary", job.salary)}
        ${field("location", "Location", job.location)}
        <button class="btn-secondary btn-sm" style="align-self:flex-start; margin-top:4px;" onclick="saveJobDetails('${job.id}')">${t("Save details")}</button>
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
        btnLabel = "Undo: I haven't applied";
        btnAction = `undoOnSiteApply('${job.id}')`;
        undoBtn = `<p style="font-size:12px; margin: 4px 0 0 0; color: var(--success-border)">${t("✓ You applied on the site.")}</p>`;
      }

      applyBlockHtml = `
        <div class="detail-section" style="border-top: 1px dashed var(--border-strong); padding-top: 12px; margin-top: 4px;">
          <div class="detail-title">${esc(t("🌐 Apply on {site}", { site: sourceName(job.discovery_source) }))}</div>
          <div style="margin-top: 6px;">
            <button class="btn-secondary btn-sm" onclick="${btnAction}">${esc(t(btnLabel))}</button>
            ${undoBtn}
          </div>
        </div>
      `;
    } else {
      // Email application: your AI writes the draft (Ask AI), you review and
      // edit it here, and the card's main button opens it in your own mail app
      // — braveduck never sends email and holds no mail password.
      const draft = job.draft_email;
      const isSent = job.draft_status === "sent";
      let emailEditorHtml;
      if (!draft) {
        emailEditorHtml = `
          <p style="font-size:12px; color:var(--muted); margin: 4px 0;">${t("No draft yet.")}</p>
          <textarea class="textarea-input" id="draftNotes-${job.id}" style="height:40px; font-size:12px;" aria-label="${esc(t("Notes for your AI"))}" placeholder="${esc(t("Optional notes for your AI (tone, what to stress)…"))}">${esc(job.draft_notes)}</textarea>
          <button class="btn-secondary btn-sm" style="margin-top:6px; align-self:flex-start;" onclick="askAI('draft', '${job.id}')">${t("Ask your AI to draft it")}</button>`;
      } else {
        const ro = isSent ? "readonly" : "";
        emailEditorHtml = `
          <div style="margin-top: 6px; display: flex; flex-direction: column; gap: 6px;">
            <div>
              <label for="draftTo-${job.id}" style="font-size:11px; font-weight:600; color:var(--muted)">${t("Recipient")}</label>
              <input type="text" id="draftTo-${job.id}" class="text-input" value="${esc(draft.to)}" ${ro} style="font-size:12px; padding: 4px 8px;" placeholder="${esc(t("recruiter@company.com"))}">
            </div>
            <div>
              <label for="draftSubject-${job.id}" style="font-size:11px; font-weight:600; color:var(--muted)">${t("Subject")}</label>
              <input type="text" id="draftSubject-${job.id}" class="text-input" value="${esc(draft.subject)}" ${ro} style="font-size:12px; padding: 4px 8px;">
            </div>
            <div>
              <label for="draftBody-${job.id}" style="font-size:11px; font-weight:600; color:var(--muted)">${t("Email Body")}</label>
              <textarea id="draftBody-${job.id}" class="textarea-input" style="height: 140px; font-size: 12px;" ${ro}>${esc(draft.body)}</textarea>
            </div>
            ${isSent
              ? `<p style="font-size:12px; color: var(--success-border); font-weight: 500; margin-top: 6px;">${t("✓ Sent (you confirmed sending it).")}</p>`
              : `<div style="display: flex; gap: var(--spacing-xs); flex-wrap: wrap;">
                   ${!draftIsPrimary(job) ? `<button class="btn-secondary btn-sm" onclick="sendEmailDraft('${job.id}')">${t("Send from my mail app")}</button>` : ""}
                   <button class="btn-secondary btn-sm" onclick="askAI('draft', '${job.id}')">${t("Re-draft")}</button>
                   <button class="btn-danger btn-sm" style="padding: 6px 12px; font-size:12px;" onclick="rejectEmailDraft('${job.id}')">${t("Discard")}</button>
                 </div>
                 <p style="font-size:11px; color:var(--muted); margin:0;">${t("Attach the tailored CV PDF in your mail app before sending.")}</p>`}
          </div>`;
      }

      applyBlockHtml = `
        <div class="detail-section" style="border-top: 1px dashed var(--border-strong); padding-top: 12px; margin-top: 4px;">
          <div class="detail-title">${t("✉ Email")}</div>
          ${emailEditorHtml}
        </div>
      `;
    }
  } else if (job.status === "Interview") {
    // Interview reply: written by you, sent from your own mail app.
    applyBlockHtml = `
      <div class="detail-section" style="border-top: 1px dashed var(--border-strong); padding-top: 12px; margin-top: 4px;">
        <div class="detail-title">${t("📨 Reply to the recruiter")}</div>
        <div style="margin-top: 6px; display: flex; flex-direction: column; gap: 6px;">
          <input type="text" id="replyTo-${job.id}" class="text-input" aria-label="${esc(t("Recruiter's email"))}" value="${esc((job.draft_email && job.draft_email.to) || job.contact_email)}" style="font-size:12px; padding: 4px 8px;" placeholder="${esc(t("recruiter@company.com"))}">
          <textarea id="replyBody-${job.id}" class="textarea-input" aria-label="${esc(t("Your reply"))}" style="height: 80px; font-size: 12px;" placeholder="${esc(t("Your reply…"))}"></textarea>
          <button class="btn-primary btn-sm" style="align-self:flex-start;" onclick="sendInterviewReply('${job.id}')">${t("Open in my mail app")}</button>
        </div>
      </div>
    `;
  }

  // 7. Tailored CV (your AI writes it; braveduck renders the PDF)
  let cvControlsHtml = "";
  if (job.status === "Saved" || job.status === "Waiting to apply" || job.status === "Applied") {
    const ready = job.cv_status === "ready";
    const when = ready && job.cv_generated_at ? new Date(job.cv_generated_at).toLocaleString(I18N.lang, { dateStyle: "short", timeStyle: "short" }) : "";
    cvControlsHtml = `
      <div class="detail-section" style="border-top: 1px solid var(--hairline); padding-top: 8px;">
        <div class="detail-title">${t("CV for this job")}</div>
        <p style="font-size:12px; margin: 2px 0 6px 0; color: var(--body);">${ready ? esc(t("Version {n} · {date}", { n: job.cv_version, date: when })) : t("Not written yet.")}</p>
        <div style="display: flex; gap: var(--spacing-xs); flex-wrap: wrap;">
          ${ready ? `<button class="btn-secondary btn-sm" onclick="downloadCV('${job.id}')">${t("Download CV.pdf")}</button>` : ""}
          <button class="btn-secondary btn-sm" onclick="askAI('cv', '${job.id}')">${ready ? t("Ask your AI to improve it") : t("Ask your AI to tailor it")}</button>
        </div>
      </div>
    `;
  }

  // 8. Quick actions: Keep (the escape hatch from moving to Closed by itself),
  // "I applied", Pin and Delete.
  const canKeep = job.expires_at || job.status === "Applied" || job.status === "Interview";
  const canMarkApplied = (job.status === "Saved" || job.status === "Waiting to apply") && job.url && job.apply_state !== "confirm";
  const quickHtml = `
    <div class="card-quick">
      ${canKeep ? `<button class="keep-toggle" role="switch" aria-checked="${Boolean(job.keep)}" title="${esc(t("Keep this job: it won't move to Closed by itself"))}" onclick="toggleKeepExemption('${job.id}')"><span class="keep-track" aria-hidden="true"></span>${t("Keep")}</button>` : ""}
      ${canMarkApplied ? `<button class="btn-secondary btn-sm" onclick="transitionCardStatus('${job.id}', 'Applied')">${t("I applied")}</button>` : ""}
      ${job.status === "Saved" ? `<button class="btn-secondary btn-sm" title="${esc(job.pinned ? t("Stop keeping it at the top") : t("Keep it at the top of the lane"))}" aria-pressed="${Boolean(job.pinned)}" onclick="togglePin('${job.id}')">📌 ${job.pinned ? t("Unpin") : t("Pin")}</button>` : ""}
      <button class="btn-secondary btn-sm card-delete" title="${esc(t("Delete this job"))}" aria-label="${esc(t("Delete job: {title}", { title: job.title }))}" onclick="deleteJobCard('${job.id}')">${t("🗑 Delete")}</button>
    </div>
  `;

  detailContainer.innerHTML = `
    <div class="card-facts">${cardFacts(job)}</div>
    ${whyHtml}
    ${primaryAction(job)}
    <details class="card-more" ${openDetails.has(`more-${job.id}`) ? "open" : ""} ontoggle="setCardDetailsOpen('more-${job.id}', this.open)">
      <summary>${t("More")}</summary>
      <div class="card-more-body">
        ${quickHtml}
        ${notesHtml}
        ${cvControlsHtml}
        ${applyBlockHtml}
        ${ratingHtml}
        ${otherModelsHtml}
        ${skillsHtml}
        ${summaryHtml}
        ${editHtml}
      </div>
    </details>
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
  const job = AppState.jobs.find(j => j.id === id);
  if (!job) {
    showToast(t("Job #{id} is no longer on the board", { id }), "info");
    return;
  }
  // A direct link to a job must reveal it even when the search, "Show N more"
  // or the mobile accordion hide it. Keep a matching search as it is.
  const selector = `.card[data-id="${CSS.escape(id)}"]`;
  if (!matchesSearch(job)) {
    AppState.filters.query = "";
    document.getElementById("jobSearch").value = "";
  }
  revealedCards.add(id);
  if (window.innerWidth <= 768) {
    localStorage.setItem(`col_collapsed_${job.status}`, "false");
  } else if (isLaneFolded(job.status, 1)) {
    localStorage.setItem(`lane_folded_${job.status}`, "false");
  }
  AppState.expandedCards.add(id);
  renderBoard();
  const card = document.querySelector(selector);
  if (!card) return;
  card.scrollIntoView({ behavior: "smooth", block: "center" });
  card.classList.add("flash");
  setTimeout(() => card.classList.remove("flash"), 2000);
}

async function togglePin(id) {
  await APIClient.pinJob(id);
  refreshAppState();
}

async function deleteJobCard(id) {
  if (confirm(t("Are you sure you want to delete this job card?"))) {
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
  const model = document.getElementById(`askModelSelect-${id}`)?.value;
  await askAI("rate", id, model);
}
