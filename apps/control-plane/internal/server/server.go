// Package server wires the control-plane HTTP surface: an English login
// page, passcode login, and an authenticated server-rendered English
// dashboard with service lifecycle management (list/create/edit/delete)
// protected by session-bound CSRF. It is English-only and depends only on
// the standard library plus the internal session and registry packages.
package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"portcullis/control-plane/internal/backups"
	"portcullis/control-plane/internal/caddyops"
	"portcullis/control-plane/internal/dump"
	"portcullis/control-plane/internal/provision"
	"portcullis/control-plane/internal/registry"
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
	// Lifecycle drives the authenticated service CRUD surface. When nil the
	// dashboard renders an English "not available" note and no lifecycle
	// route has any effect (foundation wiring without a database).
	Lifecycle *registry.Lifecycle
	// ReloadOperator runs the real Caddy reload command for the dashboard
	// action. When nil the reload action is unavailable.
	ReloadOperator registry.Operator
	// LogReader reads the configured Caddy log. When nil the log section is
	// unavailable.
	LogReader *caddyops.LogReader
	// Backups serves the read-only backup browser. When nil the backup
	// section is unavailable.
	Backups *backups.Browser
	// Dumper serves session-authenticated migration dumps. When nil the
	// dump action is unavailable.
	Dumper *dump.Dumper
	// Pinger probes registry-database readiness for GET /healthz. When nil
	// the control plane reports not ready, so a container never advertises
	// a healthy registry without a reachable database.
	Pinger Pinger
	// Now overrides the wall clock; nil means time.Now. Injected for tests.
	Now func() time.Time
}

// Pinger is the minimal database-readiness boundary satisfied by
// *pgxpool.Pool; injected so tests never connect to PostgreSQL.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Server holds the wired control-plane dependencies.
type Server struct {
	passcodeHash [sha256.Size]byte
	sessions     *session.Manager
	lifecycle    *registry.Lifecycle
	reloadOp     registry.Operator
	logs         *caddyops.LogReader
	backups      *backups.Browser
	dumper       *dump.Dumper
	pinger       Pinger
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
		lifecycle:    cfg.Lifecycle,
		reloadOp:     cfg.ReloadOperator,
		logs:         cfg.LogReader,
		backups:      cfg.Backups,
		dumper:       cfg.Dumper,
		pinger:       cfg.Pinger,
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
	mux.HandleFunc("GET /services/new", s.requireSession(s.handleNewServiceForm))
	mux.HandleFunc("POST /services", s.requireSession(s.requireCSRF(s.handleCreateService)))
	mux.HandleFunc("GET /services/{id}/edit", s.requireSession(s.handleEditServiceForm))
	mux.HandleFunc("POST /services/{id}", s.requireSession(s.requireCSRF(s.handleEditService)))
	mux.HandleFunc("POST /services/{id}/delete", s.requireSession(s.requireCSRF(s.handleDeleteService)))
	mux.HandleFunc("GET /backups/{name}", s.requireSession(s.handleBackupDownload))
	mux.HandleFunc("POST /caddy/reload", s.requireSession(s.requireCSRF(s.handleCaddyReload)))
	mux.HandleFunc("POST /services/{id}/dump", s.requireSession(s.requireCSRF(s.handleServiceDump)))
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("POST /logout", s.handleLogout)
	return mux
}

// handleHealthz reports registry readiness for the container healthcheck.
// It is intentionally unauthenticated and leaks nothing beyond a fixed
// readiness token: 200 only when the database answers a ping, so the
// control plane never advertises a ready registry before the database (and
// its applied schema) is actually usable.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if s.pinger == nil || s.pinger.Ping(ctx) != nil {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "ok\n")
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
	data := dashboardData{
		CSRF:         s.csrfFor(r),
		ReloadStatus: r.URL.Query().Get("reload"),
	}
	if s.lifecycle == nil {
		data.RegistryAvailable = false
	} else {
		data.RegistryAvailable = true
		services, err := s.lifecycle.List(r.Context())
		if err != nil {
			s.logger.Error("list services", "err", err)
			data.Error = "The service list could not be loaded."
		}
		data.Services = services
	}
	if s.logs != nil {
		entries, err := s.logs.Recent()
		if err != nil {
			s.logger.Info("caddy log unavailable", "err", err)
			data.LogsAvailable = false
		} else {
			data.LogsAvailable = true
			data.Logs = entries
		}
	}
	if s.backups != nil {
		files, err := s.backups.List()
		if err != nil {
			s.logger.Info("backup store unavailable", "err", err)
			data.BackupsAvailable = true
			data.BackupsError = "The backup store is not available."
		} else {
			data.BackupsAvailable = true
			data.Backups = files
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, "dashboard.html", data); err != nil {
		s.logger.Error("render dashboard", "err", err)
	}
}

// handleBackupDownload streams one listed backup as a download. The name
// comes only from the route segment, is validated as a safe basename, and
// is resolved inside the configured store; no request input can select a
// directory or arbitrary host path.
func (s *Server) handleBackupDownload(w http.ResponseWriter, r *http.Request) {
	if s.backups == nil {
		http.Error(w, "The backup store is not available in this build.", http.StatusServiceUnavailable)
		return
	}
	f, meta, err := s.backups.Open(r.PathValue("name"))
	if err != nil {
		s.logger.Info("backup download rejected", "err", err)
		http.Error(w, "The selected backup was not found.", http.StatusNotFound)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", meta.Name))
	w.Header().Set("Content-Length", strconv.FormatInt(meta.Size, 10))
	if _, err := io.Copy(w, f); err != nil {
		s.logger.Error("backup download stream failed", "err", err)
	}
}

// handleCaddyReload runs the injected Caddy validate then reload commands.
// Session and CSRF verification happen in the wrapping middleware before
// any command can execute. A validation failure must never reach the
// reload command and is reported through the failed PRG outcome.
func (s *Server) handleCaddyReload(w http.ResponseWriter, r *http.Request) {
	if s.reloadOp == nil {
		http.Error(w, "Caddy reload is not available in this build", http.StatusServiceUnavailable)
		return
	}
	if err := s.reloadOp.Validate(); err != nil {
		s.logger.Error("caddy validation failed before reload", "err", err)
		http.Redirect(w, r, "/dashboard?reload=failed", http.StatusSeeOther)
		return
	}
	if err := s.reloadOp.Reload(); err != nil {
		s.logger.Error("caddy reload failed", "err", err)
		http.Redirect(w, r, "/dashboard?reload=failed", http.StatusSeeOther)
		return
	}
	s.logger.Info("caddy reload succeeded via dashboard")
	http.Redirect(w, r, "/dashboard?reload=ok", http.StatusSeeOther)
}

// dashboardData renders the authenticated service list and Caddy logs.
type dashboardData struct {
	RegistryAvailable bool
	Services          []registry.Service
	CSRF              string
	Error             string
	ReloadStatus      string // "", "ok", or "failed" (from the PRG redirect)
	LogsAvailable     bool
	Logs              []caddyops.LogEntry
	BackupsAvailable  bool
	Backups           []backups.File
	BackupsError      string
}

// serviceFormData renders the create/edit service form.
type serviceFormData struct {
	Action         string
	CSRF           string
	IsEdit         bool
	ID             string
	Type           string
	Domains        string
	TLSMode        string
	ProxyContainer string
	ProxyPort      string
	StaticRoot     string
	Errors         []string
}

func (s *Server) handleNewServiceForm(w http.ResponseWriter, r *http.Request) {
	s.renderServiceForm(w, serviceFormData{Action: "/services", CSRF: s.csrfFor(r), Type: "proxy", TLSMode: string(registry.TLSACME)}, http.StatusOK)
}

func (s *Server) handleEditServiceForm(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	svc, err := s.lifecycle.Get(r.Context(), id)
	if errors.Is(err, registry.ErrNotFound) {
		http.Error(w, "service not found", http.StatusNotFound)
		return
	}
	if err != nil {
		s.logger.Error("get service", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.renderServiceForm(w, serviceFormData{
		Action: "/services/" + urlPathEscape(id), CSRF: s.csrfFor(r), IsEdit: true, ID: svc.ID,
		Type: string(svc.Type), Domains: strings.Join(svc.Domains, ", "), TLSMode: string(svc.TLSMode),
		ProxyContainer: svc.ProxyContainer, ProxyPort: strconv.Itoa(svc.ProxyPort), StaticRoot: svc.StaticRoot,
	}, http.StatusOK)
}

func (s *Server) renderServiceForm(w http.ResponseWriter, data serviceFormData, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := s.templates.ExecuteTemplate(w, "service_form.html", data); err != nil {
		s.logger.Error("render service form", "err", err)
	}
}

// requireCSRF rejects mutation requests whose CSRF token does not verify
// against the current session before any handler effect can occur.
func (s *Server) requireCSRF(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(session.CookieName)
		if err != nil {
			http.Error(w, "invalid CSRF token", http.StatusForbidden)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if err := s.sessions.VerifyCSRF(c.Value, r.PostFormValue("csrf_token"), s.now()); err != nil {
			s.logger.Info("rejected request with missing, forged, or expired CSRF token")
			http.Error(w, "invalid CSRF token", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

// csrfFor returns the CSRF token bound to the request's session, or "" when
// unavailable (forms then cannot be submitted successfully).
func (s *Server) csrfFor(r *http.Request) string {
	c, err := r.Cookie(session.CookieName)
	if err != nil || c.Value == "" {
		return ""
	}
	token, err := s.sessions.CSRFToken(c.Value, s.now())
	if err != nil {
		return ""
	}
	return token
}

// serviceFromForm builds a Service from owner form input. The service ID is
// never taken from the form: creation generates it server-side and edits
// only use the immutable path ID.
func serviceFromForm(r *http.Request, id string) (registry.Service, []string) {
	parseDomains := func(raw string) []string {
		var out []string
		for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '\n' || r == ' ' || r == '\t' }) {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
		return out
	}

	svc := registry.Service{ID: id}
	var errs []string
	svcType := r.PostFormValue("service_type")
	svc.Type = registry.ServiceType(svcType)
	svc.TLSMode = registry.TLSMode(r.PostFormValue("tls_mode"))
	svc.Domains = parseDomains(r.PostFormValue("domains"))

	switch svc.Type {
	case registry.TypeProxy:
		svc.ProxyContainer = r.PostFormValue("proxy_container")
		portRaw := r.PostFormValue("proxy_port")
		port, err := strconv.Atoi(portRaw)
		if err != nil {
			errs = append(errs, "Proxy port must be a number between 1 and 65535.")
		} else {
			svc.ProxyPort = port
		}
	case registry.TypeStatic:
		svc.StaticRoot = r.PostFormValue("static_root")
	default:
		errs = append(errs, "Service type must be proxy or static.")
	}

	if len(svc.Domains) == 0 {
		errs = append(errs, "At least one domain is required.")
	}
	return svc, errs
}

func (s *Server) handleCreateService(w http.ResponseWriter, r *http.Request) {
	if s.lifecycle == nil {
		http.Error(w, "service management is not available in this build", http.StatusServiceUnavailable)
		return
	}
	svc, fieldErrs := serviceFromForm(r, "")
	if len(fieldErrs) > 0 {
		s.renderServiceForm(w, s.formFromAttempt(r, serviceFormData{Action: "/services", Type: svcTypeForRetry(r)}, fieldErrs), http.StatusBadRequest)
		return
	}
	if provisionRequested(r) {
		if s.lifecycle.Provisioner == nil {
			http.Error(w, "Database provisioning is not available in this build.", http.StatusServiceUnavailable)
			return
		}
		created, cred, err := s.lifecycle.CreateProvisioned(r.Context(), svc)
		if err != nil {
			s.renderProvisioningCreateError(w, r, err)
			return
		}
		s.logger.Info("service created with provisioned database", "id", created.ID, "db", created.DBName)
		s.renderCredentials(w, credentialsData{
			DBName:   created.DBName,
			DBUser:   cred.DBUser,
			Password: cred.Password,
		})
		return
	}
	created, err := s.lifecycle.Create(r.Context(), svc)
	if err != nil {
		var comp *registry.CompensationError
		if errors.As(err, &comp) {
			s.logger.Error("create compensation failed", "err", err)
			http.Error(w, "The change could not be completed and its undo also failed. Manual inspection is required.", http.StatusInternalServerError)
			return
		}
		var verrs []string
		if ve, ok := err.(interface{ ValidationErrors() []string }); ok {
			verrs = ve.ValidationErrors()
		}
		if verrs == nil && isValidationError(err) {
			verrs = []string{englishValidationMessage(err)}
		}
		if verrs != nil {
			s.renderServiceForm(w, s.formFromAttempt(r, serviceFormData{Action: "/services", Type: svcTypeForRetry(r)}, verrs), http.StatusBadRequest)
			return
		}
		s.logger.Error("create service", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.logger.Info("service created", "id", created.ID)
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (s *Server) handleEditService(w http.ResponseWriter, r *http.Request) {
	if s.lifecycle == nil {
		http.Error(w, "service management is not available in this build", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	svc, fieldErrs := serviceFromForm(r, id)
	action := "/services/" + urlPathEscape(id)
	if len(fieldErrs) > 0 {
		s.renderServiceForm(w, s.formFromAttempt(r, serviceFormData{Action: action, IsEdit: true, ID: id, Type: svcTypeForRetry(r)}, fieldErrs), http.StatusBadRequest)
		return
	}
	_, err := s.lifecycle.Edit(r.Context(), svc)
	if err != nil {
		var comp *registry.CompensationError
		if errors.As(err, &comp) {
			s.logger.Error("edit compensation failed", "err", err)
			http.Error(w, "The change could not be completed and its undo also failed. Manual inspection is required.", http.StatusInternalServerError)
			return
		}
		if errors.Is(err, registry.ErrNotFound) {
			http.Error(w, "service not found", http.StatusNotFound)
			return
		}
		if isValidationError(err) {
			s.renderServiceForm(w, s.formFromAttempt(r, serviceFormData{Action: action, IsEdit: true, ID: id, Type: svcTypeForRetry(r)}, []string{englishValidationMessage(err)}), http.StatusBadRequest)
			return
		}
		s.logger.Error("edit service", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.logger.Info("service edited", "id", id)
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (s *Server) handleDeleteService(w http.ResponseWriter, r *http.Request) {
	if s.lifecycle == nil {
		http.Error(w, "service management is not available in this build", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	_, err := s.lifecycle.Delete(r.Context(), id)
	if err != nil {
		var comp *registry.CompensationError
		if errors.As(err, &comp) {
			s.logger.Error("delete compensation failed", "err", err)
			http.Error(w, "The deletion could not be completed and its undo also failed. Manual inspection is required.", http.StatusInternalServerError)
			return
		}
		if errors.Is(err, registry.ErrProvisionedService) {
			http.Error(w, "This service has a provisioned database. Automatic database decommissioning is not available yet; remove the database and role manually before deleting the service.", http.StatusConflict)
			return
		}
		if errors.Is(err, registry.ErrNotFound) {
			http.Error(w, "service not found", http.StatusNotFound)
			return
		}
		s.logger.Error("delete service", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.logger.Info("service deleted", "id", id)
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

// provisionRequested reports whether the owner opted in to project
// database provisioning on the create form.
func provisionRequested(r *http.Request) bool {
	return r.PostFormValue("provision_db") == "on"
}

// renderProvisioningCreateError maps provisioning creation failures to
// distinct English outcomes: validation feedback, failed-compensation
// notices, or the post-rollback provisioning failure. It never claims
// success.
func (s *Server) renderProvisioningCreateError(w http.ResponseWriter, r *http.Request, err error) {
	var comp *registry.CompensationError
	if errors.As(err, &comp) {
		s.logger.Error("provisioning compensation failed", "err", err)
		http.Error(w, "The change could not be completed and its undo also failed. Manual inspection is required.", http.StatusInternalServerError)
		return
	}
	if isValidationError(err) {
		s.renderServiceForm(w, s.formFromAttempt(r, serviceFormData{Action: "/services", Type: svcTypeForRetry(r)}, []string{englishValidationMessage(err)}), http.StatusBadRequest)
		return
	}
	s.logger.Error("provisioned service creation failed", "err", err)
	http.Error(w, "Database provisioning failed. The service has been removed and no partial state was kept; please try again.", http.StatusInternalServerError)
}

// credentialsData renders the one-time database credential page.
type credentialsData struct {
	DBName   string
	DBUser   string
	Password string
}

// renderCredentials emits the one-time credential page with no-store cache
// protections. There is no route that can re-display these values later.
func (s *Server) renderCredentials(w http.ResponseWriter, data credentialsData) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, "credentials.html", data); err != nil {
		s.logger.Error("render credentials page", "err", err)
	}
}

// handleServiceDump streams a pg_dump of a provisioned service's database.
// Session and session-bound CSRF verification happen in the wrapping
// middleware before any repository lookup, rate-limit mutation, or process
// start. No bearer credential is accepted.
func (s *Server) handleServiceDump(w http.ResponseWriter, r *http.Request) {
	if s.dumper == nil {
		http.Error(w, "Database dumps are not available in this build.", http.StatusServiceUnavailable)
		return
	}
	svc, err := s.lifecycle.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, registry.ErrNotFound) {
		http.Error(w, "service not found", http.StatusNotFound)
		return
	}
	if err != nil {
		s.logger.Error("resolve service for dump", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if svc.DBName == "" {
		http.Error(w, "This service has no provisioned database to dump.", http.StatusConflict)
		return
	}
	// Fail-closed identity boundary: the stored database identifiers must
	// be present and exactly match the values derivable from the immutable
	// service ID. Corrupted or missing metadata must never reach the dump
	// process as a command option. This check precedes the rate limiter and
	// any process action, so it consumes no quota.
	wantDB, wantUser, idErr := provision.Identifiers(svc.ID)
	if idErr != nil || svc.DBName == "" || svc.DBUser == "" || svc.DBName != wantDB || svc.DBUser != wantUser {
		s.logger.Error("dump rejected: inconsistent database identity", "service", svc.ID)
		http.Error(w, "This service has inconsistent database metadata; the dump was not started. Repair the service registration first.", http.StatusConflict)
		return
	}

	stream, wait, cancel, err := s.dumper.Start(r.Context(), svc.ID, svc.DBName)
	if err != nil {
		if errors.Is(err, dump.ErrRateLimited) {
			http.Error(w, "A dump for this service was already started. At most one dump per service is allowed every five minutes.", http.StatusTooManyRequests)
			return
		}
		s.logger.Error("dump process could not be started", "err", err)
		http.Error(w, "The dump process could not be started. Please try again.", http.StatusInternalServerError)
		return
	}
	defer cancel()

	// Headers are committed only after a successful start; the stream is
	// never buffered in full. On client disconnect the request context is
	// cancelled and the deferred hook terminates the child process.
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", svc.DBName+".dump"))
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, stream); err != nil {
		s.logger.Error("dump stream interrupted", "err", err)
	}
	if werr := wait(); werr != nil {
		// Post-header failure: the dump is incomplete. This is logged
		// without secrets and must not be interpreted as a completed dump.
		s.logger.Error("dump process did not complete", "err", werr)
	}
}

// formFromAttempt re-renders the service form with the submitted values and
// English validation feedback.
func (s *Server) formFromAttempt(r *http.Request, base serviceFormData, errs []string) serviceFormData {
	base.CSRF = s.csrfFor(r)
	base.Errors = errs
	if base.Type == "" {
		base.Type = r.PostFormValue("service_type")
	}
	if base.Domains == "" {
		base.Domains = r.PostFormValue("domains")
	}
	if base.TLSMode == "" {
		base.TLSMode = r.PostFormValue("tls_mode")
	}
	if base.ProxyContainer == "" {
		base.ProxyContainer = r.PostFormValue("proxy_container")
	}
	if base.ProxyPort == "" {
		base.ProxyPort = r.PostFormValue("proxy_port")
	}
	if base.StaticRoot == "" {
		base.StaticRoot = r.PostFormValue("static_root")
	}
	return base
}

func svcTypeForRetry(r *http.Request) string {
	t := r.PostFormValue("service_type")
	if t == "" {
		return "proxy"
	}
	return t
}

// isValidationError reports whether err came from domain/service validation
// (fail-closed input rejection) rather than an infrastructure failure.
func isValidationError(err error) bool {
	msg := err.Error()
	return strings.HasPrefix(msg, "registry: invalid ") || strings.HasPrefix(msg, "registry: duplicate ") ||
		strings.Contains(msg, "at least one domain") || strings.Contains(msg, "must stay inside") ||
		strings.Contains(msg, "static root") || strings.Contains(msg, "out of range") ||
		strings.Contains(msg, "port must be") || strings.Contains(msg, "service type") ||
		strings.Contains(msg, "TLS mode") || strings.Contains(msg, "must be empty for")
}

// englishValidationMessage converts an internal validation error into an
// owner-facing English message.
func englishValidationMessage(err error) string {
	return "The service details are not valid: " + err.Error()
}

func urlPathEscape(id string) string { return url.PathEscape(id) }

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
