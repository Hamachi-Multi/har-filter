package server

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Hamachi-Multi/har-filter/internal/har"
)

const (
	defaultMaxUploadBytes  = 200 << 20
	defaultMaxSessionBytes = 512 << 20
	maxExportRequestBytes  = 1 << 20
	maxReplaceIDBytes      = 128
	multipartOverhead      = 1 << 20
	defaultSessionTTL      = 30 * time.Minute
)

//go:embed static/*
var staticFiles embed.FS

type Config struct {
	MaxUploadBytes         int64
	MaxSessionBytes        int64
	MaxInflightUploadBytes int64
	SessionTTL             time.Duration
}

type App struct {
	mux                    *http.ServeMux
	maxUploadBytes         int64
	maxSessionBytes        int64
	maxInflightUploadBytes int64
	sessionTTL             time.Duration

	mu                  sync.RWMutex
	sessions            map[string]*session
	totalSessionBytes   int64
	inflightUploadBytes int64
}

type session struct {
	id            string
	fileName      string
	document      *har.Document
	createdAt     time.Time
	retainedBytes int64
	expiryTimer   *time.Timer
}

type uploadResponse struct {
	ID         string             `json:"id"`
	FileName   string             `json:"fileName"`
	EntryCount int                `json:"entryCount"`
	Entries    []har.EntrySummary `json:"entries"`
}

type exportRequest struct {
	ID      string `json:"id"`
	Indexes []int  `json:"indexes"`
}

type entryRequest struct {
	ID    string `json:"id"`
	Index int    `json:"index"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func New(config Config) *App {
	maxUploadBytes := config.MaxUploadBytes
	if maxUploadBytes <= 0 {
		maxUploadBytes = defaultMaxUploadBytes
	}
	maxSessionBytes := config.MaxSessionBytes
	if maxSessionBytes <= 0 {
		maxSessionBytes = defaultMaxSessionBytes
		if maxSessionBytes < maxUploadBytes {
			maxSessionBytes = maxUploadBytes
		}
	}
	maxInflightUploadBytes := config.MaxInflightUploadBytes
	if maxInflightUploadBytes <= 0 {
		maxInflightUploadBytes = maxSessionBytes
	}
	if maxInflightUploadBytes < maxUploadBytes {
		maxInflightUploadBytes = maxUploadBytes
	}
	sessionTTL := config.SessionTTL
	if sessionTTL <= 0 {
		sessionTTL = defaultSessionTTL
	}

	app := &App{
		mux:                    http.NewServeMux(),
		maxUploadBytes:         maxUploadBytes,
		maxSessionBytes:        maxSessionBytes,
		maxInflightUploadBytes: maxInflightUploadBytes,
		sessionTTL:             sessionTTL,
		sessions:               make(map[string]*session),
	}
	app.routes()
	return app
}

func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mux.ServeHTTP(w, r)
}

func (a *App) routes() {
	a.mux.HandleFunc("/api/upload", a.handleUpload)
	a.mux.HandleFunc("/api/export", a.handleExport)
	a.mux.HandleFunc("/api/entry", a.handleEntry)

	staticRoot, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic(fmt.Sprintf("static files unavailable: %v", err))
	}
	a.mux.Handle("/", http.FileServer(http.FS(staticRoot)))
}

func (a *App) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, errorResponse{Error: "method not allowed"})
		return
	}
	if !requestHasTrustedOrigin(r) {
		writeJSON(w, http.StatusForbidden, errorResponse{Error: "cross-origin request blocked"})
		return
	}
	reservedUploadBytes := a.maxUploadBytes
	if !a.tryReserveInflightUpload(reservedUploadBytes) {
		writeJSON(w, http.StatusTooManyRequests, errorResponse{Error: "too many uploads in progress"})
		return
	}
	defer a.releaseInflightUpload(reservedUploadBytes)

	r.Body = http.MaxBytesReader(w, r.Body, a.maxUploadBytes+multipartOverhead)
	reader, err := r.MultipartReader()
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "request body too large") {
			status = http.StatusRequestEntityTooLarge
		}
		writeJSON(w, status, errorResponse{Error: "invalid upload: " + err.Error()})
		return
	}

	var (
		data      []byte
		fileName  string
		replaceID string
	)
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: "read multipart: " + err.Error()})
			return
		}
		switch part.FormName() {
		case "replaceId":
			value, err := io.ReadAll(io.LimitReader(part, maxReplaceIDBytes+1))
			if err != nil {
				writeJSON(w, http.StatusBadRequest, errorResponse{Error: "read replaceId: " + err.Error()})
				return
			}
			if len(value) > maxReplaceIDBytes {
				writeJSON(w, http.StatusBadRequest, errorResponse{Error: "replaceId is too long"})
				return
			}
			replaceID = strings.TrimSpace(string(value))
			continue
		case "har":
			if data != nil {
				continue
			}
			fileName = safeBaseName(part.FileName())
			limited := io.LimitReader(part, a.maxUploadBytes+1)
			data, err = io.ReadAll(limited)
			if err != nil {
				status := http.StatusBadRequest
				if strings.Contains(err.Error(), "request body too large") {
					status = http.StatusRequestEntityTooLarge
				}
				writeJSON(w, status, errorResponse{Error: "read upload: " + err.Error()})
				return
			}
			if int64(len(data)) > a.maxUploadBytes {
				writeJSON(w, http.StatusRequestEntityTooLarge, errorResponse{Error: "HAR file exceeds upload limit"})
				return
			}
		default:
			continue
		}
	}
	if len(data) == 0 {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "missing har file field"})
		return
	}

	document, err := har.Parse(data)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "parse HAR: " + err.Error()})
		return
	}

	now := time.Now()
	a.cleanupExpired(now)

	id, err := newID()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "create session id"})
		return
	}

	retainedBytes := document.RetainedBytes()
	if retainedBytes > a.maxSessionBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, errorResponse{Error: "HAR file exceeds session memory limit"})
		return
	}

	a.mu.Lock()
	if replaceID != "" {
		a.deleteSessionLocked(replaceID)
	}
	if !a.evictOldestUntilFitsLocked(retainedBytes) {
		a.mu.Unlock()
		writeJSON(w, http.StatusRequestEntityTooLarge, errorResponse{Error: "HAR file exceeds session memory limit"})
		return
	}
	current := &session{
		id:            id,
		fileName:      fileName,
		document:      document,
		createdAt:     now,
		retainedBytes: retainedBytes,
	}
	a.sessions[id] = current
	a.totalSessionBytes += retainedBytes
	a.scheduleSessionExpiryLocked(current)
	a.mu.Unlock()

	writeJSON(w, http.StatusOK, uploadResponse{
		ID:         id,
		FileName:   fileName,
		EntryCount: len(document.Summaries),
		Entries:    document.Summaries,
	})
}

func (a *App) handleExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, errorResponse{Error: "method not allowed"})
		return
	}
	if !requestHasTrustedOrigin(r) {
		writeJSON(w, http.StatusForbidden, errorResponse{Error: "cross-origin request blocked"})
		return
	}

	var request exportRequest
	if err := decodeStrictJSON(w, r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid export request: " + err.Error()})
		return
	}
	if strings.TrimSpace(request.ID) == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "id is required"})
		return
	}

	current, ok := a.getSession(request.ID, time.Now())
	if !ok {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "session not found"})
		return
	}

	subset, err := current.document.BuildSubset(request.Indexes)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	setNoStoreHeaders(w.Header())
	w.Header().Set("Content-Disposition", `attachment; filename="`+filteredFileName(current.fileName)+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(subset)
}

func (a *App) handleEntry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, errorResponse{Error: "method not allowed"})
		return
	}
	if !requestHasTrustedOrigin(r) {
		writeJSON(w, http.StatusForbidden, errorResponse{Error: "cross-origin request blocked"})
		return
	}

	var request entryRequest
	if err := decodeStrictJSON(w, r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid entry request: " + err.Error()})
		return
	}
	if strings.TrimSpace(request.ID) == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "id is required"})
		return
	}

	current, ok := a.getSession(request.ID, time.Now())
	if !ok {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "session not found"})
		return
	}

	detail, err := current.document.ResponseDetail(request.Index)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (a *App) cleanupExpired(now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cleanupExpiredLocked(now)
}

func (a *App) cleanupExpiredLocked(now time.Time) {
	for id, current := range a.sessions {
		if a.isExpired(current, now) {
			a.deleteSessionLocked(id)
		}
	}
}

func (a *App) getSession(id string, now time.Time) (*session, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	current, ok := a.sessions[id]
	if !ok {
		return nil, false
	}
	if a.isExpired(current, now) {
		a.deleteSessionLocked(id)
		return nil, false
	}
	return current, true
}

func (a *App) isExpired(current *session, now time.Time) bool {
	return !current.createdAt.Add(a.sessionTTL).After(now)
}

func (a *App) scheduleSessionExpiryLocked(current *session) {
	if a.sessionTTL <= 0 {
		return
	}
	id := current.id
	createdAt := current.createdAt
	delay := time.Until(createdAt.Add(a.sessionTTL))
	if delay < 0 {
		delay = 0
	}
	current.expiryTimer = time.AfterFunc(delay, func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		current, ok := a.sessions[id]
		if !ok || !current.createdAt.Equal(createdAt) || !a.isExpired(current, time.Now()) {
			return
		}
		a.deleteSessionLocked(id)
	})
}

func (a *App) deleteSessionLocked(id string) {
	current, ok := a.sessions[id]
	if !ok {
		return
	}
	if current.expiryTimer != nil {
		current.expiryTimer.Stop()
		current.expiryTimer = nil
	}
	a.totalSessionBytes -= current.retainedBytes
	if a.totalSessionBytes < 0 {
		a.totalSessionBytes = 0
	}
	delete(a.sessions, id)
}

func (a *App) evictOldestUntilFitsLocked(incomingBytes int64) bool {
	for a.totalSessionBytes+incomingBytes > a.maxSessionBytes {
		oldestID := ""
		var oldestCreatedAt time.Time
		for id, current := range a.sessions {
			if oldestID == "" || current.createdAt.Before(oldestCreatedAt) {
				oldestID = id
				oldestCreatedAt = current.createdAt
			}
		}
		if oldestID == "" {
			return false
		}
		a.deleteSessionLocked(oldestID)
	}
	return true
}

func (a *App) tryReserveInflightUpload(bytes int64) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.inflightUploadBytes+bytes > a.maxInflightUploadBytes {
		return false
	}
	a.inflightUploadBytes += bytes
	return true
}

func (a *App) releaseInflightUpload(bytes int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.inflightUploadBytes -= bytes
	if a.inflightUploadBytes < 0 {
		a.inflightUploadBytes = 0
	}
}

func newID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	setNoStoreHeaders(w.Header())
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func setNoStoreHeaders(header http.Header) {
	header.Set("Cache-Control", "no-store")
	header.Set("Pragma", "no-cache")
}

func decodeStrictJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxExportRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	var extra struct{}
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain a single JSON object")
	}
	return nil
}

func requestHasTrustedOrigin(r *http.Request) bool {
	if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
		return sameRequestOrigin(origin, r)
	}
	if referer := strings.TrimSpace(r.Header.Get("Referer")); referer != "" {
		return sameRequestOrigin(referer, r)
	}
	return true
}

func sameRequestOrigin(raw string, r *http.Request) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return false
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return strings.EqualFold(parsed.Scheme, scheme) &&
		normalizeOriginHost(parsed.Host, parsed.Scheme) == normalizeOriginHost(r.Host, scheme)
}

func normalizeOriginHost(host string, scheme string) string {
	host = strings.TrimSpace(host)
	if parsedHost, parsedPort, err := net.SplitHostPort(host); err == nil {
		if isDefaultPort(scheme, parsedPort) {
			return strings.ToLower(parsedHost)
		}
		return strings.ToLower(net.JoinHostPort(parsedHost, parsedPort))
	}
	return strings.ToLower(host)
}

func isDefaultPort(scheme string, port string) bool {
	return (strings.EqualFold(scheme, "http") && port == "80") ||
		(strings.EqualFold(scheme, "https") && port == "443")
}

func safeBaseName(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "." || name == "/" || name == "" {
		return "capture.har"
	}
	return strings.NewReplacer(`"`, "", `\`, "", "\n", "", "\r", "").Replace(name)
}

func filteredFileName(name string) string {
	name = safeBaseName(name)
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	if strings.EqualFold(ext, ".har") {
		return base + "-filtered.har"
	}
	if ext == "" {
		return name + "-filtered.har"
	}
	return strings.TrimSuffix(name, ext) + "-filtered.har"
}
