package har

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// EntrySummary is the compact request metadata returned to the browser.
type EntrySummary struct {
	Index           int     `json:"index"`
	Method          string  `json:"method"`
	URL             string  `json:"url"`
	Host            string  `json:"host"`
	Path            string  `json:"path"`
	Status          int     `json:"status"`
	StatusText      string  `json:"statusText"`
	MimeType        string  `json:"mimeType"`
	ResourceType    string  `json:"resourceType"`
	StartedDateTime string  `json:"startedDateTime"`
	TimeMS          float64 `json:"timeMs"`
	Size            int64   `json:"size"`
}

type Header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type ResponseDetail struct {
	Index    int            `json:"index"`
	Request  DetailRequest  `json:"request"`
	Response DetailResponse `json:"response"`
}

type DetailRequest struct {
	Method   string        `json:"method"`
	URL      string        `json:"url"`
	Headers  []Header      `json:"headers"`
	PostData DetailContent `json:"postData"`
}

type DetailResponse struct {
	Status     int           `json:"status"`
	StatusText string        `json:"statusText"`
	Headers    []Header      `json:"headers"`
	Content    DetailContent `json:"content"`
}

type DetailContent struct {
	MimeType  string `json:"mimeType"`
	Size      int64  `json:"size"`
	Text      string `json:"text"`
	Encoding  string `json:"encoding"`
	Truncated bool   `json:"truncated"`
}

// Document keeps only the raw JSON needed for detail views and privacy-minimal
// subset exports.
type Document struct {
	version       json.RawMessage
	entries       []json.RawMessage
	Summaries     []EntrySummary
	retainedBytes int64
}

type entryMeta struct {
	StartedDateTime string  `json:"startedDateTime"`
	Time            float64 `json:"time"`
	Request         struct {
		Method  string   `json:"method"`
		URL     string   `json:"url"`
		Headers []Header `json:"headers"`
	} `json:"request"`
	Response struct {
		Status     int    `json:"status"`
		StatusText string `json:"statusText"`
		Content    struct {
			MimeType string `json:"mimeType"`
			Size     int64  `json:"size"`
		} `json:"content"`
	} `json:"response"`
	ResourceType string `json:"_resourceType"`
}

type entryDetailMeta struct {
	Request struct {
		Method   string   `json:"method"`
		URL      string   `json:"url"`
		Headers  []Header `json:"headers"`
		PostData struct {
			MimeType string     `json:"mimeType"`
			Text     detailText `json:"text"`
		} `json:"postData"`
	} `json:"request"`
	Response struct {
		Status     int      `json:"status"`
		StatusText string   `json:"statusText"`
		Headers    []Header `json:"headers"`
		Content    struct {
			MimeType string     `json:"mimeType"`
			Size     int64      `json:"size"`
			Text     detailText `json:"text"`
			Encoding string     `json:"encoding"`
		} `json:"content"`
	} `json:"response"`
}

const maxDetailTextBytes = 200 << 10

var maxCapturedDetailTextBytes = base64.StdEncoding.EncodedLen(maxDetailTextBytes + 1)

type detailText struct {
	Text      string
	Size      int64
	Truncated bool
}

func (t *detailText) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*t = detailText{}
		return nil
	}
	text, size, truncated, err := decodeJSONStringPreview(data, maxCapturedDetailTextBytes)
	if err != nil {
		return err
	}
	*t = detailText{
		Text:      text,
		Size:      size,
		Truncated: truncated,
	}
	return nil
}

// Parse validates a HAR document and extracts summary metadata for every entry.
func Parse(data []byte) (*Document, error) {
	if !json.Valid(data) {
		return nil, errors.New("invalid JSON")
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("decode HAR root: %w", err)
	}

	logRaw, ok := root["log"]
	if !ok {
		return nil, errors.New("HAR is missing log")
	}

	var log map[string]json.RawMessage
	if err := json.Unmarshal(logRaw, &log); err != nil {
		return nil, fmt.Errorf("decode HAR log: %w", err)
	}

	entriesRaw, ok := log["entries"]
	if !ok {
		return nil, errors.New("HAR log is missing entries")
	}

	var entries []json.RawMessage
	if err := json.Unmarshal(entriesRaw, &entries); err != nil {
		return nil, fmt.Errorf("decode HAR entries: %w", err)
	}

	summaries := make([]EntrySummary, 0, len(entries))
	for i, raw := range entries {
		summary, err := summarizeEntry(i, raw)
		if err != nil {
			return nil, fmt.Errorf("entry %d: %w", i, err)
		}
		summaries = append(summaries, summary)
	}

	var version json.RawMessage
	if raw, ok := log["version"]; ok {
		version = cloneRawMessage(raw)
	}

	return &Document{
		version:       version,
		entries:       entries,
		Summaries:     summaries,
		retainedBytes: estimateRetainedBytes(data, version, entries, summaries),
	}, nil
}

// RetainedBytes estimates the parsed HAR memory retained by this document.
func (d *Document) RetainedBytes() int64 {
	if d == nil {
		return 0
	}
	return d.retainedBytes
}

func summarizeEntry(index int, raw json.RawMessage) (EntrySummary, error) {
	var meta entryMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return EntrySummary{}, fmt.Errorf("decode entry: %w", err)
	}
	if strings.TrimSpace(meta.Request.Method) == "" {
		return EntrySummary{}, errors.New("request method is empty")
	}
	if strings.TrimSpace(meta.Request.URL) == "" {
		return EntrySummary{}, errors.New("request URL is empty")
	}

	host := ""
	requestPath := ""
	if parsed, err := url.Parse(meta.Request.URL); err == nil {
		host = parsed.Host
		requestPath = parsed.EscapedPath()
		if requestPath == "" {
			requestPath = "/"
		}
	} else {
		requestPath = path.Clean(meta.Request.URL)
	}

	return EntrySummary{
		Index:           index,
		Method:          meta.Request.Method,
		URL:             meta.Request.URL,
		Host:            host,
		Path:            requestPath,
		Status:          meta.Response.Status,
		StatusText:      meta.Response.StatusText,
		MimeType:        meta.Response.Content.MimeType,
		ResourceType:    classifyResourceType(meta.ResourceType, meta.Request.Method, meta.Request.URL, meta.Request.Headers, meta.Response.Content.MimeType),
		StartedDateTime: meta.StartedDateTime,
		TimeMS:          meta.Time,
		Size:            meta.Response.Content.Size,
	}, nil
}

func classifyResourceType(rawType string, method string, requestURL string, requestHeaders []Header, mimeType string) string {
	if normalized := normalizeResourceType(rawType); normalized != "" {
		return normalized
	}

	parsed, _ := url.Parse(requestURL)
	scheme := strings.ToLower(parsed.Scheme)
	if scheme == "ws" || scheme == "wss" {
		return "WS"
	}

	normalizedMime := strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0]))
	switch {
	case normalizedMime == "text/html" || normalizedMime == "application/xhtml+xml":
		return "Doc"
	case normalizedMime == "text/css":
		return "CSS"
	case strings.Contains(normalizedMime, "javascript") || normalizedMime == "text/ecmascript":
		return "JS"
	case strings.HasPrefix(normalizedMime, "image/"):
		return "Img"
	case strings.HasPrefix(normalizedMime, "audio/") || strings.HasPrefix(normalizedMime, "video/"):
		return "Media"
	case strings.HasPrefix(normalizedMime, "font/") || strings.Contains(normalizedMime, "font") || strings.Contains(normalizedMime, "woff"):
		return "Font"
	case normalizedMime == "application/wasm":
		return "Wasm"
	case normalizedMime == "application/json" || strings.HasSuffix(normalizedMime, "+json") || normalizedMime == "text/event-stream" || normalizedMime == "application/xml" || strings.HasSuffix(normalizedMime, "+xml"):
		return "Fetch/XHR"
	}

	lowerPath := strings.ToLower(parsed.Path)
	switch path.Ext(lowerPath) {
	case ".html", ".htm":
		return "Doc"
	case ".css":
		return "CSS"
	case ".js", ".mjs", ".cjs":
		return "JS"
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".avif", ".svg", ".ico":
		return "Img"
	case ".mp3", ".mp4", ".webm", ".ogg", ".wav", ".mov", ".m4a":
		return "Media"
	case ".woff", ".woff2", ".ttf", ".otf", ".eot":
		return "Font"
	case ".wasm":
		return "Wasm"
	}

	requestedWith := strings.ToLower(headerValue(requestHeaders, "x-requested-with"))
	if strings.Contains(requestedWith, "xmlhttprequest") {
		return "Fetch/XHR"
	}

	accept := strings.ToLower(headerValue(requestHeaders, "accept"))
	switch {
	case strings.Contains(accept, "text/html"):
		return "Doc"
	case strings.Contains(accept, "text/css"):
		return "CSS"
	case strings.Contains(accept, "image/"):
		return "Img"
	case strings.Contains(accept, "application/json") || strings.Contains(accept, "text/event-stream"):
		return "Fetch/XHR"
	}

	if strings.EqualFold(method, "POST") || strings.EqualFold(method, "PUT") || strings.EqualFold(method, "PATCH") || strings.EqualFold(method, "DELETE") {
		return "Fetch/XHR"
	}

	return "Other"
}

func normalizeResourceType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "xhr", "fetch":
		return "Fetch/XHR"
	case "document", "doc", "main_frame", "sub_frame":
		return "Doc"
	case "stylesheet", "css":
		return "CSS"
	case "script", "js":
		return "JS"
	case "image", "img", "imageset":
		return "Img"
	case "media":
		return "Media"
	case "font":
		return "Font"
	case "websocket", "ws":
		return "WS"
	case "wasm":
		return "Wasm"
	case "other", "preflight", "manifest", "ping", "csp_report":
		return "Other"
	default:
		return ""
	}
}

func headerValue(headers []Header, name string) string {
	for _, header := range headers {
		if strings.EqualFold(header.Name, name) {
			return header.Value
		}
	}
	return ""
}

func (d *Document) ResponseDetail(index int) (ResponseDetail, error) {
	if d == nil {
		return ResponseDetail{}, errors.New("document is nil")
	}
	if index < 0 || index >= len(d.entries) {
		return ResponseDetail{}, fmt.Errorf("entry index %d out of range", index)
	}

	var meta entryDetailMeta
	if err := json.Unmarshal(d.entries[index], &meta); err != nil {
		return ResponseDetail{}, fmt.Errorf("decode entry detail: %w", err)
	}

	requestText, requestWasTruncated := truncateString(meta.Request.PostData.Text.Text, maxDetailTextBytes)
	requestTruncated := requestWasTruncated || meta.Request.PostData.Text.Truncated
	responseText, responseEncoding, responseWasTruncated := decodeContentPreview(meta.Response.Content.Text.Text, meta.Response.Content.Encoding, maxDetailTextBytes)
	responseTruncated := responseWasTruncated || meta.Response.Content.Text.Truncated
	return ResponseDetail{
		Index: index,
		Request: DetailRequest{
			Method:  meta.Request.Method,
			URL:     meta.Request.URL,
			Headers: meta.Request.Headers,
			PostData: DetailContent{
				MimeType:  meta.Request.PostData.MimeType,
				Size:      meta.Request.PostData.Text.Size,
				Text:      requestText,
				Truncated: requestTruncated,
			},
		},
		Response: DetailResponse{
			Status:     meta.Response.Status,
			StatusText: meta.Response.StatusText,
			Headers:    meta.Response.Headers,
			Content: DetailContent{
				MimeType:  meta.Response.Content.MimeType,
				Size:      meta.Response.Content.Size,
				Text:      responseText,
				Encoding:  responseEncoding,
				Truncated: responseTruncated,
			},
		},
	}, nil
}

func decodeContentPreview(text string, encoding string, maxBytes int) (string, string, bool) {
	normalizedEncoding := strings.TrimSpace(encoding)
	if !strings.EqualFold(normalizedEncoding, "base64") {
		truncated, wasTruncated := truncateString(text, maxBytes)
		return truncated, encoding, wasTruncated
	}
	decoded, err := io.ReadAll(io.LimitReader(base64.NewDecoder(base64.StdEncoding, strings.NewReader(text)), int64(maxBytes+1)))
	if err != nil {
		truncated, wasTruncated := truncateString(text, maxBytes)
		return truncated, encoding, wasTruncated
	}
	if len(decoded) > maxBytes {
		return string(decoded[:maxBytes]), "", true
	}
	return string(decoded), "", false
}

func decodeJSONStringPreview(data []byte, maxBytes int) (string, int64, bool, error) {
	if len(data) < 2 || data[0] != '"' || data[len(data)-1] != '"' {
		return "", 0, false, fmt.Errorf("decode JSON string preview: value is not a string")
	}

	preview := make([]byte, 0, min(maxBytes, len(data)-2))
	var decodedSize int64
	var truncated bool
	appendDecoded := func(value []byte) bool {
		decodedSize += int64(len(value))
		if len(preview) >= maxBytes {
			truncated = true
			return false
		}
		remaining := maxBytes - len(preview)
		if len(value) > remaining {
			preview = append(preview, value[:remaining]...)
			truncated = true
			return false
		}
		preview = append(preview, value...)
		return true
	}

scan:
	for i := 1; i < len(data)-1; {
		if data[i] != '\\' {
			decodedRune, size := utf8.DecodeRune(data[i : len(data)-1])
			var encoded [utf8.UTFMax]byte
			n := utf8.EncodeRune(encoded[:], decodedRune)
			if !appendDecoded(encoded[:n]) {
				break scan
			}
			i += size
			continue
		}
		if i+1 >= len(data)-1 {
			return "", 0, false, fmt.Errorf("decode JSON string preview: incomplete escape")
		}
		switch data[i+1] {
		case '"', '\\', '/':
			if !appendDecoded(data[i+1 : i+2]) {
				break scan
			}
			i += 2
		case 'b':
			if !appendDecoded([]byte{'\b'}) {
				break scan
			}
			i += 2
		case 'f':
			if !appendDecoded([]byte{'\f'}) {
				break scan
			}
			i += 2
		case 'n':
			if !appendDecoded([]byte{'\n'}) {
				break scan
			}
			i += 2
		case 'r':
			if !appendDecoded([]byte{'\r'}) {
				break scan
			}
			i += 2
		case 't':
			if !appendDecoded([]byte{'\t'}) {
				break scan
			}
			i += 2
		case 'u':
			decodedRune, next, err := decodeJSONUnicodeEscape(data, i)
			if err != nil {
				return "", 0, false, err
			}
			var encoded [utf8.UTFMax]byte
			n := utf8.EncodeRune(encoded[:], decodedRune)
			if !appendDecoded(encoded[:n]) {
				break scan
			}
			i = next
		default:
			return "", 0, false, fmt.Errorf("decode JSON string preview: invalid escape %q", data[i+1])
		}
	}

	return string(preview), decodedSize, truncated, nil
}

func decodeJSONUnicodeEscape(data []byte, offset int) (rune, int, error) {
	if offset+6 > len(data) {
		return 0, 0, fmt.Errorf("decode JSON string preview: incomplete unicode escape")
	}
	first, ok := parseHexRune(data[offset+2 : offset+6])
	if !ok {
		return 0, 0, fmt.Errorf("decode JSON string preview: invalid unicode escape")
	}
	next := offset + 6
	if utf16.IsSurrogate(first) {
		if next+6 <= len(data) && data[next] == '\\' && data[next+1] == 'u' {
			second, ok := parseHexRune(data[next+2 : next+6])
			if ok {
				if decoded := utf16.DecodeRune(first, second); decoded != utf8.RuneError {
					return decoded, next + 6, nil
				}
			}
		}
		return utf8.RuneError, next, nil
	}
	return first, next, nil
}

func parseHexRune(data []byte) (rune, bool) {
	if len(data) != 4 {
		return 0, false
	}
	var value rune
	for _, b := range data {
		var digit rune
		switch {
		case b >= '0' && b <= '9':
			digit = rune(b - '0')
		case b >= 'a' && b <= 'f':
			digit = rune(b-'a') + 10
		case b >= 'A' && b <= 'F':
			digit = rune(b-'A') + 10
		default:
			return 0, false
		}
		value = value*16 + digit
	}
	return value, true
}

// BuildSubset returns a valid HAR whose log.entries contains only selected indexes.
func (d *Document) BuildSubset(indexes []int) ([]byte, error) {
	var out bytes.Buffer
	if err := d.WriteSubset(&out, indexes); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// ValidateSubset reports whether indexes can be exported from the document.
func (d *Document) ValidateSubset(indexes []int) error {
	_, err := d.selectedIndexes(indexes)
	return err
}

// WriteSubset writes a valid HAR whose log.entries contains only selected indexes.
func (d *Document) WriteSubset(w io.Writer, indexes []int) error {
	if d == nil {
		return errors.New("document is nil")
	}
	selectedIndexes, err := d.selectedIndexes(indexes)
	if err != nil {
		return err
	}

	if _, err := io.WriteString(w, `{"log":{`); err != nil {
		return fmt.Errorf("write HAR root: %w", err)
	}
	if len(d.version) > 0 {
		if _, err := io.WriteString(w, `"version":`); err != nil {
			return fmt.Errorf("write HAR version key: %w", err)
		}
		if _, err := w.Write(d.version); err != nil {
			return fmt.Errorf("write HAR version: %w", err)
		}
		if _, err := io.WriteString(w, `,`); err != nil {
			return fmt.Errorf("write HAR version separator: %w", err)
		}
	}

	if _, err := io.WriteString(w, `"entries":[`); err != nil {
		return fmt.Errorf("write HAR entries key: %w", err)
	}
	for i, index := range selectedIndexes {
		if i > 0 {
			if _, err := io.WriteString(w, `,`); err != nil {
				return fmt.Errorf("write HAR entry separator: %w", err)
			}
		}
		if _, err := w.Write(d.entries[index]); err != nil {
			return fmt.Errorf("write HAR entry %d: %w", index, err)
		}
	}
	if _, err := io.WriteString(w, `]}}`); err != nil {
		return fmt.Errorf("write HAR close: %w", err)
	}
	return nil
}

func (d *Document) selectedIndexes(indexes []int) ([]int, error) {
	if len(indexes) == 0 {
		return nil, errors.New("at least one entry index is required")
	}

	selected := make([]int, 0, len(indexes))
	seen := make(map[int]struct{}, len(indexes))
	for _, index := range indexes {
		if index < 0 || index >= len(d.entries) {
			return nil, fmt.Errorf("entry index %d out of range", index)
		}
		if _, ok := seen[index]; ok {
			continue
		}
		seen[index] = struct{}{}
		selected = append(selected, index)
	}
	return selected, nil
}

func estimateRetainedBytes(data []byte, version json.RawMessage, entries []json.RawMessage, summaries []EntrySummary) int64 {
	total := int64(len(data))
	total += int64(len(version))
	for _, entry := range entries {
		total += int64(len(entry))
	}
	for _, summary := range summaries {
		total += int64(len(summary.Method) + len(summary.URL) + len(summary.Host) + len(summary.Path) + len(summary.StatusText) + len(summary.MimeType) + len(summary.ResourceType) + len(summary.StartedDateTime))
	}
	return total
}

func truncateString(value string, maxBytes int) (string, bool) {
	if len(value) <= maxBytes {
		return value, false
	}
	return value[:maxBytes], true
}

func cloneRawMessage(value json.RawMessage) json.RawMessage {
	copied := make(json.RawMessage, len(value))
	copy(copied, value)
	return copied
}
