export function emptyBodyPreview(title) {
  return {
    title,
    mimeType: "",
    formatLabel: "",
    rawText: "",
    prettyText: "",
    prettyLabel: "",
    canPretty: false,
    hasPretty: false,
    prettyComputed: false,
    truncatedMessage: "",
    prettyCacheKey: "",
    hasBody: false
  };
}
