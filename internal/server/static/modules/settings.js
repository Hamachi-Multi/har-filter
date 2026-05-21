import { DEFAULT_PAGE_SIZE, PAGE_SIZE_VALUES, SETTINGS_KEY } from "./constants.js";

export function loadSettings() {
  const fallback = {
    resourceTypeFilter: "",
    pageSize: DEFAULT_PAGE_SIZE
  };

  try {
    const raw = window.localStorage.getItem(SETTINGS_KEY);
    if (!raw) {
      return fallback;
    }
    return normalizeSettings(JSON.parse(raw));
  } catch {
    return fallback;
  }
}

export function saveSettings(state) {
  try {
    window.localStorage.setItem(SETTINGS_KEY, JSON.stringify({
      resourceTypeFilter: state.resourceTypeFilter,
      pageSize: state.pageSize
    }));
  } catch {
    // localStorage can be unavailable in restricted browser contexts.
  }
}

export function normalizeSettings(settings) {
  return {
    resourceTypeFilter: typeof settings?.resourceTypeFilter === "string"
      ? settings.resourceTypeFilter
      : "",
    pageSize: PAGE_SIZE_VALUES.includes(Number(settings?.pageSize))
      ? Number(settings.pageSize)
      : DEFAULT_PAGE_SIZE
  };
}
