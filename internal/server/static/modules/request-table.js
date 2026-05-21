import { FILTER_RENDER_DEBOUNCE_MS } from "./constants.js";
import { dom } from "./dom.js";
import { formatBytes, formatNumber, resourceTypeLabel, statusGroup } from "./formatters.js";
import { updateResourceTypeFilterButtons } from "./resource-filters.js";
import { refreshScrollFades } from "./scroll.js";
import { state } from "./state.js";
import {
  clampPage,
  filteredEntries,
  pageCount,
  paginatedEntries
} from "./table-model.js";

let filterRenderTimer = 0;
let activeEntryHiddenHandler = () => {};
let rowSelectionStateHandler = () => {};

export function initRequestTable({ onActiveEntryHidden, updateRowSelectionState }) {
  activeEntryHiddenHandler = onActiveEntryHidden;
  rowSelectionStateHandler = updateRowSelectionState;
  dom.entriesBody.addEventListener("click", handleEmptyUploadPromptClick);
}

export function renderRows() {
  clearScheduledFilterRender();
  if (state.busy && state.entries.length === 0) {
    dom.entriesBody.classList.remove("is-empty");
    renderSkeletonRows();
    return;
  }

  const filtered = filteredEntries();
  clampPage(filtered.length);
  const entries = paginatedEntries(filtered);
  clearActiveEntryWhenHidden(entries);
  const rovingEntryIndex = resolveRovingEntryIndex(entries);
  if (entries.length === 0) {
    dom.entriesBody.classList.add("is-empty");
    state.focusEntryIndex = null;
    const message = state.entries.length === 0
      ? "Upload a HAR file to view requests"
      : "No requests match the current filters";
    const uploadPromptClass = !state.sessionId && state.entries.length === 0 ? " upload-empty-cell" : "";
    dom.entriesBody.innerHTML = `<tr><td colspan="7" class="empty-cell${uploadPromptClass}">${message}</td></tr>`;
    renderControls(filtered);
    refreshScrollFades();
    return;
  }

  dom.entriesBody.classList.remove("is-empty");
  const rows = document.createDocumentFragment();
  for (const entry of entries) {
    rows.append(rowElement(entry, rovingEntryIndex));
  }
  dom.entriesBody.replaceChildren(rows);
  renderControls(filtered);
  refreshScrollFades();
}

export function renderControls(filtered = filteredEntries()) {
  clampPage(filtered.length);
  dom.selectedCount.textContent = String(state.selected.size);
  dom.exportButton.disabled = state.busy || !state.sessionId || state.selected.size === 0;
  renderSelectAllCheckbox(filtered);
  renderPagination(filtered.length);
  updateResourceTypeFilterButtons();
}

export function clearActiveEntryWhenHidden(entries) {
  if (state.activeEntryIndex === null) {
    return;
  }
  if (entries.some((entry) => entry.index === state.activeEntryIndex)) {
    return;
  }
  activeEntryHiddenHandler();
}

export function renderSelectAllCheckbox(filtered) {
  const visible = paginatedEntries(filtered);
  const selectedVisibleCount = visible.filter((entry) => state.selected.has(entry.index)).length;
  dom.selectAllVisibleCheckbox.disabled = state.busy || visible.length === 0;
  dom.selectAllVisibleCheckbox.checked = visible.length > 0 && selectedVisibleCount === visible.length;
  dom.selectAllVisibleCheckbox.indeterminate = selectedVisibleCount > 0 && selectedVisibleCount < visible.length;
}

export function updateVisibleRowSelections(entries, isSelected) {
  for (const entry of entries) {
    const row = dom.entriesBody.querySelector(`tr[data-index="${entry.index}"]`);
    rowSelectionStateHandler(row, entry.index, isSelected);
  }
}

export function scheduleFilterRender() {
  clearScheduledFilterRender();
  filterRenderTimer = window.setTimeout(() => {
    filterRenderTimer = 0;
    renderRows();
  }, FILTER_RENDER_DEBOUNCE_MS);
}

export function clearScheduledFilterRender() {
  if (!filterRenderTimer) {
    return;
  }
  window.clearTimeout(filterRenderTimer);
  filterRenderTimer = 0;
}

export function renderPagination(total) {
  const pages = pageCount(total);
  const start = total === 0 ? 0 : (state.page - 1) * state.pageSize + 1;
  const end = Math.min(total, state.page * state.pageSize);
  dom.paginationSummary.textContent = total === 0 ? "0 requests" : `${start}-${end} of ${total}`;
  dom.pageIndicator.textContent = `Page ${state.page} of ${pages}`;
  dom.prevPageButton.disabled = state.busy || total === 0 || state.page <= 1;
  dom.nextPageButton.disabled = state.busy || total === 0 || state.page >= pages;
}

export function updateRowControls() {
  dom.entriesBody.querySelectorAll("input[type='checkbox'][data-index], button[data-open-entry]").forEach((control) => {
    control.disabled = state.busy;
  });
}

export function renderSkeletonRows() {
  dom.entriesBody.replaceChildren(...Array.from({ length: 6 }, () => {
    const row = document.createElement("tr");
    row.className = "skeleton-row";
    row.innerHTML = `
      <td><span class="skeleton short"></span></td>
      <td><span class="skeleton short"></span></td>
      <td><span class="skeleton long"></span></td>
      <td><span class="skeleton short"></span></td>
      <td><span class="skeleton medium"></span></td>
      <td><span class="skeleton medium"></span></td>
      <td><span class="skeleton medium"></span></td>
    `;
    return row;
  }));
  refreshScrollFades();
}

function rowElement(entry, rovingEntryIndex) {
  const row = document.createElement("tr");
  const isRoving = entry.index === rovingEntryIndex;
  row.className = rowClassName(entry);
  row.dataset.index = String(entry.index);
  row.dataset.statusGroup = statusGroup(entry.status);
  row.dataset.statusCode = String(entry.status || "");
  row.tabIndex = isRoving ? 0 : -1;
  row.setAttribute("aria-label", `Request ${entry.index + 1}: ${entry.method || ""} ${entry.status || ""} ${entry.url || ""}`.trim());
  row.innerHTML = `
    <td class="select-cell">
      <input type="checkbox" data-index="${entry.index}" tabindex="${isRoving ? 0 : -1}" ${state.selected.has(entry.index) ? "checked" : ""} ${state.busy ? "disabled" : ""} aria-label="Select request ${entry.index + 1}">
    </td>
    <td class="method"></td>
    <td class="url-cell">
      <div class="row-open-control">
        <div class="row-url-text">
          <div class="url-main"></div>
          <div class="url-sub"></div>
        </div>
        <button type="button" class="open-entry-button" data-open-entry="${entry.index}" tabindex="${isRoving ? 0 : -1}" ${state.busy ? "disabled" : ""}>Open</button>
      </div>
    </td>
    <td class="number status-cell"></td>
    <td class="number"></td>
    <td class="number"></td>
    <td></td>
  `;
  row.children[1].textContent = entry.method || "";
  row.children[1].dataset.method = entry.method || "";
  row.children[2].querySelector(".url-main").textContent = entry.url || "";
  row.children[2].querySelector(".url-sub").textContent = `${entry.host || ""}${entry.path || ""}`;
  row.children[3].textContent = entry.status ? String(entry.status) : "-";
  row.children[3].dataset.statusGroup = statusGroup(entry.status);
  row.children[4].textContent = `${formatNumber(entry.timeMs)} ms`;
  row.children[5].textContent = formatBytes(entry.size);
  row.children[6].textContent = resourceTypeLabel(entry.resourceType);
  return row;
}

function resolveRovingEntryIndex(entries) {
  if (entries.length === 0) {
    return null;
  }
  if (state.focusEntryIndex !== null && entries.some((entry) => entry.index === state.focusEntryIndex)) {
    return state.focusEntryIndex;
  }
  if (state.activeEntryIndex !== null && entries.some((entry) => entry.index === state.activeEntryIndex)) {
    state.focusEntryIndex = state.activeEntryIndex;
    return state.focusEntryIndex;
  }
  state.focusEntryIndex = entries[0].index;
  return state.focusEntryIndex;
}

function rowClassName(entry) {
  const classes = ["request-row"];
  if (state.selected.has(entry.index)) {
    classes.push("selected-row");
  }
  if (state.activeEntryIndex === entry.index) {
    classes.push("active-row");
  }
  return classes.join(" ");
}

function handleEmptyUploadPromptClick(event) {
  if (state.busy || state.sessionId || !event.target.closest(".upload-empty-cell")) {
    return;
  }
  dom.harFile.click();
}
