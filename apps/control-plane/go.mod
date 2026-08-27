module portcullis/control-plane

go 1.27

// pgx is declared now (per Slice 2 of Projects/Portcullis/go-replacement-work-slices.md)
// as the deliberate persistence driver for the future database boundary:
// ADR-0001 requires pgx with explicit SQL migrations. Nothing imports it
// yet — Slice 2 adds only the versioned SQL migrations under migrations/
// and must not connect to, migrate, or reset any database. Slice 3 will
// import it for the repository layer.
require github.com/jackc/pgx/v5 v5.10.0

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	golang.org/x/text v0.29.0 // indirect
)
