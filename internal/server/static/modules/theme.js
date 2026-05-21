import { THEME_KEY } from "./constants.js";
import { dom } from "./dom.js";

const THEMES = new Set(["light", "dark"]);

export function initThemeControls() {
  applyTheme(currentTheme(), { persist: false });
  dom.themeLightButton?.addEventListener("click", () => applyTheme("light"));
  dom.themeDarkButton?.addEventListener("click", () => applyTheme("dark"));
}

export function currentTheme() {
  const datasetTheme = document.documentElement.dataset.theme;
  if (THEMES.has(datasetTheme)) {
    return datasetTheme;
  }
  return preferredTheme();
}

export function preferredTheme() {
  try {
    const savedTheme = window.localStorage.getItem(THEME_KEY);
    if (THEMES.has(savedTheme)) {
      return savedTheme;
    }
  } catch {
    // localStorage can be unavailable in restricted browser contexts.
  }
  return window.matchMedia?.("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

export function applyTheme(theme, { persist = true } = {}) {
  const normalizedTheme = THEMES.has(theme) ? theme : "light";
  const root = document.documentElement;
  root.classList.add("is-theme-switching");
  root.dataset.theme = normalizedTheme;
  root.style.colorScheme = normalizedTheme;
  requestAnimationFrame(() => {
    requestAnimationFrame(() => root.classList.remove("is-theme-switching"));
  });
  if (dom.themeLightButton) {
    dom.themeLightButton.setAttribute("aria-pressed", String(normalizedTheme === "light"));
  }
  if (dom.themeDarkButton) {
    dom.themeDarkButton.setAttribute("aria-pressed", String(normalizedTheme === "dark"));
  }
  if (!persist) {
    return;
  }
  try {
    window.localStorage.setItem(THEME_KEY, normalizedTheme);
  } catch {
    // localStorage can be unavailable in restricted browser contexts.
  }
}
