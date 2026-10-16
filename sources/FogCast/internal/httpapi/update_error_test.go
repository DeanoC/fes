package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"syscall"
	"testing"

	"github.com/DeanoC/FogCast/appliance/store"
	"github.com/DeanoC/FogCast/protocol"
)

func TestUpdateErrorDistinguishesInsufficientStorage(t *testing.T) {
	for _, err := range []error{
		appliance.ErrInsufficientStorage,
		syscall.ENOSPC,
		errors.Join(errors.New("copy"), syscall.ENOSPC),
	} {
		recorder := httptest.NewRecorder()
		updateError(recorder, err)
		if recorder.Code != http.StatusInsufficientStorage {
			t.Fatalf("%v: status %d body %s", err, recorder.Code, recorder.Body.String())
		}
		var envelope protocol.ErrorEnvelope
		if e := json.Unmarshal(recorder.Body.Bytes(), &envelope); e != nil {
			t.Fatal(e)
		}
		if envelope.Error.Code != "UPDATE_NO_SPACE" || envelope.Error.Message != "insufficient release storage" {
			t.Fatalf("%v: %+v", err, envelope.Error)
		}
	}
	recorder := httptest.NewRecorder()
	updateError(recorder, errors.New("image SHA-256 mismatch"))
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid update status %d", recorder.Code)
	}
	var envelope protocol.ErrorEnvelope
	if e := json.Unmarshal(recorder.Body.Bytes(), &envelope); e != nil {
		t.Fatal(e)
	}
	if envelope.Error.Code != "UPDATE_INVALID" {
		t.Fatalf("code %s", envelope.Error.Code)
	}
}
