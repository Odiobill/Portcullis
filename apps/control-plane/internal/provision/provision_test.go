package provision

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// fakeExecutor records statements and can fail specific call indexes.
type fakeExecutor struct {
	sql     []string
	execErr error
	// errOnCall, when > 0, applies execErr only to that 1-based Exec call.
	errOnCall int
	// failOn maps 1-based Exec call indexes to specific errors.
	failOn map[int]error
	calls  int
}

func (f *fakeExecutor) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	f.calls++
	f.sql = append(f.sql, sql)
	if err := f.failOn[f.calls]; err != nil {
		return pgconn.CommandTag{}, err
	}
	if f.execErr != nil && (f.errOnCall == 0 || f.calls == f.errOnCall) {
		return pgconn.CommandTag{}, f.execErr
	}
	return pgconn.NewCommandTag("CREATE 1"), nil
}

var errDatabaseCreate = errors.New("database create failed")

func validSpec() Spec {
	return Spec{DBName: "portcullis_abc123", UserName: "portcullis_abc123_u", Password: "abcdefghij0123456789ABCDEFGHIJ"}
}

func TestIdentifiersAreDeterministicAndValid(t *testing.T) {
	first1, user1, err := Identifiers("svc-abc123")
	if err != nil {
		t.Fatalf("Identifiers: %v", err)
	}
	again1, userAgain1, err := Identifiers("svc-abc123")
	if err != nil {
		t.Fatalf("Identifiers again: %v", err)
	}
	if first1 != again1 || user1 != userAgain1 {
		t.Errorf("identifiers not deterministic: %q/%q vs %q/%q", first1, user1, again1, userAgain1)
	}
	if first1 != "portcullis_abc123" {
		t.Errorf("dbName = %q", first1)
	}
	if user1 != "portcullis_abc123_u" {
		t.Errorf("userName = %q", user1)
	}
	if len(first1) > 63 || len(user1) > 63 {
		t.Error("identifiers exceed PostgreSQL's 63-byte limit")
	}

	other, userOther, err := Identifiers("svc-ffffff")
	if err != nil {
		t.Fatalf("Identifiers: %v", err)
	}
	if other == first1 || userOther == user1 {
		t.Error("distinct services must get distinct identifiers")
	}
}

func TestIdentifiersRejectMalformedServiceIDs(t *testing.T) {
	for _, id := range []string{"", "abc123", "svc-", "svc-ABC", "svc-ab c", "svc-ab;cd", "svc-ab\x00"} {
		if _, _, err := Identifiers(id); err == nil {
			t.Errorf("malformed service ID %q accepted", id)
		}
	}
}

func TestGeneratePassword(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		pw, err := GeneratePassword()
		if err != nil {
			t.Fatalf("GeneratePassword: %v", err)
		}
		if len(pw) < 24 {
			t.Errorf("password too short: %d chars", len(pw))
		}
		for _, c := range pw {
			isAlnum := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
			if !isAlnum {
				t.Errorf("password contains non-alphanumeric character %q", c)
			}
		}
		if seen[pw] {
			t.Fatal("password repeated")
		}
		seen[pw] = true
	}
}

func TestSpecValidateRejectsInjectionAndUnsafeIdentifiers(t *testing.T) {
	cases := []Spec{
		{DBName: `port; DROP TABLE services`, UserName: "u", Password: "pw"},
		{DBName: "db", UserName: `user"extra`, Password: "pw"},
		{DBName: "db", UserName: "user", Password: ""},
		{DBName: "1starts-with-digit", UserName: "user", Password: "pw"},
		{DBName: "has space", UserName: "user", Password: "pw"},
		{DBName: strings.Repeat("a", 64), UserName: "user", Password: "pw"},
	}
	for i, spec := range cases {
		if err := spec.Validate(); err == nil {
			t.Errorf("case %d: unsafe spec accepted: %+v", i, spec)
		}
	}
	if err := validSpec().Validate(); err != nil {
		t.Errorf("valid spec rejected: %v", err)
	}
}

func TestProvisionRunsStatementsInOrderWithQuotedIdentifiers(t *testing.T) {
	db := &fakeExecutor{}
	admin := NewPostgresAdmin(db)
	spec := validSpec()

	if err := admin.Provision(context.Background(), spec); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if len(db.sql) != 3 {
		t.Fatalf("statements = %d (%v), want 3", len(db.sql), db.sql)
	}
	roleStmt, dbStmt, grantStmt := db.sql[0], db.sql[1], db.sql[2]
	if !strings.HasPrefix(roleStmt, `CREATE ROLE "portcullis_abc123_u" LOGIN PASSWORD '`) {
		t.Errorf("role statement = %s", roleStmt)
	}
	if !strings.Contains(dbStmt, `CREATE DATABASE "portcullis_abc123" OWNER "portcullis_abc123_u"`) {
		t.Errorf("database statement = %s", dbStmt)
	}
	if !strings.Contains(grantStmt, `GRANT ALL PRIVILEGES ON DATABASE "portcullis_abc123" TO "portcullis_abc123_u"`) {
		t.Errorf("grant statement = %s", grantStmt)
	}
}

func TestProvisionPasswordBoundNotConcatenatedFromInput(t *testing.T) {
	db := &fakeExecutor{}
	admin := NewPostgresAdmin(db)
	spec := validSpec()
	if err := admin.Provision(context.Background(), spec); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	// The password literal must appear exactly once (in the role statement)
	// and never in the database or grant statements.
	if n := strings.Count(db.sql[0], spec.Password); n != 1 {
		t.Errorf("password appears %d times in role statement", n)
	}
	if strings.Contains(db.sql[1], spec.Password) || strings.Contains(db.sql[2], spec.Password) {
		t.Error("password leaked into non-role statements")
	}
}

func TestProvisionRejectsInvalidSpecBeforeSQL(t *testing.T) {
	db := &fakeExecutor{}
	admin := NewPostgresAdmin(db)
	spec := validSpec()
	spec.UserName = `evil"; DROP ROLE x`
	if err := admin.Provision(context.Background(), spec); err == nil {
		t.Fatal("invalid spec accepted")
	}
	if len(db.sql) != 0 {
		t.Errorf("SQL executed for invalid spec: %v", db.sql)
	}
}

func TestProvisionFailureNeverContainsPassword(t *testing.T) {
	boom := errors.New("permission denied")
	db := &fakeExecutor{execErr: boom, errOnCall: 2} // role created, database fails
	admin := NewPostgresAdmin(db)
	spec := validSpec()

	err := admin.Provision(context.Background(), spec)
	if err == nil {
		t.Fatal("provisioning failure must be surfaced")
	}
	if strings.Contains(err.Error(), spec.Password) {
		t.Error("error must never contain the generated password")
	}
	// Best-effort cleanup of the partially created role must be attempted.
	if !strings.Contains(db.sql[len(db.sql)-1], `DROP ROLE IF EXISTS`) {
		t.Errorf("partial role cleanup not attempted, last statement = %s", db.sql[len(db.sql)-1])
	}
}

func TestProvisionGrantFailureCleansUpDatabaseAndRole(t *testing.T) {
	db := &fakeExecutor{execErr: errors.New("grant failed"), errOnCall: 3}
	admin := NewPostgresAdmin(db)
	err := admin.Provision(context.Background(), validSpec())
	if err == nil {
		t.Fatal("grant failure must be surfaced")
	}
	joined := strings.Join(db.sql, " | ")
	if !strings.Contains(joined, `DROP DATABASE IF EXISTS`) || !strings.Contains(joined, `DROP ROLE IF EXISTS`) {
		t.Errorf("cleanup of partial database/role not attempted: %s", joined)
	}
}

// TestCleanupFailureIsSurfacedDistinctly pins the compensation-safety rule:
// a failed DROP of the provisioner's own partial role/database must yield a
// distinct inspectable CleanupError (carrying the primary failure and the
// cleanup failure) that never contains the generated password.
func TestCleanupFailureIsSurfacedDistinctly(t *testing.T) {
	// CREATE DATABASE fails; the compensating DROP ROLE also fails.
	db := &fakeExecutor{failOn: map[int]error{
		2: errDatabaseCreate,
		3: errors.New("drop role failed"),
	}}
	admin := NewPostgresAdmin(db)
	spec := validSpec()

	err := admin.Provision(context.Background(), spec)
	var cerr *CleanupError
	if !errors.As(err, &cerr) {
		t.Fatalf("failed cleanup must surface as *CleanupError, got %v", err)
	}
	if !errors.Is(err, errDatabaseCreate) {
		t.Errorf("primary failure not preserved: %v", err)
	}
	if len(cerr.Failures) != 1 {
		t.Errorf("cleanup failures = %v, want exactly one", cerr.Failures)
	}
	if !strings.Contains(cerr.Error(), "drop role failed") {
		t.Errorf("cleanup failure detail missing: %s", cerr.Error())
	}
	if !strings.Contains(cerr.Error(), "manual inspection") {
		t.Errorf("cleanup error must demand manual inspection: %s", cerr.Error())
	}
	if strings.Contains(cerr.Error(), spec.Password) {
		t.Error("cleanup error must never contain the generated password")
	}
}

// TestGrantCleanupFailureSurfacesBothFailures covers the two-statement
// cleanup path: both compensating DROPs failing must preserve both
// material errors.
func TestGrantCleanupFailureSurfacesBothFailures(t *testing.T) {
	db := &fakeExecutor{failOn: map[int]error{
		3: errors.New("grant failed"),
		4: errors.New("drop database failed"),
		5: errors.New("drop role failed"),
	}}
	admin := NewPostgresAdmin(db)
	err := admin.Provision(context.Background(), validSpec())
	var cerr *CleanupError
	if !errors.As(err, &cerr) {
		t.Fatalf("failed cleanup must surface as *CleanupError, got %v", err)
	}
	if len(cerr.Failures) != 2 {
		t.Errorf("cleanup failures = %d, want 2", len(cerr.Failures))
	}
	if strings.Contains(cerr.Error(), validSpec().Password) {
		t.Error("cleanup error must never contain the generated password")
	}
}

// TestCleanupSuccessReturnsPlainPrimary pins that successful cleanup keeps
// the plain primary error (no false compensation signal).
func TestCleanupSuccessReturnsPlainPrimary(t *testing.T) {
	db := &fakeExecutor{execErr: errDatabaseCreate, errOnCall: 2}
	admin := NewPostgresAdmin(db)
	err := admin.Provision(context.Background(), validSpec())
	var cerr *CleanupError
	if errors.As(err, &cerr) {
		t.Fatalf("successful cleanup must not produce CleanupError, got %v", err)
	}
	if !errors.Is(err, errDatabaseCreate) {
		t.Errorf("primary failure not preserved: %v", err)
	}
}
