import { BODY_PRETTY_CACHE_LIMIT } from "./constants.js";
import { dom } from "./dom.js";
import { emptyBodyPreview } from "./body-preview.js";
import { canPrettyFormatBody, formatBodyForDisplay } from "./formatters.js";
import { bindNestedScrollChain } from "./scroll.js";
import { bodyPrettyCache, copySuccessTimers, state } from "./state.js";

let statusHandler = () => {};

export { emptyBodyPreview };

export function initBodyViewer({ setStatus }) {
  statusHandler = setStatus;

  dom.requestBodyOpenButton.addEventListener("click", () => openBodyDialog("request", dom.requestBodyOpenButton));
  dom.responseBodyOpenButton.addEventListener("click", () => openBodyDialog("response", dom.responseBodyOpenButton));
  dom.requestBodyCopyButton.addEventListener("click", () => copyBodyText("request", "raw", dom.requestBodyCopyButton));
  dom.responseBodyCopyButton.addEventListener("click", () => copyBodyText("response", "raw", dom.responseBodyCopyButton));
  dom.bodyDialogCopyPrettyButton.addEventListener("click", () => copyBodyText(state.bodyDialogKind, "pretty", dom.bodyDialogCopyPrettyButton));
  dom.bodyDialogCopyRawButton.addEventListener("click", () => copyBodyText(state.bodyDialogKind, "raw", dom.bodyDialogCopyRawButton));

  dom.bodyDialogPrettyButton.addEventListener("click", () => {
    state.bodyDialogMode = "pretty";
    renderBodyDialog();
  });

  dom.bodyDialogRawButton.addEventListener("click", () => {
    state.bodyDialogMode = "raw";
    renderBodyDialog();
  });

  dom.bodyDialogContent.addEventListener("pointerdown", () => {
    dom.bodyDialogContent.focus({ preventScroll: true });
  });

  dom.bodyDialogContent.addEventListener("keydown", (event) => {
    if (!isSelectAllShortcut(event)) {
      return;
    }
    event.preventDefault();
    selectBodyDialogContent();
  });

  dom.bodyDialog.addEventListener("click", (event) => {
    if (event.target === dom.bodyDialog) {
      dom.bodyDialog.close();
    }
  });

  dom.bodyDialog.addEventListener("close", () => restoreDialogFocus("body"));
}

export function bodyDisplay(title, source, fallback, truncatedMessage, prettyCacheKey = "") {
  const text = source.text || "";
  if (!text) {
    return {
      ...emptyBodyPreview(title),
      rawText: fallback
    };
  }
  const truncated = source.truncated ? truncatedMessage : "";
  const rawText = text + (truncated ? `\n\n${truncated}` : "");
  const mimeType = source.mimeType || "";
  return {
    title,
    mimeType,
    formatLabel: mimeType,
    rawText,
    prettyText: "",
    prettyLabel: "",
    canPretty: canPrettyFormatBody(text, mimeType),
    hasPretty: false,
    prettyComputed: false,
    truncatedMessage: truncated,
    prettyCacheKey,
    hasBody: true
  };
}

export function entryDialogBodyElement(body, kind) {
  const wrapper = document.createElement("div");
  const header = document.createElement("div");
  header.className = "entry-dialog-body-header";
  const title = document.createElement("div");
  title.className = "entry-dialog-body-title";
  title.textContent = body.formatLabel ? `Body · ${body.formatLabel}` : "Body";
  const actions = document.createElement("div");
  actions.className = "entry-dialog-body-actions";
  const copyButton = document.createElement("button");
  copyButton.type = "button";
  copyButton.textContent = "Copy";
  copyButton.disabled = !body.hasBody;
  copyButton.addEventListener("click", () => copyBodyText(kind, "raw", copyButton));
  const openButton = document.createElement("button");
  openButton.type = "button";
  openButton.textContent = "Open";
  openButton.disabled = !body.hasBody;
  openButton.addEventListener("click", () => openBodyDialog(kind, openButton));
  actions.append(copyButton, openButton);
  const pre = document.createElement("pre");
  pre.tabIndex = 0;
  pre.textContent = body.rawText;
  pre.addEventListener("pointerdown", () => {
    pre.focus({ preventScroll: true });
  });
  pre.addEventListener("keydown", (event) => {
    if (!isSelectAllShortcut(event)) {
      return;
    }
    event.preventDefault();
    selectNodeContents(pre);
  });
  bindNestedScrollChain(pre);
  header.append(title, actions);
  wrapper.append(header, pre);
  return wrapper;
}

export function setBodyPreview(kind, options) {
  const target = kind === "request"
    ? { body: dom.requestBody, openButton: dom.requestBodyOpenButton, copyButton: dom.requestBodyCopyButton }
    : { body: dom.responseBody, openButton: dom.responseBodyOpenButton, copyButton: dom.responseBodyCopyButton };
  const rawText = typeof options.text === "string" ? options.text : "";
  const hasBody = rawText.length > 0;
  const previewText = hasBody
    ? rawText + (options.truncatedMessage ? `\n\n${options.truncatedMessage}` : "")
    : options.fallback;
  const rawDisplay = hasBody
    ? previewText
    : options.fallback;
  const mimeType = options.mimeType || "";

  target.body.textContent = previewText;
  target.openButton.disabled = !hasBody;
  target.copyButton.disabled = !hasBody;
  state.bodyPreviews[kind] = {
    title: options.title,
    mimeType,
    formatLabel: mimeType,
    rawText: rawDisplay,
    prettyText: "",
    prettyLabel: "",
    canPretty: hasBody && canPrettyFormatBody(rawText, mimeType),
    hasPretty: false,
    prettyComputed: false,
    truncatedMessage: options.truncatedMessage || "",
    prettyCacheKey: options.prettyCacheKey || "",
    hasBody
  };
}

export async function copyBodyText(kind, mode = "raw", trigger = null) {
  const preview = state.bodyPreviews[kind];
  if (!preview?.hasBody) {
    return;
  }
  const prettyMode = mode === "pretty" && ensurePrettyBodyPreview(preview);
  const text = prettyMode ? preview.prettyText : preview.rawText;
  try {
    await copyTextToClipboard(text);
    showCopySuccess(trigger);
    statusHandler(prettyMode ? "Copied pretty body" : "Copied raw body", false);
  } catch {
    statusHandler("Could not copy body", true);
  }
}

export function openBodyDialog(kind, trigger = document.activeElement) {
  const preview = state.bodyPreviews[kind];
  if (!preview?.hasBody) {
    return;
  }
  state.bodyDialogTrigger = trigger;
  state.bodyDialogKind = kind;
  state.bodyDialogMode = preview.canPretty ? "pretty" : "raw";
  renderBodyDialog();
  dom.bodyDialog.showModal();
  document.querySelector("#bodyDialogCloseButton")?.focus();
}

export function renderBodyDialog() {
  const preview = state.bodyPreviews[state.bodyDialogKind];
  if (!preview?.hasBody) {
    return;
  }
  const prettyMode = state.bodyDialogMode === "pretty" && ensurePrettyBodyPreview(preview);
  dom.bodyDialogTitle.textContent = preview.title;
  dom.bodyDialogMeta.textContent = prettyMode ? preview.prettyLabel : (preview.mimeType || "raw body");
  dom.bodyDialogContent.textContent = prettyMode ? preview.prettyText : preview.rawText;
  dom.bodyDialogPrettyButton.disabled = !preview.hasPretty;
  dom.bodyDialogCopyPrettyButton.disabled = !preview.hasPretty;
  dom.bodyDialogCopyRawButton.disabled = false;
  dom.bodyDialogPrettyButton.classList.toggle("active", prettyMode);
  dom.bodyDialogRawButton.classList.toggle("active", !prettyMode);
  dom.bodyDialogPrettyButton.setAttribute("aria-pressed", String(prettyMode));
  dom.bodyDialogRawButton.setAttribute("aria-pressed", String(!prettyMode));
}

export function selectBodyDialogContent() {
  selectNodeContents(dom.bodyDialogContent);
}

export function selectNodeContents(node) {
  const selection = window.getSelection();
  if (!selection) {
    return;
  }
  const range = document.createRange();
  range.selectNodeContents(node);
  selection.removeAllRanges();
  selection.addRange(range);
}

export function isSelectAllShortcut(event) {
  return (event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "a";
}

export function restoreDialogFocus(kind) {
  const trigger = kind === "body" ? state.bodyDialogTrigger : state.entryDialogTrigger;
  if (trigger && document.contains(trigger) && typeof trigger.focus === "function") {
    trigger.focus();
  }
  if (kind === "body") {
    state.bodyDialogTrigger = null;
  } else {
    state.entryDialogTrigger = null;
  }
}

export function ensurePrettyBodyPreview(preview) {
  if (!preview?.hasBody || !preview.canPretty) {
    return false;
  }
  if (!preview.prettyComputed) {
    const cached = cachedPrettyBody(preview.prettyCacheKey);
    if (cached) {
      applyPrettyBodyPreview(preview, cached);
    } else {
      const rawText = preview.rawText || "";
      const truncatedMessage = preview.truncatedMessage || "";
      const suffix = truncatedMessage ? `\n\n${truncatedMessage}` : "";
      const text = suffix && rawText.endsWith(suffix) ? rawText.slice(0, -suffix.length) : rawText;
      const formatted = formatBodyForDisplay(text, preview.mimeType || "", truncatedMessage);
      applyPrettyBodyPreview(preview, formatted);
      cachePrettyBody(preview.prettyCacheKey, formatted);
    }
  }
  return preview.hasPretty;
}

export function applyPrettyBodyPreview(preview, formatted) {
  preview.prettyText = formatted.text;
  preview.prettyLabel = formatted.label;
  preview.hasPretty = formatted.isPretty;
  preview.prettyComputed = true;
}

export function cachedPrettyBody(key) {
  if (!key) {
    return null;
  }
  const cached = bodyPrettyCache.get(key);
  if (!cached) {
    return null;
  }
  bodyPrettyCache.delete(key);
  bodyPrettyCache.set(key, cached);
  return cached;
}

export function cachePrettyBody(key, formatted) {
  if (!key) {
    return;
  }
  bodyPrettyCache.delete(key);
  bodyPrettyCache.set(key, {
    text: formatted.text,
    label: formatted.label,
    isPretty: formatted.isPretty
  });
  while (bodyPrettyCache.size > BODY_PRETTY_CACHE_LIMIT) {
    bodyPrettyCache.delete(bodyPrettyCache.keys().next().value);
  }
}

export function clearBodyPrettyCache() {
  bodyPrettyCache.clear();
}

export function bodyPreviewCacheKey(index, kind) {
  if (!state.sessionId || index === null || Number.isNaN(Number(index))) {
    return "";
  }
  return `${state.sessionId}:${index}:${kind}`;
}

function showCopySuccess(button) {
  if (!button) {
    return;
  }
  const originalText = button.dataset.copyLabel || button.textContent;
  const pendingTimer = copySuccessTimers.get(button);
  if (pendingTimer) {
    window.clearTimeout(pendingTimer);
  }
  button.dataset.copyLabel = originalText;
  button.classList.add("copy-success");
  button.textContent = "Copied";
  const timer = window.setTimeout(() => {
    button.classList.remove("copy-success");
    button.textContent = button.dataset.copyLabel || originalText;
    delete button.dataset.copyLabel;
    copySuccessTimers.delete(button);
  }, 1100);
  copySuccessTimers.set(button, timer);
}

async function copyTextToClipboard(text) {
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(text);
    return;
  }
  const textarea = document.createElement("textarea");
  textarea.value = text;
  textarea.setAttribute("readonly", "");
  textarea.style.position = "fixed";
  textarea.style.top = "-1000px";
  document.body.append(textarea);
  textarea.select();
  const copied = document.execCommand("copy");
  textarea.remove();
  if (!copied) {
    throw new Error("copy failed");
  }
}
