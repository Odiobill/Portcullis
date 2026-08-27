package registry

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// fakeRows implements pgx.Rows over in-memory services (no database).
type fakeRows struct {
	services []Service
	i        int
	err      error
	closed   bool
}

func (r *fakeRows) Next() bool { r.i++; return r.i <= len(r.services) }

func (r *fakeRows) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if r.i < 1 || r.i > len(r.services) {
		return errors.New("fakeRows: no row")
	}
	s := r.services[r.i-1]
	if len(dest) != 10 {
		return fmt.Errorf("fakeRows: want 10 dest, got %d", len(dest))
	}
	type assign struct {
		dst  any
		want any
	}
	pairs := []assign{
		{dest[0], s.ID},
		{dest[1], string(s.Type)},
		{dest[2], s.Domains},
		{dest[3], string(s.TLSMode)},
		{dest[4], s.ProxyContainer},
		{dest[5], int32(s.ProxyPort)},
		{dest[6], s.StaticRoot},
		{dest[7], s.DBName},
		{dest[8], s.DBUser},
		{dest[9], s.CreatedAt},
	}
	for _, p := range pairs {
		dst := reflect.ValueOf(p.dst)
		if dst.Kind() != reflect.Pointer || dst.IsNil() {
			return fmt.Errorf("fakeRows: dest must be non-nil pointer, got %T", p.dst)
		}
		dst.Elem().Set(reflect.ValueOf(p.want))
	}
	return nil
}

func (r *fakeRows) Values() ([]any, error)                       { return nil, nil }
func (r *fakeRows) RawValues() [][]byte                          { return nil }
func (r *fakeRows) Conn() *pgx.Conn                              { return nil }
func (r *fakeRows) Err() error                                   { return r.err }
func (r *fakeRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeRows) Close()                                       { r.closed = true }

// fakeRow implements pgx.Row.
type fakeRow struct {
	service Service
	err     error
}

func (r *fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	fr := &fakeRows{services: []Service{r.service}}
	fr.i = 1 // position on the single row
	return fr.Scan(dest...)
}

// fakeExecutor implements the repository's minimal pgx surface, recording
// SQL and arguments.
type fakeExecutor struct {
	execSQL  []string
	execArgs [][]any
	execErr  error
	// execTag, when set, is the command tag returned by Exec (nil = a
	// one-row tag).
	execTag *pgconn.CommandTag

	querySQL []string
	queryArg [][]any
	queryRes *fakeRows
	queryErr error

	rowRes *fakeRow
	rowSQL []string
	rowArg [][]any
}

func (f *fakeExecutor) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.execSQL = append(f.execSQL, sql)
	f.execArgs = append(f.execArgs, args)
	if f.execErr != nil {
		return pgconn.CommandTag{}, f.execErr
	}
	if f.execTag != nil {
		return *f.execTag, nil
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func (f *fakeExecutor) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	f.querySQL = append(f.querySQL, sql)
	f.queryArg = append(f.queryArg, args)
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	return f.queryRes, nil
}

func (f *fakeExecutor) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	f.rowSQL = append(f.rowSQL, sql)
	f.rowArg = append(f.rowArg, args)
	return f.rowRes
}

func repoService() Service {
	return Service{
		ID:             "svc-alpha",
		Type:           TypeProxy,
		Domains:        []string{"app.example.com"},
		TLSMode:        TLSACME,
		ProxyContainer: "app_container",
		ProxyPort:      3000,
		CreatedAt:      time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC),
	}
}

func TestRepoCreateMapsProxyOptionalFieldsToSQLNull(t *testing.T) {
	db := &fakeExecutor{}
	repo := NewPostgresRepository(db)
	s := repoService() // proxy: no static root; blank optional DB identifiers

	if err := repo.Create(context.Background(), s); err != nil {
		t.Fatalf("Create: %v", err)
	}
	wantArgs := []any{s.ID, string(TypeProxy), s.Domains, string(TLSACME), s.ProxyContainer, int32(s.ProxyPort), nil, nil, nil}
	if !reflect.DeepEqual(db.execArgs[0], wantArgs) {
		t.Errorf("proxy args = %v, want %v (static_root, db_name, db_user must be SQL NULL)", db.execArgs[0], wantArgs)
	}
}

func TestRepoCreateMapsStaticOptionalFieldsToSQLNull(t *testing.T) {
	db := &fakeExecutor{}
	repo := NewPostgresRepository(db)
	s := validStaticService() // static: no proxy container/port

	if err := repo.Create(context.Background(), s); err != nil {
		t.Fatalf("Create: %v", err)
	}
	wantArgs := []any{s.ID, string(TypeStatic), s.Domains, string(TLSInternal), nil, nil, s.StaticRoot, nil, nil}
	if !reflect.DeepEqual(db.execArgs[0], wantArgs) {
		t.Errorf("static args = %v, want %v (proxy_container, proxy_port, db identifiers must be SQL NULL)", db.execArgs[0], wantArgs)
	}
}

func TestRepoUpdateMapsOptionalFieldsToSQLNull(t *testing.T) {
	db := &fakeExecutor{}
	repo := NewPostgresRepository(db)
	proxy := repoService()
	static := validStaticService()

	if err := repo.Update(context.Background(), proxy); err != nil {
		t.Fatalf("Update(proxy): %v", err)
	}
	wantProxy := []any{proxy.ID, string(TypeProxy), proxy.Domains, string(TLSACME), proxy.ProxyContainer, int32(proxy.ProxyPort), nil, nil, nil}
	if !reflect.DeepEqual(db.execArgs[0], wantProxy) {
		t.Errorf("proxy update args = %v, want %v", db.execArgs[0], wantProxy)
	}
	if err := repo.Update(context.Background(), static); err != nil {
		t.Fatalf("Update(static): %v", err)
	}
	wantStatic := []any{static.ID, string(TypeStatic), static.Domains, string(TLSInternal), nil, nil, static.StaticRoot, nil, nil}
	if !reflect.DeepEqual(db.execArgs[1], wantStatic) {
		t.Errorf("static update args = %v, want %v", db.execArgs[1], wantStatic)
	}
}

func TestRepoCreateMapsPresentOptionalDBIdentifiers(t *testing.T) {
	db := &fakeExecutor{}
	repo := NewPostgresRepository(db)
	s := repoService()
	s.DBName = "svc_alpha_db"
	s.DBUser = "svc_alpha_user"
	if err := repo.Create(context.Background(), s); err != nil {
		t.Fatalf("Create: %v", err)
	}
	wantArgs := []any{s.ID, string(TypeProxy), s.Domains, string(TLSACME), s.ProxyContainer, int32(s.ProxyPort), nil, "svc_alpha_db", "svc_alpha_user"}
	if !reflect.DeepEqual(db.execArgs[0], wantArgs) {
		t.Errorf("args = %v, want %v (present DB identifiers must be passed through)", db.execArgs[0], wantArgs)
	}
}

func TestRepoUpdateZeroRowsReturnsNotFound(t *testing.T) {
	zero := pgconn.NewCommandTag("UPDATE 0")
	db := &fakeExecutor{execTag: &zero}
	repo := NewPostgresRepository(db)
	err := repo.Update(context.Background(), repoService())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("zero-row Update must return ErrNotFound, got %v", err)
	}
}

func TestRepoDeleteZeroRowsReturnsNotFound(t *testing.T) {
	zero := pgconn.NewCommandTag("DELETE 0")
	db := &fakeExecutor{execTag: &zero}
	repo := NewPostgresRepository(db)
	err := repo.Delete(context.Background(), "svc-alpha")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("zero-row Delete must return ErrNotFound, got %v", err)
	}
}

func TestRepoUpdatePreservesExecutionError(t *testing.T) {
	boom := errors.New("db down")
	db := &fakeExecutor{execErr: boom}
	repo := NewPostgresRepository(db)
	if err := repo.Update(context.Background(), repoService()); !errors.Is(err, boom) {
		t.Fatalf("want wrapped execution error, got %v", err)
	}
}

func TestRepoUpdateOneRowSucceeds(t *testing.T) {
	one := pgconn.NewCommandTag("UPDATE 1")
	db := &fakeExecutor{execTag: &one}
	repo := NewPostgresRepository(db)
	if err := repo.Update(context.Background(), repoService()); err != nil {
		t.Fatalf("one-row Update must succeed, got %v", err)
	}
}

func TestRepoDeleteOneRowSucceeds(t *testing.T) {
	one := pgconn.NewCommandTag("DELETE 1")
	db := &fakeExecutor{execTag: &one}
	repo := NewPostgresRepository(db)
	if err := repo.Delete(context.Background(), "svc-alpha"); err != nil {
		t.Fatalf("one-row Delete must succeed, got %v", err)
	}
}

func TestRepoCreateUsesParameterizedInsert(t *testing.T) {
	db := &fakeExecutor{}
	repo := NewPostgresRepository(db)
	s := repoService()

	if err := repo.Create(context.Background(), s); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(db.execSQL) != 1 {
		t.Fatalf("expected 1 exec, got %d", len(db.execSQL))
	}
	sql := db.execSQL[0]
	if !strings.Contains(sql, "INSERT INTO services") {
		t.Errorf("SQL is not an INSERT INTO services: %s", sql)
	}
	if !strings.Contains(sql, "$1") || !strings.Contains(sql, "$9") {
		t.Errorf("SQL is not fully parameterized: %s", sql)
	}
	if strings.Contains(sql, s.ID) || strings.Contains(sql, "app.example.com") {
		t.Error("user data must not be interpolated into SQL text")
	}
	wantArgs := []any{s.ID, string(TypeProxy), s.Domains, string(TLSACME), s.ProxyContainer, int32(s.ProxyPort), nil, nil, nil}
	if !reflect.DeepEqual(db.execArgs[0], wantArgs) {
		t.Errorf("args = %v, want %v", db.execArgs[0], wantArgs)
	}
}

func TestRepoCreateRejectsInvalidServiceBeforeSQL(t *testing.T) {
	db := &fakeExecutor{}
	repo := NewPostgresRepository(db)
	s := repoService()
	s.Domains = []string{"not a domain"}
	if err := repo.Create(context.Background(), s); err == nil {
		t.Fatal("invalid service accepted by repository")
	}
	if len(db.execSQL) != 0 {
		t.Error("SQL executed for invalid service")
	}
}

func TestRepoCreatePropagatesExecError(t *testing.T) {
	boom := errors.New("db down")
	db := &fakeExecutor{execErr: boom}
	repo := NewPostgresRepository(db)
	if err := repo.Create(context.Background(), repoService()); !errors.Is(err, boom) {
		t.Fatalf("want original error, got %v", err)
	}
}

func TestRepoUpdateUsesParameterizedUpdate(t *testing.T) {
	db := &fakeExecutor{}
	repo := NewPostgresRepository(db)
	s := repoService()
	if err := repo.Update(context.Background(), s); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(db.execSQL) != 1 || !strings.Contains(db.execSQL[0], "UPDATE services SET") {
		t.Fatalf("SQL is not an UPDATE: %v", db.execSQL)
	}
	if !strings.Contains(db.execSQL[0], "WHERE id = $1") {
		t.Errorf("UPDATE must key on the immutable ID: %s", db.execSQL[0])
	}
}

func TestRepoDeleteUsesParameterizedDelete(t *testing.T) {
	db := &fakeExecutor{}
	repo := NewPostgresRepository(db)
	if err := repo.Delete(context.Background(), "svc-alpha"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(db.execSQL) != 1 || !strings.Contains(db.execSQL[0], "DELETE FROM services WHERE id = $1") {
		t.Fatalf("SQL is not a keyed DELETE: %v", db.execSQL)
	}
	if !reflect.DeepEqual(db.execArgs[0], []any{"svc-alpha"}) {
		t.Errorf("args = %v", db.execArgs[0])
	}
}

func TestRepoGetScansRow(t *testing.T) {
	s := repoService()
	db := &fakeExecutor{rowRes: &fakeRow{service: s}}
	repo := NewPostgresRepository(db)
	got, err := repo.Get(context.Background(), "svc-alpha")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != s.ID || got.Type != s.Type || got.TLSMode != s.TLSMode ||
		got.ProxyContainer != s.ProxyContainer || got.ProxyPort != s.ProxyPort {
		t.Errorf("scanned service mismatch: %+v", got)
	}
	if !strings.Contains(db.rowSQL[0], "SELECT") || !strings.Contains(db.rowSQL[0], "WHERE id = $1") {
		t.Errorf("Get SQL unexpected: %s", db.rowSQL[0])
	}
}

func TestRepoGetNotFound(t *testing.T) {
	db := &fakeExecutor{rowRes: &fakeRow{err: pgx.ErrNoRows}}
	repo := NewPostgresRepository(db)
	_, err := repo.Get(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestRepoListScansRows(t *testing.T) {
	a := repoService()
	b := validStaticService()
	db := &fakeExecutor{queryRes: &fakeRows{services: []Service{a, b}}}
	repo := NewPostgresRepository(db)
	got, err := repo.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 || got[0].ID != a.ID || got[1].ID != b.ID {
		t.Errorf("List = %+v", got)
	}
	if len(db.querySQL) != 1 || strings.Contains(db.querySQL[0], "WHERE") {
		t.Errorf("List must select all services: %s", db.querySQL[0])
	}
}

func TestRepoListPropagatesQueryError(t *testing.T) {
	boom := errors.New("query failed")
	db := &fakeExecutor{queryErr: boom}
	repo := NewPostgresRepository(db)
	if _, err := repo.List(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("want original error, got %v", err)
	}
}
