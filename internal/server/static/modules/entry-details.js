import {
  bodyDisplay,
  bodyPreviewCacheKey,
  emptyBodyPreview,
  entryDialogBodyElement,
  setBodyPreview
} from "./body-viewer.js";
import { dom } from "./dom.js";
import { statusLabel } from "./formatters.js";
import { refreshScrollFades } from "./scroll.js";
import { state } from "./state.js";

export function setDetailLoading() {
  dom.requestPanel.classList.add("has-detail");
  dom.responsePanel.classList.add("has-detail");
  dom.requestMeta.textContent = "Loading request";
  dom.requestMethod.textContent = "Loading";
  dom.requestStatus.textContent = "Loading";
  dom.requestHeaders.textContent = "";
  setBodyPreview("request", {
    title: "Request body",
    fallback: "Loading"
  });
  dom.responseMeta.textContent = "Loading response";
  dom.responseMethod.textContent = "Loading";
  dom.responseStatus.textContent = "Loading";
  dom.responseHeaders.textContent = "";
  setBodyPreview("response", {
    title: "Response body",
    fallback: "Loading"
  });
}

export function setDetailError(message) {
  dom.requestMeta.textContent = "Could not load request";
  dom.requestMethod.textContent = "-";
  dom.requestStatus.textContent = "-";
  dom.requestHeaders.textContent = "";
  setBodyPreview("request", {
    title: "Request body",
    fallback: message
  });
  dom.responseStatus.textContent = "-";
  dom.responseMeta.textContent = "Could not load response";
  dom.responseMethod.textContent = "-";
  dom.responseHeaders.textContent = "";
  setBodyPreview("response", {
    title: "Response body",
    fallback: message
  });
}

export function setEntryDialogLoading(message) {
  dom.entryDialogRequestMeta.textContent = message;
  dom.entryDialogResponseMeta.textContent = "";
  dom.entryDialogRequest.textContent = "";
  dom.entryDialogResponse.textContent = "";
  state.bodyPreviews.entryRequest = emptyBodyPreview("Request body");
  state.bodyPreviews.entryResponse = emptyBodyPreview("Response body");
}

export function renderEntryDialog(detail) {
  const request = detail.request || {};
  const response = detail.response || {};
  const requestBody = bodyDisplay("Request body", request.postData || {}, "No request body captured in this HAR entry", "[Request body truncated for preview]", bodyPreviewCacheKey(state.entryDialogIndex, "request"));
  const responseBody = bodyDisplay("Response body", response.content || {}, "No response body captured in this HAR entry", "[Response body truncated for preview]", bodyPreviewCacheKey(state.entryDialogIndex, "response"));
  state.bodyPreviews.entryRequest = requestBody;
  state.bodyPreviews.entryResponse = responseBody;

  dom.entryDialogRequestMeta.textContent = request.method || "-";
  dom.entryDialogResponseMeta.textContent = statusLabel(response);
  dom.entryDialogRequest.replaceChildren(
    entryDialogField("URL", request.url || "-"),
    ...entryDialogHeaderElements(request.headers || []),
    entryDialogBodyElement(requestBody, "entryRequest")
  );
  dom.entryDialogResponse.replaceChildren(
    entryDialogField("MIME", response.content?.mimeType || "-"),
    ...entryDialogHeaderElements(response.headers || []),
    entryDialogBodyElement(responseBody, "entryResponse")
  );
  refreshScrollFades(dom.entryDialog);
}

export function renderEntryDetail(detail, index) {
  const request = detail.request || {};
  const response = detail.response || {};
  renderRequestDetail(request, response, index);
  renderResponseDetail(request, response, index);
  refreshScrollFades();
}

export function clearDetailPanels() {
  state.detailRequestId += 1;
  dom.requestPanel.classList.remove("has-detail");
  dom.responsePanel.classList.remove("has-detail");
  dom.requestMeta.textContent = "";
  dom.requestMethod.textContent = "";
  dom.requestStatus.textContent = "";
  dom.requestHeaders.textContent = "";
  setBodyPreview("request", {
    title: "Request body",
    fallback: "No request selected"
  });
  dom.responseMeta.textContent = "";
  dom.responseMethod.textContent = "";
  dom.responseStatus.textContent = "";
  dom.responseHeaders.textContent = "";
  setBodyPreview("response", {
    title: "Response body",
    fallback: "No response selected"
  });
  refreshScrollFades();
}

function renderRequestDetail(request, response, index) {
  const postData = request.postData || {};
  dom.requestMeta.textContent = request.url || "Request";
  dom.requestMethod.textContent = request.method || "-";
  dom.requestStatus.textContent = statusLabel(response);
  dom.requestHeaders.replaceChildren(...headerElements(request.headers || []));
  setBodyPreview("request", {
    title: "Request body",
    mimeType: postData.mimeType || "",
    text: postData.text || "",
    fallback: "No request body captured in this HAR entry",
    truncatedMessage: postData.truncated ? "[Request body truncated for preview]" : "",
    prettyCacheKey: bodyPreviewCacheKey(index, "request")
  });
}

function renderResponseDetail(request, response, index) {
  const content = response.content || {};
  dom.responseMeta.textContent = request.url || "Response URL unavailable";
  dom.responseMethod.textContent = request.method || "-";
  dom.responseStatus.textContent = statusLabel(response);
  dom.responseHeaders.replaceChildren(...headerElements(response.headers || []));
  setBodyPreview("response", {
    title: "Response body",
    mimeType: content.mimeType || "",
    text: content.text || "",
    fallback: "No response body captured in this HAR entry",
    truncatedMessage: content.truncated ? "[Response body truncated for preview]" : "",
    prettyCacheKey: bodyPreviewCacheKey(index, "response")
  });
}

function entryDialogHeaderElements(headers) {
  return headers.map((header) => entryDialogField(header.name || "", header.value || ""));
}

function entryDialogField(name, value) {
  const item = document.createElement("div");
  item.className = "entry-dialog-field";
  const label = document.createElement("span");
  label.textContent = name;
  const content = document.createElement("strong");
  content.textContent = value;
  item.append(label, content);
  return item;
}

function headerElements(headers) {
  return headers.map((header) => {
    const item = document.createElement("div");
    item.className = "detail-header";
    const name = document.createElement("span");
    name.textContent = header.name || "";
    const value = document.createElement("strong");
    value.textContent = header.value || "";
    item.append(name, value);
    return item;
  });
}
