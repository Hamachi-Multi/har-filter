package har_test

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Hamachi-Multi/har-filter/internal/har"
)

const sampleHAR = `{
  "log": {
    "version": "1.2",
    "creator": { "name": "Browser", "version": "125.0" },
    "pages": [{ "id": "page_1", "title": "Example" }],
    "entries": [
      {
        "startedDateTime": "2026-05-17T10:00:00.000Z",
        "time": 42.7,
        "request": {
          "method": "GET",
          "url": "https://example.com/api/users?active=true",
          "httpVersion": "HTTP/2",
          "headers": [{ "name": "accept", "value": "application/json" }]
        },
        "response": {
          "status": 200,
          "statusText": "OK",
          "headers": [{ "name": "content-type", "value": "application/json" }],
          "content": { "mimeType": "application/json", "size": 73, "text": "{\"ok\":true}" }
        },
        "customField": "preserved"
      },
      {
        "startedDateTime": "2026-05-17T10:00:01.000Z",
        "time": 318.4,
        "request": {
          "method": "POST",
          "url": "https://example.com/api/orders",
          "httpVersion": "HTTP/2",
          "headers": [{ "name": "content-type", "value": "application/json" }],
          "postData": { "mimeType": "application/json", "text": "{\"orderId\":42}" }
        },
        "response": {
          "status": 201,
          "statusText": "Created",
          "content": { "mimeType": "application/json", "size": 129 }
        }
      }
    ]
  }
}`

func TestParseSummarizesEntriesAndExportsPrivacyMinimalSubset(t *testing.T) {
	doc, err := har.Parse([]byte(sampleHAR))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if got, want := len(doc.Summaries), 2; got != want {
		t.Fatalf("summary count = %d, want %d", got, want)
	}

	first := doc.Summaries[0]
	if first.Index != 0 {
		t.Fatalf("first index = %d, want 0", first.Index)
	}
	if first.Method != "GET" {
		t.Fatalf("first method = %q, want GET", first.Method)
	}
	if first.URL != "https://example.com/api/users?active=true" {
		t.Fatalf("first url = %q", first.URL)
	}
	if first.Host != "example.com" {
		t.Fatalf("first host = %q, want example.com", first.Host)
	}
	if first.Path != "/api/users" {
		t.Fatalf("first path = %q, want /api/users", first.Path)
	}
	if first.Status != 200 {
		t.Fatalf("first status = %d, want 200", first.Status)
	}
	if first.MimeType != "application/json" {
		t.Fatalf("first mime type = %q, want application/json", first.MimeType)
	}
	if first.ResourceType != "Fetch/XHR" {
		t.Fatalf("first resource type = %q, want Fetch/XHR", first.ResourceType)
	}
	if first.Size != 73 {
		t.Fatalf("first size = %d, want 73", first.Size)
	}

	subset, err := doc.BuildSubset([]int{1})
	if err != nil {
		t.Fatalf("BuildSubset returned error: %v", err)
	}

	var decoded struct {
		Log map[string]json.RawMessage `json:"log"`
	}
	if err := json.Unmarshal(subset, &decoded); err != nil {
		t.Fatalf("subset is invalid JSON: %v", err)
	}
	if string(decoded.Log["version"]) != `"1.2"` {
		t.Fatalf("version = %s, want 1.2", decoded.Log["version"])
	}
	if _, ok := decoded.Log["creator"]; ok {
		t.Fatalf("subset preserves creator metadata: %s", subset)
	}
	if _, ok := decoded.Log["pages"]; ok {
		t.Fatalf("subset preserves page metadata: %s", subset)
	}
	if _, ok := decoded.Log["customField"]; ok {
		t.Fatalf("subset preserves custom log metadata: %s", subset)
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(decoded.Log["entries"], &entries); err != nil {
		t.Fatalf("entries are invalid JSON: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("subset entries = %d, want 1", len(entries))
	}
	if strings.Contains(string(subset), "/api/users") {
		t.Fatalf("subset contains unselected request: %s", subset)
	}
	if !strings.Contains(string(subset), "/api/orders") {
		t.Fatalf("subset does not contain selected request: %s", subset)
	}
}

func TestParseReportsRetainedBytesAboveRawUploadSize(t *testing.T) {
	doc, err := har.Parse([]byte(sampleHAR))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if got, wantMoreThan := doc.RetainedBytes(), int64(len(sampleHAR)); got <= wantMoreThan {
		t.Fatalf("RetainedBytes() = %d, want more than raw upload size %d", got, wantMoreThan)
	}
}

func TestParseClassifiesBrowserResourceTypes(t *testing.T) {
	input := `{
	  "log": {
	    "entries": [
	      {
	        "startedDateTime": "2026-05-17T10:00:00.000Z",
	        "time": 1,
	        "request": { "method": "GET", "url": "https://example.com/", "headers": [{ "name": "accept", "value": "text/html" }] },
	        "response": { "status": 200, "content": { "mimeType": "text/html" } }
	      },
	      {
	        "startedDateTime": "2026-05-17T10:00:01.000Z",
	        "time": 1,
	        "request": { "method": "GET", "url": "https://example.com/app.css" },
	        "response": { "status": 200, "content": { "mimeType": "text/css" } }
	      },
	      {
	        "startedDateTime": "2026-05-17T10:00:02.000Z",
	        "time": 1,
	        "request": { "method": "GET", "url": "https://example.com/app.js" },
	        "response": { "status": 200, "content": { "mimeType": "application/javascript" } }
	      },
	      {
	        "startedDateTime": "2026-05-17T10:00:03.000Z",
	        "time": 1,
	        "request": { "method": "GET", "url": "https://example.com/logo.svg" },
	        "response": { "status": 200, "content": { "mimeType": "image/svg+xml" } }
	      },
	      {
	        "startedDateTime": "2026-05-17T10:00:04.000Z",
	        "time": 1,
	        "request": { "method": "GET", "url": "wss://example.com/socket" },
	        "response": { "status": 101, "content": { "mimeType": "" } }
	      }
	    ]
	  }
	}`

	doc, err := har.Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	var got []string
	for _, summary := range doc.Summaries {
		got = append(got, summary.ResourceType)
	}
	want := []string{"Doc", "CSS", "JS", "Img", "WS"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("resource types = %v, want %v", got, want)
	}
}

func TestResponseDetailReturnsResponseHeadersAndBody(t *testing.T) {
	doc, err := har.Parse([]byte(sampleHAR))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	detail, err := doc.ResponseDetail(0)
	if err != nil {
		t.Fatalf("ResponseDetail returned error: %v", err)
	}

	if detail.Index != 0 {
		t.Fatalf("index = %d, want 0", detail.Index)
	}
	if detail.Request.Method != "GET" {
		t.Fatalf("request method = %q, want GET", detail.Request.Method)
	}
	if detail.Request.URL != "https://example.com/api/users?active=true" {
		t.Fatalf("request url = %q", detail.Request.URL)
	}
	if detail.Response.Status != 200 {
		t.Fatalf("response status = %d, want 200", detail.Response.Status)
	}
	if detail.Response.Headers[0].Name != "content-type" {
		t.Fatalf("header name = %q, want content-type", detail.Response.Headers[0].Name)
	}
	if detail.Response.Content.Text != "{\"ok\":true}" {
		t.Fatalf("content text = %q", detail.Response.Content.Text)
	}
	if detail.Response.Content.MimeType != "application/json" {
		t.Fatalf("mime type = %q, want application/json", detail.Response.Content.MimeType)
	}

	postDetail, err := doc.ResponseDetail(1)
	if err != nil {
		t.Fatalf("ResponseDetail returned error for post entry: %v", err)
	}
	if postDetail.Request.PostData.MimeType != "application/json" {
		t.Fatalf("post mime type = %q, want application/json", postDetail.Request.PostData.MimeType)
	}
	if postDetail.Request.PostData.Text != "{\"orderId\":42}" {
		t.Fatalf("post body = %q, want order JSON", postDetail.Request.PostData.Text)
	}
}

func TestResponseDetailDecodesBase64ResponseText(t *testing.T) {
	input := `{
	  "log": {
	    "entries": [
	      {
	        "startedDateTime": "2026-05-17T10:00:00.000Z",
	        "time": 1,
	        "request": { "method": "GET", "url": "https://example.com/api/base64" },
	        "response": {
	          "status": 200,
	          "content": {
	            "mimeType": "application/json",
	            "encoding": "base64",
	            "text": "eyJvayI6dHJ1ZX0="
	          }
	        }
	      }
	    ]
	  }
	}`

	doc, err := har.Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	detail, err := doc.ResponseDetail(0)
	if err != nil {
		t.Fatalf("ResponseDetail returned error: %v", err)
	}
	if detail.Response.Content.Text != `{"ok":true}` {
		t.Fatalf("content text = %q, want decoded JSON", detail.Response.Content.Text)
	}
	if detail.Response.Content.Encoding != "" {
		t.Fatalf("encoding = %q, want empty after decode", detail.Response.Content.Encoding)
	}
}

func TestResponseDetailLimitsBase64DecodeToPreviewSize(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 200<<10+4096))) + "$$"
	input := `{
	  "log": {
	    "entries": [
	      {
	        "startedDateTime": "2026-05-17T10:00:00.000Z",
	        "time": 1,
	        "request": { "method": "GET", "url": "https://example.com/api/base64-large" },
	        "response": {
	          "status": 200,
	          "content": {
	            "mimeType": "text/plain",
	            "encoding": "base64",
	            "text": ` + quoteJSONString(encoded) + `
	          }
	        }
	      }
	    ]
	  }
	}`

	doc, err := har.Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	detail, err := doc.ResponseDetail(0)
	if err != nil {
		t.Fatalf("ResponseDetail returned error: %v", err)
	}
	if got, want := len(detail.Response.Content.Text), 200<<10; got != want {
		t.Fatalf("decoded preview length = %d, want %d", got, want)
	}
	if strings.Trim(detail.Response.Content.Text, "x") != "" {
		t.Fatalf("decoded preview contains unexpected bytes")
	}
	if detail.Response.Content.Encoding != "" {
		t.Fatalf("encoding = %q, want empty after preview decode", detail.Response.Content.Encoding)
	}
	if !detail.Response.Content.Truncated {
		t.Fatalf("truncated = false, want true")
	}
}

func TestResponseDetailRejectsInvalidIndex(t *testing.T) {
	doc, err := har.Parse([]byte(sampleHAR))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if _, err := doc.ResponseDetail(2); err == nil {
		t.Fatalf("ResponseDetail returned nil error for out-of-range index")
	}
}

func TestParseRejectsInvalidHAR(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "invalid json", input: `{`},
		{name: "missing log", input: `{ "notLog": {} }`},
		{name: "missing entries", input: `{ "log": { "version": "1.2" } }`},
		{name: "entries not array", input: `{ "log": { "entries": {} } }`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := har.Parse([]byte(tt.input)); err == nil {
				t.Fatalf("Parse returned nil error")
			}
		})
	}
}

func TestBuildSubsetRejectsInvalidIndexes(t *testing.T) {
	doc, err := har.Parse([]byte(sampleHAR))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	for _, indexes := range [][]int{{}, {-1}, {2}} {
		if _, err := doc.BuildSubset(indexes); err == nil {
			t.Fatalf("BuildSubset(%v) returned nil error", indexes)
		}
	}
}

func quoteJSONString(value string) string {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(raw)
}
