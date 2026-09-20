package fogcast

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

type persistenceLibraryClient struct {
	*fakeServiceClient
	root          string
	layouts       map[string]*protocol.RuntimeContract
	record        protocol.CoreData
	dataErr       error
	mutateCalls   int
	wrongIdentity bool
	libraryCalls  int
}

func (c *persistenceLibraryClient) admit(ctx context.Context, n int64, r io.Reader) (corepackage.Staged, error) {
	return corepackage.Stage(ctx, c.root, n, r)
}
func (c *persistenceLibraryClient) InspectCore(ctx context.Context, n int64, r io.Reader) (protocol.CoreInspection, error) {
	p, err := c.admit(ctx, n, r)
	if err != nil {
		return protocol.CoreInspection{}, err
	}
	defer p.Cleanup()
	return protocol.CoreInspection{PackageID: p.PackageID, Descriptor: p.Descriptor, Compatible: true, PersistenceLayout: c.layouts[p.PackageID]}, nil
}
func (c *persistenceLibraryClient) InspectCoreData(ctx context.Context, n int64, r io.Reader, id string) (protocol.CoreDataInspection, error) {
	p, err := c.admit(ctx, n, r)
	if err != nil {
		return protocol.CoreDataInspection{}, err
	}
	defer p.Cleanup()
	if p.PackageID != id {
		return protocol.CoreDataInspection{}, errors.New("wrong sent package")
	}
	if c.dataErr != nil {
		return protocol.CoreDataInspection{}, c.dataErr
	}
	d := c.record
	d.PackageID = id
	d.CoreID = p.Descriptor.Core.ID
	d.Layout = c.layouts[id]
	d.Mode = "persistent"
	if d.Layout == nil {
		d.Mode = "volatile"
		d.Revision = "absent"
		d.PaddleSpeed = 1
		d.BestRally = 0
	}
	if c.wrongIdentity {
		d.CoreID = "other.core"
	}
	return protocol.CoreDataInspection{CoreData: d, Descriptor: p.Descriptor}, nil
}
func (c *persistenceLibraryClient) UpdateCoreSettings(ctx context.Context, n int64, r io.Reader, u protocol.CoreSettingsUpdate) (protocol.CoreDataInspection, error) {
	c.mutateCalls++
	result, err := c.InspectCoreData(ctx, n, r, u.ExpectedPackageID)
	if err != nil {
		return result, err
	}
	if u.ExpectedRevision != c.record.Revision {
		return result, &protocol.APIError{Code: protocol.CodeStaleRevision, Message: "stale"}
	}
	c.record.PaddleSpeed = u.PaddleSpeed
	c.record.Revision = strings.Repeat("d", 64)
	result.PaddleSpeed = c.record.PaddleSpeed
	result.Revision = c.record.Revision
	return result, nil
}
func (c *persistenceLibraryClient) LoadLibraryCore(ctx context.Context, n int64, r io.Reader, id string) (protocol.Status, error) {
	c.libraryCalls++
	return c.LoadCore(ctx, n, r)
}
func coreDataService(t *testing.T) (*Service, *persistenceLibraryClient) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	store, err := catalog.OpenContext(ctx, filepath.Join(root, "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	packages, err := corepackage.NewStore(filepath.Join(root, "packages"))
	if err != nil {
		t.Fatal(err)
	}
	client := &persistenceLibraryClient{fakeServiceClient: &fakeServiceClient{statusResult: protocol.Status{State: protocol.StateIdle}, healthResult: protocol.Health{TargetID: "kit-id"}}, root: t.TempDir(), layouts: map[string]*protocol.RuntimeContract{}, record: protocol.CoreData{Revision: "absent", PaddleSpeed: 1, BestRally: 17}}
	s := newService(Config{RequestTimeout: time.Second, UploadTimeout: time.Second}, Paths{}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, client)
	s.corePackages = packages
	return s, client
}
func importDataPackage(t *testing.T, s *Service, c *persistenceLibraryClient, version, layout string) corepackage.Inspection {
	t.Helper()
	extra := ""
	if layout != "" {
		extra = "\n[[interfaces]]\nid = \"fes.persistence.words\"\nmajor = 1\nminor = 0\nrequired = true\n\n[[interfaces]]\nid = \"" + layout + "\"\nmajor = 1\nminor = 0\nrequired = true\n"
	}
	raw := libraryPackageFixture(t, version, extra)
	p, _, err := s.ImportCorePackage(context.Background(), int64(len(raw)), bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if layout != "" {
		c.layouts[p.PackageID] = &protocol.RuntimeContract{ID: layout, Major: 1}
	}
	return p
}
func TestCoreDataSelectionProtectsContractBeforeFirstRecord(t *testing.T) {
	s, c := coreDataService(t)
	ctx := context.Background()
	v := importDataPackage(t, s, c, "0.1.0", "")
	p := importDataPackage(t, s, c, "0.2.0", "fes.pong.progress")
	same := importDataPackage(t, s, c, "0.3.0", "fes.pong.progress")
	other := importDataPackage(t, s, c, "0.4.0", "example.other")
	entry, err := s.CreateCoreEntry(ctx, "Pong", v.PackageID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SelectCoreEntry(ctx, entry.GameID, v.PackageID, p.PackageID); err != nil {
		t.Fatal("upgrade", err)
	}
	for _, candidate := range []string{v.PackageID, other.PackageID} {
		if _, err = s.SelectCoreEntry(ctx, entry.GameID, p.PackageID, candidate); err == nil {
			t.Fatal("incompatible selection accepted")
		}
		got, _ := s.CoreEntry(ctx, entry.GameID)
		if got.PackageID != p.PackageID {
			t.Fatal("rejected CAS changed selection")
		}
	}
	if _, err = s.SelectCoreEntry(ctx, entry.GameID, p.PackageID, same.PackageID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SelectCoreEntry(ctx, entry.GameID, same.PackageID, p.PackageID); err != nil {
		t.Fatal("rollback", err)
	}
	c.dataErr = &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "offline"}
	if _, err = s.SelectCoreEntry(ctx, entry.GameID, p.PackageID, same.PackageID); err == nil {
		t.Fatal("offline treated compatible")
	}
}
func TestCoreDataServiceUsesDurableRevisionAndExactTargetIdentity(t *testing.T) {
	s, c := coreDataService(t)
	ctx := context.Background()
	p := importDataPackage(t, s, c, "0.2.0", "fes.pong.progress")
	entry, err := s.CreateCoreEntry(ctx, "Pong", p.PackageID)
	if err != nil {
		t.Fatal(err)
	}
	data, err := s.CoreSettings(ctx, entry.GameID)
	if err != nil || data.TargetID != "kit-id" || data.BestRally != 17 || data.Revision != "absent" {
		t.Fatalf("data=%+v err=%v", data, err)
	}
	u := protocol.CoreSettingsUpdate{ExpectedPackageID: p.PackageID, ExpectedRevision: "absent", PaddleSpeed: 2}
	data, err = s.SetCoreSettings(ctx, entry.GameID, u)
	if err != nil || data.BestRally != 17 || data.PaddleSpeed != 2 || data.Revision == "absent" {
		t.Fatalf("write=%+v %v", data, err)
	}
	if _, err = s.SetCoreSettings(ctx, entry.GameID, u); err == nil {
		t.Fatal("stale write accepted")
	}
	calls := c.mutateCalls
	u.ExpectedPackageID = strings.Repeat("e", 64)
	if _, err = s.SetCoreSettings(ctx, entry.GameID, u); err == nil || c.mutateCalls != calls {
		t.Fatal("stale selection dispatched")
	}
	u.ExpectedPackageID = p.PackageID
	u.PaddleSpeed = 3
	if _, err = s.SetCoreSettings(ctx, entry.GameID, u); err == nil || c.mutateCalls != calls {
		t.Fatal("invalid enum dispatched")
	}
	c.wrongIdentity = true
	if _, err = s.CoreProgress(ctx, entry.GameID); err == nil {
		t.Fatal("wrong namespace accepted")
	}
	c.wrongIdentity = false
	c.dataErr = &protocol.APIError{Code: protocol.CodeBusy, Message: "active"}
	u.PaddleSpeed = 1
	if _, err = s.SetCoreSettings(ctx, entry.GameID, u); err == nil {
		t.Fatal("active namespace accepted")
	}
}
