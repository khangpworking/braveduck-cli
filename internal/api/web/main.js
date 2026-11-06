document.addEventListener("DOMContentLoaded", () => {
  applyI18n();
  const langBtn = document.getElementById("langBtn");
  const other = I18N.lang === "vi" ? "en" : "vi";
  langBtn.textContent = "🌐 " + I18N.names[other];
  langBtn.addEventListener("click", () => I18N.set(other));
  // Toggle Drawer
  const drawer = document.getElementById("addJobDrawer");
  const overlay = document.getElementById("drawerOverlay");
  const openBtn = document.getElementById("openDrawerBtn");
  const closeBtn = document.getElementById("closeDrawerBtn");

  function openDrawer() {
    drawer.classList.add("open");
    overlay.classList.add("open");
    renderFBGroups();
  }

  function closeDrawer() {
    drawer.classList.remove("open");
    overlay.classList.remove("open");
    // Clear extract results banner
    const banner = document.getElementById("extractResultBanner");
    banner.style.display = "none";
  }

  openBtn.addEventListener("click", openDrawer);
  document.getElementById("agentKeyNew").addEventListener("click", newAgentKey);
  document.getElementById("agentBtn").addEventListener("click", openAgentModal);
  document.getElementById("askConnectOpen").addEventListener("click", openAgentModal);
  document.getElementById("agentKeyRevoke").addEventListener("click", revokeAgentKey);
  closeBtn.addEventListener("click", closeDrawer);
  overlay.addEventListener("click", closeDrawer);

  // Add Job Button Event
  const extractBtn = document.getElementById("extractAddBtn");
  const pasteTextarea = document.getElementById("pasteJDTextarea");
  const extractResultBanner = document.getElementById("extractResultBanner");

  extractBtn.addEventListener("click", async () => {
    const content = pasteTextarea.value;
    if (!content.trim()) {
      showToast("Paste a link or the job text first", "info");
      pasteTextarea.focus();
      return;
    }
    try {
      extractBtn.disabled = true;
      extractBtn.innerText = "Adding…";
      
      const newJob = await APIClient.addJob(content);
      if (newJob.group) { // a Facebook group's link: following it
        const g = newJob.group;
        extractResultBanner.className = "success";
        extractResultBanner.style.backgroundColor = "#dcfce7";
        extractResultBanner.style.color = "#166534";
        extractResultBanner.style.border = "1px solid #bbf7d0";
        extractResultBanner.innerHTML = newJob.added
          ? `📘 <strong>Following this Facebook group.</strong><br>braveduck checks whether it's public; if it is, job posts that match your search terms come to your board by themselves.`
          : `📘 You already follow this group.`;
        extractResultBanner.style.display = "block";
        pasteTextarea.value = "";
        renderFBGroups();
        return;
      }

      extractResultBanner.className = "success";
      extractResultBanner.style.backgroundColor = "#dcfce7";
      extractResultBanner.style.color = "#166534";
      extractResultBanner.style.border = "1px solid #bbf7d0";
      extractResultBanner.innerHTML = `✅ <strong>Job added</strong><br>${esc(newJob.title)}` +
        (newJob.company ? ` at ${esc(newJob.company)}` : "");
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
      extractBtn.innerText = "Add";
    }
  });

  // Search terms window
  const keywordsModal = document.getElementById("keywordsModalOverlay");
  document.getElementById("keywordsBtn").addEventListener("click", openKeywords);
  document.getElementById("saveKeywordsBtn").addEventListener("click", saveKeywords);

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
    document.getElementById("ratingAdvanced").open = false;
    showJev(await APIClient.getJev());
    ratingModal.classList.add("open");
  });

  // Jev: a short notice that the profile is sent, and a per-user opt-out.
  const jevOptOut = document.getElementById("jevOptOutCheckbox");
  function showJev(j) {
    document.getElementById("jevBlock").hidden = !(j.available || j.opt_out);
    document.getElementById("jevNotice").hidden = j.opt_out;
    jevOptOut.checked = j.opt_out;
  }
  jevOptOut.addEventListener("change", async () => {
    try {
      showJev(await APIClient.setJevOptOut(jevOptOut.checked));
    } catch (e) {
      jevOptOut.checked = !jevOptOut.checked;
      showToast(e.message, "error");
    }
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

  document.getElementById("ratingProfileBtn").addEventListener("click", () => {
    ratingModal.classList.remove("open");
    openProfile();
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
  document.getElementById("checkSourcesBtn").addEventListener("click", checkSources);

  // Settings menu: one button for the less-used windows. A pick closes the
  // menu and opens its window (each item keeps its old id and listener).
  const settingsBtn = document.getElementById("settingsBtn");
  const settingsMenu = document.getElementById("settingsMenu");
  const closeSettings = (focusButton) => {
    if (settingsMenu.hidden) return;
    settingsMenu.hidden = true;
    settingsBtn.setAttribute("aria-expanded", "false");
    if (focusButton) settingsBtn.focus();
  };
  settingsBtn.addEventListener("click", e => {
    e.stopPropagation();
    if (!settingsMenu.hidden) return closeSettings(false);
    const r = settingsBtn.getBoundingClientRect();
    settingsMenu.hidden = false;
    const left = Math.max(8, Math.min(r.right - settingsMenu.offsetWidth, document.documentElement.clientWidth - settingsMenu.offsetWidth - 8));
    settingsMenu.style.left = `${left + window.scrollX}px`;
    settingsMenu.style.top = `${r.bottom + 4 + window.scrollY}px`;
    settingsBtn.setAttribute("aria-expanded", "true");
    settingsMenu.querySelector("button").focus();
  });
  settingsMenu.addEventListener("click", () => closeSettings(false));
  document.getElementById("notifyBtn").addEventListener("click", toggleNotify);
  document.getElementById("exportBtn").addEventListener("click", downloadJobsCSV);
  renderNotifyButton();
  settingsMenu.addEventListener("keydown", e => {
    const items = [...settingsMenu.querySelectorAll("button")];
    const i = items.indexOf(document.activeElement);
    if (e.key === "ArrowDown") { e.preventDefault(); items[(i + 1) % items.length].focus(); }
    if (e.key === "ArrowUp") { e.preventDefault(); items[(i - 1 + items.length) % items.length].focus(); }
    if (e.key === "Escape") closeSettings(true);
  });
  document.addEventListener("click", e => {
    if (!settingsMenu.contains(e.target)) closeSettings(false);
  });

  // Search: filters cards as you type; "/" jumps to it, Esc clears it.
  const search = document.getElementById("jobSearch");
  search.addEventListener("input", () => {
    AppState.filters.query = search.value;
    renderBoard();
  });
  search.addEventListener("keydown", e => {
    if (e.key === "Escape") {
      search.value = "";
      AppState.filters.query = "";
      renderBoard();
      search.blur();
    }
  });
  // Keyboard: "/" search, "n" add a job, "?" list the shortcuts, Esc closes
  // the open window or drawer (the settings menu and search handle their own).
  document.addEventListener("keydown", e => {
    const typing = /^(INPUT|TEXTAREA|SELECT)$/.test(document.activeElement.tagName) || document.activeElement.isContentEditable;
    if (e.key === "Escape") {
      // Not while writing in a text area (the profile editor, a paste):
      // an unsaved edit mustn't vanish on a stray Esc.
      if (document.activeElement.tagName === "TEXTAREA") return;
      const open = document.querySelector(".modal-overlay.open .modal-close-btn") ||
        (document.getElementById("addJobDrawer").classList.contains("open") && document.getElementById("closeDrawerBtn"));
      if (open) {
        e.preventDefault();
        open.click();
      }
      return;
    }
    if (typing || e.ctrlKey || e.metaKey || e.altKey) return;
    if (e.key === "/") {
      e.preventDefault();
      search.focus();
    } else if (e.key === "n") {
      e.preventDefault();
      document.getElementById("openDrawerBtn").click();
      setTimeout(() => document.getElementById("pasteJDTextarea").focus(), 50);
    } else if (e.key === "?") {
      e.preventDefault();
      showToast("Shortcuts: / search · n add a job · Esc close a window · ? this list", "info");
    }
  });

  const profileModal = document.getElementById("profileModalOverlay");
  document.getElementById("profileBtn").addEventListener("click", openProfile);
  document.getElementById("saveProfileBtn").addEventListener("click", saveProfile);
  document.getElementById("profileModeBtn").addEventListener("click", toggleProfileMode);

  const cvModal = document.getElementById("cvModalOverlay");
  document.getElementById("myCVBtn").addEventListener("click", async () => {
    document.getElementById("cvResult").hidden = true;
    cvModal.classList.add("open");
    await renderMyCV();
  });
  wireCVUpload(cvModal);

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
      profileModal.classList.remove("open");
      document.getElementById("askModalOverlay").classList.remove("open");
      document.getElementById("agentModalOverlay").classList.remove("open");
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
