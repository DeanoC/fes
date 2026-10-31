package httpapi

import (
	"context"
	"io"
	"net/http"

	"github.com/DeanoC/FogCast/protocol"
)

// diskImageController stages hard-disk base images on the kit by digest.
type diskImageController interface {
	DiskImage(context.Context, string) (protocol.DiskImageStatus, *protocol.APIError)
	StageDiskImage(context.Context, string, int64, io.Reader) (protocol.DiskImageStatus, *protocol.APIError)
}

// registerDiskImageRoutes adds the probe, which only reads, and the upload,
// which the kit lease guards. Hosts treat a 404 as an agent that predates
// hard-disk launches.
func registerDiskImageRoutes(mux *http.ServeMux, token string, controller DevelopmentController) {
	handle := func(path string, upload bool) {
		mux.Handle(path, authenticate(token, exactMethod(http.MethodPost, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, ok := controller.(diskImageController)
			if !ok {
				writeAPIError(w, 400, &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"})
				return
			}
			images := r.Header.Values(protocol.DiskImageHeader)
			if r.URL.RawQuery != "" || len(images) != 1 || protocol.ValidateDigest(images[0]) != nil || len(r.TransferEncoding) != 0 {
				writeBadRequest(w, r, "disk image request requires one image digest")
				return
			}
			var result protocol.DiskImageStatus
			var apiErr *protocol.APIError
			if upload {
				if !exactContentType(r, "application/octet-stream") || !protocol.AdmitAtaDiskSize(r.ContentLength) {
					writeBadRequest(w, r, "disk image upload requires a raw image of 1 to 128 MiB in whole sectors")
					return
				}
				r.Body = http.MaxBytesReader(w, r.Body, r.ContentLength)
				result, apiErr = c.StageDiskImage(r.Context(), images[0], r.ContentLength, r.Body)
			} else {
				if r.ContentLength > 0 {
					writeBadRequest(w, r, "disk image probe takes no body")
					return
				}
				result, apiErr = c.DiskImage(r.Context(), images[0])
			}
			if apiErr != nil {
				setRequestError(r, apiErr.Code)
				writeAPIError(w, statusForError(apiErr.Code), apiErr)
				return
			}
			writeJSON(w, 200, result)
		}))))
	}
	handle("/v1/library/disk-images/probe", false)
	handle("/v1/library/disk-images", true)
}
