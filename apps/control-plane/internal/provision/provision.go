// Package provision implements the opt-in project database provisioning
// boundary: server-owned identifier derivation, cryptographically random
// password generation, and the PostgreSQL administration statements used to
// create one isolated database and role. Identifiers are strictly
// validated and quoted (doubling embedded quotes); the password is
// server-generated from an alphanumeric charset (so the SQL literal cannot
// be injected) and additionally single-quote-escaped defensively —
// PostgreSQL utility statements do not accept bind parameters for
// PASSWORD, which is why a bound literal is not possible here. The
// password is never logged and never appears in returned errors.
package provision

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// passwordLength yields ~190 bits of entropy over the alphanumeric charset.
const passwordLength = 32

// maxIdentifierLen is PostgreSQL's identifier byte limit.
const maxIdentifierLen = 63

var (
	serviceIDPattern = regexp.MustCompile(`^svc-[a-z0-9]+$`)
	identifierRuneOK = func(c rune) bool {
		return (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_'
	}
)

// Spec is one provisioning request. DBName/UserName are server-derived;
// Password is server-generated.
type Spec struct {
	DBName   string
	UserName string
	Password string
}

// Validate rejects unsafe identifiers and empty passwords before any SQL.
func (s Spec) Validate() error {
	for name, v := range map[string]string{"database name": s.DBName, "user name": s.UserName} {
		if v == "" {
			return fmt.Errorf("provision: %s must not be empty", name)
		}
		if len(v) > maxIdentifierLen {
			return fmt.Errorf("provision: %s exceeds %d characters", name, maxIdentifierLen)
		}
		if !identifierRuneOK(rune(v[0])) || v[0] == '_' {
			return fmt.Errorf("provision: %s must start with a lowercase letter", name)
		}
		for _, c := range v {
			if !identifierRuneOK(c) {
				return fmt.Errorf("provision: %s contains character %q", name, c)
			}
		}
	}
	if s.Password == "" {
		return errors.New("provision: password must not be empty")
	}
	return nil
}

// Identifiers derives the deterministic, server-owned database and role
// names from an opaque service ID ("svc-<lowercase alnum>"). The owner
// cannot submit or override them.
func Identifiers(serviceID string) (dbName, userName string, err error) {
	if !serviceIDPattern.MatchString(serviceID) {
		return "", "", fmt.Errorf("provision: service ID %q cannot derive database identifiers", serviceID)
	}
	suffix := strings.TrimPrefix(serviceID, "svc-")
	dbName = "portcullis_" + suffix
	userName = "portcullis_" + suffix + "_u"
	if len(dbName) > maxIdentifierLen || len(userName) > maxIdentifierLen {
		return "", "", fmt.Errorf("provision: derived identifiers exceed %d characters", maxIdentifierLen)
	}
	return dbName, userName, nil
}

// GeneratePassword returns a cryptographically random alphanumeric
// password. The charset excludes quotes and backslashes so the SQL literal
// cannot be injected; escaping is applied defensively on top.
func GeneratePassword() (string, error) {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	out := make([]byte, passwordLength)
	buf := make([]byte, passwordLength)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("provision: entropy failure: %w", err)
	}
	for i, b := range buf {
		out[i] = charset[int(b)%len(charset)]
	}
	return string(out), nil
}

// Credential is the one-time provisioning payload shown to the owner
// exactly once. It is never persisted.
type Credential struct {
	DBName   string
	DBUser   string
	Password string
}

// Executor is the minimal pgx surface the administrator needs; both
// *pgxpool.Pool and *pgx.Tx satisfy it.
type Executor interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// PostgresAdmin is the PostgreSQL provisioning implementation.
type PostgresAdmin struct {
	db Executor
}

// NewPostgresAdmin returns an administrator over the injected executor.
func NewPostgresAdmin(db Executor) *PostgresAdmin {
	return &PostgresAdmin{db: db}
}

// quoteIdent renders a strictly validated identifier as a quoted SQL
// identifier, doubling embedded quotes defensively.
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// escapePassword renders a password as a quoted SQL literal, doubling
// embedded single quotes defensively (the generated charset excludes them).
func escapePassword(password string) string {
	return `'` + strings.ReplaceAll(password, "'", "''") + `'`
}

// Provision creates the role, the database owned by it, and the privilege
// grant, in that order. On failure the partially created objects are
// dropped best-effort (this is compensation of the provisioner's own
// partial work, not service decommission) and the primary error — which
// never contains the password — is returned.
func (a *PostgresAdmin) Provision(ctx context.Context, spec Spec) error {
	if err := spec.Validate(); err != nil {
		return err
	}

	createRole := fmt.Sprintf(`CREATE ROLE %s LOGIN PASSWORD %s`,
		quoteIdent(spec.UserName), escapePassword(spec.Password))
	createDB := fmt.Sprintf(`CREATE DATABASE %s OWNER %s`,
		quoteIdent(spec.DBName), quoteIdent(spec.UserName))
	grant := fmt.Sprintf(`GRANT ALL PRIVILEGES ON DATABASE %s TO %s`,
		quoteIdent(spec.DBName), quoteIdent(spec.UserName))

	if _, err := a.db.Exec(ctx, createRole); err != nil {
		return fmt.Errorf("provision: create role: %w", err)
	}
	if _, err := a.db.Exec(ctx, createDB); err != nil {
		a.cleanup(ctx, fmt.Sprintf(`DROP ROLE IF EXISTS %s`, quoteIdent(spec.UserName)))
		return fmt.Errorf("provision: create database: %w", err)
	}
	if _, err := a.db.Exec(ctx, grant); err != nil {
		a.cleanup(ctx,
			fmt.Sprintf(`DROP DATABASE IF EXISTS %s`, quoteIdent(spec.DBName)),
			fmt.Sprintf(`DROP ROLE IF EXISTS %s`, quoteIdent(spec.UserName)))
		return fmt.Errorf("provision: grant privileges: %w", err)
	}
	return nil
}

// cleanup best-effort executes compensating statements; secondary failures
// are intentionally ignored because the primary provisioning error is what
// the caller must observe.
func (a *PostgresAdmin) cleanup(ctx context.Context, statements ...string) {
	for _, stmt := range statements {
		_, _ = a.db.Exec(ctx, stmt)
	}
}
