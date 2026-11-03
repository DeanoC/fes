package kitpackages

import (
	"context"
	"errors"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/corepackage"
)

// ErrStoreUnavailable reports that the durable store could not be opened.
var ErrStoreUnavailable = errors.New("kit packages: durable store unavailable")

// viewLockWait bounds a call that arrives without a deadline. The store lock
// waits until its context ends, and another process can hold it for a whole
// archive install.
var viewLockWait = time.Minute

// viewReopenDelay spaces attempts to open an unavailable durable store.
const viewReopenDelay = 5 * time.Second

// View is the kit's installed packages as the kit host reads them: the
// durable store on FESDATA3, which takes imports, then the image's factory
// packages. A package in both is the same bytes, so it is listed once.
//
// As in mister-agent, an unavailable durable store withholds the factory
// packages too: every call returns ErrStoreUnavailable. A failed open is
// retried at most every viewReopenDelay, so a host that starts before
// FESDATA3 is ready recovers without a restart.
type View struct {
	open    func() (*Store, error)
	factory *Factory
	now     func() time.Time

	mu      sync.Mutex
	durable *Store
	retryAt time.Time
}

// NewView opens the durable store with open, now and on later calls while
// it is unavailable.
func NewView(open func() (*Store, error), factory *Factory) *View {
	v := &View{open: open, factory: factory, now: time.Now}
	_, _ = v.store()
	return v
}

func (v *View) store() (*Store, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.durable != nil {
		return v.durable, nil
	}
	if now := v.now(); now.Before(v.retryAt) {
		return nil, ErrStoreUnavailable
	}
	store, err := v.open()
	if err != nil || store == nil {
		v.retryAt = v.now().Add(viewReopenDelay)
		return nil, ErrStoreUnavailable
	}
	v.durable = store
	return store, nil
}

func (v *View) Import(ctx context.Context, length int64, reader io.Reader) (corepackage.Inspection, bool, error) {
	store, err := v.store()
	if err != nil {
		return corepackage.Inspection{}, false, err
	}
	ctx, cancel := boundedContext(ctx)
	defer cancel()
	return store.Install(ctx, length, reader)
}

func (v *View) List(ctx context.Context) ([]corepackage.Inspection, error) {
	store, err := v.store()
	if err != nil {
		return nil, err
	}
	ctx, cancel := boundedContext(ctx)
	defer cancel()
	durable, err := store.List(ctx)
	if err != nil {
		return nil, err
	}
	factory, err := v.factory.List(ctx)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(durable))
	result := make([]corepackage.Inspection, 0, len(durable)+len(factory))
	for _, inspection := range durable {
		seen[inspection.PackageID] = struct{}{}
		result = append(result, inspection)
	}
	for _, inspection := range factory {
		if _, ok := seen[inspection.PackageID]; !ok {
			result = append(result, inspection)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].PackageID < result[j].PackageID })
	return result, nil
}

func (v *View) Inspect(ctx context.Context, id string) (corepackage.Inspection, error) {
	store, err := v.store()
	if err != nil {
		return corepackage.Inspection{}, err
	}
	ctx, cancel := boundedContext(ctx)
	defer cancel()
	inspection, err := store.Inspect(ctx, id)
	if errors.Is(err, ErrPackageNotFound) {
		return v.factory.Inspect(ctx, id)
	}
	return inspection, err
}

func (v *View) Read(ctx context.Context, id string) (corepackage.Inspection, []byte, error) {
	store, err := v.store()
	if err != nil {
		return corepackage.Inspection{}, nil, err
	}
	ctx, cancel := boundedContext(ctx)
	defer cancel()
	inspection, data, err := store.Read(ctx, id)
	if errors.Is(err, ErrPackageNotFound) {
		return v.factory.Read(ctx, id)
	}
	return inspection, data, err
}

func boundedContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, viewLockWait)
}
