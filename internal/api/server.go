package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/reviz-tw/Episteme/internal/auth"
	"github.com/reviz-tw/Episteme/internal/config"
	"github.com/reviz-tw/Episteme/internal/inference"
	"github.com/reviz-tw/Episteme/internal/retrieval"
	"github.com/reviz-tw/Episteme/internal/storage"
	"github.com/reviz-tw/Episteme/internal/storage/db"
	"golang.org/x/time/rate"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type Server struct {
	Store     *storage.Store
	Config    config.Config
	Search    *retrieval.Service
	TEI       *inference.TEI
	Vectors   storage.VectorStore
	MCP       http.Handler
	LegacyMCP http.Handler
	limiter   *rate.Limiter
	authSlots chan struct{}
}

func JSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
func Fail(w http.ResponseWriter, e error) {
	status := 400
	message := e.Error()
	if errors.Is(e, pgx.ErrNoRows) {
		status = 404
		message = "找不到資料"
	} else if errors.Is(e, storage.ErrConflict) {
		status = 409
	} else if errors.Is(e, storage.ErrReview) || errors.Is(e, storage.ErrMetadataPending) {
		status = 409
	}
	JSON(w, status, map[string]string{"error": message})
}
func Decode(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return fmt.Errorf("JSON 格式錯誤: %w", e)
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return fmt.Errorf("請只傳送一個 JSON 物件")
	}
	return nil
}
func (s *Server) Handler() http.Handler {
	s.limiter = rate.NewLimiter(rate.Every(time.Second), 10)
	s.authSlots = make(chan struct{}, 2)
	m := http.NewServeMux()
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, c := context.WithTimeout(r.Context(), 2*time.Second)
		defer c()
		if e := s.Store.Pool.Ping(ctx); e != nil {
			JSON(w, 503, map[string]string{"status": "unavailable"})
			return
		}
		JSON(w, 200, map[string]string{"status": "ok"})
	})
	m.HandleFunc("GET /api/v1/auth/me", s.me)
	m.HandleFunc("POST /api/v1/auth/setup", s.setup)
	m.HandleFunc("POST /api/v1/auth/login", s.login)
	m.HandleFunc("POST /api/v1/auth/logout", s.logout)
	m.HandleFunc("GET /api/v1/documents", s.documents)
	m.HandleFunc("POST /api/v1/documents/upload", s.upload)
	m.HandleFunc("GET /api/v1/documents/{id}", s.document)
	m.HandleFunc("PATCH /api/v1/documents/{id}", s.patchDocument)
	m.HandleFunc("POST /api/v1/documents/{id}/metadata/extract", s.extractMetadata)
	m.HandleFunc("GET /api/v1/documents/{id}/original", s.original)
	m.HandleFunc("DELETE /api/v1/documents/{id}", s.deleteDocument)
	m.HandleFunc("GET /api/v1/documents/{id}/chunks", s.chunks)
	m.HandleFunc("PATCH /api/v1/documents/{id}/chunks", s.saveDraft)
	m.HandleFunc("PATCH /api/v1/chunks/{id}", s.patchChunk)
	m.HandleFunc("POST /api/v1/chunks/{id}/split", s.splitChunk)
	m.HandleFunc("POST /api/v1/chunks/{id}/merge", s.mergeChunk)
	m.HandleFunc("POST /api/v1/documents/{id}/commit", s.commit)
	m.HandleFunc("POST /api/v1/documents/{id}/rechunk", s.rechunk)
	m.HandleFunc("POST /api/v1/chunks/{id}/reindex", s.reindex)
	m.HandleFunc("GET /api/v1/indexing/jobs", s.jobs)
	m.HandleFunc("POST /api/v1/indexing/rebuild", s.rebuild)
	m.HandleFunc("POST /api/v1/indexing/jobs/{id}", s.jobAction)
	m.HandleFunc("GET /api/v1/indexing/stream", s.stream)
	m.HandleFunc("GET /api/v1/dashboard", s.dashboard)
	m.HandleFunc("POST /api/v1/retrieval/search", s.search)
	if s.MCP != nil {
		m.Handle("/mcp", s.MCP)
	}
	if s.LegacyMCP != nil {
		m.Handle("/mcp/sse", s.LegacyMCP)
		m.Handle("/mcp/message", s.LegacyMCP)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/login" || r.URL.Path == "/api/v1/auth/setup" {
			select {
			case s.authSlots <- struct{}{}:
				defer func() { <-s.authSlots }()
			default:
				JSON(w, 429, map[string]string{"error": "請稍後再試"})
				return
			}
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "same-origin")
		if origin := r.Header.Get("Origin"); origin != "" && origin != s.Config.Origin {
			JSON(w, 403, map[string]string{"error": "不允許的來源"})
			return
		}
		if strings.HasPrefix(r.URL.Path, "/mcp") {
			token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if s.Config.MCPToken == "" || subtle.ConstantTimeCompare([]byte(token), []byte(s.Config.MCPToken)) != 1 {
				JSON(w, 401, map[string]string{"error": "MCP bearer token required"})
				return
			}
		}
		if strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/api/v1/auth/") {
			c, e := r.Cookie("episteme_session")
			if e != nil {
				JSON(w, 401, map[string]string{"error": "請先登入"})
				return
			}
			if _, e = auth.Username(r.Context(), s.Store, c.Value); e != nil {
				JSON(w, 401, map[string]string{"error": "登入已到期"})
				return
			}
		}
		m.ServeHTTP(w, r)
	})
}
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	count, e := db.New(s.Store.Pool).CountUsers(r.Context())
	if e != nil {
		JSON(w, 503, map[string]string{"error": "資料庫無法使用"})
		return
	}
	name := ""
	if c, e := r.Cookie("episteme_session"); e == nil {
		name, _ = auth.Username(r.Context(), s.Store, c.Value)
	}
	JSON(w, 200, map[string]any{"setup_required": count == 0, "username": name, "authenticated": name != ""})
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	if !s.limiter.Allow() {
		JSON(w, 429, map[string]string{"error": "請稍後再試"})
		return
	}
	var v credentials
	if e := Decode(w, r, &v); e != nil {
		Fail(w, e)
		return
	}
	if e := auth.Setup(r.Context(), s.Store, strings.TrimSpace(v.Username), v.Password); e != nil {
		Fail(w, e)
		return
	}
	JSON(w, 201, map[string]bool{"created": true})
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !s.limiter.Allow() {
		JSON(w, 429, map[string]string{"error": "請稍後再試"})
		return
	}
	var v credentials
	if e := Decode(w, r, &v); e != nil {
		Fail(w, e)
		return
	}
	if len(v.Password) > 1024 {
		Fail(w, errors.New("密碼過長"))
		return
	}
	token, e := auth.Login(r.Context(), s.Store, v.Username, v.Password)
	if e != nil {
		JSON(w, 401, map[string]string{"error": "帳號或密碼錯誤"})
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "episteme_session", Value: token, Path: "/", HttpOnly: true, Secure: s.Config.SecureCookie, SameSite: http.SameSiteStrictMode, MaxAge: 86400})
	JSON(w, 200, map[string]bool{"authenticated": true})
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, e := r.Cookie("episteme_session"); e == nil {
		if _, e = s.Store.Pool.Exec(r.Context(), `DELETE FROM sessions WHERE token_hash=$1`, auth.TokenHash(c.Value)); e != nil {
			Fail(w, e)
			return
		}
	}
	http.SetCookie(w, &http.Cookie{Name: "episteme_session", Value: "", Path: "/", HttpOnly: true, Secure: s.Config.SecureCookie, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	JSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) documents(w http.ResponseWriter, r *http.Request) {
	v, e := s.Store.Documents(r.Context())
	if e != nil {
		Fail(w, e)
		return
	}
	JSON(w, 200, v)
}
func (s *Server) document(w http.ResponseWriter, r *http.Request) {
	v, e := s.Store.Document(r.Context(), r.PathValue("id"))
	if e != nil {
		Fail(w, e)
		return
	}
	JSON(w, 200, v)
}
func (s *Server) chunks(w http.ResponseWriter, r *http.Request) {
	if _, e := s.Store.Document(r.Context(), r.PathValue("id")); e != nil {
		Fail(w, e)
		return
	}
	v, e := storage.Chunks(r.Context(), s.Store.Pool, r.PathValue("id"))
	if e != nil {
		Fail(w, e)
		return
	}
	JSON(w, 200, v)
}
func (s *Server) jobs(w http.ResponseWriter, r *http.Request) {
	v, e := s.Store.Jobs(r.Context())
	if e != nil {
		Fail(w, e)
		return
	}
	JSON(w, 200, v)
}
func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	var in retrieval.Input
	if e := Decode(w, r, &in); e != nil {
		Fail(w, e)
		return
	}
	ctx, c := context.WithTimeout(r.Context(), 100*time.Second)
	defer c()
	v, e := s.Search.Search(ctx, in)
	if e != nil {
		if errors.Is(e, retrieval.ErrInvalidInput) {
			Fail(w, e)
			return
		}
		slog.Warn("search", "error", e)
		JSON(w, 502, map[string]string{"error": e.Error()})
		return
	}
	JSON(w, 200, v)
}
