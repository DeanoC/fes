//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/nativefabric"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
)

// This bounded frontend observes a fresh SD selection after the independently
// verified display return. It supplies no reconfiguration or ordinary admission.
func verifyWindowStorageReturn(session *nativefabric.WindowSession, store *packagestore.Store, old *packagestore.Handoff, request packagestore.Request) error {
	returned, err := session.Close() // Cached return only; no second firmware close.
	if err != nil {
		return err
	}
	fmt.Printf("FES_NATIVE_FPGA_WINDOW_STORAGE_RETURN BEGIN owner=%x closed_generation=%d\n", returned.After.Owner, returned.After.Generation)
	if err := session.ResumeStorageAfterReturn(); err != nil {
		return err
	}
	active, err := store.ActiveReceipt()
	if err != nil || active.Owner != returned.After.Owner || active.Generation != returned.After.Generation+2 {
		return errors.New("native window SD resume differs from its owned return")
	}
	if _, err := old.Selection(); err == nil {
		return errors.New("native window SD resume kept the old containment handoff valid")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	selected, err := store.Retain(ctx, request)
	if err != nil {
		return err
	}
	handoff, err := store.Contain(selected)
	if err != nil {
		return err
	}
	closed := handoff.Receipt()
	if closed.Active || closed.Prepared || closed.Owner != returned.After.Owner || closed.Generation != returned.After.Generation+3 ||
		handoff.ReturnedFrom().Owner != returned.After.Owner || handoff.ReturnedFrom().Generation != returned.After.Generation {
		return errors.New("native window fresh SD handoff lost return provenance")
	}
	fmt.Printf("FES_NATIVE_FPGA_WINDOW_STORAGE_RETURN PASS package_id=%s owner=%x closed_generation=%d resumed_generation=%d contained_generation=%d source=sd fresh_selection=1 data_closed=1 memory_retained=1 runtime_admission=unavailable\n",
		selected.Inspection().PackageID, closed.Owner, returned.After.Generation, active.Generation, closed.Generation)
	return nil
}
