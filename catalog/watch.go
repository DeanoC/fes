package catalog

import (
	"context"
	"errors"
	"time"

	"github.com/DeanoC/FogCast/internal/systems"
)

// DefaultFolderWatchInterval is the host poll interval for SMB-backed
// folder-watch. Inotify is not required and is often silent on guest SMB.
const DefaultFolderWatchInterval = 5 * time.Second

// FolderWatcher reconciles every table-mapped library root into the catalog
// store when ROMs appear or disappear. Roots come from the caller so the watch
// paths stay config-driven.
type FolderWatcher struct {
	Scan     func(context.Context, []Root) (ScanReport, error)
	Roots    func() []Root
	Interval time.Duration
	// OnError is invoked for a reconcile failure that is not cancellation.
	// The watcher keeps polling so transient SMB errors can recover.
	OnError func()
}

func (w FolderWatcher) roots() []Root {
	if w.Roots == nil {
		return nil
	}
	roots := w.Roots()
	out := make([]Root, 0, len(roots))
	for _, root := range roots {
		if !systems.Mapped(root.System) {
			continue
		}
		out = append(out, root)
	}
	return out
}

// Reconcile scans the current watch roots once. Callers change behavior by
// changing the Roots callback or the configured watch root it returns.
func (w FolderWatcher) Reconcile(ctx context.Context) (ScanReport, error) {
	if err := ctx.Err(); err != nil {
		return ScanReport{}, err
	}
	if w.Scan == nil {
		return ScanReport{}, errWatchScanRequired
	}
	return w.Scan(ctx, w.roots())
}

var errWatchScanRequired = errors.New("catalog folder watcher requires a scanner")

// Run reconciles immediately, then on Interval, until ctx is cancelled.
func (w FolderWatcher) Run(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	report, err := w.Reconcile(ctx)
	w.reportReconcile(ctx, report, err)
	if err := ctx.Err(); err != nil {
		return err
	}
	interval := w.Interval
	if interval <= 0 {
		interval = DefaultFolderWatchInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			report, err := w.Reconcile(ctx)
			w.reportReconcile(ctx, report, err)
			if err := ctx.Err(); err != nil {
				return err
			}
		}
	}
}

func (w FolderWatcher) reportReconcile(ctx context.Context, report ScanReport, err error) {
	if ctx.Err() != nil {
		return
	}
	if err == nil && !folderWatchReportOffline(report) {
		return
	}
	if w.OnError != nil {
		w.OnError()
	}
}

func folderWatchReportOffline(report ScanReport) bool {
	for _, root := range report.Roots {
		if root.Offline {
			return true
		}
	}
	return false
}
