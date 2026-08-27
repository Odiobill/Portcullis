package config

import "testing"

func TestLoadFailsClosedOnMissingPasscode(t *testing.T) {
	t.Setenv("PORTCULLIS_PASSCODE", "")
	t.Setenv("PORTCULLIS_SESSION_SECRET", "some-secret-at-least-32-bytes-long!!")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error when PORTCULLIS_PASSCODE is missing/empty, got nil")
	}
}

func TestLoadFailsClosedOnMissingSessionSecret(t *testing.T) {
	t.Setenv("PORTCULLIS_PASSCODE", "correct-passcode")
	t.Setenv("PORTCULLIS_SESSION_SECRET", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error when PORTCULLIS_SESSION_SECRET is missing/empty, got nil")
	}
}

func TestLoadAcceptsPresentConfig(t *testing.T) {
	t.Setenv("PORTCULLIS_PASSCODE", "correct-passcode")
	t.Setenv("PORTCULLIS_SESSION_SECRET", "some-secret-at-least-32-bytes-long!!")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected success with both variables set, got %v", err)
	}
	if cfg.Passcode != "correct-passcode" {
		t.Errorf("Passcode = %q, want %q", cfg.Passcode, "correct-passcode")
	}
	if cfg.SessionSecret == "" {
		t.Error("SessionSecret must not be empty")
	}
	if cfg.SessionTTL <= 0 {
		t.Errorf("SessionTTL = %v, want a positive default", cfg.SessionTTL)
	}
}

func TestLoadRejectsShortSessionSecret(t *testing.T) {
	t.Setenv("PORTCULLIS_PASSCODE", "correct-passcode")
	t.Setenv("PORTCULLIS_SESSION_SECRET", "short")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for a too-short session secret, got nil")
	}
}
