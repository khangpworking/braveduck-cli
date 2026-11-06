// --- Profile form ------------------------------------------------------------
// The friendly editor for "what you're looking for": tags, buttons and plain
// fields instead of Markdown. The server keeps the profile as Markdown and
// writes only the lines this form owns (service/profileform.go); "Edit as
// text" opens the whole file for anyone who wants it.

let profileForm = null; // { form, options } from GET /api/profile/form

// el builds an element: el("div", {class: "x"}, child, "text", …).
function el(tag, attrs, ...kids) {
  const e = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (k === "class") e.className = v;
    else if (k.startsWith("on")) e.addEventListener(k.slice(2), v);
    else if (v !== false && v !== undefined && v !== null) e.setAttribute(k, v === true ? "" : v);
  }
  for (const k of kids.flat()) if (k !== null && k !== undefined) e.append(k);
  return e;
}

// tagsField: a list the user types into (Enter or comma adds), with
// suggestions to tap. onChange (optional) runs after every change.
function tagsField(label, hint, list, suggestions, onChange) {
  const box = el("div", { class: "pf-tags" });
  const input = el("input", { type: "text", class: "text-input pf-tag-input", "aria-label": label, placeholder: t("Type and press Enter") });
  const sugg = el("div", { class: "pf-suggestions" });
  // A key press on a button that goes away (detail 0: not a mouse or tap)
  // moves on to the input, so keyboard users don't lose their place.
  const refocus = e => { if (e.detail === 0) input.focus(); };
  // What's saved stays as written; only a suggestion we offered is translated.
  const shown = v => (suggestions || []).includes(v) ? t(v) : v;
  const render = () => {
    const typing = document.activeElement === input; // re-adding the input drops its focus
    box.replaceChildren(...list.map((v, i) => el("span", { class: "pf-tag" }, shown(v),
      el("button", { type: "button", class: "pf-tag-x", "aria-label": t("Remove {term}", {term: shown(v)}), onclick: e => { list.splice(i, 1); render(); refocus(e); } }, "×"))), input);
    if (typing) input.focus();
    sugg.replaceChildren(...(suggestions || []).filter(s => !list.some(v => v.toLowerCase() === s.toLowerCase()))
      .map(s => el("button", { type: "button", class: "pf-suggestion", onclick: e => { list.push(s); render(); refocus(e); } }, "+ " + t(s))));
    if (onChange) onChange();
  };
  const add = () => {
    for (const part of input.value.split(/[,;\n]/)) {
      const v = part.trim();
      if (v && !list.some(x => x.toLowerCase() === v.toLowerCase())) list.push(v);
    }
    input.value = "";
    render();
  };
  input.addEventListener("keydown", e => {
    if (e.key === "Enter" || e.key === ",") { e.preventDefault(); add(); }
    else if (e.key === "Backspace" && !input.value && list.length) { list.pop(); render(); }
  });
  input.addEventListener("blur", add); // typed but didn't press Enter: keep it
  render();
  return el("div", { class: "pf-field" }, el("label", { class: "pf-label" }, label), hint ? el("div", { class: "pf-hint" }, hint) : null, box, sugg);
}

// choiceField: buttons that toggle (several may be on).
function choiceField(label, list, options) {
  const row = el("div", { class: "pf-choices", role: "group", "aria-label": label });
  const offered = options; // the saved value stays English; only these are translated
  const render = () => row.replaceChildren(...options.map(o => {
    const on = list.includes(o);
    return el("button", { type: "button", class: "pf-choice" + (on ? " on" : ""), "aria-pressed": String(on),
      onclick: () => { on ? list.splice(list.indexOf(o), 1) : list.push(o); render(); } }, offered.includes(o) ? t(o) : o);
  }));
  // Values the form doesn't offer stay as written (and still show).
  options = options.concat(list.filter(v => !options.includes(v)));
  render();
  return el("div", { class: "pf-field" }, el("label", { class: "pf-label" }, label), row);
}

// textField: one line of text bound to obj[key].
function textField(label, obj, key, placeholder) {
  const input = el("input", { type: "text", class: "text-input", value: obj[key] || "", placeholder: placeholder || "", "aria-label": label });
  input.addEventListener("input", () => { obj[key] = input.value; });
  return el("div", { class: "pf-field" }, el("label", { class: "pf-label" }, label), input);
}

function areaField(label, obj, key, hint, rows) {
  const ta = el("textarea", { class: "textarea-input", rows: rows || 4, "aria-label": label });
  ta.value = obj[key] || "";
  ta.addEventListener("input", () => { obj[key] = ta.value; });
  return el("div", { class: "pf-field" }, el("label", { class: "pf-label" }, label), hint ? el("div", { class: "pf-hint" }, hint) : null, ta);
}

// moneyField: amount, currency, period and gross/net. A figure written in a
// way these fields can't show is kept as text until a new amount replaces it.
function moneyField(label, m, opts) {
  const fmt = n => n ? Number(n).toLocaleString(I18N.lang) : "";
  const amount = el("input", { type: "text", inputmode: "numeric", class: "text-input pf-amount", value: fmt(m.amount), placeholder: t("e.g. 20,000,000"), "aria-label": t("{label} amount", { label }) });
  amount.addEventListener("input", () => {
    const n = parseInt(amount.value.replace(/[^\d]/g, ""), 10) || 0;
    m.amount = n;
    if (n) m.text = "";
    amount.value = fmt(n);
  });
  const select = (key, choices, ariaLabel) => {
    const s = el("select", { class: "text-input pf-select", "aria-label": ariaLabel },
      ...choices.map(([v, text]) => el("option", { value: v, selected: (m[key] || "") === v }, text)));
    s.addEventListener("change", () => { m[key] = s.value; });
    return s;
  };
  if (!m.currency) m.currency = "VND";
  if (!m.period) m.period = "month";
  const row = el("div", { class: "pf-money" }, amount,
    select("currency", opts.currencies.map(c => [c, c]), t("{label} currency", { label })),
    select("period", opts.periods.map(p => [p, t("per " + p)]), t("{label} period", { label })),
    select("basis", [["", t("gross or net?")], ["gross", t("gross (before tax)")], ["net", t("net (take-home)")]], t("{label} gross or net", { label })));
  const was = m.text ? el("div", { class: "pf-hint" }, t("You wrote: \"{text}\". Fill in an amount to replace it.", { text: m.text })) : null;
  return el("div", { class: "pf-field" }, el("label", { class: "pf-label" }, label), row, was);
}

function pfSection(title, ...fields) {
  return el("section", { class: "pf-section" }, el("h4", { class: "pf-title" }, title), ...fields);
}

function renderProfileForm() {
  const f = profileForm.form, o = profileForm.options;
  // Fields the form doesn't show (target pay, schedule, …) stay in the
  // profile: they go back to the server unchanged.
  const locationLink = el("div", { class: "pf-field pf-hint" },
    t("📍 Where you live and how far you'll travel to work: "),
    el("button", { type: "button", class: "btn-link", onclick: () => {
      document.getElementById("profileModalOverlay").classList.remove("open");
      document.getElementById("locationBtn").click();
    } }, t("set your location →")));
  document.getElementById("profileFormBody").replaceChildren(
    pfSection(t("1. What job do you want?"),
      tagsField(t("Job titles"), t("The roles you'd apply for, e.g. Business Analyst, Kế toán."), f.roles, []),
      choiceField(t("Level"), f.seniority, o.seniority),
      choiceField(t("Type of job"), f.job_types, o.job_types),
      choiceField(t("Work mode"), f.work_modes, o.work_modes),
      locationLink),
    pfSection(t("2. Pay"),
      moneyField(t("The least you'd accept"), f.min_pay, o)),
    pfSection(t("3. Deal-breakers"),
      tagsField(t("A job is a \"no\" if…"), t("Saturday work and unstated pay are also flagged on job cards. Anything you add here guides the rating."), f.deal_breakers, o.deal_breakers)),
    pfSection(t("4. About you"),
      areaField(t("Your experience and skills"), f, "about_me", t("Filled in from your CV when you upload one. Edit freely."), 6)),
    el("details", { class: "pf-more" }, el("summary", {}, t("More (optional)")),
      tagsField(t("Industries you like"), "", f.industries_want, []),
      tagsField(t("Industries to avoid"), "", f.industries_avoid, []),
      tagsField(t("Languages you can work in"), "", f.languages, o.languages),
      areaField(t("Anything else the rater should know"), f, "notes", "", 3)));
}

function showProfileMode(form) {
  document.getElementById("profileFormBody").hidden = !form;
  document.getElementById("profileTextBody").hidden = form;
  document.getElementById("profileModeBtn").textContent = t(form ? "Edit as text instead" : "Back to the form");
}

async function openProfile() {
  const status = document.getElementById("profileStatus");
  document.getElementById("profileModalOverlay").classList.add("open");
  showProfileMode(true);
  try {
    profileForm = await APIClient.get("profile/form");
    const p = await APIClient.get("profile");
    document.getElementById("profileText").value = p.text || "";
    renderProfileStatus(p);
  } catch (e) {
    status.innerText = t("Couldn't load your profile: {error}", { error: t(e.message) });
    return;
  }
  renderProfileForm();
}

function renderProfileStatus(p) {
  document.getElementById("profileStatus").textContent = p.filled
    ? t("✓ Filled in — jobs can be rated against it.")
    : t("Not filled in yet, so jobs can't be rated. Start with the job titles you want.");
}

async function toggleProfileMode() {
  const toText = !document.getElementById("profileFormBody").hidden;
  if (toText) {
    // Save the form first, so the text shows what was just entered.
    if (!(await saveProfile(true))) return;
  } else {
    if (!(await saveProfile(true))) return;
    profileForm = await APIClient.get("profile/form");
    renderProfileForm();
  }
  showProfileMode(!toText);
}

// saveProfile saves whichever editor is showing; quiet skips the toast.
async function saveProfile(quiet) {
  const btn = document.getElementById("saveProfileBtn");
  const formMode = !document.getElementById("profileFormBody").hidden;
  btn.disabled = true;
  btn.textContent = t("Saving…");
  try {
    if (formMode && profileForm) {
      // Keep a tag the user typed without pressing Enter.
      document.querySelectorAll("#profileFormBody .pf-tag-input").forEach(i => i.dispatchEvent(new Event("blur")));
      profileForm = await APIClient.put("profile/form", { form: profileForm.form });
      renderProfileForm();
    } else {
      await APIClient.put("profile", { text: document.getElementById("profileText").value });
    }
    const p = await APIClient.get("profile");
    document.getElementById("profileText").value = p.text || "";
    renderProfileStatus(p);
    if (quiet !== true) showToast(t("Profile saved"), "success");
    renderSetup();
    return true;
  } catch (e) {
    showToast(t("Couldn't save: {error}", { error: t(e.message) }), "error");
    return false;
  } finally {
    btn.disabled = false;
    btn.textContent = t("Save");
  }
}
