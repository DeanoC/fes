package fogcast

import (
	"context"
	"errors"
	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corecatalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"os"
	"regexp"
)

var librarySourceRE = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,127}$`)

func validLibrarySourceID(id string) bool { return librarySourceRE.MatchString(id) }

type AvailableCore struct {
	LibrarySourceID string `json:"library_source_id"`
	corecatalog.Entry
	SourceID      string                  `json:"source_id"`
	ArtifactState string                  `json:"artifact_state"`
	Descriptor    *corepackage.Descriptor `json:"descriptor,omitempty"`
}

func (s *Service) availableCatalog() (corecatalog.Catalog, error) {
	if s.coreCatalogPath == "" || !validLibrarySourceID(s.coreLibrarySourceID) {
		return corecatalog.Catalog{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	c, err := corecatalog.Load(s.coreCatalogPath)
	if err != nil {
		return c, canonicalError(protocol.CodeSourceUnavailable, errors.New("core catalog is unavailable"))
	}
	return c, nil
}
func (s *Service) AvailableCores(ctx context.Context) ([]AvailableCore, error) {
	c, err := s.availableCatalog()
	if err != nil {
		return nil, err
	}
	installed, err := s.CorePackages(ctx)
	if err != nil {
		return nil, err
	}
	values := make([]AvailableCore, 0, len(c.Entries))
	for _, e := range c.Entries {
		state := "unproduced"
		if e.PackageID != "" {
			state = "unavailable"
			r, _, openErr := c.OpenPackage(e.CoreID)
			if openErr == nil {
				r.Close()
				state = "available"
			}
		}
		item := AvailableCore{Entry: e, SourceID: c.SourceID, LibrarySourceID: s.coreLibrarySourceID, ArtifactState: state}
		for _, p := range installed {
			if p.PackageID == e.PackageID && p.Descriptor.Core.ID == e.CoreID {
				item.ArtifactState = "installed"
				d := p.Descriptor
				item.Descriptor = &d
				break
			}
		}
		values = append(values, item)
	}
	return values, nil
}
func (s *Service) InstallAvailableCore(ctx context.Context, sourceID, coreID, packageID string) (InstalledCorePackage, error) {
	if err := ctx.Err(); err != nil {
		return InstalledCorePackage{}, err
	}
	c, err := s.availableCatalog()
	if err != nil {
		return InstalledCorePackage{}, err
	}
	if sourceID != c.SourceID {
		return InstalledCorePackage{}, canonicalError(protocol.CodeStaleRevision, nil)
	}
	r, e, err := c.OpenPackage(coreID)
	if err != nil {
		return InstalledCorePackage{}, canonicalError(protocol.CodeSourceUnavailable, errors.New("published package is unavailable"))
	}
	defer r.Close()
	if packageID != e.PackageID {
		return InstalledCorePackage{}, canonicalError(protocol.CodeStaleRevision, nil)
	}
	// Stage validates canonical package bytes and identity before any store import.
	temporary, err := os.MkdirTemp("", "fogcast-catalog-")
	if err != nil {
		return InstalledCorePackage{}, err
	}
	defer os.RemoveAll(temporary)
	staged, err := corepackage.Stage(ctx, temporary, e.ArchiveSize, r)
	if err != nil {
		return InstalledCorePackage{}, canonicalError(protocol.CodeInvalidArchive, nil)
	}
	defer staged.Cleanup()
	if staged.PackageID != packageID || staged.Descriptor.Core.ID != coreID {
		return InstalledCorePackage{}, canonicalError(protocol.CodeInvalidArchive, nil)
	}
	bytesReader, _, err := c.OpenPackage(coreID)
	if err != nil {
		return InstalledCorePackage{}, err
	}
	defer bytesReader.Close()
	p, _, err := s.ImportCorePackage(ctx, e.ArchiveSize, bytesReader)
	if err != nil {
		return InstalledCorePackage{}, err
	}
	return InstalledCorePackage{Inspection: p, Entries: []catalog.CoreEntry{}, Compatibility: "unknown"}, nil
}
