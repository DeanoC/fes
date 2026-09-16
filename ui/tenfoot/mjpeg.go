package tenfoot

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"

	"github.com/DeanoC/FogCast/hostclient"
)

const (
	previewContentType = "multipart/x-mixed-replace"
	previewJPEGType    = "image/jpeg"
	maxPreviewJPEGSize = 8 << 20 // matches remotemedia.maxPreviewJPEGSize
)

// PreviewUnavailable is a graceful miss: no route, inactive decoder, kit down,
// or a transport failure. It is never a launch or Stop failure.
type PreviewUnavailable struct {
	Status  int
	Message string
}

func (e PreviewUnavailable) Error() string {
	msg := strings.TrimSpace(e.Message)
	if msg == "" {
		msg = "session preview is unavailable"
	}
	if e.Status > 0 {
		return fmt.Sprintf("session preview unavailable (%d): %s", e.Status, msg)
	}
	return msg
}

// IsPreviewUnavailable reports a graceful preview miss (404/503/inactive/network).
func IsPreviewUnavailable(err error) bool {
	if err == nil {
		return false
	}
	var u PreviewUnavailable
	return errors.As(err, &u)
}

func previewUnavailablePermanent(err error) bool {
	var u PreviewUnavailable
	if !errors.As(err, &u) {
		return false
	}
	return u.Status == http.StatusNotFound
}

// MJPEGStream reads JPEG parts from a host multipart/x-mixed-replace body.
// Parts are framed by Content-Length (the host writes one JPEG then waits), so
// this does not use mime/multipart, which would block until the next boundary.
type MJPEGStream struct {
	br       *bufio.Reader
	closer   io.Closer
	boundary string
}

func previewBoundary(contentType string) (string, error) {
	media, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return "", PreviewUnavailable{Message: "session preview is unavailable"}
	}
	if !strings.EqualFold(media, previewContentType) {
		return "", PreviewUnavailable{Message: "session preview is unavailable"}
	}
	boundary := strings.TrimSpace(params["boundary"])
	if boundary == "" {
		return "", PreviewUnavailable{Message: "session preview is unavailable"}
	}
	return boundary, nil
}

// NewMJPEGStream consumes a host preview body. Caller must Close.
func NewMJPEGStream(body io.ReadCloser, contentType string) (*MJPEGStream, error) {
	if body == nil {
		return nil, PreviewUnavailable{Message: "session preview is unavailable"}
	}
	boundary, err := previewBoundary(contentType)
	if err != nil {
		return nil, err
	}
	return &MJPEGStream{br: bufio.NewReader(body), closer: body, boundary: boundary}, nil
}

func (s *MJPEGStream) skipToBoundary() error {
	marker := "--" + s.boundary
	closeMarker := marker + "--"
	for {
		line, err := s.br.ReadBytes('\n')
		if err != nil {
			return err
		}
		trimmed := string(bytes.TrimSpace(line))
		if trimmed == closeMarker {
			return io.EOF
		}
		if trimmed == marker {
			return nil
		}
	}
}

// NextJPEG returns the next image/jpeg part. Non-JPEG parts are skipped.
func (s *MJPEGStream) NextJPEG() ([]byte, error) {
	if s == nil || s.br == nil {
		return nil, PreviewUnavailable{Message: "session preview is unavailable"}
	}
	for {
		if err := s.skipToBoundary(); err != nil {
			return nil, err
		}
		hdr, err := textproto.NewReader(s.br).ReadMIMEHeader()
		if err != nil {
			return nil, err
		}
		ct := hostclient.ContentTypeMain(hdr.Get("Content-Type"))
		size, err := strconv.Atoi(strings.TrimSpace(hdr.Get("Content-Length")))
		if err != nil || size < 1 {
			return nil, PreviewUnavailable{Message: "session preview is unavailable"}
		}
		if size > maxPreviewJPEGSize {
			if _, err := io.CopyN(io.Discard, s.br, int64(size)); err != nil {
				return nil, err
			}
			s.consumePartTrailer()
			continue
		}
		data := make([]byte, size)
		if _, err := io.ReadFull(s.br, data); err != nil {
			return nil, err
		}
		s.consumePartTrailer()
		if ct != "" && ct != previewJPEGType {
			continue
		}
		return data, nil
	}
}

func (s *MJPEGStream) consumePartTrailer() {
	b, err := s.br.Peek(2)
	if err != nil || len(b) < 2 {
		return
	}
	if b[0] == '\r' && b[1] == '\n' {
		_, _ = s.br.Discard(2)
	}
}

// Close closes the underlying HTTP body.
func (s *MJPEGStream) Close() error {
	if s == nil || s.closer == nil {
		return nil
	}
	err := s.closer.Close()
	s.closer = nil
	s.br = nil
	return err
}
