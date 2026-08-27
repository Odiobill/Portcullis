package registry

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateRejectsDuplicateDomains(t *testing.T) {
	cases := map[string][]string{
		"exact-duplicate":        {"app.example.com", "app.example.com"},
		"case-collision":         {"App.Example.com", "app.example.com"},
		"trailing-dot-collision": {"app.example.com.", "app.example.com"},
		"three-way":              {"a.example.com", "A.example.com", "a.example.com."},
	}
	for name, domains := range cases {
		s := validProxyService()
		s.Domains = domains
		if err := s.Validate(); err == nil {
			t.Errorf("duplicate domains (%s) accepted: %v", name, domains)
		}
	}
}

func TestValidateAcceptsDistinctDomains(t *testing.T) {
	s := validProxyService()
	s.Domains = []string{"App.Example.com", "alt.example.com"}
	if err := s.Validate(); err != nil {
		t.Fatalf("distinct domains rejected: %v", err)
	}
}

func TestNormalizeDomains(t *testing.T) {
	got, err := NormalizeDomains([]string{"App.Example.COM.", "alt.example.com"})
	if err != nil {
		t.Fatalf("NormalizeDomains: %v", err)
	}
	want := []string{"app.example.com", "alt.example.com"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("normalized[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNormalizeDomainsRejectsCollisions(t *testing.T) {
	for _, domains := range [][]string{
		{"app.example.com", "app.example.com"},
		{"App.Example.com", "app.example.com"},
		{"app.example.com.", "APP.example.com"},
	} {
		if _, err := NormalizeDomains(domains); err == nil {
			t.Errorf("collision not rejected for %v", domains)
		}
	}
}

func TestNormalizeDomainsRejectsEmptyAndInvalid(t *testing.T) {
	if _, err := NormalizeDomains(nil); err == nil {
		t.Error("empty domain list accepted")
	}
	if _, err := NormalizeDomains([]string{"not a domain"}); err == nil {
		t.Error("invalid domain accepted by normalization")
	}
}

func TestNewServiceIDIsOpaqueAndUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id, err := NewServiceID()
		if err != nil {
			t.Fatalf("NewServiceID: %v", err)
		}
		if !strings.HasPrefix(id, "svc-") {
			t.Errorf("ID %q lacks opaque prefix", id)
		}
		if strings.ContainsAny(id, "{};'\"`\\ \n") {
			t.Errorf("ID %q contains unsafe characters", id)
		}
		if err := (Service{ID: id}).Validate(); err == nil {
			// Service.Validate on an otherwise-empty service fails on other
			// fields, so only assert the ID is within the allowed charset by
			// constructing a full service below.
		}
		s := validProxyService()
		s.ID = id
		if err := s.Validate(); err != nil {
			t.Errorf("generated ID %q rejected by validation: %v", id, err)
		}
		if seen[id] {
			t.Fatalf("duplicate generated ID %q", id)
		}
		seen[id] = true
	}
}

// Silence unused-import guards if adjustments are needed later.
var _ = errors.New
