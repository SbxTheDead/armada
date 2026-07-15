package domain

import "errors"

// Sentinel errors shared across the domain. Transport layers (HTTP, gRPC) map
// these to protocol-specific status codes so that business logic never needs to
// import a transport package.
var (
	// ErrNotFound is returned when a requested aggregate does not exist.
	ErrNotFound = errors.New("resource not found")

	// ErrAlreadyExists is returned on a uniqueness violation (e.g. duplicate
	// system FQDN within a tenant).
	ErrAlreadyExists = errors.New("resource already exists")

	// ErrValidation wraps input that failed a domain invariant. Callers should
	// use errors.Is(err, ErrValidation) and surface the wrapped message.
	ErrValidation = errors.New("validation failed")

	// ErrEnrollmentToken is returned when an enrollment token is unknown,
	// expired, revoked, or already consumed.
	ErrEnrollmentToken = errors.New("invalid enrollment token")

	// ErrUnauthorized is returned when an agent presents credentials that do
	// not match a known, active system identity.
	ErrUnauthorized = errors.New("unauthorized")
)
