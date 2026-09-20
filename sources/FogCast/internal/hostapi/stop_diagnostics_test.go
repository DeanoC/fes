package hostapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/protocol"
)

func TestStopStageHTTPPreservesSafePhase(t *testing.T) {
	for _, source := range []error{&protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Phase: "save", Message: "secret /private/path"}, errors.Join(context.Canceled, errors.New("secret"))} {
		w := httptest.NewRecorder()
		writeSessionError(w, fogcast.WithStopStage(source, "target_stop"))
		var body struct {
			Error apiError `json:"error"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Error.StopStage != "target_stop" || strings.Contains(w.Body.String(), "secret") || strings.Contains(w.Body.String(), "private") {
			t.Fatalf("unsafe/lost context: %s", w.Body.String())
		}
		var api *protocol.APIError
		if errors.As(source, &api) && body.Error.Phase != "save" {
			t.Fatalf("phase lost: %s", w.Body.String())
		}
	}
}
