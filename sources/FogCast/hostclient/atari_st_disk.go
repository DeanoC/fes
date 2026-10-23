package hostclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/DeanoC/FogCast/atarist"
	"github.com/DeanoC/FogCast/protocol"
)

// BlankAtariStRequest asks the host to format one drive-A image. Zero
// geometry is the 720 KiB disk. Bind attaches it to an existing title.
type BlankAtariStRequest struct {
	Name              string
	Tracks            int
	Heads             int
	Sectors           int
	Bind              bool
	GameID            string
	ExpectedPackageID string
	ExpectedMediaID   string
}

// BlankAtariStDisk is the stored image. Entry is set when the host bound it.
type BlankAtariStDisk struct {
	MediaID string     `json:"media_id"`
	Size    int64      `json:"size"`
	Name    string     `json:"name"`
	Tracks  int        `json:"tracks"`
	Heads   int        `json:"heads"`
	Sectors int        `json:"sectors"`
	Created bool       `json:"created"`
	Entry   *CoreEntry `json:"entry,omitempty"`
}

// CreateBlankAtariStDisk stores a formatted .st through the host library.
// HTTP 200 and 201 are both success: the same bytes may already be stored.
func (c *Client) CreateBlankAtariStDisk(ctx context.Context, req BlankAtariStRequest) (BlankAtariStDisk, error) {
	geometry, ok := atarist.Geometry(req.Tracks, req.Heads, req.Sectors)
	if !ok || strings.TrimSpace(req.Name) == "" {
		return BlankAtariStDisk{}, fmt.Errorf("blank ST disk name or geometry is invalid")
	}
	if req.Bind && (protocol.ValidateGameID(req.GameID) != nil || protocol.ValidateDigest(req.ExpectedPackageID) != nil || (req.ExpectedMediaID != "" && protocol.ValidateDigest(req.ExpectedMediaID) != nil)) {
		return BlankAtariStDisk{}, fmt.Errorf("blank ST disk binding is invalid")
	}
	payload := map[string]any{"name": req.Name, "tracks": geometry.Tracks, "heads": geometry.Heads, "sectors": geometry.Sectors}
	if req.Bind {
		payload["bind"] = true
		payload["game_id"] = req.GameID
		payload["expected_package_id"] = req.ExpectedPackageID
		payload["expected_media_id"] = req.ExpectedMediaID
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return BlankAtariStDisk{}, err
	}
	httpReq, err := c.NewRequest(ctx, http.MethodPost, "/api/v1/library/atari-st-disks", bytes.NewReader(data))
	if err != nil {
		return BlankAtariStDisk{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	resp, err := c.HTTPClient().Do(httpReq)
	if err != nil {
		return BlankAtariStDisk{}, err
	}
	defer resp.Body.Close()
	body, err := ReadResponseBody(resp, maxResponseBytes)
	if err != nil {
		return BlankAtariStDisk{}, err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return BlankAtariStDisk{}, APIStatusError(resp.StatusCode, body)
	}
	var disk BlankAtariStDisk
	if err := json.Unmarshal(body, &disk); err != nil {
		return BlankAtariStDisk{}, fmt.Errorf("blank ST disk: %w", err)
	}
	want := int64(geometry.Tracks) * int64(geometry.Heads) * int64(geometry.Sectors) * 512
	if protocol.ValidateDigest(disk.MediaID) != nil || disk.Size != want || disk.Tracks != geometry.Tracks || disk.Heads != geometry.Heads || disk.Sectors != geometry.Sectors || disk.Name == "" {
		return BlankAtariStDisk{}, fmt.Errorf("blank ST disk response does not match the request")
	}
	if (resp.StatusCode == http.StatusCreated) != disk.Created {
		return BlankAtariStDisk{}, fmt.Errorf("blank ST disk creation flag does not match the response")
	}
	if req.Bind {
		if disk.Entry == nil || disk.Entry.GameID != req.GameID || disk.Entry.PackageID != req.ExpectedPackageID || disk.Entry.MediaRole != protocol.DiskRole || disk.Entry.MediaID != disk.MediaID {
			return BlankAtariStDisk{}, fmt.Errorf("blank ST disk was stored without the requested title binding")
		}
	} else if disk.Entry != nil {
		return BlankAtariStDisk{}, fmt.Errorf("blank ST disk response bound a title that was not requested")
	}
	return disk, nil
}
