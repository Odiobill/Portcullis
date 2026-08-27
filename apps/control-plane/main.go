// Command control-plane is the Portcullis Go control plane (Slice 1).
// It serves the English login page, owner passcode login, an authenticated
// placeholder dashboard, and logout, with cryptographically verifiable
// expiring sessions per ADR-0002.
//
// Configuration fails closed: the process exits unless PORTCULLIS_PASSCODE
// and PORTCULLIS_SESSION_SECRET are set to acceptable values.
package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"portcullis/control-plane/internal/config"
	"portcullis/control-plane/internal/server"
	"portcullis/control-plane/internal/session"
)

func main() {
	if err := run(); err != nil {
		slog.Error("control-plane: startup failed", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}

	sessions, err := session.NewManager(cfg.SessionSecret, cfg.SessionTTL)
	if err != nil {
		return fmt.Errorf("session manager: %w", err)
	}

	addr := os.Getenv("PORTCULLIS_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	srv := server.New(server.Config{Passcode: cfg.Passcode, SessionManager: sessions})
	slog.Info("control-plane listening", "addr", addr)
	return http.ListenAndServe(addr, srv.Handler())
}
