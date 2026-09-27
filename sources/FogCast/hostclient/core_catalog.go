package hostclient

import (
	"context"
	"fmt"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"net/http"
	"net/url"
)

// CoreReference is scoped to the serving source, independently of an executor.
type CoreReference struct {
	LibrarySourceID string `json:"library_source_id,omitempty"`
	SourceID        string `json:"source_id"`
	CoreID          string `json:"core_id"`
	PackageID       string `json:"package_id"`
}
type AvailableCore struct {
	CoreReference
	Label         string `json:"label"`
	System        string `json:"system"`
	Standing      string `json:"standing"`
	ArtifactState string `json:"artifact_state"`
}
type SetupROM struct {
	ID         string `json:"id"`
	Role       string `json:"role"`
	SourceSize int64  `json:"source_size"`
	Binding    string `json:"binding"`
}
type CoreSetup struct {
	CoreReference
	Descriptor      corepackage.Descriptor         `json:"descriptor"`
	ROMs            []SetupROM                     `json:"roms"`
	Entries         []CoreEntry                    `json:"entries"`
	Capabilities    protocol.CoreMediaCapabilities `json:"capabilities"`
	FirmwareMediaID string                         `json:"firmware_media_id,omitempty"`
}
type CoreSetupRequest struct {
	CoreReference
	Title            string            `json:"title"`
	ROMs             map[string]string `json:"roms,omitempty"`
	MediaRole        string            `json:"media_role,omitempty"`
	MediaID          string            `json:"media_id,omitempty"`
	FirmwareRequired bool              `json:"firmware_required,omitempty"`
}
type CoreSetupResult struct {
	SourceID            string    `json:"source_id"`
	PublicationSourceID string    `json:"publication_source_id"`
	Entry               CoreEntry `json:"entry"`
}

func (c *Client) AvailableCores(ctx context.Context) ([]AvailableCore, error) {
	var v struct {
		Cores []AvailableCore `json:"cores"`
	}
	err := c.getJSON(ctx, "/api/v1/core-catalog", &v)
	return v.Cores, err
}
func (c *Client) InstallAvailableCore(ctx context.Context, ref CoreReference) error {
	if err := validCoreReference(ref); err != nil {
		return err
	}
	var value struct {
		PackageID string `json:"package_id"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/core-catalog/install", map[string]string{"source_id": ref.SourceID, "core_id": ref.CoreID, "package_id": ref.PackageID}, &value); err != nil {
		return err
	}
	if value.PackageID != ref.PackageID {
		return fmt.Errorf("installed core identity mismatch")
	}
	return nil
}
func (c *Client) CoreSetup(ctx context.Context, sourceID, coreID, packageID string) (CoreSetup, error) {
	ref := CoreReference{SourceID: sourceID, CoreID: coreID, PackageID: packageID}
	if err := validCoreReference(ref); err != nil {
		return CoreSetup{}, err
	}
	q := url.Values{"source_id": {sourceID}, "package_id": {packageID}}
	var v CoreSetup
	err := c.getJSON(ctx, "/api/v1/core-catalog/"+url.PathEscape(coreID)+"/setup?"+q.Encode(), &v)
	if err == nil && (v.SourceID != ref.SourceID || v.CoreID != ref.CoreID || v.PackageID != ref.PackageID) {
		err = fmt.Errorf("core setup identity mismatch")
	}
	return v, err
}
func (c *Client) CreateCoreSetupEntry(ctx context.Context, req CoreSetupRequest) (CoreSetupResult, error) {
	if req.LibrarySourceID == "" {
		return CoreSetupResult{}, fmt.Errorf("library source identity is required")
	}
	if err := validCoreReference(req.CoreReference); err != nil {
		return CoreSetupResult{}, err
	}
	var v CoreSetupResult
	err := c.doJSON(ctx, http.MethodPost, "/api/v1/core-catalog/entries", req, &v)
	if err == nil && (v.SourceID != req.LibrarySourceID || v.PublicationSourceID != req.SourceID || v.Entry.CoreID != req.CoreID || v.Entry.PackageID != req.PackageID || protocol.ValidateGameID(v.Entry.GameID) != nil) {
		err = fmt.Errorf("core setup entry identity mismatch")
	}
	return v, err
}
func validCoreReference(ref CoreReference) error {
	if ref.SourceID == "" || ref.CoreID == "" || protocol.ValidateDigest(ref.PackageID) != nil {
		return fmt.Errorf("invalid core reference")
	}
	return nil
}
