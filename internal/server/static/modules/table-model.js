import { entrySearchTextCache, state } from "./state.js";

export function filteredEntries() {
  const key = `${state.resourceTypeFilter}\u0000${state.filter}`;
  if (state.filterCache.version === state.entriesVersion && state.filterCache.key === key) {
    return state.filterCache.entries;
  }
  const entries = state.entries.filter(matchesEntryFilters);
  state.filterCache = {
    version: state.entriesVersion,
    key,
    entries
  };
  return entries;
}

export function matchesEntryFilters(entry) {
  if (state.resourceTypeFilter && entry.resourceType !== state.resourceTypeFilter) {
    return false;
  }
  if (!state.filter) {
    return true;
  }
  const searchText = entrySearchTextCache.get(entry) || entrySearchText(entry);
  return searchText.includes(state.filter);
}

export function normalizeEntries(entries) {
  entries.forEach((entry) => {
    entrySearchTextCache.set(entry, entrySearchText(entry));
  });
  return entries;
}

export function entrySearchText(entry) {
  return [
    entry.method,
    entry.url,
    entry.host,
    entry.path,
    entry.status,
    entry.mimeType,
    entry.resourceType
  ].join(" ").toLowerCase();
}

export function invalidateFilterCache() {
  state.filterCache = {
    version: -1,
    key: "",
    entries: []
  };
}

export function paginatedEntries(entries) {
  const start = (state.page - 1) * state.pageSize;
  return entries.slice(start, start + state.pageSize);
}

export function pageCount(total) {
  return Math.max(1, Math.ceil(total / state.pageSize));
}

export function clampPage(total) {
  state.page = Math.min(Math.max(1, state.page), pageCount(total));
}
