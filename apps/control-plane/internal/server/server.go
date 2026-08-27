// Package server wires the control-plane HTTP surface: an English login
// page, passcode login, an authenticated placeholder dashboard, and logout.
// It is server-rendered, English-only, and depends only on the standard
// library plus the internal session package.
package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"html/template"
	"log/slog"
	"net/http"
	"time"

	"portcullis/control-plane/internal/session"
)

//
//go:embed templates/*.html
var templatesFS embed.FS

// Config carries the constructed dependencies for the HTTP server.
type Config struct {
	Passcode       string
	SessionManager *session.Manager
	Logger         *slog.Logger
	// Now overrides the wall clock; nil means time.Now. Injected for tests.
	Now func() time.Time
}

// Server holds the wired control-plane dependencies.
type Server struct {
	passcodeHash [sha256.Size]byte
	sessions     *session.Manager
	logger       *slog.Logger
	templates    *template.Template
	now          func() time.Time
}

// New builds the HTTP server. Templates are embedded, so construction
// cannot fail on template parsing at runtime.
func New(cfg Config) *Server {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Server{
		passcodeHash: sha256.Sum256([]byte(cfg.Passcode)),
		sessions:     cfg.SessionManager,
		logger:       logger,
		templates:    template.Must(template.ParseFS(templatesFS, "templates/*.html")),
		now:          now,
	}
}

// Handler returns the routed HTTP handler for the control plane.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	})
	mux.HandleFunc("GET /login", s.handleLoginPage)
	mux.HandleFunc("POST /login", s.handleLogin)
	mux.HandleFunc("GET /dashboard", s.requireSession(s.handleDashboard))
	mux.HandleFunc("POST /logout", s.handleLogout)
	return mux
}

// constantTimePasscode compares the submitted passcode to the expected one
// via SHA-256 digests and subtle.ConstantTimeCompare, normalizing length so
// comparison time does not leak passcode length or prefix matches.
func (s *Server) constantTimePasscode(submitted string) bool {
	submittedHash := sha256.Sum256([]byte(submitted))
	return subtle.ConstantTimeCompare(submittedHash[:], s.passcodeHash[:]) == 1
}

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, "login.html", nil); err != nil {
		s.logger.Error("render login page", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !s.constantTimePasscode(r.PostFormValue("passcode")) {
		s.logger.Info("failed login attempt")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		if err := s.templates.ExecuteTemplate(w, "login.html", map[string]bool{"Failed": true}); err != nil {
			s.logger.Error("render login page", "err", err)
		}
		return
	}

	token, expires, err := s.sessions.Create(s.now())
	if err != nil {
		s.logger.Error("create session", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, s.newSessionCookie(token, expires))
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

// requireSession rejects requests without a currently valid session cookie
// by redirecting to the login page.
func (s *Server) requireSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(session.CookieName)
		if err != nil || s.sessions.Verify(c.Value, s.now()) != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, "dashboard.html", nil); err != nil {
		s.logger.Error("render dashboard", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(session.CookieName); err == nil && c.Value != "" {
		s.sessions.Revoke(c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     session.CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// newSessionCookie builds the owner-session cookie with the mandatory
// security attributes from ADR-0002.
func (s *Server) newSessionCookie(value string, expires time.Time) *http.Cookie {
	return &http.Cookie{
		Name:     session.CookieName,
		Value:    value,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(expires.Sub(s.now()).Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}
}
