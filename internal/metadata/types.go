package metadata

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/DeanoC/FogCast-POC/protocol"
)

// ProviderName identifies an upstream adapter without exposing its wire model.
type ProviderName string

const ProviderIGDB ProviderName = "igdb"

type LookupInput struct {
	Title  string
	System protocol.System
}

type ProviderQuery struct {
	NormalizedTitle string
	System          protocol.System
	Region          string
}

type ProviderResult struct {
	PlatformID string
	Candidates []Candidate
}

type ArtworkRole string

const (
	ArtworkCover    ArtworkRole = "cover"
	ArtworkBackdrop ArtworkRole = "backdrop"
)

type ArtworkRef struct {
	Role ArtworkRole
	ID   string
}

type Candidate struct {
	ProviderID       string
	Name             string
	AlternativeNames []string
	PlatformIDs      []string
	Summary          string
	FirstReleaseYear int
	Genres           []string
	Studios          []string
	Players          string
	Artwork          []ArtworkRef
	UpdatedAt        time.Time
	Checksum         string
}

type Provider interface {
	Name() ProviderName
	Lookup(context.Context, ProviderQuery) (ProviderResult, error)
}

type Outcome string

const (
	OutcomeExact     Outcome = "exact"
	OutcomeConfident Outcome = "confident"
	OutcomeAmbiguous Outcome = "ambiguous"
	OutcomeNoMatch   Outcome = "no_match"
)

type Presentation struct {
	Summary           string
	Year              string
	Genre             string
	Studio            string
	Players           string
	CoverArtworkID    string
	BackdropArtworkID string
}

type Result struct {
	Outcome      Outcome
	Presentation Presentation
	Attribution  Attribution
}

type Attribution struct {
	Provider ProviderName
	Label    string
}

type Runtime interface {
	Lookup(context.Context, LookupInput) (Result, error)
	OpenArtwork(context.Context, string) (Artwork, error)
	Close() error
}

type Artwork struct {
	MIME   string
	Size   int64
	Reader io.ReadCloser
}

type ErrorCode string

const (
	ErrDisabled            ErrorCode = "disabled"
	ErrUnconfigured        ErrorCode = "unconfigured"
	ErrUnauthorized        ErrorCode = "unauthorized"
	ErrRateLimited         ErrorCode = "rate_limited"
	ErrUpstreamUnavailable ErrorCode = "upstream_unavailable"
	ErrInvalidResponse     ErrorCode = "invalid_response"
	ErrPolicyBlocked       ErrorCode = "policy_blocked"
	ErrProviderRemoved     ErrorCode = "provider_removed"
	ErrStorage             ErrorCode = "storage_failure"
	ErrCanceled            ErrorCode = "canceled"
	ErrDeadline            ErrorCode = "deadline"
)

type OpError struct {
	Code       ErrorCode
	RetryAfter time.Duration
	cause      error
}

func (e *OpError) Error() string {
	if e == nil {
		return "metadata: unknown"
	}
	return fmt.Sprintf("metadata: %s", e.Code)
}

func (e *OpError) Is(target error) bool {
	if e == nil {
		return false
	}
	switch e.Code {
	case ErrCanceled:
		return target == context.Canceled || errors.Is(e.cause, context.Canceled)
	case ErrDeadline:
		return target == context.DeadlineExceeded || errors.Is(e.cause, context.DeadlineExceeded)
	default:
		return false
	}
}

func copyLookupInput(input LookupInput) LookupInput {
	return LookupInput{Title: input.Title, System: input.System}
}

func newOpError(code ErrorCode, cause error) *OpError {
	if code == "" {
		code = ErrUpstreamUnavailable
	}
	return &OpError{Code: code, cause: cause}
}

func opCode(err error) ErrorCode {
	var op *OpError
	if errors.As(err, &op) {
		return op.Code
	}
	return ""
}

// ObservationKind is the closed, privacy-safe set of runtime counter labels.
// It deliberately contains no title, provider identifier, URL, path, or raw
// error data.
type ObservationKind string

const (
	ObservationExact           ObservationKind = "exact"
	ObservationConfident       ObservationKind = "confident"
	ObservationAmbiguous       ObservationKind = "ambiguous"
	ObservationNoMatch         ObservationKind = "no_match"
	ObservationUnauthorized    ObservationKind = "unauthorized"
	ObservationRateLimited     ObservationKind = "rate_limited"
	ObservationOffline         ObservationKind = "offline"
	ObservationInvalidResponse ObservationKind = "invalid_response"
	ObservationArtworkRejected ObservationKind = "artwork_rejected"
	ObservationCacheHit        ObservationKind = "cache_hit"
	ObservationCacheMiss       ObservationKind = "cache_miss"
)

// CounterSnapshot is the canonical internal observability projection.
type CounterSnapshot struct {
	Provider ProviderName
	Counters map[ObservationKind]uint64
}
