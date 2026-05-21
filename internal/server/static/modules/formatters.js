import { VOID_MARKUP_TAGS } from "./constants.js";

export function formatNumber(value) {
  if (typeof value !== "number" || Number.isNaN(value)) {
    return "0";
  }
  return new Intl.NumberFormat("en-US", { maximumFractionDigits: 1 }).format(value);
}

export function formatBytes(value) {
  if (!value || value < 0) {
    return "-";
  }
  if (value < 1024) {
    return `${value} B`;
  }
  if (value < 1024 * 1024) {
    return `${formatNumber(value / 1024)} KB`;
  }
  return `${formatNumber(value / 1024 / 1024)} MB`;
}

export function filteredFileName(name) {
  const clean = name || "capture.har";
  return clean.toLowerCase().endsWith(".har")
    ? `${clean.slice(0, -4)}-filtered.har`
    : `${clean}-filtered.har`;
}

export function statusLabel(response) {
  return response?.status ? String(response.status) : "-";
}

export function statusGroup(status) {
  if (status >= 500) {
    return "server-error";
  }
  if (status >= 400) {
    return "client-error";
  }
  if (status >= 300) {
    return "redirect";
  }
  if (status >= 200) {
    return "success";
  }
  return "unknown";
}

export function statusTone(message) {
  const normalized = message.toLowerCase();
  if (normalized.includes("uploading") || normalized.includes("creating")) {
    return "working";
  }
  if (normalized.includes("ready") || normalized.includes("loaded") || normalized.includes("created")) {
    return "ready";
  }
  return "idle";
}

export function resourceTypeLabel(type) {
  return type || "Other";
}

export function formatBodyForDisplay(text, mimeType, truncatedMessage) {
  const pretty = globalThis.formatPrettyBody(text, mimeType);
  const displayText = truncatedMessage ? `${pretty.text}\n\n${truncatedMessage}` : pretty.text;
  return { text: displayText, label: pretty.label, isPretty: pretty.isPretty };
}

export function canPrettyFormatBody(text, mimeType) {
  if (!text) {
    return false;
  }
  const type = (mimeType || "").toLowerCase();
  if (type.includes("json") || type.includes("x-www-form-urlencoded") || type.includes("html") || type.includes("xml") || isJavaScriptMimeType(type)) {
    return true;
  }
  const firstNonSpace = text.match(/\S/)?.[0] || "";
  return firstNonSpace === "{" || firstNonSpace === "[" || firstNonSpace === "<";
}

export function formatPrettyBody(text, mimeType) {
  const type = (mimeType || "").toLowerCase();
  const trimmed = text.trim();

  if (type.includes("json") || trimmed.startsWith("{") || trimmed.startsWith("[")) {
    try {
      return {
        text: JSON.stringify(JSON.parse(text), null, 2),
        label: mimeType ? `${mimeType} · formatted JSON` : "formatted JSON",
        isPretty: true
      };
    } catch {
      return { text, label: mimeType || "text/plain", isPretty: false };
    }
  }

  if (type.includes("x-www-form-urlencoded")) {
    const params = new URLSearchParams(text);
    return {
      text: Array.from(params.entries())
      .map(([key, value]) => `${key}=${value}`)
      .join("\n"),
      label: `${mimeType} · decoded form`,
      isPretty: true
    };
  }

  if (type.includes("html") || type.includes("xml") || /^<[\s\S]+>$/.test(trimmed)) {
    const prettyMarkup = formatMarkup(text);
    return {
      text: prettyMarkup,
      label: mimeType ? `${mimeType} · formatted markup` : "formatted markup",
      isPretty: prettyMarkup !== text
    };
  }

  if (isJavaScriptMimeType(type)) {
    const prettyJavaScript = formatJavaScript(text);
    return {
      text: prettyJavaScript,
      label: mimeType ? `${mimeType} · formatted JavaScript` : "formatted JavaScript",
      isPretty: prettyJavaScript !== text
    };
  }

  return { text, label: mimeType || "text/plain", isPretty: false };
}

export function isJavaScriptMimeType(type) {
  return type.includes("javascript") || type.includes("ecmascript");
}

export function formatJavaScript(text) {
  let output = "";
  let indent = 0;
  let atLineStart = false;

  const appendIndent = () => {
    if (atLineStart) {
      output += "  ".repeat(indent);
      atLineStart = false;
    }
  };
  const trimOutputEnd = () => {
    output = output.replace(/[ \t]+$/g, "");
  };
  const appendNewline = () => {
    trimOutputEnd();
    if (!output.endsWith("\n")) {
      output += "\n";
    }
    atLineStart = true;
  };
  const nextNonSpace = (start) => {
    for (let index = start; index < text.length; index += 1) {
      if (!/\s/.test(text[index])) {
        return text[index];
      }
    }
    return "";
  };
  const previousOutputNonSpace = () => {
    for (let index = output.length - 1; index >= 0; index -= 1) {
      if (!/\s/.test(output[index])) {
        return output[index];
      }
    }
    return "";
  };
  const previousOutputToken = () => {
    const match = output.match(/([A-Za-z_$][\w$]*|.)\s*$/);
    return match ? match[1] : "";
  };
  const isRegexLiteralStart = () => {
    const token = previousOutputToken();
    return token === ""
      || ["(", "[", "{", ":", ";", ",", "=", "!", "?", "&", "|", "+", "-", "*", "~", "^", "<", ">"].includes(token)
      || ["return", "case", "throw", "delete", "void", "typeof", "instanceof", "in", "await", "yield"].includes(token);
  };

  for (let index = 0; index < text.length; index += 1) {
    const char = text[index];
    const next = text[index + 1] || "";

    if (char === "\"" || char === "'" || char === "`") {
      appendIndent();
      const quote = char;
      output += char;
      index += 1;
      for (; index < text.length; index += 1) {
        output += text[index];
        if (text[index] === "\\") {
          index += 1;
          if (index < text.length) {
            output += text[index];
          }
          continue;
        }
        if (text[index] === quote) {
          break;
        }
      }
      continue;
    }

    if (char === "/" && next === "/") {
      appendIndent();
      for (; index < text.length && text[index] !== "\n"; index += 1) {
        output += text[index];
      }
      appendNewline();
      continue;
    }

    if (char === "/" && next === "*") {
      appendIndent();
      output += "/*";
      index += 2;
      for (; index < text.length; index += 1) {
        output += text[index];
        if (text[index] === "*" && text[index + 1] === "/") {
          output += "/";
          index += 1;
          break;
        }
      }
      continue;
    }

    if (char === "/" && isRegexLiteralStart()) {
      appendIndent();
      output += char;
      index += 1;
      let inCharacterClass = false;
      for (; index < text.length; index += 1) {
        output += text[index];
        if (text[index] === "\\") {
          index += 1;
          if (index < text.length) {
            output += text[index];
          }
          continue;
        }
        if (text[index] === "[") {
          inCharacterClass = true;
          continue;
        }
        if (text[index] === "]") {
          inCharacterClass = false;
          continue;
        }
        if (text[index] === "/" && !inCharacterClass) {
          while (/[A-Za-z]/.test(text[index + 1] || "")) {
            index += 1;
            output += text[index];
          }
          break;
        }
      }
      continue;
    }

    if (/\s/.test(char)) {
      if (!output.endsWith(" ") && !output.endsWith("\n")) {
        output += " ";
      }
      continue;
    }

    appendIndent();

    if (char === "{") {
      trimOutputEnd();
      if (!output.endsWith(" ")) {
        output += " ";
      }
      output += "{";
      indent += 1;
      appendNewline();
      continue;
    }

    if (char === "}") {
      appendNewline();
      indent = Math.max(0, indent - 1);
      appendIndent();
      output += "}";
      if (![";", ",", ")", "]"].includes(nextNonSpace(index + 1))) {
        appendNewline();
      }
      continue;
    }

    if (char === ";") {
      trimOutputEnd();
      output += ";";
      appendNewline();
      continue;
    }

    if (char === ",") {
      trimOutputEnd();
      output += ", ";
      continue;
    }

    if (char === "=" && next === ">") {
      trimOutputEnd();
      output += " => ";
      index += 1;
      continue;
    }

    if (char === "=" && (next === "=" || ["=", "!", "<", ">"].includes(previousOutputNonSpace()))) {
      output += char;
      continue;
    }

    if (char === "=" && next !== ">") {
      trimOutputEnd();
      output += " = ";
      continue;
    }

    output += char;
  }

  return output.trim();
}

export function formatMarkup(text) {
  const lines = text.trim().replace(/>\s*</g, ">\n<").split("\n");
  let depth = 0;
  return lines.map((line) => {
    const trimmed = line.trim();
    if (/^<\//.test(trimmed)) {
      depth = Math.max(0, depth - 1);
    }
    const out = `${"  ".repeat(depth)}${trimmed}`;
    if (/^<[^!?/][^>]*[^/]?>$/.test(trimmed) && !/^<[^>]+>.*<\/[^>]+>$/.test(trimmed) && !isSelfClosingMarkupLine(trimmed)) {
      depth += 1;
    }
    return out;
  }).join("\n");
}

export function isSelfClosingMarkupLine(trimmed) {
  if (trimmed.endsWith("/>")) {
    return true;
  }
  const match = trimmed.match(/^<([A-Za-z][A-Za-z0-9:-]*)\b/);
  return Boolean(match && VOID_MARKUP_TAGS.has(match[1].toLowerCase()));
}

globalThis.formatPrettyBody = formatPrettyBody;
