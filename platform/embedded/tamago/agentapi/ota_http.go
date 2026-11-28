// SPDX-License-Identifier: GPL-3.0-or-later

package agentapi

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/DeanoC/fes/platform/embedded/tamago/ota"
)

type otaAPIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type otaHandler struct {
	mgr                  *ota.Manager
	allowUnauthenticated bool
	tokenDigest          [32]byte
	maxUploadBytes       int64
}

func newOTAMux(fallback http.Handler, mgr *ota.Manager, token string, allowUnauthenticated bool, maxUploadBytes int64) http.Handler {
	if maxUploadBytes <= 0 {
		maxUploadBytes = ota.DefaultMaxImageBytes
	}
	h := &otaHandler{
		mgr:                  mgr,
		allowUnauthenticated: allowUnauthenticated,
		tokenDigest:          sha256.Sum256([]byte(token)),
		maxUploadBytes:       maxUploadBytes,
	}
	mux := http.NewServeMux()
	mux.Handle("GET /v1/ota", h.authenticate(http.HandlerFunc(h.handleStatus)))
	mux.Handle("POST /v1/ota/stage", h.authenticate(http.HandlerFunc(h.handleStage)))
	mux.Handle("POST /v1/ota/activate", h.authenticate(http.HandlerFunc(h.handleActivate)))
	mux.Handle("POST /v1/ota/confirm", h.authenticate(http.HandlerFunc(h.handleConfirm)))
	mux.Handle("POST /v1/ota/rollback", h.authenticate(http.HandlerFunc(h.handleRollback)))
	mux.Handle("POST /v1/ota/cancel", h.authenticate(http.HandlerFunc(h.handleCancel)))
	mux.Handle("/", fallback)
	return mux
}

func parseBearerToken(header string) (string, bool) {
	fields := strings.Fields(header)
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") || fields[1] == "" {
		return "", false
	}
	return fields[1], true
}

func (h *otaHandler) isAuthenticated(r *http.Request) bool {
	if h.allowUnauthenticated {
		return true
	}
	authz := r.Header.Values("Authorization")
	if len(authz) != 1 {
		return false
	}
	provided, ok := parseBearerToken(authz[0])
	if !ok {
		return false
	}
	actual := sha256.Sum256([]byte(provided))
	return subtle.ConstantTimeCompare(actual[:], h.tokenDigest[:]) == 1
}

func (h *otaHandler) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.isAuthenticated(r) {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized", "valid bearer token required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *otaHandler) handleStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.mgr.Status())
}

func (h *otaHandler) handleStage(w http.ResponseWriter, r *http.Request) {
	version := strings.TrimSpace(r.Header.Get("X-OTA-Version"))
	if version == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "X-OTA-Version header is required")
		return
	}
	target := strings.TrimSpace(r.Header.Get("X-OTA-Target"))
	expectedSHA := strings.TrimSpace(r.Header.Get("X-OTA-SHA256"))
	var signature []byte
	if encoded := strings.TrimSpace(r.Header.Get("X-OTA-Signature")); encoded != "" {
		decoded, err := base64.StdEncoding.Strict().DecodeString(encoded)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid X-OTA-Signature header")
			return
		}
		signature = decoded
	}

	var epoch uint64
	if rawEpoch := strings.TrimSpace(r.Header.Get("X-OTA-Rollback-Epoch")); rawEpoch != "" {
		parsed, err := strconv.ParseUint(rawEpoch, 10, 64)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid X-OTA-Rollback-Epoch header")
			return
		}
		epoch = parsed
	}

	r.Body = http.MaxBytesReader(w, r.Body, h.maxUploadBytes)
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeAPIError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "OTA payload exceeds max upload limit")
			return
		}
		writeAPIError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if len(payload) == 0 {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "empty OTA payload")
		return
	}

	rec, err := h.mgr.Stage(r.Context(), ota.StageRequest{
		Version:        version,
		Target:         target,
		Payload:        payload,
		ExpectedSHA256: expectedSHA,
		RollbackEpoch:  epoch,
		Signature:      signature,
	})
	if err != nil {
		writeOTAError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		SlotRecord ota.SlotRecord `json:"slot_record"`
		State      ota.State      `json:"state"`
	}{
		SlotRecord: rec,
		State:      h.mgr.Status(),
	})
}

func (h *otaHandler) handleActivate(w http.ResponseWriter, r *http.Request) {
	if !requireEmptyBody(w, r) {
		return
	}
	st, err := h.mgr.Activate(r.Context())
	if err != nil {
		writeOTAError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (h *otaHandler) handleConfirm(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Slot ota.Slot `json:"slot"`
	}
	hasBody, ok := decodeOptionalJSON(w, r, 1024, &body)
	if !ok {
		return
	}
	slot := body.Slot
	if !hasBody || slot == "" {
		st := h.mgr.Status()
		if st.Phase == ota.PhaseTrial {
			slot = st.PendingSlot
		} else {
			slot = st.ActiveSlot
		}
	}
	st, err := h.mgr.ConfirmBoot(r.Context(), slot)
	if err != nil {
		writeOTAError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (h *otaHandler) handleRollback(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Reason string `json:"reason"`
	}
	_, ok := decodeOptionalJSON(w, r, 1024, &body)
	if !ok {
		return
	}
	st, err := h.mgr.Rollback(r.Context(), body.Reason)
	if err != nil {
		writeOTAError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (h *otaHandler) handleCancel(w http.ResponseWriter, r *http.Request) {
	if !requireEmptyBody(w, r) {
		return
	}
	st, err := h.mgr.CancelPending(r.Context())
	if err != nil {
		writeOTAError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func requireEmptyBody(w http.ResponseWriter, r *http.Request) bool {
	if r.Body == nil {
		return true
	}
	extra, err := io.ReadAll(io.LimitReader(r.Body, 1))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return false
	}
	if len(extra) > 0 {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "unexpected request body")
		return false
	}
	return true
}

func decodeOptionalJSON(w http.ResponseWriter, r *http.Request, maxBytes int64, dst any) (bool, bool) {
	if r.Body == nil {
		return false, true
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBytes+1))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return false, false
	}
	if int64(len(raw)) > maxBytes {
		writeAPIError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "request JSON body too large")
		return false, false
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return false, true
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return false, false
	}
	if dec.Decode(new(any)) != io.EOF {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "trailing JSON data")
		return false, false
	}
	return true, true
}

func writeOTAError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ota.ErrInvalidPhase), errors.Is(err, ota.ErrSlotUnavailable):
		writeAPIError(w, http.StatusConflict, "ota_conflict", err.Error())
	case errors.Is(err, ota.ErrTargetMismatch), errors.Is(err, ota.ErrDigestMismatch),
		errors.Is(err, ota.ErrRollbackRejected), errors.Is(err, ota.ErrSignatureInvalid):
		writeAPIError(w, http.StatusBadRequest, "ota_invalid", err.Error())
	case errors.Is(err, ota.ErrInsufficientSpace):
		writeAPIError(w, http.StatusInsufficientStorage, "insufficient_storage", err.Error())
	default:
		writeAPIError(w, http.StatusInternalServerError, "ota_error", err.Error())
	}
}

func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, struct {
		Error otaAPIError `json:"error"`
	}{
		Error: otaAPIError{Code: code, Message: message},
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}
