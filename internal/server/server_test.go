package server_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Hamachi-Multi/har-filter/internal/har"
	"github.com/Hamachi-Multi/har-filter/internal/server"
)

const apiSampleHAR = `{
  "log": {
    "version": "1.2",
    "creator": { "name": "Browser", "version": "125.0" },
    "entries": [
      {
        "startedDateTime": "2026-05-17T10:00:00.000Z",
        "time": 42.7,
        "request": {
          "method": "GET",
          "url": "https://example.com/api/users?active=true",
          "headers": [{ "name": "accept", "value": "application/json" }]
        },
        "response": {
          "status": 200,
          "statusText": "OK",
          "headers": [{ "name": "content-type", "value": "application/json" }],
          "content": { "mimeType": "application/json", "size": 73, "text": "{\"ok\":true}" }
        }
      },
      {
        "startedDateTime": "2026-05-17T10:00:01.000Z",
        "time": 318.4,
        "request": {
          "method": "POST",
          "url": "https://example.com/api/orders",
          "headers": [{ "name": "content-type", "value": "application/json" }],
          "postData": { "mimeType": "application/json", "text": "{\"orderId\":42}" }
        },
        "response": { "status": 201, "statusText": "Created", "content": { "mimeType": "application/json", "size": 129 } }
      }
    ]
  }
}`

func TestUploadParsesHARAndReturnsSummaries(t *testing.T) {
	app := server.New(server.Config{MaxUploadBytes: 1 << 20})
	response := uploadHAR(t, app, "capture.har", apiSampleHAR)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}

	var payload struct {
		ID         string `json:"id"`
		FileName   string `json:"fileName"`
		EntryCount int    `json:"entryCount"`
		Entries    []struct {
			Index  int    `json:"index"`
			Method string `json:"method"`
			URL    string `json:"url"`
			Status int    `json:"status"`
			Type   string `json:"resourceType"`
		} `json:"entries"`
	}
	decodeJSON(t, response.Body, &payload)

	if payload.ID == "" {
		t.Fatalf("id is empty")
	}
	if payload.FileName != "capture.har" {
		t.Fatalf("file name = %q, want capture.har", payload.FileName)
	}
	if payload.EntryCount != 2 || len(payload.Entries) != 2 {
		t.Fatalf("entry counts = %d/%d, want 2/2", payload.EntryCount, len(payload.Entries))
	}
	if payload.Entries[1].Method != "POST" {
		t.Fatalf("second method = %q, want POST", payload.Entries[1].Method)
	}
	if payload.Entries[1].Status != 201 {
		t.Fatalf("second status = %d, want 201", payload.Entries[1].Status)
	}
	if payload.Entries[0].Type != "Fetch/XHR" {
		t.Fatalf("first resource type = %q, want Fetch/XHR", payload.Entries[0].Type)
	}
}

func TestHARDataResponsesDisableBrowserStorageCaches(t *testing.T) {
	app := server.New(server.Config{MaxUploadBytes: 1 << 20})
	uploadResponse := uploadHAR(t, app, "capture.har", apiSampleHAR)
	assertNoStoreHeaders(t, uploadResponse)

	var uploadPayload struct {
		ID string `json:"id"`
	}
	decodeJSON(t, uploadResponse.Body, &uploadPayload)

	entryResponse := entryDetailWithHeaders(t, app, uploadPayload.ID, 0, nil)
	if entryResponse.Code != http.StatusOK {
		t.Fatalf("entry status = %d, body = %s", entryResponse.Code, entryResponse.Body.String())
	}
	assertNoStoreHeaders(t, entryResponse)

	exportResponse := exportHAR(t, app, uploadPayload.ID, []int{0})
	if exportResponse.Code != http.StatusOK {
		t.Fatalf("export status = %d, body = %s", exportResponse.Code, exportResponse.Body.String())
	}
	assertNoStoreHeaders(t, exportResponse)
}

func TestUploadRejectsInvalidHAR(t *testing.T) {
	app := server.New(server.Config{MaxUploadBytes: 1 << 20})
	response := uploadHAR(t, app, "bad.har", `{ "log": { "entries": {} } }`)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "error") {
		t.Fatalf("body does not contain error JSON: %s", response.Body.String())
	}
}

func TestUploadRejectsFileAboveConfiguredLimit(t *testing.T) {
	app := server.New(server.Config{MaxUploadBytes: 16})
	response := uploadHAR(t, app, "too-large.har", apiSampleHAR)

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body = %s", response.Code, response.Body.String())
	}
}

func TestUploadWithReplaceIDDeletesPreviousSessionAfterSuccessfulUpload(t *testing.T) {
	app := server.New(server.Config{MaxUploadBytes: 1 << 20})
	firstUpload := uploadHAR(t, app, "first.har", apiSampleHAR)
	var firstPayload struct {
		ID string `json:"id"`
	}
	decodeJSON(t, firstUpload.Body, &firstPayload)

	secondUpload := uploadHARReplacing(t, app, "second.har", apiSampleHAR, firstPayload.ID)
	if secondUpload.Code != http.StatusOK {
		t.Fatalf("second upload status = %d, body = %s", secondUpload.Code, secondUpload.Body.String())
	}
	var secondPayload struct {
		ID string `json:"id"`
	}
	decodeJSON(t, secondUpload.Body, &secondPayload)
	if secondPayload.ID == "" {
		t.Fatalf("second upload id is empty")
	}
	if secondPayload.ID == firstPayload.ID {
		t.Fatalf("second upload reused first session id")
	}

	oldExport := exportHAR(t, app, firstPayload.ID, []int{0})
	if oldExport.Code != http.StatusNotFound {
		t.Fatalf("old session export status = %d, want 404; body = %s", oldExport.Code, oldExport.Body.String())
	}

	newExport := exportHAR(t, app, secondPayload.ID, []int{0})
	if newExport.Code != http.StatusOK {
		t.Fatalf("new session export status = %d, body = %s", newExport.Code, newExport.Body.String())
	}
}

func TestExportAndEntryRejectExpiredSessions(t *testing.T) {
	app := server.New(server.Config{
		MaxUploadBytes: 1 << 20,
		SessionTTL:     time.Nanosecond,
	})
	uploadResponse := uploadHAR(t, app, "capture.har", apiSampleHAR)
	var uploadPayload struct {
		ID string `json:"id"`
	}
	decodeJSON(t, uploadResponse.Body, &uploadPayload)

	time.Sleep(time.Millisecond)

	exportResponse := exportHAR(t, app, uploadPayload.ID, []int{0})
	if exportResponse.Code != http.StatusNotFound {
		t.Fatalf("expired export status = %d, want 404; body = %s", exportResponse.Code, exportResponse.Body.String())
	}

	entryRequest := httptest.NewRequest(http.MethodPost, "/api/entry", strings.NewReader(`{"id":`+quote(uploadPayload.ID)+`,"index":0}`))
	entryRequest.Header.Set("Content-Type", "application/json")
	entryResponse := httptest.NewRecorder()
	app.ServeHTTP(entryResponse, entryRequest)
	if entryResponse.Code != http.StatusNotFound {
		t.Fatalf("expired entry status = %d, want 404; body = %s", entryResponse.Code, entryResponse.Body.String())
	}
}

func TestUploadEvictsOldestSessionWhenMemoryLimitIsExceeded(t *testing.T) {
	doc, err := har.Parse([]byte(apiSampleHAR))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	oneSessionLimit := doc.RetainedBytes() + 64
	app := server.New(server.Config{
		MaxUploadBytes:  1 << 20,
		MaxSessionBytes: oneSessionLimit,
	})

	firstUpload := uploadHAR(t, app, "first.har", apiSampleHAR)
	var firstPayload struct {
		ID string `json:"id"`
	}
	decodeJSON(t, firstUpload.Body, &firstPayload)

	secondUpload := uploadHAR(t, app, "second.har", apiSampleHAR)
	if secondUpload.Code != http.StatusOK {
		t.Fatalf("second upload status = %d, want 200; body = %s", secondUpload.Code, secondUpload.Body.String())
	}
	var secondPayload struct {
		ID string `json:"id"`
	}
	decodeJSON(t, secondUpload.Body, &secondPayload)

	oldExport := exportHAR(t, app, firstPayload.ID, []int{0})
	if oldExport.Code != http.StatusNotFound {
		t.Fatalf("old session export status = %d, want 404; body = %s", oldExport.Code, oldExport.Body.String())
	}
	newExport := exportHAR(t, app, secondPayload.ID, []int{0})
	if newExport.Code != http.StatusOK {
		t.Fatalf("new session export status = %d, want 200; body = %s", newExport.Code, newExport.Body.String())
	}
}

func TestUploadRejectsHARAboveRetainedSessionLimit(t *testing.T) {
	app := server.New(server.Config{
		MaxUploadBytes:  1 << 20,
		MaxSessionBytes: int64(len(apiSampleHAR)),
	})

	response := uploadHAR(t, app, "capture.har", apiSampleHAR)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body = %s", response.Code, response.Body.String())
	}
}

func TestExportReturnsSubsetHAR(t *testing.T) {
	app := server.New(server.Config{MaxUploadBytes: 1 << 20})
	uploadResponse := uploadHAR(t, app, "capture.har", apiSampleHAR)

	var uploadPayload struct {
		ID string `json:"id"`
	}
	decodeJSON(t, uploadResponse.Body, &uploadPayload)

	body := strings.NewReader(`{"id":` + quote(uploadPayload.ID) + `,"indexes":[1]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/export", body)
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	app.ServeHTTP(response, req)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("content type = %q, want application/json", contentType)
	}
	if disposition := response.Header().Get("Content-Disposition"); !strings.Contains(disposition, "capture-filtered.har") {
		t.Fatalf("content disposition = %q", disposition)
	}

	if strings.Contains(response.Body.String(), "/api/users") {
		t.Fatalf("export contains unselected request: %s", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "/api/orders") {
		t.Fatalf("export missing selected request: %s", response.Body.String())
	}
}

func TestExportAndEntryRejectTrailingJSON(t *testing.T) {
	app := server.New(server.Config{MaxUploadBytes: 1 << 20})
	uploadResponse := uploadHAR(t, app, "capture.har", apiSampleHAR)
	var uploadPayload struct {
		ID string `json:"id"`
	}
	decodeJSON(t, uploadResponse.Body, &uploadPayload)

	exportBody := strings.NewReader(`{"id":` + quote(uploadPayload.ID) + `,"indexes":[0]} {"id":"extra"}`)
	exportRequest := httptest.NewRequest(http.MethodPost, "/api/export", exportBody)
	exportRequest.Header.Set("Content-Type", "application/json")
	exportResponse := httptest.NewRecorder()
	app.ServeHTTP(exportResponse, exportRequest)
	if exportResponse.Code != http.StatusBadRequest {
		t.Fatalf("export status = %d, want 400; body = %s", exportResponse.Code, exportResponse.Body.String())
	}

	entryBody := strings.NewReader(`{"id":` + quote(uploadPayload.ID) + `,"index":0} {"id":"extra"}`)
	entryRequest := httptest.NewRequest(http.MethodPost, "/api/entry", entryBody)
	entryRequest.Header.Set("Content-Type", "application/json")
	entryResponse := httptest.NewRecorder()
	app.ServeHTTP(entryResponse, entryRequest)
	if entryResponse.Code != http.StatusBadRequest {
		t.Fatalf("entry status = %d, want 400; body = %s", entryResponse.Code, entryResponse.Body.String())
	}
}

func TestAPIRejectsCrossOriginBrowserRequests(t *testing.T) {
	app := server.New(server.Config{MaxUploadBytes: 1 << 20})
	uploadResponse := uploadHAR(t, app, "capture.har", apiSampleHAR)
	var uploadPayload struct {
		ID string `json:"id"`
	}
	decodeJSON(t, uploadResponse.Body, &uploadPayload)

	crossOriginUpload := uploadHARWithHeaders(t, app, "cross.har", apiSampleHAR, map[string]string{
		"Origin": "http://evil.example",
	})
	if crossOriginUpload.Code != http.StatusForbidden {
		t.Fatalf("cross-origin upload status = %d, want 403; body = %s", crossOriginUpload.Code, crossOriginUpload.Body.String())
	}

	crossOriginExport := exportHARWithHeaders(t, app, uploadPayload.ID, []int{0}, map[string]string{
		"Origin": "http://evil.example",
	})
	if crossOriginExport.Code != http.StatusForbidden {
		t.Fatalf("cross-origin export status = %d, want 403; body = %s", crossOriginExport.Code, crossOriginExport.Body.String())
	}

	crossRefererEntry := entryDetailWithHeaders(t, app, uploadPayload.ID, 0, map[string]string{
		"Referer": "http://evil.example/page",
	})
	if crossRefererEntry.Code != http.StatusForbidden {
		t.Fatalf("cross-referer entry status = %d, want 403; body = %s", crossRefererEntry.Code, crossRefererEntry.Body.String())
	}
}

func TestAPIAllowsSameOriginAndNonBrowserRequests(t *testing.T) {
	app := server.New(server.Config{MaxUploadBytes: 1 << 20})

	sameOriginUpload := uploadHARWithHeaders(t, app, "same-origin.har", apiSampleHAR, map[string]string{
		"Origin": "http://example.com",
	})
	if sameOriginUpload.Code != http.StatusOK {
		t.Fatalf("same-origin upload status = %d, want 200; body = %s", sameOriginUpload.Code, sameOriginUpload.Body.String())
	}
	var payload struct {
		ID string `json:"id"`
	}
	decodeJSON(t, sameOriginUpload.Body, &payload)

	sameOriginExport := exportHARWithHeaders(t, app, payload.ID, []int{0}, map[string]string{
		"Origin": "http://example.com",
	})
	if sameOriginExport.Code != http.StatusOK {
		t.Fatalf("same-origin export status = %d, want 200; body = %s", sameOriginExport.Code, sameOriginExport.Body.String())
	}

	nonBrowserEntry := entryDetailWithHeaders(t, app, payload.ID, 0, nil)
	if nonBrowserEntry.Code != http.StatusOK {
		t.Fatalf("non-browser entry status = %d, want 200; body = %s", nonBrowserEntry.Code, nonBrowserEntry.Body.String())
	}
}

func TestEntryDetailReturnsResponseContent(t *testing.T) {
	app := server.New(server.Config{MaxUploadBytes: 1 << 20})
	uploadResponse := uploadHAR(t, app, "capture.har", apiSampleHAR)

	var uploadPayload struct {
		ID string `json:"id"`
	}
	decodeJSON(t, uploadResponse.Body, &uploadPayload)

	body := strings.NewReader(`{"id":` + quote(uploadPayload.ID) + `,"index":0}`)
	req := httptest.NewRequest(http.MethodPost, "/api/entry", body)
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	app.ServeHTTP(response, req)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"response"`) {
		t.Fatalf("entry detail does not use lower-case response key: %s", response.Body.String())
	}
	if strings.Contains(response.Body.String(), `"Response"`) {
		t.Fatalf("entry detail contains upper-case Response key: %s", response.Body.String())
	}

	var payload struct {
		Index   int `json:"index"`
		Request struct {
			Method  string `json:"method"`
			URL     string `json:"url"`
			Headers []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"headers"`
			PostData struct {
				MimeType string `json:"mimeType"`
				Text     string `json:"text"`
			} `json:"postData"`
		} `json:"request"`
		Response struct {
			Status  int `json:"status"`
			Headers []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"headers"`
			Content struct {
				MimeType string `json:"mimeType"`
				Text     string `json:"text"`
			} `json:"content"`
		} `json:"response"`
	}
	decodeJSON(t, response.Body, &payload)
	if payload.Index != 0 {
		t.Fatalf("index = %d, want 0", payload.Index)
	}
	if payload.Request.Method != "GET" {
		t.Fatalf("request method = %q, want GET", payload.Request.Method)
	}
	if payload.Request.Headers[0].Name != "accept" {
		t.Fatalf("request header name = %q, want accept", payload.Request.Headers[0].Name)
	}
	if payload.Response.Status != 200 {
		t.Fatalf("status = %d, want 200", payload.Response.Status)
	}
	if payload.Response.Headers[0].Name != "content-type" {
		t.Fatalf("header name = %q, want content-type", payload.Response.Headers[0].Name)
	}
	if payload.Response.Content.Text != "{\"ok\":true}" {
		t.Fatalf("content text = %q", payload.Response.Content.Text)
	}

	postBody := strings.NewReader(`{"id":` + quote(uploadPayload.ID) + `,"index":1}`)
	postReq := httptest.NewRequest(http.MethodPost, "/api/entry", postBody)
	postReq.Header.Set("Content-Type", "application/json")
	postResponse := httptest.NewRecorder()
	app.ServeHTTP(postResponse, postReq)
	if postResponse.Code != http.StatusOK {
		t.Fatalf("post detail status = %d, body = %s", postResponse.Code, postResponse.Body.String())
	}
	var postPayload struct {
		Request struct {
			PostData struct {
				MimeType string `json:"mimeType"`
				Text     string `json:"text"`
			} `json:"postData"`
		} `json:"request"`
	}
	decodeJSON(t, postResponse.Body, &postPayload)
	if postPayload.Request.PostData.Text != "{\"orderId\":42}" {
		t.Fatalf("post request body = %q, want order JSON", postPayload.Request.PostData.Text)
	}
}

func TestEntryDetailRejectsMissingSessionAndInvalidIndex(t *testing.T) {
	app := server.New(server.Config{MaxUploadBytes: 1 << 20})

	missingSession := httptest.NewRequest(http.MethodPost, "/api/entry", strings.NewReader(`{"id":"missing","index":0}`))
	missingSession.Header.Set("Content-Type", "application/json")
	missingResponse := httptest.NewRecorder()
	app.ServeHTTP(missingResponse, missingSession)
	if missingResponse.Code != http.StatusNotFound {
		t.Fatalf("missing session status = %d, want 404", missingResponse.Code)
	}

	uploadResponse := uploadHAR(t, app, "capture.har", apiSampleHAR)
	var uploadPayload struct {
		ID string `json:"id"`
	}
	decodeJSON(t, uploadResponse.Body, &uploadPayload)

	invalidIndex := httptest.NewRequest(http.MethodPost, "/api/entry", strings.NewReader(`{"id":`+quote(uploadPayload.ID)+`,"index":9}`))
	invalidIndex.Header.Set("Content-Type", "application/json")
	invalidResponse := httptest.NewRecorder()
	app.ServeHTTP(invalidResponse, invalidIndex)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid index status = %d, want 400; body = %s", invalidResponse.Code, invalidResponse.Body.String())
	}
}

func TestExportRejectsMissingSessionAndInvalidIndexes(t *testing.T) {
	app := server.New(server.Config{MaxUploadBytes: 1 << 20})

	missingSession := httptest.NewRequest(http.MethodPost, "/api/export", strings.NewReader(`{"id":"missing","indexes":[0]}`))
	missingSession.Header.Set("Content-Type", "application/json")
	missingResponse := httptest.NewRecorder()
	app.ServeHTTP(missingResponse, missingSession)
	if missingResponse.Code != http.StatusNotFound {
		t.Fatalf("missing session status = %d, want 404", missingResponse.Code)
	}

	uploadResponse := uploadHAR(t, app, "capture.har", apiSampleHAR)
	var uploadPayload struct {
		ID string `json:"id"`
	}
	decodeJSON(t, uploadResponse.Body, &uploadPayload)

	invalidIndex := httptest.NewRequest(http.MethodPost, "/api/export", strings.NewReader(`{"id":`+quote(uploadPayload.ID)+`,"indexes":[7]}`))
	invalidIndex.Header.Set("Content-Type", "application/json")
	invalidResponse := httptest.NewRecorder()
	app.ServeHTTP(invalidResponse, invalidIndex)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid index status = %d, want 400; body = %s", invalidResponse.Code, invalidResponse.Body.String())
	}
}

func TestIndexUsesSingleFilePickerClickTarget(t *testing.T) {
	app := server.New(server.Config{MaxUploadBytes: 1 << 20})
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}

	body := response.Body.String()
	for _, expected := range []string{
		`<input id="harFile" class="visually-hidden" name="har" type="file" accept=".har,application/json" aria-describedby="selectedFileMeta">`,
		`id="statusMessage" class="upload-field file-picker idle"`,
		`for="harFile"`,
		`class="upload-field-label"`,
		`id="uploadActionText" class="upload-field-action" role="status" aria-live="polite"`,
		`id="uploadErrorText" class="upload-field-error" role="status" aria-live="polite" hidden`,
		`<strong id="fileName">No file selected</strong>`,
		`id="selectedFileMeta" class="upload-field-meta"`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("index missing merged upload status control %q", expected)
		}
	}
	for _, unexpected := range []string{
		`id="uploadForm"`,
		`class="upload-form"`,
		`class="file-row file-picker"`,
		`class="file-target"`,
		`class="file-label"`,
		`class="file-target-action"`,
		`class="summary-line file-summary`,
		`class="upload-card`,
		`class="upload-card-mark"`,
		`id="selectedFileName"`,
		`id="uploadStatusText"`,
		`class="upload-field-status"`,
		`id="uploadHelp"`,
		`aria-describedby="uploadHelp`,
		`role="status" aria-live="polite">
              <span class="upload-field-label">HAR file</span>`,
	} {
		if strings.Contains(body, unexpected) {
			t.Fatalf("index still contains old upload control %q", unexpected)
		}
	}
}

func TestIndexIncludesThemeToggleControls(t *testing.T) {
	app := server.New(server.Config{MaxUploadBytes: 1 << 20})
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}

	body := response.Body.String()
	for _, expected := range []string{
		`window.localStorage.getItem("har-filter.theme.v1")`,
		`document.documentElement.dataset.theme = theme`,
		`class="theme-control"`,
		`id="themeLightButton"`,
		`id="themeDarkButton"`,
		`aria-label="Theme"`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("index missing theme control %q", expected)
		}
	}
}

func TestStylesDoesNotForceEmptyPageScrollbar(t *testing.T) {
	app := server.New(server.Config{MaxUploadBytes: 1 << 20})
	request := httptest.NewRequest(http.MethodGet, "/styles.css", nil)
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}

	body := response.Body.String()
	if strings.Contains(body, "scrollbar-gutter") {
		t.Fatalf("styles still reserve scrollbar gutter: %s", body)
	}
	if strings.Contains(body, "overflow-y: scroll") {
		t.Fatalf("styles still force an empty page scrollbar: %s", body)
	}
	if !strings.Contains(body, "overflow-y: auto") {
		t.Fatalf("styles do not use automatic page scrollbar behavior: %s", body)
	}
}

func TestStylesIncludeReviewDrivenInterfaceRefinements(t *testing.T) {
	app := server.New(server.Config{MaxUploadBytes: 1 << 20})
	request := httptest.NewRequest(http.MethodGet, "/styles.css", nil)
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}

	body := response.Body.String()
	for _, expected := range []string{
		"position: sticky",
		".upload-field.ready .upload-field-action",
		".method::before",
		`--status-success: #08715f`,
		`--status-error: #991b1b`,
		`.request-row:not([data-status-code="200"]) .select-cell::after`,
		`--red-rail: rgba(180, 35, 24, 0.62)`,
		`background: var(--red-rail)`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("styles missing %q", expected)
		}
	}
}

func TestStylesIncludeDarkThemeTokens(t *testing.T) {
	app := server.New(server.Config{MaxUploadBytes: 1 << 20})
	request := httptest.NewRequest(http.MethodGet, "/styles.css", nil)
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}

	body := response.Body.String()
	for _, expected := range []string{
		`[data-theme="dark"]`,
		`color-scheme: dark`,
		`--background: #101214`,
		`--surface: #14171b`,
		`--text: #e7e9ec`,
		`.theme-control`,
		`.theme-control button[aria-pressed="true"]`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("styles missing dark theme rule %q", expected)
		}
	}
}

func TestAppMarksRowsWithStatusGroups(t *testing.T) {
	app := server.New(server.Config{MaxUploadBytes: 1 << 20})
	request := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}

	body := frontendScriptBundle(t, app)
	for _, expected := range []string{
		"row.dataset.statusGroup = statusGroup(entry.status)",
		`row.dataset.statusCode = String(entry.status || "")`,
		"function statusGroup(status)",
		"function statusTone(message)",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("app.js missing %q", expected)
		}
	}
}

func TestIndexIncludesRequestTypeFilterAndDetailPanels(t *testing.T) {
	app := server.New(server.Config{MaxUploadBytes: 1 << 20})
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}

	body := response.Body.String()
	for _, expected := range []string{
		`class="request-column"`,
		`id="resourceTypeFilters"`,
		`id="prevPageButton"`,
		`id="nextPageButton"`,
		`id="selectAllVisibleCheckbox"`,
		`id="requestPanel" class="detail-panel request-panel detail-column"`,
		`id="requestScroll" class="detail-scroll"`,
		`<span>Method</span>`,
		`<span>URL</span>`,
		`<span>Status</span>`,
		`<strong id="requestMeta"></strong>`,
		`<strong id="requestMethod"></strong>`,
		`<strong id="requestStatus"></strong>`,
		`id="requestBodyOpenButton"`,
		`id="requestBodyCopyButton"`,
		`id="requestBody"`,
		`id="responsePanel" class="detail-panel response-panel detail-column"`,
		`id="responseScroll" class="detail-scroll"`,
		`<strong id="responseMeta"></strong>`,
		`<strong id="responseMethod"></strong>`,
		`<strong id="responseStatus"></strong>`,
		`id="responseBodyOpenButton"`,
		`id="responseBodyCopyButton"`,
		`id="responseBody"`,
		`id="bodyDialog"`,
		`id="bodyDialogPrettyButton"`,
		`id="bodyDialogRawButton"`,
		`id="bodyDialogCopyPrettyButton"`,
		`id="bodyDialogCopyRawButton"`,
		`<button id="bodyDialogPrettyButton" type="button" class="active">Pretty</button>
            <button id="bodyDialogRawButton" type="button">Raw</button>`,
		`class="body-dialog-copy-actions" role="group" aria-label="Copy body"`,
		`<button id="bodyDialogCopyPrettyButton" type="button">Copy Pretty</button>
            <button id="bodyDialogCopyRawButton" type="button">Copy Raw</button>`,
		`id="bodyDialogContent"`,
		`id="entryDialog"`,
		`id="entryDialogRequest"`,
		`id="entryDialogResponse"`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("index missing %q", expected)
		}
	}
	if strings.Contains(body, `id="closeResponseButton"`) {
		t.Fatalf("index still contains response close button")
	}
}

func TestIndexUsesHeaderCheckboxInsteadOfToolbarSelectionButtons(t *testing.T) {
	app := server.New(server.Config{MaxUploadBytes: 1 << 20})
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}

	body := response.Body.String()
	for _, expected := range []string{
		`<th class="select-cell">`,
		`id="selectAllVisibleCheckbox"`,
		`aria-label="Select or clear visible requests"`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("index missing header checkbox rule %q", expected)
		}
	}
	for _, unexpected := range []string{
		`id="selectVisibleButton"`,
		`id="clearButton"`,
		`class="toolbar-actions"`,
		`Select visible`,
		`Clear selection`,
	} {
		if strings.Contains(body, unexpected) {
			t.Fatalf("index still contains toolbar selection control %q", unexpected)
		}
	}
}

func TestIndexAllowsPreUploadViewSettings(t *testing.T) {
	app := server.New(server.Config{MaxUploadBytes: 1 << 20})
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}

	body := response.Body.String()
	for _, expected := range []string{
		`class="page-size-control"`,
		`<span>Rows</span>`,
		`id="pageSizeSelect"`,
		`<option value="25" selected>25</option>`,
		`<option value="100">100</option>`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("index missing pre-upload setting %q", expected)
		}
	}
	for _, unexpected := range []string{
		`<select id="pageSizeSelect" disabled>`,
		`<input id="filterInput" type="search" placeholder="Search method, status, host, or path" disabled>`,
	} {
		if strings.Contains(body, unexpected) {
			t.Fatalf("index still disables pre-upload setting %q", unexpected)
		}
	}
}

func TestStylesUseThreeColumnRequestAndDetailLayout(t *testing.T) {
	app := server.New(server.Config{MaxUploadBytes: 1 << 20})
	request := httptest.NewRequest(http.MethodGet, "/styles.css", nil)
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}

	body := response.Body.String()
	for _, expected := range []string{
		"height: 100dvh",
		"overflow-y: auto",
		"grid-template-rows: auto minmax(0, 1fr)",
		"grid-template-columns: minmax(0, 1.7fr) minmax(260px, 0.48fr) minmax(300px, 0.52fr)",
		"grid-auto-rows: minmax(0, 1fr)",
		"align-items: stretch",
		"grid-template-rows: auto minmax(0, 1fr)",
		"grid-template-rows: auto minmax(0, 1fr) auto",
		"container-type: inline-size",
		"grid-template-columns: minmax(300px, clamp(420px, 48cqw, 560px)) minmax(0, 1fr) auto auto auto",
		".summary-panel .summary-line:first-of-type",
		"grid-template-columns: auto minmax(220px, clamp(320px, 44cqw, 560px)) minmax(0, 1fr) auto",
		`"filter-label filter-input . page-size"`,
		`"type-label type-filters type-filters type-filters"`,
		"table-layout: fixed",
		"min-width: 760px",
		"width: clamp(104px, 10cqw, 140px)",
		"grid-template-columns: minmax(76px, clamp(86px, 30cqw, 118px)) minmax(0, 1fr)",
		"scrollbar-width: none",
		"margin-top: -1px",
		"display: block",
		"height: auto",
		"min-height: 420px",
		".request-column",
		".request-panel",
		".response-panel",
		".detail-column",
		"height: 100%",
		"position: sticky",
		"#exportButton:disabled",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("styles missing %q", expected)
		}
	}
}

func TestStylesLockViewportHeightWithoutForcingPageScrollbar(t *testing.T) {
	app := server.New(server.Config{MaxUploadBytes: 1 << 20})
	request := httptest.NewRequest(http.MethodGet, "/styles.css", nil)
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}

	body := response.Body.String()
	for _, expected := range []string{
		"height: 100dvh",
		"overflow-y: auto",
		"height: 100%",
		"min-height: 0",
		"overflow: auto",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("styles missing %q", expected)
		}
	}
}

func TestStylesUseCompactTopAlignedHeader(t *testing.T) {
	app := server.New(server.Config{MaxUploadBytes: 1 << 20})
	request := httptest.NewRequest(http.MethodGet, "/styles.css", nil)
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}

	body := response.Body.String()
	for _, expected := range []string{
		"padding: 0 0 20px",
		"padding: 10px 0 12px",
		"padding-top: 12px",
		"font-size: 22px",
		"grid-template-columns: minmax(260px, 1fr)",
		".upload-field.file-picker",
		".upload-field-action",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("styles missing compact header rule %q", expected)
		}
	}
}

func TestStylesCenterPaginationControls(t *testing.T) {
	app := server.New(server.Config{MaxUploadBytes: 1 << 20})
	request := httptest.NewRequest(http.MethodGet, "/styles.css", nil)
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}

	body := response.Body.String()
	for _, expected := range []string{
		"grid-template-columns: minmax(120px, 1fr) auto minmax(120px, 1fr)",
		"grid-column: 2",
		"justify-self: center",
		"grid-column: auto",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("styles missing centered pagination rule %q", expected)
		}
	}
}

func TestStylesKeepTypeFiltersOnOneLine(t *testing.T) {
	app := server.New(server.Config{MaxUploadBytes: 1 << 20})
	request := httptest.NewRequest(http.MethodGet, "/styles.css", nil)
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}

	body := response.Body.String()
	for _, expected := range []string{
		"flex-wrap: nowrap",
		"overflow-x: auto",
		"scrollbar-width: thin",
		".type-filter-group::-webkit-scrollbar",
		"height: 8px",
		"flex-wrap: wrap",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("styles missing single-line type filter rule %q", expected)
		}
	}
}

func TestStylesHideInternalScrollbarsAndShowScrollFades(t *testing.T) {
	app := server.New(server.Config{MaxUploadBytes: 1 << 20})
	request := httptest.NewRequest(http.MethodGet, "/styles.css", nil)
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}

	body := response.Body.String()
	for _, expected := range []string{
		".scroll-fade-target",
		".scroll-fade-target.can-scroll-up",
		".scroll-fade-target.can-scroll-down",
		".scroll-fade-target.can-scroll-up.can-scroll-down",
		"body:has(dialog[open])",
		"overflow-y: hidden",
		"rgba(31, 33, 31, 0.24)",
		"rgba(31, 33, 31, 0.3)",
		"scrollbar-width: none",
		"::-webkit-scrollbar",
		"display: none",
		".table-wrap::-webkit-scrollbar",
		".detail-scroll",
		"scroll-padding-bottom: 12px",
		"padding-bottom: 12px",
		".detail-summary",
		".detail-body-section",
		".detail-body-toolbar",
		"border-top: 1px solid var(--border)",
		".page-size-control",
		".body-dialog",
		"width: min(1280px, calc(100vw - 48px))",
		"height: calc(100dvh - 96px)",
		".body-dialog-shell",
		"height: 100%",
		".body-dialog::-webkit-scrollbar",
		".body-dialog-actions",
		".body-dialog-actions button.active",
		".copy-success",
		".body-dialog-content",
		"min-height: 0",
		"max-height: none",
		"max-height: calc(80dvh - 77px)",
		"user-select: text",
		".entry-dialog::-webkit-scrollbar",
		"height: calc(100dvh - 48px)",
		"tbody tr.selected-row .select-cell::before",
		"top: -1px",
		"bottom: -1px",
		"left: 0",
		"background: var(--primary)",
		"border-bottom-color: var(--primary-soft)",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("styles missing %q", expected)
		}
	}
}

func TestAppIncludesDragSelectionAndEntryDetailLogic(t *testing.T) {
	app := server.New(server.Config{MaxUploadBytes: 1 << 20})
	request := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}

	body := frontendScriptBundle(t, app)
	for _, expected := range []string{
		"dragSelection",
		"applyDragSelection",
		"loadEntryDetail",
		"closeEntryDetail",
		"entriesBody.addEventListener(\"dblclick\"",
		"entriesBody.addEventListener(\"keydown\"",
		"activateRowClickSelection",
		"setRowSelected",
		"row.dataset.focusedBeforePointer",
		"const wasFocusedBeforePointer = row.dataset.focusedBeforePointer === \"true\"",
		"delete row.dataset.focusedBeforePointer",
		"document.activeElement === row",
		"row.contains(document.activeElement)",
		"closeEntryDetail();",
		"row.blur()",
		"row.focus({ preventScroll: true })",
		"state.activeEntryIndex !== index",
		"detailLoader(index, restoreFocus)",
		"open-entry-button",
		"event.detail > 1",
		"ENTRY_DIALOG_DOUBLE_CLICK_MS",
		"entryClickTiming",
		"rememberEntryClick",
		"isRecentEntryClick",
		"shouldIgnoreEntryDoubleClick",
		"entryClickTiming.suppressDoubleClickUntil = event.timeStamp + 100",
		"openEntryDialog",
		"renderEntryDialog",
		"entryDialogBodyElement(requestBody, \"entryRequest\")",
		"entryDialogBodyElement(responseBody, \"entryResponse\")",
		"entry-dialog-body-header",
		"entry-dialog-body-actions",
		"pre.tabIndex = 0",
		"pre.focus({ preventScroll: true })",
		"selectNodeContents(pre)",
		"bindNestedScrollChain(pre)",
		"function bindNestedScrollChain(pre)",
		"parent.scrollTop += normalizedWheelDeltaY(event)",
		"function normalizedWheelDeltaY(event)",
		"function isWheelPointerOutside(el, event)",
		"setEntryDialogLoading",
		"entryDialog.showModal",
		"restoreDialogFocus",
		"detailRequestId",
		"state.activeEntryIndex !== index",
		"/api/entry",
		"renderEntryDetail",
		"renderRequestDetail",
		"renderRequestDetail(request, response, index)",
		"requestMethod.textContent = request.method || \"-\"",
		"requestStatus.textContent = statusLabel(response)",
		"responseMethod.textContent = request.method || \"-\"",
		"responseStatus.textContent = statusLabel(response)",
		"responseMeta.textContent = request.url || \"Response URL unavailable\"",
		"function statusLabel(response)",
		"updateActiveRowClasses",
		"state.activeEntryIndex = index;\n  updateActiveRowClasses();",
		"state.activeEntryIndex = null;\n  clearDetailPanels();\n  updateActiveRowClasses();",
		"setBodyPreview",
		"copyBodyText",
		"copyTextToClipboard",
		"showCopySuccess",
		"setTimeout",
		"copy-success",
		"openBodyDialog",
		"renderBodyDialog",
		"bodyDialogContent.addEventListener(\"pointerdown\"",
		"bodyDialogContent.addEventListener(\"keydown\"",
		"selectBodyDialogContent",
		"selectNodeContents(dom.bodyDialogContent)",
		"isSelectAllShortcut",
		"ensurePrettyBodyPreview",
		"bodyPrettyCache",
		"bodyPreviewCacheKey",
		"canPrettyFormatBody",
		"formatBodyForDisplay",
		"formatPrettyBody",
		"isJavaScriptMimeType",
		"formatJavaScript",
		"state.bodyDialogMode",
		"bodyDialog.showModal",
		"handleFileDrop",
		"document.addEventListener(\"dragover\"",
		"document.addEventListener(\"drop\"",
		"renderResourceTypeFilters",
		"resourceTypeLabel",
		"paginatedEntries",
		"renderPagination",
		"uploadActionText",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("app.js missing %q", expected)
		}
	}
	for _, unexpected := range []string{
		"window.open",
		"popup.document",
		"writeEntryPopupShell",
		"state.selected.has(index) && state.activeEntryIndex === index && document.activeElement === row",
		"const shouldSelect = !state.selected.has(index)",
		"setRowSelected(row, index, shouldSelect)",
		"setRowSelected(row, index, true)",
		"updateRowSelectionState(row, index, true)",
		"const formatted = formatBodyForDisplay(text, source.mimeType || \"\", truncated)",
		"const formatted = hasBody\n    ? formatBodyForDisplay(rawText, options.mimeType || \"\", options.truncatedMessage || \"\")",
	} {
		if strings.Contains(body, unexpected) {
			t.Fatalf("app.js still contains popup-window code %q", unexpected)
		}
	}
}

func TestAppMarkupFormatterKeepsVoidTagsFromIncreasingIndent(t *testing.T) {
	app := server.New(server.Config{MaxUploadBytes: 1 << 20})
	request := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}

	body := frontendScriptBundle(t, app)
	for _, expected := range []string{
		"const VOID_MARKUP_TAGS",
		"function isSelfClosingMarkupLine",
		"VOID_MARKUP_TAGS.has",
		"trimmed.endsWith(\"/>\")",
		"!isSelfClosingMarkupLine(trimmed)",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("app.js markup formatter missing %q", expected)
		}
	}
}

func TestAppUsesHeaderCheckboxForSelectAllAndClearAll(t *testing.T) {
	app := server.New(server.Config{MaxUploadBytes: 1 << 20})
	request := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}

	body := frontendScriptBundle(t, app)
	for _, expected := range []string{
		`selectAllVisibleCheckbox: document.querySelector("#selectAllVisibleCheckbox")`,
		"selectAllVisibleCheckbox.addEventListener(\"change\"",
		"updateVisibleRowSelections(visible, isSelected)",
		"renderControls(filtered)",
		"selectAllVisibleCheckbox.indeterminate",
		"state.selected.delete(entry.index)",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("app.js missing header checkbox behavior %q", expected)
		}
	}
	for _, unexpected := range []string{
		"selectVisibleButton",
		"clearButton",
		"state.selected.clear()",
	} {
		if strings.Contains(body, unexpected) {
			t.Fatalf("app.js contains obsolete selection behavior %q", unexpected)
		}
	}
}

func TestAppKeepsRowSelectionAndActiveDetailSemanticsSeparate(t *testing.T) {
	app := server.New(server.Config{MaxUploadBytes: 1 << 20})
	request := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}

	body := frontendScriptBundle(t, app)
	for _, expected := range []string{
		"function updateRowSelectionState",
		"updateRowSelectionState(checkbox.closest(\"tr\"), index, checkbox.checked)",
		"updateRowSelectionState(row, index, isSelected)",
		"updateRowSelectionState(checkbox.closest(\"tr\"), index, dragSelection.targetChecked)",
		"row.setAttribute(\"aria-current\", \"true\")",
		"row.removeAttribute(\"aria-current\")",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("app.js missing row selection/accessibility behavior %q", expected)
		}
	}
	if strings.Contains(body, "aria-selected") {
		t.Fatalf("app.js should not use aria-selected for row checkbox or active-detail state")
	}
}

func TestAppCopySuccessRestoresOriginalButtonLabelAfterRepeatedClicks(t *testing.T) {
	app := server.New(server.Config{MaxUploadBytes: 1 << 20})
	request := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}

	body := frontendScriptBundle(t, app)
	for _, expected := range []string{
		"const copySuccessTimers = new WeakMap()",
		"button.dataset.copyLabel || button.textContent",
		"window.clearTimeout(pendingTimer)",
		"delete button.dataset.copyLabel",
		"copySuccessTimers.delete(button)",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("app.js missing copy success behavior %q", expected)
		}
	}
}

func TestAppIncludesScrollFadeBinding(t *testing.T) {
	app := server.New(server.Config{MaxUploadBytes: 1 << 20})
	request := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}

	body := frontendScriptBundle(t, app)
	for _, expected := range []string{
		"const scrollFadeSelector",
		`".table-wrap"`,
		`".detail-scroll"`,
		`".entry-dialog-scroll"`,
		`".entry-dialog pre"`,
		"function updateScrollFade(el)",
		"function refreshScrollFades(root = document)",
		"el.classList.toggle(\"can-scroll-up\"",
		"el.classList.toggle(\"can-scroll-down\"",
		"el.dataset.scrollFadeBound",
		"addEventListener(\"scroll\"",
		"addEventListener(\"resize\"",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("app.js missing %q", expected)
		}
	}
	if strings.Contains(body, `".workspace"`) {
		t.Fatalf("app.js should not attach scroll fade to workspace")
	}
}

func TestAppPersistsViewSettingsInLocalStorage(t *testing.T) {
	app := server.New(server.Config{MaxUploadBytes: 1 << 20})
	request := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}

	body := frontendScriptBundle(t, app)
	for _, expected := range []string{
		`const SETTINGS_KEY = "har-filter.settings.v1"`,
		"loadSettings()",
		"saveSettings(state)",
		"window.localStorage.getItem(SETTINGS_KEY)",
		"window.localStorage.setItem(SETTINGS_KEY",
		"resourceTypeFilter: savedSettings.resourceTypeFilter",
		"pageSize: savedSettings.pageSize",
		"pageSize: state.pageSize",
		`filter: ""`,
		"PAGE_SIZE_VALUES",
		"pageSizeSelect",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("app.js missing %q", expected)
		}
	}
	for _, unexpected := range []string{
		"filter: savedSettings.filter",
		"filter: state.filter",
	} {
		if strings.Contains(body, unexpected) {
			t.Fatalf("app.js still persists filter setting %q", unexpected)
		}
	}
}

func TestAppShowsAllTypeFiltersAndEnablesPreUploadSettings(t *testing.T) {
	app := server.New(server.Config{MaxUploadBytes: 1 << 20})
	request := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}

	body := frontendScriptBundle(t, app)
	for _, expected := range []string{
		`const DEFAULT_RESOURCE_TYPES = ["Fetch/XHR", "Doc", "CSS", "JS", "Img", "Media", "Font", "WS", "Wasm", "Other"]`,
		"return DEFAULT_RESOURCE_TYPES.concat(Array.from(present).sort())",
		"const disabled = state.busy;",
		"filterInput.disabled = isBusy;",
		"saveSettings(state);\n  renderRows();",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("app.js missing %q", expected)
		}
	}
}

func TestStylesIncludeDragAndDropFilePickerState(t *testing.T) {
	app := server.New(server.Config{MaxUploadBytes: 1 << 20})
	request := httptest.NewRequest(http.MethodGet, "/styles.css", nil)
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}

	body := response.Body.String()
	for _, expected := range []string{
		"body.is-dragging-file::after",
		"inset: 0",
		"border-radius: 0",
		"body.is-dragging-file .upload-field.file-picker",
		"body.is-dragging-file .upload-field-action",
		"border: 2px solid var(--drag-border)",
		"--drag-border: #7dbbb3",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("styles missing %q", expected)
		}
	}
}

func TestAppKeepsNativeCheckboxClickForSingleSelection(t *testing.T) {
	app := server.New(server.Config{MaxUploadBytes: 1 << 20})
	request := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}

	body := frontendScriptBundle(t, app)
	if strings.Contains(body, "applyDragSelection(checkbox);\n  event.preventDefault();") {
		t.Fatalf("pointerdown drag handler prevents native checkbox click: %s", body)
	}
	if !strings.Contains(body, "entriesBody.addEventListener(\"change\"") {
		t.Fatalf("app.js no longer has native checkbox change handling")
	}
}

func TestAppAllowsRowSurfaceDragSelection(t *testing.T) {
	app := server.New(server.Config{MaxUploadBytes: 1 << 20})
	request := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}

	body := frontendScriptBundle(t, app)
	for _, expected := range []string{
		"dragSelection.startRow",
		"dragSelection.dragging",
		"dragSelection.suppressClick",
		"const ROW_DRAG_SELECTION_THRESHOLD_PX = 12",
		"const row = event.target.closest(\"tr[data-index]\")",
		"event.target.closest(\"button[data-open-entry]\")",
		"dragSelection.targetChecked = !checkbox.checked",
		"entriesBody.addEventListener(\"pointermove\"",
		"Math.hypot(event.clientX - dragSelection.startX, event.clientY - dragSelection.startY)",
		"distance < ROW_DRAG_SELECTION_THRESHOLD_PX",
		"startDragSelection()",
		"applyDragSelectionToRow(dragSelection.startRow)",
		"applyDragSelectionToRow(row)",
		"if (dragSelection.suppressClick)",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("app.js missing row drag selection behavior %q", expected)
		}
	}
}

func frontendScriptBundle(t *testing.T, handler http.Handler) string {
	t.Helper()

	paths := []string{
		"/app.js",
		"/modules/constants.js",
		"/modules/body-preview.js",
		"/modules/settings.js",
		"/modules/state.js",
		"/modules/dom.js",
		"/modules/theme.js",
		"/modules/formatters.js",
		"/modules/table-model.js",
		"/modules/resource-filters.js",
		"/modules/request-table.js",
		"/modules/row-interactions.js",
		"/modules/upload-flow.js",
		"/modules/entry-details.js",
		"/modules/scroll.js",
		"/modules/body-viewer.js",
	}
	var bundle strings.Builder
	for _, path := range paths {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", path, response.Code)
		}
		bundle.WriteString(response.Body.String())
		bundle.WriteByte('\n')
	}
	return bundle.String()
}

func uploadHAR(t *testing.T, handler http.Handler, name string, content string) *httptest.ResponseRecorder {
	t.Helper()
	return uploadHARReplacing(t, handler, name, content, "")
}

func uploadHARWithHeaders(t *testing.T, handler http.Handler, name string, content string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	return uploadHARReplacingWithHeaders(t, handler, name, content, "", headers)
}

func uploadHARReplacing(t *testing.T, handler http.Handler, name string, content string, replaceID string) *httptest.ResponseRecorder {
	t.Helper()
	return uploadHARReplacingWithHeaders(t, handler, name, content, replaceID, nil)
}

func uploadHARReplacingWithHeaders(t *testing.T, handler http.Handler, name string, content string, replaceID string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if replaceID != "" {
		if err := writer.WriteField("replaceId", replaceID); err != nil {
			t.Fatalf("WriteField replaceId: %v", err)
		}
	}
	part, err := writer.CreateFormFile("har", name)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := io.WriteString(part, content); err != nil {
		t.Fatalf("write multipart: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

func exportHAR(t *testing.T, handler http.Handler, id string, indexes []int) *httptest.ResponseRecorder {
	t.Helper()
	return exportHARWithHeaders(t, handler, id, indexes, nil)
}

func exportHARWithHeaders(t *testing.T, handler http.Handler, id string, indexes []int, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	payload := struct {
		ID      string `json:"id"`
		Indexes []int  `json:"indexes"`
	}{ID: id, Indexes: indexes}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal export request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/export", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

func entryDetailWithHeaders(t *testing.T, handler http.Handler, id string, index int, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	body := strings.NewReader(`{"id":` + quote(id) + `,"index":` + fmt.Sprint(index) + `}`)
	req := httptest.NewRequest(http.MethodPost, "/api/entry", body)
	req.Header.Set("Content-Type", "application/json")
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

func decodeJSON(t *testing.T, reader io.Reader, target any) {
	t.Helper()

	if err := json.NewDecoder(reader).Decode(target); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
}

func assertNoStoreHeaders(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if cacheControl := response.Header().Get("Cache-Control"); cacheControl != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cacheControl)
	}
	if pragma := response.Header().Get("Pragma"); pragma != "no-cache" {
		t.Fatalf("Pragma = %q, want no-cache", pragma)
	}
}

func quote(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}
