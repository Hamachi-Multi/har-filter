import {
  ENTRY_DIALOG_DOUBLE_CLICK_MS,
  ROW_DRAG_SELECTION_THRESHOLD_PX
} from "./constants.js";
import { dom } from "./dom.js";
import { clearDetailPanels } from "./entry-details.js";
import { dragSelection, entryClickTiming, state } from "./state.js";

let detailLoader = () => {};
let entryDialogOpener = () => {};
let controlsRenderer = () => {};

export function initRowInteractions({ loadEntryDetail, openEntryDialog, renderControls }) {
  detailLoader = loadEntryDetail;
  entryDialogOpener = openEntryDialog;
  controlsRenderer = renderControls;

  dom.entriesBody.addEventListener("change", handleRowChange);
  dom.entriesBody.addEventListener("click", handleRowClick);
  dom.entriesBody.addEventListener("keydown", handleRowKeydown);
  dom.entriesBody.addEventListener("dblclick", handleRowDoubleClick);
  dom.entriesBody.addEventListener("pointerdown", handleRowPointerDown);
  dom.entriesBody.addEventListener("pointermove", handleRowPointerMove);
  dom.entriesBody.addEventListener("pointerover", handleRowPointerOver);

  window.addEventListener("pointerup", finishPointerSelection);
  window.addEventListener("pointercancel", finishPointerSelection);
}

export function closeEntryDetail(focusIndex = null) {
  state.activeEntryIndex = null;
  clearDetailPanels();
  updateActiveRowClasses();
  if (focusIndex !== null) {
    focusEntryRow(focusIndex);
  }
}

export function updateActiveRowClasses() {
  dom.entriesBody.querySelectorAll("tr[data-index]").forEach((row) => {
    const isActive = Number(row.dataset.index) === state.activeEntryIndex;
    row.classList.toggle("active-row", isActive);
    if (isActive) {
      row.setAttribute("aria-current", "true");
    } else {
      row.removeAttribute("aria-current");
    }
  });
}

export function focusEntryRow(index) {
  updateRovingRowFocus(index);
  dom.entriesBody.querySelector(`tr[data-index="${index}"]`)?.focus();
}

export function updateRovingRowFocus(index) {
  if (Number.isNaN(index)) {
    return;
  }
  state.focusEntryIndex = index;
  dom.entriesBody.querySelectorAll("tr[data-index]").forEach((row) => {
    const isRoving = Number(row.dataset.index) === index;
    row.tabIndex = isRoving ? 0 : -1;
    row.querySelectorAll("input[type='checkbox'][data-index], button[data-open-entry]").forEach((control) => {
      control.tabIndex = isRoving ? 0 : -1;
    });
  });
}

export function setRowSelected(row, index, isSelected) {
  if (isSelected) {
    state.selected.add(index);
  } else {
    state.selected.delete(index);
  }
  updateRowSelectionState(row, index, isSelected);
  controlsRenderer();
}

export function updateRowSelectionState(row, index, isSelected) {
  if (!row) {
    return;
  }
  row.classList.toggle("selected-row", isSelected);
  const checkbox = row.querySelector("input[type='checkbox'][data-index]");
  if (checkbox && Number(checkbox.dataset.index) === index) {
    checkbox.checked = isSelected;
  }
}

export function activateRowClickSelection(row, index, restoreFocus = false) {
  if (state.busy || Number.isNaN(index)) {
    return false;
  }
  const wasFocusedBeforePointer = row.dataset.focusedBeforePointer === "true";
  delete row.dataset.focusedBeforePointer;
  updateRovingRowFocus(index);
  if (!restoreFocus && state.activeEntryIndex === index) {
    closeEntryDetail();
    if (wasFocusedBeforePointer || document.activeElement === row || row.contains(document.activeElement)) {
      row.blur();
    }
    return true;
  }
  if (restoreFocus) {
    focusEntryRow(index);
  } else {
    row.focus({ preventScroll: true });
  }
  if (state.activeEntryIndex !== index) {
    detailLoader(index, restoreFocus);
  }
  return true;
}

export function rememberEntryClick(index, event) {
  entryClickTiming.index = index;
  entryClickTiming.timeStamp = event.timeStamp;
}

export function isRecentEntryClick(index, event) {
  const elapsed = event.timeStamp - entryClickTiming.timeStamp;
  return entryClickTiming.index === index
    && elapsed >= 0
    && elapsed <= ENTRY_DIALOG_DOUBLE_CLICK_MS;
}

export function shouldIgnoreEntryDoubleClick(index, event) {
  if (entryClickTiming.index === index && event.timeStamp <= entryClickTiming.suppressDoubleClickUntil) {
    entryClickTiming.suppressDoubleClickUntil = 0;
    return true;
  }
  return !isRecentEntryClick(index, event);
}

export function startDragSelection() {
  if (dragSelection.dragging) {
    return;
  }
  dragSelection.dragging = true;
  dragSelection.suppressClick = true;
}

export function finishDragSelection() {
  dragSelection.active = false;
  dragSelection.dragging = false;
  dragSelection.pointerId = null;
  dragSelection.startRow = null;
  dragSelection.touched.clear();
  setTimeout(() => {
    dragSelection.suppressClick = false;
  }, 0);
}

export function applyDragSelectionToRow(row) {
  const checkbox = row?.querySelector("input[type='checkbox'][data-index]");
  if (checkbox) {
    applyDragSelection(checkbox);
  }
}

export function applyDragSelection(checkbox) {
  const index = Number(checkbox.dataset.index);
  if (dragSelection.touched.has(index)) {
    return;
  }
  dragSelection.touched.add(index);
  checkbox.checked = dragSelection.targetChecked;
  if (dragSelection.targetChecked) {
    state.selected.add(index);
  } else {
    state.selected.delete(index);
  }
  updateRowSelectionState(checkbox.closest("tr"), index, dragSelection.targetChecked);
}

function handleRowChange(event) {
  const checkbox = event.target.closest("input[type='checkbox'][data-index]");
  if (!checkbox) {
    return;
  }
  const index = Number(checkbox.dataset.index);
  updateRovingRowFocus(index);
  if (checkbox.checked) {
    state.selected.add(index);
  } else {
    state.selected.delete(index);
  }
  updateRowSelectionState(checkbox.closest("tr"), index, checkbox.checked);
  controlsRenderer();
}

function handleRowClick(event) {
  if (dragSelection.suppressClick) {
    event.preventDefault();
    event.stopPropagation();
    dragSelection.suppressClick = false;
    return;
  }
  const openButton = event.target.closest("button[data-open-entry]");
  if (openButton) {
    const index = Number(openButton.dataset.openEntry);
    updateRovingRowFocus(index);
    entryDialogOpener(index, openButton);
    return;
  }
  if (event.target.closest("input[type='checkbox']")) {
    return;
  }
  const row = event.target.closest("tr[data-index]");
  if (!row) {
    return;
  }
  const index = Number(row.dataset.index);
  if (event.detail > 1) {
    if (isRecentEntryClick(index, event)) {
      return;
    }
    entryClickTiming.suppressDoubleClickUntil = event.timeStamp + 100;
  }
  rememberEntryClick(index, event);
  activateRowClickSelection(row, index);
}

function handleRowKeydown(event) {
  if (handleRovingKeydown(event)) {
    return;
  }
  if (event.target.closest("input, button, select, textarea")) {
    return;
  }
  if (event.key !== "Enter" && event.key !== " ") {
    return;
  }
  const row = event.target.closest("tr[data-index]");
  if (!row) {
    return;
  }
  event.preventDefault();
  const index = Number(row.dataset.index);
  activateRowClickSelection(row, index, true);
}

function handleRovingKeydown(event) {
  if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) {
    return false;
  }
  const row = event.target.closest("tr[data-index]");
  if (!row) {
    return false;
  }
  const rows = Array.from(dom.entriesBody.querySelectorAll("tr[data-index]"));
  const currentPosition = rows.indexOf(row);
  if (currentPosition < 0) {
    return false;
  }
  event.preventDefault();
  let nextPosition = currentPosition;
  if (event.key === "ArrowDown") {
    nextPosition = Math.min(rows.length - 1, currentPosition + 1);
  } else if (event.key === "ArrowUp") {
    nextPosition = Math.max(0, currentPosition - 1);
  } else if (event.key === "Home") {
    nextPosition = 0;
  } else if (event.key === "End") {
    nextPosition = rows.length - 1;
  }
  const nextRow = rows[nextPosition];
  const nextIndex = Number(nextRow.dataset.index);
  updateRovingRowFocus(nextIndex);
  nextRow.focus({ preventScroll: true });
  return true;
}

function handleRowDoubleClick(event) {
  if (event.target.closest("input[type='checkbox']")) {
    return;
  }
  const row = event.target.closest("tr[data-index]");
  if (!row) {
    return;
  }
  const index = Number(row.dataset.index);
  if (shouldIgnoreEntryDoubleClick(index, event)) {
    return;
  }
  entryDialogOpener(index, row);
}

function handleRowPointerDown(event) {
  if (event.button !== 0 || event.target.closest("button[data-open-entry]")) {
    return;
  }
  const row = event.target.closest("tr[data-index]");
  const checkbox = row?.querySelector("input[type='checkbox'][data-index]");
  if (!row || !checkbox || checkbox.disabled) {
    return;
  }
  row.dataset.focusedBeforePointer = (document.activeElement === row || row.contains(document.activeElement)) ? "true" : "false";
  dragSelection.active = true;
  dragSelection.dragging = false;
  dragSelection.targetChecked = !checkbox.checked;
  dragSelection.touched = new Set();
  dragSelection.pointerId = event.pointerId;
  dragSelection.startX = event.clientX;
  dragSelection.startY = event.clientY;
  dragSelection.startRow = row;
  dragSelection.suppressClick = false;
}

function handleRowPointerMove(event) {
  if (!dragSelection.active || event.pointerId !== dragSelection.pointerId) {
    return;
  }
  const distance = Math.hypot(event.clientX - dragSelection.startX, event.clientY - dragSelection.startY);
  if (!dragSelection.dragging && distance < ROW_DRAG_SELECTION_THRESHOLD_PX) {
    return;
  }
  startDragSelection();
  event.preventDefault();
  applyDragSelectionToRow(dragSelection.startRow);
  const row = event.target.closest("tr[data-index]");
  applyDragSelectionToRow(row);
}

function handleRowPointerOver(event) {
  if (!dragSelection.active || !dragSelection.dragging) {
    return;
  }
  const row = event.target.closest("tr[data-index]");
  applyDragSelectionToRow(row);
}

function finishPointerSelection() {
  if (!dragSelection.active) {
    return;
  }
  finishDragSelection();
  controlsRenderer();
}
