package registry

import (
	"context"
	"errors"
	"fmt"

	"portcullis/control-plane/internal/provision"
)

// CompensationError reports an operation failure whose compensation also
// failed. It is always distinct from the plain operation error and must
// never be reported as success; the owner may need to inspect both the
// registry and the generated files manually.
type CompensationError struct {
	// Operation names the lifecycle operation ("create", "edit", "delete").
	Operation string
	// Primary is the original operation failure.
	Primary error
	// Compensate is the failure of the compensation itself.
	Compensate error
}

func (e *CompensationError) Error() string {
	return fmt.Sprintf("registry: %s failed and compensation also failed; manual inspection required: primary: %v; compensate: %v",
		e.Operation, e.Primary, e.Compensate)
}

func (e *CompensationError) Unwrap() error { return e.Primary }

// ErrCompensationFailed matches any CompensationError via errors.Is.
var ErrCompensationFailed = errors.New("registry: compensation failed")

func (e *CompensationError) Is(target error) bool { return target == ErrCompensationFailed }

// ErrProvisionedService is returned when an operation would affect a
// service that carries provisioned database identifiers, but the operation
// cannot do so safely (deletion: automatic database decommissioning is not
// implemented).
var ErrProvisionedService = errors.New("registry: service has a provisioned database; automatic database decommissioning is not available")

// Provisioner provisions one isolated project database and role for an
// opted-in service. Implemented by provision.PostgresAdmin; injected so
// tests never connect to PostgreSQL.
type Provisioner interface {
	Provision(ctx context.Context, spec provision.Spec) error
}

// Lifecycle orchestrates the authenticated service lifecycle across the
// repository and the rollback-safe generated-Caddyfile Store. Compensation
// ordering:
//
//   - create: persist, then deploy; on deploy failure the freshly persisted
//     record is removed (repo delete).
//   - edit: read prior, persist change, then deploy; on deploy failure the
//     prior record is restored (repo update with the prior record).
//   - delete: remove the generated Caddyfile first, then delete the record;
//     if the DB delete fails, the prior generated Caddyfile is restored via
//     the authoritative Slice-2 deploy path (never direct file writes).
//
// A compensation failure is surfaced as *CompensationError and never as
// success. sites/manual is never touched: the Store is scoped to the
// generated directory only.
type Lifecycle struct {
	repo  ServiceRepository
	store *Store
	newID func() string
	// Provisioner, when set, enables opt-in project database provisioning
	// during creation.
	Provisioner Provisioner
}

// NewLifecycle returns a Lifecycle. newID must generate opaque, unique
// service IDs; nil selects the built-in cryptographic generator.
func NewLifecycle(repo ServiceRepository, store *Store, newID func() string) *Lifecycle {
	if newID == nil {
		return &Lifecycle{repo: repo, store: store, newID: defaultNewID}
	}
	return &Lifecycle{repo: repo, store: store, newID: newID}
}

func defaultNewID() string {
	id, err := NewServiceID()
	if err != nil {
		// Entropy failure is a hard stop; panic is unacceptable in a
		// request path, so surface a deterministic unusable ID that fails
		// validation downstream.
		return ""
	}
	return id
}

// prepare normalizes domains and validates before any effect.
func prepare(s Service) (Service, error) {
	domains, err := NormalizeDomains(s.Domains)
	if err != nil {
		return Service{}, err
	}
	s.Domains = domains
	if err := s.Validate(); err != nil {
		return Service{}, err
	}
	return s, nil
}

// Create generates a server-side opaque ID (any form-supplied ID is
// ignored), persists the service, and deploys its generated Caddyfile. If
// deployment fails, the freshly persisted record is removed.
func (l *Lifecycle) Create(ctx context.Context, s Service) (Service, error) {
	s.ID = l.newID()
	prepared, err := prepare(s)
	if err != nil {
		return Service{}, err
	}
	if err := l.repo.Create(ctx, prepared); err != nil {
		return Service{}, err
	}
	if err := l.store.Deploy(prepared); err != nil {
		if cerr := l.repo.Delete(ctx, prepared.ID); cerr != nil {
			return Service{}, &CompensationError{Operation: "create", Primary: err, Compensate: cerr}
		}
		return Service{}, err
	}
	return prepared, nil
}

// Edit persists changes to an existing service and redeploys its generated
// Caddyfile. The prior record is preserved; if deployment fails, the prior
// record is restored.
func (l *Lifecycle) Edit(ctx context.Context, s Service) (Service, error) {
	prepared, err := prepare(s)
	if err != nil {
		return Service{}, err
	}
	prior, err := l.repo.Get(ctx, prepared.ID)
	if err != nil {
		return Service{}, err
	}
	if err := l.repo.Update(ctx, prepared); err != nil {
		return Service{}, err
	}
	if err := l.store.Deploy(prepared); err != nil {
		if cerr := l.repo.Update(ctx, prior); cerr != nil {
			return Service{}, &CompensationError{Operation: "edit", Primary: err, Compensate: cerr}
		}
		return Service{}, err
	}
	return prepared, nil
}

// Delete removes the generated Caddyfile first, then deletes the record. If
// the DB deletion fails, the prior generated Caddyfile is restored through
// the Slice-2 deploy path. Services with provisioned database identifiers
// fail closed before any effect: automatic database decommissioning is not
// implemented and deleting the record would orphan the database.
func (l *Lifecycle) Delete(ctx context.Context, id string) (Service, error) {
	if err := validateID(id); err != nil {
		return Service{}, err
	}
	prior, err := l.repo.Get(ctx, id)
	if err != nil {
		return Service{}, err
	}
	if prior.DBName != "" || prior.DBUser != "" {
		return prior, fmt.Errorf("%w", ErrProvisionedService)
	}
	return prior, l.deleteUnchecked(ctx, prior, id)
}

// deleteUnchecked performs the Caddyfile-removal-then-record-delete with
// restore. It is the shared implementation for owner-initiated deletion of
// unprovisioned services and for internal compensation of a partially
// provisioned creation (where removing the just-created database-less
// record is the required cleanup).
func (l *Lifecycle) deleteUnchecked(ctx context.Context, prior Service, id string) error {
	if err := l.store.Remove(id); err != nil {
		return err
	}
	if err := l.repo.Delete(ctx, id); err != nil {
		if rerr := l.store.Deploy(prior); rerr != nil {
			return &CompensationError{Operation: "delete", Primary: err, Compensate: rerr}
		}
		return err
	}
	return nil
}

// List returns all registered services.
func (l *Lifecycle) List(ctx context.Context) ([]Service, error) {
	return l.repo.List(ctx)
}

// Get returns one registered service.
func (l *Lifecycle) Get(ctx context.Context, id string) (Service, error) {
	if err := validateID(id); err != nil {
		return Service{}, err
	}
	return l.repo.Get(ctx, id)
}

// CreateProvisioned is the opt-in variant of Create: after the service is
// persisted and deployed, one isolated project database and role are
// provisioned with server-derived identifiers and a cryptographically
// random password. If provisioning fails, the accepted lifecycle
// compensation path removes the service again (generated Caddyfile first,
// then the record); a failed compensation surfaces as *CompensationError
// and is never success. The generated password is returned exactly once in
// the Credential and is never persisted or logged.
func (l *Lifecycle) CreateProvisioned(ctx context.Context, s Service) (Service, *provision.Credential, error) {
	if l.Provisioner == nil {
		return Service{}, nil, errors.New("registry: database provisioning is not available")
	}
	s.ID = l.newID()
	prepared, err := prepare(s)
	if err != nil {
		return Service{}, nil, err
	}
	dbName, dbUser, err := provision.Identifiers(prepared.ID)
	if err != nil {
		return Service{}, nil, err
	}
	prepared.DBName, prepared.DBUser = dbName, dbUser

	if err := l.repo.Create(ctx, prepared); err != nil {
		return Service{}, nil, err
	}
	if err := l.store.Deploy(prepared); err != nil {
		if cerr := l.repo.Delete(ctx, prepared.ID); cerr != nil {
			return Service{}, nil, &CompensationError{Operation: "create", Primary: err, Compensate: cerr}
		}
		return Service{}, nil, err
	}

	password, err := provision.GeneratePassword()
	if err != nil {
		return Service{}, nil, l.compensateProvisionedCreate(ctx, prepared, err)
	}
	spec := provision.Spec{DBName: dbName, UserName: dbUser, Password: password}
	if perr := l.Provisioner.Provision(ctx, spec); perr != nil {
		// Registry/Caddy compensation via the accepted delete path.
		delErr := l.deleteUnchecked(ctx, prepared, prepared.ID)
		var cleanup *provision.CleanupError
		isCleanupFailure := errors.As(perr, &cleanup)

		switch {
		case delErr != nil:
			// Registry/Caddy compensation failed: preserve both material
			// errors (including any provisioner cleanup failure inside
			// perr) as a failed compensation.
			primary := perr
			if isCleanupFailure {
				primary = cleanup
			}
			return Service{}, nil, &CompensationError{Operation: "create-provision", Primary: primary, Compensate: delErr}
		case isCleanupFailure:
			// Registry/Caddy cleanup succeeded, but the provisioner could
			// not remove its own partial database/role: this is a failed
			// compensation and must surface as manual-inspection evidence,
			// never as a complete-rollback claim.
			return Service{}, nil, &CompensationError{
				Operation:  "create-provision",
				Primary:    cleanup.Primary,
				Compensate: errors.Join(cleanup.Failures...),
			}
		default:
			return Service{}, nil, perr
		}
	}
	return prepared, &provision.Credential{DBName: dbName, DBUser: dbUser, Password: password}, nil
}

// compensateProvisionedCreate removes a partially provisioned creation via
// the accepted lifecycle delete path, surfacing failed compensation
// distinctly.
func (l *Lifecycle) compensateProvisionedCreate(ctx context.Context, prepared Service, primary error) error {
	if err := l.deleteUnchecked(ctx, prepared, prepared.ID); err != nil {
		return &CompensationError{Operation: "create-provision", Primary: primary, Compensate: err}
	}
	return primary
}
