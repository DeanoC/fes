package hostapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/internal/metadata"
	"github.com/DeanoC/FogCast/protocol"
)

func configuredSessionTarget(service Service, name string) bool {
	if name == "" {
		return true
	}
	resolver, ok := service.(interface {
		TargetConfigForName(string) fogcast.TargetConfig
	})
	if !ok {
		return true
	}
	return resolver.TargetConfigForName(name).Name != ""
}

func rejectUnexpectedHost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath := r.URL.Path
		if strings.HasPrefix(r.RequestURI, "/") {
			requestPath = strings.SplitN(r.RequestURI, "?", 2)[0]
		}
		if strings.HasPrefix(requestPath, "/api/v1/presentation/") && (strings.Contains(requestPath, "/../") || strings.Contains(requestPath, "/./") || strings.HasSuffix(requestPath, "/..") || strings.HasSuffix(requestPath, "/.")) {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "presentation path is invalid")
			return
		}
		host := r.Host
		if host == "" {
			host = r.URL.Host
		}
		hostname, _, err := net.SplitHostPort(host)
		if err != nil {
			hostname = strings.Trim(host, "[]")
		}
		ip := net.ParseIP(hostname)
		if !strings.EqualFold(hostname, "localhost") && (ip == nil || !ip.IsLoopback()) {
			writeError(w, http.StatusForbidden, "HOST_NOT_ALLOWED", "host is not allowed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

var presentationHandlePattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func safePresentation(result metadata.Result) (presentationPayload, presentationAttribution, bool) {
	label, ok := presentationLabel(result.Attribution)
	if !ok {
		return presentationPayload{}, presentationAttribution{}, false
	}
	values := []struct {
		value string
		limit int
	}{
		{result.Presentation.Summary, 240},
		{result.Presentation.Year, 20},
		{result.Presentation.Genre, 40},
		{result.Presentation.Studio, 60},
		{result.Presentation.Players, 40},
		{result.Presentation.Series, 80},
	}
	for _, value := range values {
		if !utf8.ValidString(value.value) {
			return presentationPayload{}, presentationAttribution{}, false
		}
	}
	return presentationPayload{
		Summary:               boundedPresentationText(result.Presentation.Summary, 240),
		Year:                  boundedPresentationText(result.Presentation.Year, 20),
		Genre:                 boundedPresentationText(result.Presentation.Genre, 40),
		Studio:                boundedPresentationText(result.Presentation.Studio, 60),
		Players:               boundedPresentationText(result.Presentation.Players, 40),
		Series:                boundedPresentationText(result.Presentation.Series, 80),
		CoverArtworkHandle:    safePresentationHandle(result.Presentation.CoverArtworkID),
		BackdropArtworkHandle: safePresentationHandle(result.Presentation.BackdropArtworkID),
		LogoHandle:            safePresentationHandle(result.Presentation.LogoArtworkID),
		MarqueeHandle:         safePresentationHandle(result.Presentation.MarqueeArtworkID),
		Box3DHandle:           safePresentationHandle(result.Presentation.Box3DArtworkID),
	}, presentationAttribution{Provider: string(result.Attribution.Provider), Label: label}, true
}

func presentationLabel(attribution metadata.Attribution) (string, bool) {
	switch {
	case attribution.Provider == metadata.ProviderIGDB && attribution.Label == "Data from IGDB.com":
		return attribution.Label, true
	case attribution.Provider == metadata.ProviderLaunchBox && attribution.Label == "Data from LaunchBox Games Database":
		return attribution.Label, true
	default:
		return "", false
	}
}

func boundedPresentationText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if !utf8.ValidString(value) {
		return ""
	}
	runes := []rune(value)
	if len(runes) > limit {
		runes = runes[:limit]
	}
	return string(runes)
}

func safePresentationHandle(value string) string {
	if presentationHandlePattern.MatchString(value) {
		return value
	}
	return ""
}

func publicErrorMessage(code protocol.ErrorCode) string {
	switch code {
	case protocol.CodeCorruptData:
		return "stored core data is corrupt"
	case protocol.CodeIncompatibleData:
		return "stored or selected core data is incompatible"
	case protocol.CodeStaleRevision:
		return "core package selection or data revision changed; refresh before retrying"
	case protocol.CodeSaveFailed:
		return "core data could not be durably written; inspect status before retrying"
	case protocol.CodeBadRequest:
		return "FogCast request is invalid"
	case protocol.CodeUnauthorized:
		return "target authentication failed"
	case protocol.CodeROMNotFound:
		return "catalog game was not found"
	case protocol.CodeBusy:
		return "another launch or stop transition is running"
	case protocol.CodeUnavailable:
		return "configured software emulator core is unavailable"
	case protocol.CodeUnsupportedSystem:
		return "game system is unsupported"
	case protocol.CodeUnsupportedOperation:
		return "requested operation is unsupported"
	case protocol.CodeInvalidROMPath:
		return "target ROM path is invalid"
	case protocol.CodeSourceUnavailable:
		return "game source is unavailable"
	case protocol.CodeCorePackageStoreUnavailable:
		return "core package storage is unavailable"
	case protocol.CodeTransferFailed:
		return "content transfer failed"
	case protocol.CodeKitLeaseDenied:
		return "kit lease is foreign; HID is fail-closed"
	case protocol.CodeKitLeaseBusy:
		return "Another session owns the kit; release it from that session before retrying."
	case protocol.CodeVersionMismatch:
		return "target API version is missing or unsupported; expected v1"
	case protocol.CodeMiSTerUnavailable:
		return "MiSTer is unavailable"
	case protocol.CodeCoreTimeout:
		return "core transition timed out"
	case protocol.CodeUnrecognizedCore:
		return "active core is unrecognized"
	default:
		return "FogCast operation failed internally"
	}
}

func publicGame(game catalog.Game) gameResult {
	canonical := game.CanonicalTitle
	if canonical == "" {
		canonical = catalog.ParseDump(game.Title).CanonicalTitle
	}
	result := gameResult{
		ID: game.ID, Title: game.Title, System: game.System, Kind: game.Kind,
		State: game.State, RootOnline: game.RootOnline, ContentPrepared: game.Content != nil,
		Execution: fogcast.ExecutionFPGANative, Platform: game.System, Launchable: catalog.Launchable(game.System),
		CanonicalTitle: canonical, Region: game.Region, Revision: game.Revision, DumpFlags: game.DumpFlags,
		GroupKey: game.GroupKey, VariantCount: game.VariantCount, Genre: game.Genre, Year: game.Year,
	}
	if result.VariantCount <= 0 {
		result.VariantCount = 1
	}
	return result
}

func metadataLookupTitles(game catalog.Game) []string {
	title := strings.TrimSpace(game.Title)
	canonical := strings.TrimSpace(game.CanonicalTitle)
	if canonical == "" {
		canonical = strings.TrimSpace(catalog.ParseDump(title).CanonicalTitle)
	}
	if canonical != "" {
		normalizedCanonical, canonicalErr := metadata.NormalizeTitle(canonical)
		decoratedTitle, decoratedErr := metadata.DecoratedTitle(title)
		if canonicalErr == nil && decoratedErr == nil && decoratedTitle != normalizedCanonical {
			return []string{title, canonical}
		}
		return []string{canonical}
	}
	return []string{game.Title}
}

func lookupGameMetadata(ctx context.Context, runtime metadata.Runtime, game catalog.Game) (metadata.Result, error) {
	var fallback metadata.Result
	titles := metadataLookupTitles(game)
	for index, title := range titles {
		result, err := runtime.Lookup(ctx, metadata.LookupInput{Title: title, System: game.System})
		if err != nil {
			return metadata.Result{}, err
		}
		if result.Outcome != metadata.OutcomeNoMatch || index == len(titles)-1 {
			return result, nil
		}
		fallback = result
	}
	return fallback, nil
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResult{Error: apiError{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func decodeSingleJSON(w http.ResponseWriter, r *http.Request, value any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "request body must contain one valid JSON object")
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "request body must contain exactly one JSON object")
		return errors.New("trailing JSON")
	}
	return nil
}

func rejectBody(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1)
	body, err := io.ReadAll(r.Body)
	if err != nil || len(body) != 0 {
		return errors.New("non-empty body")
	}
	return nil
}

func writeSessionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, fogcast.ErrSessionChanged):
		writeError(w, http.StatusConflict, "SESSION_CHANGED", fogcast.ErrSessionChanged.Error())
		return
	case errors.Is(err, meshcontent.ErrContentMissingNoSource):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": apiError{
			Code: "CONTENT_MISSING", Message: "required content is missing and no source advertises it",
		}})
		return
	case errors.Is(err, meshcontent.ErrContentPullFailed):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": apiError{
			Code: "CONTENT_PULL_FAILED", Message: "required content pull failed",
		}})
		return
	case errors.Is(err, meshcontent.ErrContentLinkFailed):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": apiError{
			Code: "CONTENT_LINK_FAILED", Message: "mesh link failed",
		}})
		return
	case errors.Is(err, meshcontent.ErrContentUnreachable):
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": apiError{
			Code: "CONTENT_UNREACHABLE", Message: "mesh content could not be read",
		}})
		return
	case errors.Is(err, meshcontent.ErrCheckingTimeout):
		writeJSON(w, http.StatusGatewayTimeout, map[string]any{"error": apiError{
			Code: "CONTENT_CHECKING_TIMEOUT", Message: "required content stayed checking until the host timeout",
		}})
		return
	case errors.Is(err, meshcontent.ErrLeaseNotFree):
		writeJSON(w, http.StatusForbidden, map[string]any{"error": apiError{
			Code: "KIT_LEASE_DENIED", Message: "session does not own the kit lease",
		}})
		return
	}
	var lease *meshcontent.LeaseDeniedError
	if errors.As(err, &lease) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": apiError{
			Code: "KIT_LEASE_DENIED", Message: "session does not own the kit lease",
		}})
		return
	}
	var blocked *meshcontent.ExecuteBlockedError
	if errors.As(err, &blocked) {
		switch blocked.Block {
		case meshcontent.BlockEnsureProgress:
			// Launch waits with a positive checking timeout, so this
			// 409 is not produced by POST /api/v1/session/launch. The
			// timeout wins and the status is CONTENT_CHECKING_TIMEOUT.
			// Ensure with CheckingTimeout 0 can still return the block.
			writeJSON(w, http.StatusConflict, map[string]any{"error": apiError{
				Code: "CONTENT_CHECKING", Message: "required content is still being checked",
			}})
			return
		case meshcontent.BlockContentMissing:
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": apiError{
				Code: "CONTENT_MISSING", Message: "required content is missing",
			}})
			return
		case meshcontent.BlockVersionSkew, meshcontent.BlockNoExecutor:
			writeJSON(w, http.StatusConflict, map[string]any{"error": apiError{
				Code: "ABI_INELIGIBLE", Message: "the bound executor cannot run the required package ABI",
			}})
			return
		}
	}
	var drifted *fogcast.LaunchSnapshotError
	if errors.As(err, &drifted) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": apiError{
			Code: "LAUNCH_CHANGED", Message: drifted.Error(),
		}})
		return
	}
	var apiErr *protocol.APIError
	if errors.As(err, &apiErr) {
		status := http.StatusInternalServerError
		if apiErr.Code == protocol.CodeBusy || apiErr.Code == protocol.CodeKitLeaseBusy {
			status = http.StatusConflict
		}
		if apiErr.Code == protocol.CodeUnavailable || apiErr.Code == protocol.CodeCorePackageStoreUnavailable {
			status = http.StatusServiceUnavailable
		}
		if apiErr.Code == protocol.CodeKitLeaseDenied {
			status = http.StatusForbidden
		}
		if apiErr.Code == protocol.CodeROMNotFound {
			status = http.StatusNotFound
		}
		if apiErr.Code == protocol.CodeBadRequest || apiErr.Code == protocol.CodeUnsupportedSystem || apiErr.Code == protocol.CodeUnsupportedOperation {
			status = http.StatusBadRequest
		}
		message, phase := fogcast.SafeTargetDiagnostic(apiErr)
		if message == "" {
			message = publicErrorMessage(apiErr.Code)
		}
		writeJSON(w, status, map[string]any{"error": apiError{
			StopStage: fogcast.StopStage(err),
			Code:      string(apiErr.Code), Message: message, Phase: phase,
			NoChange: apiErr.NoChange,
			Expected: apiErr.Expected, Observed: apiErr.Observed,
		}})
		return
	}
	writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": apiError{
		Code: "TARGET_UNAVAILABLE", Message: "session operation failed", StopStage: fogcast.StopStage(err),
	}})
}

func targetConnection(service Service, targetName ...string) *fogcast.TargetConnection {
	if len(targetName) > 0 && strings.TrimSpace(targetName[0]) != "" {
		if provider, ok := service.(interface {
			TargetConnectionForName(string) fogcast.TargetConnection
		}); ok {
			connection := provider.TargetConnectionForName(targetName[0])
			return &connection
		}
	}
	if provider, ok := service.(interface {
		TargetConnection() fogcast.TargetConnection
	}); ok {
		connection := provider.TargetConnection()
		return &connection
	}
	return nil
}
