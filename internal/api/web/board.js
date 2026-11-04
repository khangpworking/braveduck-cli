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
    return Number.isInteger(m) ? String(m) : m.toFixed(1).replace(/\.0$/, "");
  };
  let out = s.replace(/(\d{6,})\s*-\s*(\d{6,})/, (_, a, b) => `${millions(a)}–${millions(b)}`)
    .replace(/\d{6,}/g, n => millions(n));
  out = out.replace(/\s*VND\s*\/\s*month/i, " triệu/tháng").replace(/\s*VND\s*\/\s*year/i, " triệu/năm").replace(/\s*VND\b/i, " triệu");
  return out;
}

// payUndisclosed: no salary, or a word instead of a figure — thỏa thuận /
// thương lượng / cạnh tranh / negotiable / competitive, any case or accents.
function payUndisclosed(salary) {
  const s = foldText(salary).trim();
  return !s || /thoa thuan|thuong luong|canh tranh|negotiable|competitive/.test(s);
}

// worksSaturday: the job text says Saturday is a working day ("thứ 2 - thứ
// 7", "T7", "Saturday"), as a whole word, and not negated ("nghỉ thứ 7",
// "không làm T7", "trừ thứ 7", "off on Saturday", "Saturdays off").
function worksSaturday(jd) {
  const text = foldText(jd);
  const re = /(^|[^a-z0-9])(t7|thu 7|thu bay|saturdays?)(?=$|[^a-z0-9])/g;
  let m;
  while ((m = re.exec(text))) {
    const before = text.slice(Math.max(0, m.index - 14), m.index + m[1].length);
    const after = text.slice(m.index + m[0].length, m.index + m[0].length + 8);
    const negated = /(nghi|khong lam|khong|tru|off|except|no)\s*(on\s*)?(lam\s*)?(vao\s*)?(ngay\s*)?$/.test(before) ||
      /^\s*(off|nghi)\b/.test(after);
    if (!negated) return true;
  }
  return false;
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
  const words = foldText(AppState.filters.query).split(/\s+/).filter(Boolean);
  if (words.length) {
    filteredJobs = filteredJobs.filter(j => {
      const hay = foldText([j.title, j.company, j.location, j.skills, j.salary, j.notes, j.discovery_source].join(" "));
      return words.every(w => hay.includes(w));
    });
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
    const folded = isLaneFolded(colName, colJobs.length);

    const colEl = document.createElement("div");
    colEl.className = `column ${isCollapsed ? "" : "expanded"}${folded ? " folded" : ""}`;
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
      <div class="column-header" onclick="toggleColumnCollapse('${colName}')"
           title="${window.innerWidth > 768 ? (folded ? `Show ${colName}` : `Fold ${colName} away`) : ""}">
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
  if (worksSaturday(job.jd_full)) {
    dealbreakers.push("⛔ Làm T7");
  }
  if (payUndisclosed(job.salary)) {
    dealbreakers.push("⛔ Lương thỏa thuận");
  }
  const maxKm = (AppState.location && AppState.location.max_km) || 10;
  if (job.company_geo_status === "ok" && job.company_distance_km > maxKm) {
    dealbreakers.push(`⛔ ${Math.round(job.company_distance_km)} km away`);
  }
  if (job.company_active_date && job.company_active_date !== "🏢 …" && job.company_active_date !== "🏢 Unknown") {
    const year = new Date(job.company_active_date).getFullYear();
    if (new Date().getFullYear() - year < 2) {
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
  const salaryHtml = job.salary && !payUndisclosed(job.salary) ? `<span class="salary-chip" title="${esc(job.salary)}">💰 ${esc(formatSalary(job.salary))}</span>` : "";

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
    // No location yet: nothing to measure from, so no badge (📍 Location sets it).
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
  // Expiry badges follow the hourly sweep (service.SweepExpired): Saved or
  // Waiting jobs move to Closed 7 days after the posting closes; Applied jobs
  // 27 days after applying with no answer. Kept or pinned cards stay put.
  const DAY = 1000 * 60 * 60 * 24;
  // Whole calendar days from today to a YYYY-MM-DD date, in local time (the
  // sweep counts the same way).
  const startOfToday = new Date(new Date().setHours(0, 0, 0, 0));
  const calendarDays = iso => Math.round((new Date(`${iso}T00:00:00`) - startOfToday) / DAY);
  const notApplied = job.status === "Saved" || job.status === "Waiting to apply";
  const toClosed = (days, why) =>
    `<span class="expiry-badge red" title="${esc(why)} Keep 🛟 or pin it to stop that.">⛔ to Closed in ${days > 0 ? days : 0}d</span>`;
  if (notApplied && job.expires_at) {
    const deadline = new Date(`${job.expires_at}T00:00:00`);
    const daysLeft = calendarDays(job.expires_at);
    if (job.keep) {
      expiryHtml = `<span class="expiry-badge grey">🛟 kept</span>`;
    } else if (job.pinned) {
      expiryHtml = daysLeft < 0 ? `<span class="expiry-badge red">⛔ closed</span>` : `<span class="expiry-badge amber">📅 deadline</span>`;
    } else if (daysLeft < 0) {
      expiryHtml = toClosed(7 + daysLeft, "The posting has closed: this card moves to Closed 7 days after its deadline.");
    } else if (daysLeft <= 10) {
      expiryHtml = `<span class="expiry-badge amber">⚠ ${daysLeft}d left</span>`;
    } else {
      expiryHtml = `<span class="expiry-badge grey">${deadline.toLocaleDateString('vi-VN', { day: 'numeric', month: 'short' })}</span>`;
    }
  } else if (job.status === "Applied" && job.applied_on) {
    // Silence clock, from the day the application was sent.
    const daysApplied = -calendarDays(job.applied_on);
    if (job.keep) {
      expiryHtml = `<span class="expiry-badge grey">🛟 kept</span>`;
    } else if (job.pinned) {
      expiryHtml = daysApplied >= 20 ? `<span class="expiry-badge grey">⛔ no reply</span>` : `<span class="expiry-badge grey">📮 ${daysApplied}d</span>`;
    } else if (daysApplied >= 20) {
      expiryHtml = toClosed(27 - daysApplied, "No answer yet: this card moves to Closed 27 days after applying.");
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

  if (job.apply_state === "confirm") {
    const site = siteFor(job);
    automationHtml = `<span class="apply-confirm">Did you submit it${site ? ` on ${esc(site.name)}` : ""}?
      <button class="btn-primary btn-sm" onclick="confirmApplied('${job.id}', true)">Yes, I applied</button>
      <button class="btn-secondary btn-sm" onclick="confirmApplied('${job.id}', false)">Not yet</button></span>`;
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
    pinBtnHtml = `<button class="icon-btn-sm" title="${pinTitle}" aria-label="${pinTitle}: ${esc(job.title)}" aria-pressed="${isPinned}" onclick="togglePin('${job.id}')">${pinIcon}</button>`;
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
            <a href="${safeUrl(job.url)}" target="_blank" rel="noopener noreferrer" class="icon-btn-sm" title="Open original link" aria-label="Open the listing: ${esc(job.title)}" onclick="event.stopPropagation()">↗</a>
            ${pinBtnHtml}
            <button class="icon-btn-sm" title="Delete job" aria-label="Delete job: ${esc(job.title)}" style="color:var(--error);" onclick="deleteJobCard('${job.id}')">✕</button>
          </div>
        </div>
      </div>
    </div>
    
    <div class="card-meta">
      ${salaryHtml}
      ${masothueHtml}
      ${distanceHtml}
      ${dealbreakersHtml}
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
      <button class="icon-btn expand-btn" title="Toggle Details" aria-label="Details: ${esc(job.title)}" aria-expanded="${isOpen}" onclick="toggleCardDetail('${job.id}')">${isOpen ? "▴" : "▾"}</button>
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
  const rateAction = `<button class="btn-secondary btn-sm" onclick="askAI('rate', '${job.id}')">${job.score_status === "scored" ? "Rate again" : "Rate"}</button>`;
  let assessmentHtml = `<div class="detail-section">
    <div class="detail-title">AI Assessment (${esc(AppState.ratingConfig.default_model)})</div>
    ${rateAction}
  </div>`;
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
        ${rateAction}
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
          ${applyButton(job, ready)}
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
  const job = AppState.jobs.find(j => j.id === id);
  if (!job) {
    showToast(`Job #${id} is no longer on the board`, "info");
    return;
  }
  // A direct link to a job must reveal it even when the current filters or
  // mobile accordion hide it. Keep matching filters as they are.
  const selector = `.card[data-id="${CSS.escape(id)}"]`;
  if (!document.querySelector(selector)) {
    AppState.filters.platform = "All";
    AppState.filters.query = "";
    document.getElementById("jobSearch").value = "";
    renderPlatformFilters();
  }
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
  const model = document.getElementById(`askModelSelect-${id}`)?.value;
  await askAI("rate", id, model);
}
