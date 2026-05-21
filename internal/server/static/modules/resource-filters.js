import { DEFAULT_RESOURCE_TYPES } from "./constants.js";
import { dom } from "./dom.js";
import { resourceTypeLabel } from "./formatters.js";
import { saveSettings } from "./settings.js";
import { state } from "./state.js";

export function renderResourceTypeFilters() {
  const counts = new Map();
  for (const entry of state.entries) {
    const type = entry.resourceType || "Other";
    counts.set(type, (counts.get(type) || 0) + 1);
  }

  const types = resourceTypes(counts);
  const buttons = [
    resourceTypeButton("", "All", state.entries.length),
    ...types.map((type) => resourceTypeButton(type, resourceTypeLabel(type), counts.get(type) || 0))
  ];
  dom.resourceTypeFilters.replaceChildren(...buttons);
  updateResourceTypeFilterButtons();
}

export function resourceTypeButton(type, label, count) {
  const button = document.createElement("button");
  button.type = "button";
  button.className = "type-filter-button";
  button.dataset.resourceType = type;
  button.textContent = count > 0 ? `${label} ${count}` : label;
  return button;
}

export function updateResourceTypeFilterButtons() {
  const disabled = state.busy;
  dom.resourceTypeFilters.querySelectorAll("button[data-resource-type]").forEach((button) => {
    const active = button.dataset.resourceType === state.resourceTypeFilter;
    button.disabled = disabled;
    button.classList.toggle("active", active);
    button.setAttribute("aria-pressed", String(active));
  });
}

export function resourceTypes(counts) {
  const present = new Set(counts.keys());
  for (const type of DEFAULT_RESOURCE_TYPES) {
    present.delete(type);
  }
  return DEFAULT_RESOURCE_TYPES.concat(Array.from(present).sort());
}

export function resetMissingResourceTypeFilter() {
  if (!state.resourceTypeFilter) {
    return;
  }
  if (state.entries.some((entry) => entry.resourceType === state.resourceTypeFilter)) {
    return;
  }
  state.resourceTypeFilter = "";
  saveSettings(state);
}
