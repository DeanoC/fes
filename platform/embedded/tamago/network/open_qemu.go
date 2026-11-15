//go:build tamago && arm64 && !de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package network

import "context"

func Open(ctx context.Context, config Config) (*Interface, error) {
	c, err := config.normalized()
	if err != nil {
		return nil, err
	}
	return OpenDevice(ctx, c.Address, c.Gateway, func() (Device, error) { return openNIC(c.MMIOAddress) })
}
