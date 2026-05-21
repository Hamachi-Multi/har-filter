import { clearBodyPrettyCache } from "./body-viewer.js";
import { dom } from "./dom.js";
import { clearDetailPanels } from "./entry-details.js";
import { formatBytes } from "./formatters.js";
import { resetMissingResourceTypeFilter } from "./resource-filters.js";
import { state } from "./state.js";
import { invalidateFilterCache, normalizeEntries } from "./table-model.js";

let statusSetter = () => {};
let busySetter = () => {};
let skeletonRenderer = () => {};
let rowsRenderer = () => {};
let appRenderer = () => {};

export function initUploadFlow({ setStatus, setBusy, renderSkeletonRows, renderRows, render }) {
  statusSetter = setStatus;
  busySetter = setBusy;
  skeletonRenderer = renderSkeletonRows;
  rowsRenderer = renderRows;
  appRenderer = render;

  dom.harFile.addEventListener("change", handleFileInputChange);
  document.addEventListener("dragenter", handleFileDragEnter);
  document.addEventListener("dragover", handleFileDragOver);
  document.addEventListener("dragleave", handleFileDragLeave);
  document.addEventListener("drop", handleFileDrop);
}

export function renderSelectedFile() {
  const file = state.uploadFile || dom.harFile.files[0];
  dom.filePicker.classList.toggle("has-file", Boolean(file || state.fileName));
  dom.fileName.textContent = file?.name || state.fileName || "No file selected";
  dom.selectedFileMeta.textContent = file ? formatBytes(file.size) : ".har or JSON";
  renderUploadAction();
}

export function renderUploadAction() {
  if (dom.statusMessage.classList.contains("error")) {
    dom.uploadActionText.textContent = "Error";
    return;
  }
  if (state.busy) {
    dom.uploadActionText.textContent = "Uploading";
    return;
  }
  dom.uploadActionText.textContent = state.uploadFile || state.fileName ? "Replace" : "Select";
}

export function hasFileDrag(event) {
  return Array.from(event.dataTransfer?.types || []).includes("Files");
}

export function showFileDropState() {
  document.body.classList.add("is-dragging-file");
  dom.filePicker.classList.add("is-dragging-file");
}

export function clearFileDropState() {
  state.fileDragDepth = 0;
  document.body.classList.remove("is-dragging-file");
  dom.filePicker.classList.remove("is-dragging-file");
}

export async function uploadSelectedFile() {
  const file = state.uploadFile || dom.harFile.files[0];
  if (!file) {
    statusSetter("Choose a HAR file to upload", true);
    return;
  }

  const form = new FormData();
  if (state.sessionId) {
    form.append("replaceId", state.sessionId);
  }
  form.append("har", file);
  statusSetter("Uploading", false);
  busySetter(true);
  skeletonRenderer();
  let uploadSucceeded = false;

  try {
    const response = await fetch("/api/upload", {
      method: "POST",
      body: form
    });
    const payload = await response.json();
    if (!response.ok) {
      throw new Error(payload.error || "Upload failed");
    }

    state.sessionId = payload.id;
    state.fileName = payload.fileName;
    state.entries = normalizeEntries(payload.entries || []);
    state.entriesVersion += 1;
    invalidateFilterCache();
    clearBodyPrettyCache();
    state.selected = new Set();
    state.page = 1;
    state.activeEntryIndex = null;
    state.focusEntryIndex = null;
    resetMissingResourceTypeFilter();
    clearDetailPanels();
    statusSetter("Ready", false);
    appRenderer();
    uploadSucceeded = true;
  } catch (error) {
    statusSetter(error.message, true);
  } finally {
    busySetter(false);
    if (!uploadSucceeded) {
      rowsRenderer();
    }
  }
}

function handleFileInputChange() {
  const file = dom.harFile.files[0] || null;
  if (!file) {
    renderSelectedFile();
    return;
  }
  state.uploadFile = file;
  renderSelectedFile();
  uploadSelectedFile();
}

function handleFileDragEnter(event) {
  if (!hasFileDrag(event)) {
    return;
  }
  event.preventDefault();
  if (state.busy) {
    return;
  }
  state.fileDragDepth += 1;
  showFileDropState();
}

function handleFileDragOver(event) {
  if (!hasFileDrag(event)) {
    return;
  }
  event.preventDefault();
  if (state.busy) {
    event.dataTransfer.dropEffect = "none";
    return;
  }
  event.dataTransfer.dropEffect = "copy";
  showFileDropState();
}

function handleFileDragLeave(event) {
  if (!hasFileDrag(event) || state.busy) {
    return;
  }
  state.fileDragDepth = Math.max(0, state.fileDragDepth - 1);
  if (state.fileDragDepth === 0) {
    clearFileDropState();
  }
}

function handleFileDrop(event) {
  if (!hasFileDrag(event)) {
    return;
  }
  event.preventDefault();
  clearFileDropState();
  if (state.busy) {
    return;
  }
  const file = event.dataTransfer?.files?.[0];
  if (!file) {
    statusSetter("Drop a HAR file to upload", true);
    return;
  }
  state.uploadFile = file;
  renderSelectedFile();
  uploadSelectedFile();
}
