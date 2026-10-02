package main

import (
	"time"

	"github.com/DeanoC/FogCast/ui/gfx"
)

// Keep a bounded refresh for presentation fields not represented by renderKey
// (for example a renamed title). Input and asynchronous work still run on every
// launcher tick; motion and known scene changes bypass this idle interval.
const kitIdleRefresh = time.Second

type kitFramePainter struct {
	gfx.Device
	last     time.Time
	key      renderKey
	revision uint64
	moving   bool
}

func newKitFramePainter(d gfx.Device) *kitFramePainter {
	if menu, ok := d.(*gfx.MenuDisplay); ok {
		menu.SetChangeDriven(true)
	}
	return &kitFramePainter{Device: d}
}

func (p *kitFramePainter) shouldPaint(now time.Time, key renderKey, moving bool) bool {
	if p.revision != 0 && !moving && !p.moving && key == p.key && now.Sub(p.last) < kitIdleRefresh {
		// An idle scene must still detect new menu generations, retry failed
		// presents and submit after Resume. Reuse the rendered revision without
		// scanning or copying the framebuffer.
		p.presentRevision()
		return false
	}
	p.last, p.key, p.moving = now, key, moving
	return true
}

func (p *kitFramePainter) Present() {
	p.revision++
	p.presentRevision()
	if _, ok := p.Device.(interface{ PresentRevision(uint64) }); !ok {
		p.Device.Present()
	}
}

func (p *kitFramePainter) presentRevision() {
	if d, ok := p.Device.(interface{ PresentRevision(uint64) }); ok {
		d.PresentRevision(p.revision)
	}
}
