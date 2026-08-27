package registry

import (
	"context"
	"errors"
	"fmt"
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
// the Slice-2 deploy path.
func (l *Lifecycle) Delete(ctx context.Context, id string) (Service, error) {
	if err := validateID(id); err != nil {
		return Service{}, err
	}
	prior, err := l.repo.Get(ctx, id)
	if err != nil {
		return Service{}, err
	}
	if err := l.store.Remove(id); err != nil {
		return Service{}, err
	}
	if err := l.repo.Delete(ctx, id); err != nil {
		if rerr := l.store.Deploy(prior); rerr != nil {
			return prior, &CompensationError{Operation: "delete", Primary: err, Compensate: rerr}
		}
		return prior, err
	}
	return prior, nil
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
