// Package server is the local HTTP API and the host of the embedded web UI.
// It listens on 127.0.0.1 only and every API call needs the session token.
package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"q3vigilai/internal/ai"
	"q3vigilai/internal/chat"
	"q3vigilai/internal/fetch"
	"q3vigilai/internal/i18n"
	"q3vigilai/internal/pipeline"
	"q3vigilai/internal/store"
)

// Server holds what the handlers need.
type Server struct {
	St      *store.Store
	Engine  *pipeline.Engine
	Sched   *pipeline.Scheduler
	Chat    *chat.Service
	Fetch   *fetch.Client
	Web     fs.FS
	Version string
	DataDir string

	// AI returns the current provider and a note describing how it was chosen.
	AI func() (ai.Provider, string)
	// ReloadAI rebuilds the provider after the AI settings changed.
	ReloadAI func()
	// SetAutostart turns "start with Windows" on or off.
	SetAutostart func(on bool) error
	// OpenPath opens a file or folder with the system's default application.
	OpenPath func(path string) error
	// Changed is called after anything the tray icon reflects has changed.
	Changed func()
	// ShowWindow brings the UI up; a second launch of the app calls it.
	ShowWindow func()
	// TopicSaved is called after a topic was created or changed, so it can be
	// applied to the items collected before.
	TopicSaved func(topicID int64)

	Token string
	port  int
	ln    net.Listener
	hub   hub

	// lastUse is the time of the latest request, in Unix nanoseconds. The
	// memory trimmer leaves the app alone while someone is using it.
	lastUse atomic.Int64
}

// NewToken returns a random session token.
func NewToken() string {
	b := make([]byte, 24)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Listen binds a random port on the loopback interface.
func (s *Server) Listen() error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	s.ln = ln
	s.port = ln.Addr().(*net.TCPAddr).Port
	return nil
}

// IdleFor returns how long no request has come in. A server nobody has talked
// to yet counts from its start.
func (s *Server) IdleFor() time.Duration {
	last := s.lastUse.Load()
	if last == 0 {
		s.lastUse.CompareAndSwap(0, time.Now().UnixNano())
		last = s.lastUse.Load()
	}
	return time.Since(time.Unix(0, last))
}

// Port returns the bound port.
func (s *Server) Port() int { return s.port }

// URL returns the address that signs the browser in and opens the given view.
func (s *Server) URL(view string) string {
	u := fmt.Sprintf("http://127.0.0.1:%d/auth?t=%s", s.port, s.Token)
	if view != "" {
		u += "&go=" + url.QueryEscape(view)
	}
	return u
}

// Serve runs until ctx is cancelled.
func (s *Server) Serve(ctx context.Context) error {
	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		srv.Shutdown(sctx)
	}()
	if err := srv.Serve(s.ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Broadcast publishes a live event to every open UI window.
func (s *Server) Broadcast(event string, data any) {
	s.hub.send(event, data)
	if s.Changed != nil && (event == "scan.started" || event == "scan.finished" || event == "alert.created") {
		s.Changed()
	}
}

// Handler builds the routes.
func (s *Server) Handler() http.Handler {
	s.lastUse.CompareAndSwap(0, time.Now().UnixNano()) // idle is counted from the start
	mux := http.NewServeMux()
	api := func(pattern string, h func(http.ResponseWriter, *http.Request) error) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if !s.authorized(r) {
				fail(w, http.StatusUnauthorized, "unauthorized", i18n.T("Phiên làm việc không hợp lệ. Hãy mở Q3VigilAI từ biểu tượng ở khay hệ thống."))
				return
			}
			if err := h(w, r); err != nil {
				s.writeErr(w, err)
			}
		})
	}
	mux.HandleFunc("GET /auth", s.handleAuth)
	api("GET /api/status", s.getStatus)
	api("GET /api/meta", s.getMeta)
	api("GET /api/i18n", s.getI18n)
	api("GET /api/events", s.getEvents)
	api("POST /api/notify/test", func(w http.ResponseWriter, r *http.Request) error {
		s.Sched.Notifier.Test()
		return ok(w, map[string]bool{"sent": true})
	})
	api("POST /api/show", func(w http.ResponseWriter, r *http.Request) error {
		if s.ShowWindow != nil {
			go s.ShowWindow()
		}
		return ok(w, map[string]bool{"shown": true})
	})

	api("GET /api/alerts", s.listAlerts)
	api("GET /api/alerts/{id}", s.getAlert)
	api("PATCH /api/alerts/{id}", s.patchAlert)
	api("POST /api/alerts/mark-read", s.markRead)

	api("GET /api/topics", s.listTopics)
	api("POST /api/topics", s.saveTopic)
	api("POST /api/topics/preview", s.previewTopic)
	api("GET /api/topics/{id}", s.getTopic)
	api("PUT /api/topics/{id}", s.saveTopic)
	api("DELETE /api/topics/{id}", s.deleteTopic)

	api("GET /api/sources", s.listSources)
	api("POST /api/sources", s.addSource)
	api("POST /api/sources/test", s.testSource)
	api("PATCH /api/sources/{id}", s.patchSource)
	api("DELETE /api/sources/{id}", s.deleteSource)

	api("GET /api/documents", s.listDocuments)
	api("POST /api/documents/fetch", s.fetchDocument)
	api("GET /api/documents/{id}", s.getDocument)
	api("GET /api/documents/{id}/file/{n}", s.getDocumentFile)
	api("POST /api/documents/{id}/open-folder", s.openDocumentFolder)

	api("POST /api/scan", s.startScan)
	api("GET /api/scans", s.listScans)

	api("GET /api/chat/sessions", s.listChats)
	api("POST /api/chat/sessions", s.newChat)
	api("GET /api/chat/sessions/{id}", s.getChat)
	api("DELETE /api/chat/sessions/{id}", s.deleteChat)
	api("POST /api/chat/sessions/{id}/messages", s.postChatMessage)

	api("GET /api/settings", s.getSettings)
	api("PUT /api/settings", s.putSettings)
	api("GET /api/providers", s.getProviders)
	api("POST /api/providers/test", s.testProvider)
	api("POST /api/backup", s.backup)

	static := http.FileServerFS(s.Web)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			fail(w, http.StatusNotFound, "not_found", i18n.T("Không có địa chỉ API này."))
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		static.ServeHTTP(w, r)
	})
	return s.guard(mux)
}

// guard rejects requests that did not come from a page this server served:
// the Host header must be the loopback address (which stops DNS rebinding),
// and a state-changing request must not carry a foreign Origin.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hosts := []string{fmt.Sprintf("127.0.0.1:%d", s.port), fmt.Sprintf("localhost:%d", s.port)}
		okHost := s.port == 0
		for _, h := range hosts {
			if r.Host == h {
				okHost = true
			}
		}
		if !okHost {
			fail(w, http.StatusForbidden, "bad_host", i18n.T("Yêu cầu bị từ chối."))
			return
		}
		if o := r.Header.Get("Origin"); o != "" && r.Method != http.MethodGet && r.Method != http.MethodHead {
			if o != "http://"+hosts[0] && o != "http://"+hosts[1] && s.port != 0 {
				fail(w, http.StatusForbidden, "bad_origin", i18n.T("Yêu cầu bị từ chối."))
				return
			}
		}
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		// The live-event stream is a request that never ends and says nothing
		// about whether anyone is using the app.
		if r.URL.Path != "/api/events" {
			s.lastUse.Store(time.Now().UnixNano())
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) authorized(r *http.Request) bool {
	got := r.Header.Get("X-Q3-Token")
	if got == "" {
		if c, err := r.Cookie("q3t"); err == nil {
			got = c.Value
		}
	}
	return s.Token != "" && subtle.ConstantTimeCompare([]byte(got), []byte(s.Token)) == 1
}

// handleAuth exchanges the token in the launch URL for a cookie, so the
// token does not stay in the address or the history.
func (s *Server) handleAuth(w http.ResponseWriter, r *http.Request) {
	if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("t")), []byte(s.Token)) != 1 {
		fail(w, http.StatusUnauthorized, "unauthorized", i18n.T("Liên kết không hợp lệ. Hãy mở Q3VigilAI từ biểu tượng ở khay hệ thống."))
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "q3t", Value: s.Token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
	target := "/"
	if v := r.URL.Query().Get("go"); v != "" && isView(v) {
		target = "/#/" + v
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func isView(v string) bool {
	for _, r := range v {
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '/' && r != '-' {
			return false
		}
	}
	return len(v) < 60
}

// ---- helpers ---------------------------------------------------------------

// apiError carries a message in the source language plus its arguments; it is
// worded in the current language only when the reply is written.
type apiError struct {
	status  int
	code    string
	message string
	args    []any
}

func (e *apiError) Error() string { return i18n.T(e.message, e.args...) }

func apiErr(status int, code, message string, args ...any) error {
	return &apiError{status, code, message, args}
}

func bad(code, message string, args ...any) error {
	return apiErr(http.StatusBadRequest, code, message, args...)
}

func fail(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func (s *Server) writeErr(w http.ResponseWriter, err error) {
	var ae *apiError
	switch {
	case errors.As(err, &ae):
		fail(w, ae.status, ae.code, ae.Error())
	case errors.Is(err, store.ErrNotFound):
		fail(w, http.StatusNotFound, "not_found", i18n.T("Không tìm thấy."))
	case errors.Is(err, pipeline.ErrBusy):
		fail(w, http.StatusConflict, "busy", i18n.T("Đang có một lượt quét chạy."))
	case errors.Is(err, fetch.ErrBlocked):
		fail(w, http.StatusBadRequest, "source_blocked", err.Error())
	default:
		fail(w, http.StatusInternalServerError, "internal", err.Error())
	}
}

func ok(w http.ResponseWriter, v any) error {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	return json.NewEncoder(w).Encode(v)
}

func body(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	if err := dec.Decode(v); err != nil {
		return bad("bad_json", "Dữ liệu gửi lên không hợp lệ.")
	}
	return nil
}

func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, store.ErrNotFound
	}
	return id, nil
}

func qInt(r *http.Request, key string, def int) int {
	if n, err := strconv.Atoi(r.URL.Query().Get(key)); err == nil {
		return n
	}
	return def
}

func (s *Server) changed() {
	if s.Changed != nil {
		s.Changed()
	}
}

// ---- live events -----------------------------------------------------------

type hub struct {
	mu      sync.Mutex
	clients map[chan []byte]bool
}

func (h *hub) send(event string, data any) {
	payload, err := json.Marshal(data)
	if err != nil {
		return
	}
	msg := []byte("event: " + event + "\ndata: " + string(payload) + "\n\n")
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		select {
		case c <- msg:
		default: // a stalled window must not block the scan
		}
	}
}

func (s *Server) getEvents(w http.ResponseWriter, r *http.Request) error {
	fl, okf := w.(http.Flusher)
	if !okf {
		return errors.New("streaming unsupported")
	}
	c := make(chan []byte, 64)
	s.hub.mu.Lock()
	if s.hub.clients == nil {
		s.hub.clients = map[chan []byte]bool{}
	}
	s.hub.clients[c] = true
	s.hub.mu.Unlock()
	defer func() {
		s.hub.mu.Lock()
		delete(s.hub.clients, c)
		s.hub.mu.Unlock()
	}()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	fmt.Fprint(w, ": connected\n\n")
	fl.Flush()
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return nil
		case msg := <-c:
			w.Write(msg)
			fl.Flush()
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		}
	}
}
