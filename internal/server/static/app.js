import { DEFAULT_PAGE_SIZE, PAGE_SIZE_VALUES } from "./modules/constants.js";
import { initBodyViewer, restoreDialogFocus } from "./modules/body-viewer.js";
import { dom } from "./modules/dom.js";
import {
  renderEntryDetail,
  renderEntryDialog,
  setDetailError,
  setDetailLoading,
  setEntryDialogLoading
} from "./modules/entry-details.js";
import { filteredFileName, statusTone } from "./modules/formatters.js";
import {
  renderResourceTypeFilters,
  updateResourceTypeFilterButtons
} from "./modules/resource-filters.js";
import {
  initRequestTable,
  renderControls,
  renderRows,
  renderSkeletonRows,
  scheduleFilterRender,
  updateRowControls,
  updateVisibleRowSelections
} from "./modules/request-table.js";
import {
  closeEntryDetail,
  focusEntryRow,
  initRowInteractions,
  updateActiveRowClasses,
  updateRowSelectionState
} from "./modules/row-interactions.js";
import { refreshScrollFades } from "./modules/scroll.js";
import { saveSettings } from "./modules/settings.js";
import { state } from "./modules/state.js";
import { filteredEntries, paginatedEntries } from "./modules/table-model.js";
import { initThemeControls } from "./modules/theme.js";
import {
  initUploadFlow,
  renderSelectedFile,
  renderUploadAction
} from "./modules/upload-flow.js";

initBodyViewer({ setStatus });
initThemeControls();
initRequestTable({
  onActiveEntryHidden: closeEntryDetail,
  updateRowSelectionState
});
initRowInteractions({
  loadEntryDetail,
  openEntryDialog,
  renderControls
});
initUploadFlow({
  setStatus,
  setBusy,
  renderSkeletonRows,
  renderRows,
  render
});

dom.pageSizeSelect.addEventListener("change", () => {
  const value = Number(dom.pageSizeSelect.value);
  state.pageSize = PAGE_SIZE_VALUES.includes(value) ? value : DEFAULT_PAGE_SIZE;
  state.page = 1;
  saveSettings(state);
  renderRows();
});

dom.entryDialog.addEventListener("click", (event) => {
  if (event.target === dom.entryDialog) {
    dom.entryDialog.close();
  }
});

dom.entryDialog.addEventListener("close", () => {
  state.entryDialogRequestId += 1;
  state.entryDialogIndex = null;
  restoreDialogFocus("entry");
});

dom.filterInput.addEventListener("input", () => {
  state.filter = dom.filterInput.value.trim().toLowerCase();
  state.page = 1;
  scheduleFilterRender();
});

dom.resourceTypeFilters.addEventListener("click", (event) => {
  const button = event.target.closest("button[data-resource-type]");
  if (!button || button.disabled) {
    return;
  }
  state.resourceTypeFilter = button.dataset.resourceType;
  state.page = 1;
  saveSettings(state);
  updateResourceTypeFilterButtons();
  renderRows();
});

window.addEventListener("resize", () => refreshScrollFades());

dom.prevPageButton.addEventListener("click", () => {
  state.page = Math.max(1, state.page - 1);
  renderRows();
});

dom.nextPageButton.addEventListener("click", () => {
  state.page += 1;
  renderRows();
});

dom.selectAllVisibleCheckbox.addEventListener("change", () => {
  const filtered = filteredEntries();
  const visible = paginatedEntries(filtered);
  const isSelected = dom.selectAllVisibleCheckbox.checked;
  if (isSelected) {
    for (const entry of visible) {
      state.selected.add(entry.index);
    }
  } else {
    for (const entry of visible) {
      state.selected.delete(entry.index);
    }
  }
  updateVisibleRowSelections(visible, isSelected);
  renderControls(filtered);
});

dom.exportButton.addEventListener("click", async () => {
  if (!state.sessionId || state.selected.size === 0) {
    setStatus("Select requests to export", true);
    return;
  }

  setStatus("Creating HAR file", false);
  setBusy(true);

  try {
    const response = await fetch("/api/export", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        id: state.sessionId,
        indexes: Array.from(state.selected).sort((a, b) => a - b)
      })
    });

    if (!response.ok) {
      const payload = await response.json();
      throw new Error(payload.error || "Export failed");
    }

    const blob = await response.blob();
    const link = document.createElement("a");
    link.href = URL.createObjectURL(blob);
    link.download = filteredFileName(state.fileName);
    document.body.append(link);
    link.click();
    link.remove();
    URL.revokeObjectURL(link.href);
    setStatus("Created a HAR file from the selected requests", false);
  } catch (error) {
    setStatus(error.message, true);
  } finally {
    setBusy(false);
  }
});

function render() {
  renderSelectedFile();
  dom.entryCount.textContent = String(state.entries.length);
  dom.filterInput.value = state.filter;
  dom.pageSizeSelect.value = String(state.pageSize);
  renderResourceTypeFilters();
  renderRows();
  refreshScrollFades();
}

function setBusy(isBusy) {
  state.busy = isBusy;
  document.body.classList.toggle("is-busy", isBusy);
  dom.harFile.disabled = isBusy;
  dom.filterInput.disabled = isBusy;
  dom.pageSizeSelect.disabled = isBusy;
  renderUploadAction();
  renderControls();
  updateRowControls();
}

function setStatus(message, isError) {
  dom.statusMessage.dataset.statusMessage = message;
  dom.statusMessage.title = message || "Choose or replace HAR file";
  dom.uploadErrorText.textContent = isError ? message : "";
  dom.uploadErrorText.hidden = !isError;
  dom.statusMessage.classList.remove("idle", "working", "ready", "error");
  dom.statusMessage.classList.add(isError ? "error" : statusTone(message));
  renderUploadAction();
}

async function loadEntryDetail(index, restoreFocus = false) {
  if (!state.sessionId) {
    return;
  }
  const requestId = ++state.detailRequestId;
  state.activeEntryIndex = index;
  updateActiveRowClasses();
  if (restoreFocus) {
    focusEntryRow(index);
  }
  setDetailLoading();

  try {
    const response = await fetch("/api/entry", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ id: state.sessionId, index })
    });
    const payload = await response.json();
    if (!response.ok) {
      throw new Error(payload.error || "Request detail failed");
    }
    if (requestId !== state.detailRequestId || state.activeEntryIndex !== index) {
      return;
    }
    renderEntryDetail(payload, index);
  } catch (error) {
    if (requestId !== state.detailRequestId || state.activeEntryIndex !== index) {
      return;
    }
    setDetailError(error.message);
  }
}

async function openEntryDialog(index, trigger = document.activeElement) {
  if (!state.sessionId) {
    return;
  }
  const sessionId = state.sessionId;
  const requestId = ++state.entryDialogRequestId;
  state.entryDialogIndex = index;
  state.entryDialogTrigger = trigger;
  setEntryDialogLoading("Loading request");
  if (!dom.entryDialog.open) {
    dom.entryDialog.showModal();
  }
  document.querySelector("#entryDialogCloseButton")?.focus();

  try {
    const response = await fetch("/api/entry", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ id: sessionId, index })
    });
    const payload = await response.json();
    if (!response.ok) {
      throw new Error(payload.error || "Request detail failed");
    }
    if (!isCurrentEntryDialogRequest(requestId, sessionId, index)) {
      return;
    }
    renderEntryDialog(payload);
  } catch (error) {
    if (!isCurrentEntryDialogRequest(requestId, sessionId, index)) {
      return;
    }
    setEntryDialogLoading(error.message);
  }
}

function isCurrentEntryDialogRequest(requestId, sessionId, index) {
  return requestId === state.entryDialogRequestId
    && sessionId === state.sessionId
    && index === state.entryDialogIndex
    && dom.entryDialog.open;
}

render();
