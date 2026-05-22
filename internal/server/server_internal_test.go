package server

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSessionExpiresWithoutFollowupTraffic(t *testing.T) {
	app := New(Config{
		MaxUploadBytes: 1 << 20,
		SessionTTL:     5 * time.Millisecond,
	})

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("har", "capture.har")
	if err != nil {
		t.Fatalf("CreateFormFile returned error: %v", err)
	}
	if _, err := part.Write([]byte(apiSampleHARInternal)); err != nil {
		t.Fatalf("write HAR returned error: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer returned error: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/upload", body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("upload status = %d, body = %s", response.Code, response.Body.String())
	}

	deadline := time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(deadline) {
		app.mu.RLock()
		count := len(app.sessions)
		app.mu.RUnlock()
		if count == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}

	app.mu.RLock()
	count := len(app.sessions)
	app.mu.RUnlock()
	t.Fatalf("session count = %d, want 0 after TTL without followup request", count)
}

func TestSessionExpiryTimerStopsWhenSessionIsReplaced(t *testing.T) {
	app := New(Config{
		MaxUploadBytes: 1 << 20,
		SessionTTL:     time.Minute,
	})

	firstID := uploadInternalHAR(t, app, "first.har", "")

	app.mu.RLock()
	firstTimer := app.sessions[firstID].expiryTimer
	app.mu.RUnlock()
	if firstTimer == nil {
		t.Fatalf("first session expiry timer is nil")
	}

	secondID := uploadInternalHAR(t, app, "second.har", firstID)
	if secondID == firstID {
		t.Fatalf("replacement upload reused session id")
	}
	if firstTimer.Stop() {
		t.Fatalf("replaced session expiry timer was still active")
	}

	app.mu.RLock()
	_, oldSessionExists := app.sessions[firstID]
	secondTimer := app.sessions[secondID].expiryTimer
	app.mu.RUnlock()
	if oldSessionExists {
		t.Fatalf("old session still exists after replace")
	}
	if secondTimer == nil {
		t.Fatalf("second session expiry timer is nil")
	}
}

func TestUploadRejectsConcurrentRequestsAboveInflightBudget(t *testing.T) {
	app := New(Config{
		MaxUploadBytes:         1 << 20,
		MaxInflightUploadBytes: 1 << 20,
	})
	hold := make(chan struct{})
	firstDone := make(chan int, 1)
	var firstStarted sync.WaitGroup
	firstStarted.Add(1)

	go func() {
		body := &blockingBody{
			prefix: []byte("not a complete multipart body"),
			hold:   hold,
		}
		request := httptest.NewRequest(http.MethodPost, "/api/upload", body)
		request.Header.Set("Content-Type", "multipart/form-data; boundary=hold")
		response := httptest.NewRecorder()
		firstStarted.Done()
		app.ServeHTTP(response, request)
		firstDone <- response.Code
	}()
	firstStarted.Wait()

	deadline := time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(deadline) {
		app.mu.RLock()
		inflight := app.inflightUploadBytes
		app.mu.RUnlock()
		if inflight > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}

	response := uploadInternalHARResponse(t, app, "second.har", apiSampleHARInternal)
	if response.Code != http.StatusTooManyRequests {
		close(hold)
		t.Fatalf("second upload status = %d, want 429; body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "too many uploads") {
		close(hold)
		t.Fatalf("second upload body = %s, want too many uploads error", response.Body.String())
	}

	close(hold)
	if code := <-firstDone; code == http.StatusTooManyRequests {
		t.Fatalf("first upload was rejected by inflight budget")
	}

	deadline = time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(deadline) {
		app.mu.RLock()
		inflight := app.inflightUploadBytes
		app.mu.RUnlock()
		if inflight == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	app.mu.RLock()
	inflight := app.inflightUploadBytes
	app.mu.RUnlock()
	t.Fatalf("inflightUploadBytes = %d, want 0 after first upload exits", inflight)
}

func uploadInternalHAR(t *testing.T, app *App, name string, replaceID string) string {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if replaceID != "" {
		if err := writer.WriteField("replaceId", replaceID); err != nil {
			t.Fatalf("WriteField returned error: %v", err)
		}
	}
	part, err := writer.CreateFormFile("har", name)
	if err != nil {
		t.Fatalf("CreateFormFile returned error: %v", err)
	}
	if _, err := part.Write([]byte(apiSampleHARInternal)); err != nil {
		t.Fatalf("write HAR returned error: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer returned error: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/upload", body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("upload status = %d, body = %s", response.Code, response.Body.String())
	}

	var payload struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode upload response returned error: %v", err)
	}
	if payload.ID == "" {
		t.Fatalf("upload id is empty")
	}
	return payload.ID
}

func uploadInternalHARResponse(t *testing.T, app *App, name string, harBody string) *httptest.ResponseRecorder {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("har", name)
	if err != nil {
		t.Fatalf("CreateFormFile returned error: %v", err)
	}
	if _, err := part.Write([]byte(harBody)); err != nil {
		t.Fatalf("write HAR returned error: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer returned error: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/upload", body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)
	return response
}

type blockingBody struct {
	prefix []byte
	hold   <-chan struct{}
	sent   bool
}

func (b *blockingBody) Read(p []byte) (int, error) {
	if !b.sent {
		b.sent = true
		return copy(p, b.prefix), nil
	}
	<-b.hold
	return 0, io.EOF
}

const apiSampleHARInternal = `{
  "log": {
    "entries": [
      {
        "startedDateTime": "2026-05-17T10:00:00.000Z",
        "time": 1,
        "request": { "method": "GET", "url": "https://example.com/" },
        "response": { "status": 200, "content": { "mimeType": "text/html" } }
      }
    ]
  }
}`
