package registry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrNotFound is returned by repository and lifecycle lookups when a
// service ID does not exist.
var ErrNotFound = errors.New("registry: service not found")

// ServiceRepository is the persistence boundary for the fresh services
// registry. Implementations must use parameterized queries only; no SQL is
// ever derived from owner form input.
type ServiceRepository interface {
	Create(ctx context.Context, s Service) error
	Update(ctx context.Context, s Service) error
	Delete(ctx context.Context, id string) error
	Get(ctx context.Context, id string) (Service, error)
	List(ctx context.Context) ([]Service, error)
}

// Executor is the minimal pgx surface the repository needs. Both
// *pgxpool.Pool and *pgx.Tx satisfy it, which keeps the repository
// injectable and testable without a live database.
type Executor interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// PostgresRepository is the pgx implementation of ServiceRepository.
type PostgresRepository struct {
	db Executor
}

// NewPostgresRepository returns a repository over the given executor.
func NewPostgresRepository(db Executor) *PostgresRepository {
	return &PostgresRepository{db: db}
}

// createSQL inserts one service row; created_at is database-defaulted.
const createSQL = `INSERT INTO services
	(id, service_type, domains, tls_mode, proxy_container, proxy_port, static_root, db_name, db_user)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`

// updateSQL changes all mutable columns of an existing service. The ID is
// immutable and only ever used as the key.
const updateSQL = `UPDATE services SET
	service_type = $2, domains = $3, tls_mode = $4, proxy_container = $5,
	proxy_port = $6, static_root = $7, db_name = $8, db_user = $9
	WHERE id = $1`

const deleteSQL = `DELETE FROM services WHERE id = $1`

// selectColumns coalesces nullable columns so Go code scans plain types.
const selectColumns = `id, service_type, domains, tls_mode,
	COALESCE(proxy_container, ''), COALESCE(proxy_port, 0),
	COALESCE(static_root, ''), COALESCE(db_name, ''), COALESCE(db_user, ''), created_at`

const getSQL = `SELECT ` + selectColumns + ` FROM services WHERE id = $1`

const listSQL = `SELECT ` + selectColumns + ` FROM services ORDER BY id`

// sqlOptional maps an absent optional value to SQL NULL; the schema's
// type-discriminant CHECK requires absent fields to be NULL, not zero
// values.
func sqlOptional(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// repositoryArgs maps a validated service to the positional parameters of
// createSQL/updateSQL. Fields not belonging to the service type are SQL
// NULL (proxy: no static_root; static: no proxy_container/proxy_port), as
// are blank optional DB identifiers.
func repositoryArgs(s Service) []any {
	var container, port, root any
	switch s.Type {
	case TypeProxy:
		container, port = s.ProxyContainer, int32(s.ProxyPort)
	case TypeStatic:
		root = s.StaticRoot
	}
	return []any{s.ID, string(s.Type), s.Domains, string(s.TLSMode), container, port, root,
		sqlOptional(s.DBName), sqlOptional(s.DBUser)}
}

// Create persists a new service. The service is validated first so invalid
// data can never reach SQL.
func (r *PostgresRepository) Create(ctx context.Context, s Service) error {
	if err := s.Validate(); err != nil {
		return err
	}
	_, err := r.db.Exec(ctx, createSQL, repositoryArgs(s)...)
	if err != nil {
		return fmt.Errorf("registry: insert service %q: %w", s.ID, err)
	}
	return nil
}

// Update persists changes to an existing service (keyed by its immutable
// ID). A zero-row result means the record does not exist and returns
// ErrNotFound instead of a false success.
func (r *PostgresRepository) Update(ctx context.Context, s Service) error {
	if err := s.Validate(); err != nil {
		return err
	}
	tag, err := r.db.Exec(ctx, updateSQL, repositoryArgs(s)...)
	if err != nil {
		return fmt.Errorf("registry: update service %q: %w", s.ID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("registry: update service %q: %w", s.ID, ErrNotFound)
	}
	return nil
}

// Delete removes the service record keyed by its immutable ID. A zero-row
// result means the record does not exist and returns ErrNotFound instead
// of a false success.
func (r *PostgresRepository) Delete(ctx context.Context, id string) error {
	if err := validateID(id); err != nil {
		return err
	}
	tag, err := r.db.Exec(ctx, deleteSQL, id)
	if err != nil {
		return fmt.Errorf("registry: delete service %q: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("registry: delete service %q: %w", id, ErrNotFound)
	}
	return nil
}

// Get returns one service by ID, or ErrNotFound.
func (r *PostgresRepository) Get(ctx context.Context, id string) (Service, error) {
	if err := validateID(id); err != nil {
		return Service{}, err
	}
	row := r.db.QueryRow(ctx, getSQL, id)
	var s Service
	var stype, tlsmode string
	var port int32
	if err := row.Scan(&s.ID, &stype, &s.Domains, &tlsmode,
		&s.ProxyContainer, &port, &s.StaticRoot, &s.DBName, &s.DBUser, &s.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Service{}, ErrNotFound
		}
		return Service{}, fmt.Errorf("registry: get service %q: %w", id, err)
	}
	s.Type, s.TLSMode, s.ProxyPort = ServiceType(stype), TLSMode(tlsmode), int(port)
	return s, nil
}

// List returns all registered services ordered by ID.
func (r *PostgresRepository) List(ctx context.Context) ([]Service, error) {
	rows, err := r.db.Query(ctx, listSQL)
	if err != nil {
		return nil, fmt.Errorf("registry: list services: %w", err)
	}
	defer rows.Close()

	var out []Service
	for rows.Next() {
		var s Service
		var stype, tlsmode string
		var port int32
		if err := rows.Scan(&s.ID, &stype, &s.Domains, &tlsmode,
			&s.ProxyContainer, &port, &s.StaticRoot, &s.DBName, &s.DBUser, &s.CreatedAt); err != nil {
			return nil, fmt.Errorf("registry: scan service row: %w", err)
		}
		s.Type, s.TLSMode, s.ProxyPort = ServiceType(stype), TLSMode(tlsmode), int(port)
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("registry: list services: %w", err)
	}
	return out, nil
}

// NewServiceID generates an opaque, immutable service identifier
// ("svc-" + 24 hex characters) from cryptographic entropy. IDs are never
// derived from user input.
func NewServiceID() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("registry: entropy failure: %w", err)
	}
	return "svc-" + hex.EncodeToString(b), nil
}
