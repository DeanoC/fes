package metadata

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestOpErrorIsSafeAndClassifiesContext(t *testing.T) {
	secret := errors.New("https://upstream.test/body SECRET-TOKEN")
	err := &OpError{Code: ErrUnauthorized, RetryAfter: 7 * time.Second, cause: secret}
	if got := err.Error(); got != "metadata: unauthorized" {
		t.Fatalf("Error() = %q", got)
	}
	if strings.Contains(err.Error(), "upstream") || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("safe error leaked private cause: %q", err.Error())
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("unauthorized error unexpectedly classified as context cancellation")
	}
	cancelled := &OpError{Code: ErrCanceled, cause: context.Canceled}
	if !errors.Is(cancelled, context.Canceled) {
		t.Fatal("canceled operation did not classify context cancellation")
	}
}

func TestLookupInputAndResultTypesKeepProviderDataDetached(t *testing.T) {
	input := LookupInput{Title: "  Sonic  ", System: "megadrive"}
	copy := copyLookupInput(input)
	if &copy == &input || copy.Title != input.Title || copy.System != input.System {
		t.Fatalf("lookup input was not detached: %#v", copy)
	}
	result := Result{Outcome: OutcomeExact, Attribution: Attribution{Provider: ProviderIGDB, Label: "Data from IGDB.com"}}
	if result.Attribution.Label != "Data from IGDB.com" {
		t.Fatalf("unexpected attribution: %#v", result.Attribution)
	}
}
