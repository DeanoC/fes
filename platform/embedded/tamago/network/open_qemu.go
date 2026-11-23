//go:build tamago && arm64 && !de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package network

import (
	"context"

	"github.com/DeanoC/fes/platform/embedded/tamago/dmaops"
)

func Open(ctx context.Context, config Config) (*Interface, error) {
	return OpenWithPool(ctx, config, nil)
}

// OpenWithPool opens the QEMU VirtIO NIC allocating its virtqueue rings and
// packet buffers from pool (which must use dmaops.Uncached), or reserving from
// tamago/dma when pool is nil.
func OpenWithPool(ctx context.Context, config Config, pool *dmaops.Pool) (*Interface, error) {
	c, err := config.normalized()
	if err != nil {
		return nil, err
	}
	return OpenDevice(ctx, c.Address, c.Gateway, func() (Device, error) { return openNIC(c.MMIOAddress, pool) })
}
