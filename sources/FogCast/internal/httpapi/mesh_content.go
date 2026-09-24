package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/DeanoC/FogCast/internal/meshcontent"
)

// WithMeshContent serves the kit content store for the node this agent
// is. The routes are an operational seam so a host Ensure can drive
// that store. They are not an Ensure-result wire format. The handler
// does not program the FPGA. Pull and link are admitted by the kit
// lease guard; node, slot, and source reads are not.
func WithMeshContent(executor meshcontent.Executor) Option {
	return func(options *serverOptions) {
		options.meshContent = executor
	}
}

func registerMeshContentRoutes(mux *http.ServeMux, token string, executor meshcontent.Executor, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	mux.Handle("GET /v1/mesh/content/node", authenticate(token, exactMethod(http.MethodGet, meshNodeHandler(executor))))
	mux.Handle("GET /v1/mesh/content/slot", authenticate(token, exactMethod(http.MethodGet, meshSlotHandler(executor))))
	mux.Handle("GET /v1/mesh/content/slots", authenticate(token, exactMethod(http.MethodGet, meshSlotsHandler(executor))))
	mux.Handle("GET /v1/mesh/content/source", authenticate(token, exactMethod(http.MethodGet, meshSourceHandler(executor))))
	mux.Handle("POST /v1/mesh/content/pull", authenticate(token, exactMethod(http.MethodPost, meshPullHandler(executor, logger))))
	mux.Handle("POST /v1/mesh/content/link", authenticate(token, exactMethod(http.MethodPost, meshLinkHandler(executor))))
}

type meshABIJSON struct {
	ID    string `json:"id"`
	Major int    `json:"major"`
}

type meshNodeJSON struct {
	NodeID   string        `json:"node_id"`
	ABIs     []meshABIJSON `json:"abis"`
	Packages []string      `json:"packages,omitempty"`
}

type meshStateJSON struct {
	State string `json:"state"`
}

type meshSourceJSON struct {
	Advertises bool `json:"advertises"`
}

type meshSlotFactJSON struct {
	ID         string `json:"id"`
	State      string `json:"state"`
	Advertises bool   `json:"advertises"`
}

type meshSlotsJSON struct {
	Slots []meshSlotFactJSON `json:"slots"`
}

type meshLinkJSON struct {
	ContentID string `json:"content_id"`
}

func meshNodeHandler(executor meshcontent.Executor) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "mesh node does not accept a query")
			return
		}
		abis := executor.EligibleABIs()
		out := make([]meshABIJSON, 0, len(abis))
		for _, abi := range abis {
			out = append(out, meshABIJSON{ID: abi.ID, Major: abi.Major})
		}
		var packages []string
		if holder, ok := executor.(meshcontent.PackageHolder); ok && holder != nil {
			packages = holder.Packages()
		}
		writeJSON(w, http.StatusOK, meshNodeJSON{NodeID: executor.NodeID(), ABIs: out, Packages: packages})
	})
}

func meshSlotHandler(executor meshcontent.Executor) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := meshQueryID(w, r)
		if !ok {
			return
		}
		writeJSON(w, http.StatusOK, meshStateJSON{State: string(executor.Slot(id))})
	})
}

func meshSlotsHandler(executor meshcontent.Executor) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := r.URL.Query()["id"]
		if len(raw) == 0 || len(raw) > meshcontent.MaxSlotBatch || len(r.URL.Query()) != 1 {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "mesh slots require one to 128 ids")
			return
		}
		out := make([]meshSlotFactJSON, 0, len(raw))
		for _, text := range raw {
			if r.Context().Err() != nil {
				return
			}
			id, err := meshcontent.ParseContentID(text)
			if err != nil {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "mesh content-id is invalid")
				return
			}
			state := executor.Slot(id)
			switch state {
			case meshcontent.StatePresent, meshcontent.StateChecking, meshcontent.StateMissing:
			default:
				state = meshcontent.StateMissing
			}
			out = append(out, meshSlotFactJSON{
				ID:         id.String(),
				State:      string(state),
				Advertises: executor.SourceAdvertises(id),
			})
		}
		writeJSON(w, http.StatusOK, meshSlotsJSON{Slots: out})
	})
}

func meshSourceHandler(executor meshcontent.Executor) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := meshQueryID(w, r)
		if !ok {
			return
		}
		writeJSON(w, http.StatusOK, meshSourceJSON{Advertises: executor.SourceAdvertises(id)})
	})
}

func meshPullHandler(executor meshcontent.Executor, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := meshQueryID(w, r)
		if !ok {
			return
		}
		// The kit pulls from its content source. The body is not a byte
		// upload and is not an Ensure result.
		r.Body = http.MaxBytesReader(w, r.Body, 1)
		body, err := io.ReadAll(r.Body)
		if err != nil || len(body) != 0 {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "mesh pull does not accept a body")
			return
		}
		state, err := executor.Pull(r.Context(), id)
		if err != nil {
			if r.Context().Err() != nil {
				logger.Info("mesh content pull canceled", "content_id", id.String(), "error", r.Context().Err())
				return
			}
			if errors.Is(err, meshcontent.ErrContentPullFailed) {
				writeError(w, http.StatusUnprocessableEntity, "CONTENT_PULL_FAILED", "required content pull failed")
				return
			}
			writeError(w, http.StatusInternalServerError, "INTERNAL", "mesh pull failed")
			return
		}
		writeJSON(w, http.StatusOK, meshStateJSON{State: string(state)})
	})
}

func meshLinkHandler(executor meshcontent.Executor) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		if name == "" || len(r.URL.Query()) != 1 {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "mesh link requires a name")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 512)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var request meshLinkJSON
		if decoder.Decode(&request) != nil || decoder.Decode(&struct{}{}) != io.EOF {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "mesh link body is invalid")
			return
		}
		id, err := meshcontent.ParseContentID(request.ContentID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "mesh link content-id is invalid")
			return
		}
		if err := executor.LinkExpansion(name, id); err != nil {
			writeError(w, http.StatusUnprocessableEntity, "CONTENT_LINK_FAILED", "mesh link failed")
			return
		}
		writeJSON(w, http.StatusOK, meshLinkJSON{ContentID: id.String()})
	})
}

func meshQueryID(w http.ResponseWriter, r *http.Request) (meshcontent.ContentID, bool) {
	if len(r.URL.Query()) != 1 || r.URL.Query().Get("id") == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "mesh content requires one id")
		return meshcontent.ContentID{}, false
	}
	id, err := meshcontent.ParseContentID(r.URL.Query().Get("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "mesh content-id is invalid")
		return meshcontent.ContentID{}, false
	}
	return id, true
}
