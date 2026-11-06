// --- Language ------------------------------------------------------------------
// The board speaks Vietnamese by default, English on request (⚙ Settings).
// English text is the key: t("Add Job") returns the Vietnamese for it, or the
// English itself when there's no translation yet, so nothing ever shows a
// blank. Placeholders: t("{n} new jobs", { n: 3 }).
//
// Translations live in i18n-vi-*.js files (one per area of the board), each
// calling I18N.add("vi", { "English": "Tiếng Việt", … }). Static HTML is
// translated by attribute: data-i18n (text), data-i18n-placeholder,
// data-i18n-title, data-i18n-aria-label — the attribute's value is the key,
// or empty to use the element's own English text.

const I18N = {
  langs: ["vi", "en"],
  names: { vi: "Tiếng Việt", en: "English" },
  dict: { vi: {}, en: {} },
  lang: "vi",

  add(lang, entries) {
    Object.assign(this.dict[lang] || (this.dict[lang] = {}), entries);
  },

  // pick reads the user's saved choice, else the server's default.
  pick() {
    let saved = "";
    try { saved = localStorage.getItem("braveduck.lang") || ""; } catch {}
    const served = document.documentElement.lang; // the server's default (BRAVEDUCK_LANG)
    this.lang = this.langs.includes(saved) ? saved : this.langs.includes(served) ? served : "vi";
    document.documentElement.lang = this.lang;
  },

  // set saves a choice and reloads, so every part of the board redraws in it.
  set(lang) {
    if (!this.langs.includes(lang)) return;
    try { localStorage.setItem("braveduck.lang", lang); } catch {}
    location.reload();
  },
};

function t(key, vars) {
  let s = (I18N.dict[I18N.lang] && I18N.dict[I18N.lang][key]) || key;
  if (vars) s = s.replace(/\{(\w+)\}/g, (m, k) => (k in vars ? String(vars[k]) : m));
  return s;
}

// applyI18n translates the static HTML under root by its data-i18n* attributes.
function applyI18n(root) {
  const scope = root || document;
  scope.querySelectorAll("[data-i18n]").forEach(el => {
    const key = el.getAttribute("data-i18n") || el.textContent.trim();
    el.setAttribute("data-i18n", key); // keep the English key for a later pass
    el.textContent = t(key);
  });
  for (const attr of ["placeholder", "title", "aria-label"]) {
    scope.querySelectorAll(`[data-i18n-${attr}]`).forEach(el => {
      const key = el.getAttribute(`data-i18n-${attr}`) || el.getAttribute(attr) || "";
      el.setAttribute(`data-i18n-${attr}`, key);
      el.setAttribute(attr, t(key));
    });
  }
}

// Dates and numbers in the chosen language.
function fmtDate(d, opts) {
  return new Date(d).toLocaleDateString(I18N.lang === "vi" ? "vi-VN" : "en-GB", opts);
}

I18N.pick();
