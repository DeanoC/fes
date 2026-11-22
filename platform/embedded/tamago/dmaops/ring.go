// SPDX-License-Identifier: GPL-3.0-or-later

package dmaops

import (
	"fmt"
	"sync"
)

// RingConfig configures a fixed power-of-two DMA descriptor ring backed by a Pool.
//
// Note on MemoryMode: rings whose descriptors are updated concurrently by CPU
// and device hardware (such as XGMAC or VirtIO) should be allocated from an
// Uncached pool unless DescSize is a full multiple of CacheLineSize.
type RingConfig struct {
	// Count is the number of descriptor slots; must be a power of two >= 1.
	Count uint32
	// DescSize is the byte size of each descriptor (>= 8).
	DescSize uint64
	// DescAlign is the descriptor table alignment (0 defaults to 16).
	DescAlign uint64
	// BufSize is the optional per-slot data buffer size (0 creates a
	// descriptor-only ring, e.g. for SDHCI ADMA2 scatter-gather tables).
	BufSize uint64
	// BufAlign is the optional per-slot data buffer alignment.
	BufAlign uint64
}

// Slot identifies one descriptor slot and its optional paired data buffer.
type Slot struct {
	Index    uint32
	DescPhys uint64
	Desc     []byte
	Buf      *Buffer

	seq uint64
}

// Ring manages FIFO acquisition, publication, completion sync, and fail-closed
// poisoning over a fixed DMA descriptor table and optional per-slot buffers.
type Ring struct {
	mu        sync.Mutex
	pool      *Pool
	count     uint32
	mask      uint64
	descSize  uint64
	descs     *Buffer
	bufs      []*Buffer
	allocSeq  uint64
	pubSeq    uint64
	compSeq   uint64
	closed    bool
	poisonErr error
}

// NewRing allocates a contiguous descriptor table and optional per-slot data
// buffers from pool. On partial allocation failure, all already-allocated
// buffers are cleanly returned to pool.
func NewRing(pool *Pool, cfg RingConfig) (*Ring, error) {
	if pool == nil || cfg.Count == 0 || !isPowerOfTwo(uint64(cfg.Count)) || cfg.DescSize < 8 {
		return nil, ErrInvalidConfig
	}
	if cfg.DescAlign != 0 && !isPowerOfTwo(cfg.DescAlign) {
		return nil, ErrInvalidConfig
	}
	if cfg.BufAlign != 0 && !isPowerOfTwo(cfg.BufAlign) {
		return nil, ErrInvalidConfig
	}
	if pool.Mode() == Cached {
		line := pool.Coherency().CacheLineSize()
		if cfg.DescSize%line != 0 {
			return nil, fmt.Errorf("%w: cached ring DescSize (%d) must be a multiple of cache line (%d)", ErrInvalidConfig, cfg.DescSize, line)
		}
	}
	descAlign := cfg.DescAlign
	if descAlign == 0 {
		descAlign = 16
	}
	totalDesc := uint64(cfg.Count) * cfg.DescSize
	if totalDesc/cfg.DescSize != uint64(cfg.Count) {
		return nil, ErrInvalidConfig
	}
	descs, err := pool.Alloc(totalDesc, descAlign)
	if err != nil {
		return nil, err
	}
	var bufs []*Buffer
	if cfg.BufSize > 0 {
		bufs = make([]*Buffer, cfg.Count)
		for i := uint32(0); i < cfg.Count; i++ {
			b, err := pool.Alloc(cfg.BufSize, cfg.BufAlign)
			if err != nil {
				for j := uint32(0); j < i; j++ {
					_ = bufs[j].Free()
				}
				_ = descs.Free()
				return nil, err
			}
			bufs[i] = b
		}
	}
	return &Ring{
		pool:     pool,
		count:    cfg.Count,
		mask:     uint64(cfg.Count - 1),
		descSize: cfg.DescSize,
		descs:    descs,
		bufs:     bufs,
	}, nil
}

// Count returns the number of slots in the ring.
func (r *Ring) Count() uint32 { return r.count }

// DescSize returns the byte stride of each descriptor.
func (r *Ring) DescSize() uint64 { return r.descSize }

// DescPhysAddr returns the physical base address of the contiguous descriptor table.
func (r *Ring) DescPhysAddr() uint64 { return r.descs.PhysAddr() }

// DescBuffer returns the underlying descriptor table Buffer.
func (r *Ring) DescBuffer() *Buffer { return r.descs }

// InFlight returns the number of published slots awaiting Retire.
func (r *Ring) InFlight() uint32 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return uint32(r.pubSeq - r.compSeq)
}

// Acquired returns the number of slots claimed by Acquire (published or pending).
func (r *Ring) Acquired() uint32 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return uint32(r.allocSeq - r.compSeq)
}

// Err returns the ring's poison error, if any.
func (r *Ring) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.poisonErr != nil {
		return r.poisonErr
	}
	return r.pool.Err()
}

func (r *Ring) checkLiveLocked() error {
	if r.closed {
		return ErrReleased
	}
	if r.poisonErr != nil {
		return fmt.Errorf("%w: %v", ErrPoisoned, r.poisonErr)
	}
	if err := r.pool.Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrPoisoned, err)
	}
	return nil
}

func (r *Ring) makeSlotLocked(seq uint64) (Slot, error) {
	idx := uint32(seq & r.mask)
	offset := uint64(idx) * r.descSize
	phys, data, err := r.descs.Slice(offset, r.descSize)
	if err != nil {
		return Slot{}, err
	}
	var buf *Buffer
	if len(r.bufs) > 0 {
		buf = r.bufs[idx]
	}
	return Slot{
		Index:    idx,
		DescPhys: phys,
		Desc:     data,
		Buf:      buf,
		seq:      seq,
	}, nil
}

// Acquire claims the next producer slot in FIFO order.
func (r *Ring) Acquire() (Slot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkLiveLocked(); err != nil {
		return Slot{}, err
	}
	if r.allocSeq-r.compSeq >= uint64(r.count) {
		return Slot{}, ErrRingFull
	}
	slot, err := r.makeSlotLocked(r.allocSeq)
	if err != nil {
		return Slot{}, err
	}
	r.allocSeq++
	return slot, nil
}

// ReleaseAcquired cancels the most recently acquired, not-yet-published slot.
func (r *Ring) ReleaseAcquired(slot Slot) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkLiveLocked(); err != nil {
		return err
	}
	if r.allocSeq == r.pubSeq || slot.seq != r.allocSeq-1 || slot.Index != uint32(slot.seq&r.mask) {
		return ErrInvalidSlot
	}
	r.allocSeq--
	return nil
}

// Publish syncs the slot's data buffer (if present) and descriptor bytes for
// device DMA and marks the slot in flight. Slots must be published in Acquire order.
func (r *Ring) Publish(slot Slot, dir Direction) error {
	if !dir.valid() {
		return ErrInvalidConfig
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkLiveLocked(); err != nil {
		return err
	}
	if r.pubSeq >= r.allocSeq || slot.seq != r.pubSeq || slot.Index != uint32(slot.seq&r.mask) {
		return ErrInvalidSlot
	}
	if slot.Buf != nil {
		if err := slot.Buf.SyncForDevice(dir); err != nil {
			return err
		}
	}
	offset := uint64(slot.Index) * r.descSize
	if err := r.descs.SyncRangeForDevice(offset, r.descSize, ToDevice); err != nil {
		return err
	}
	r.pubSeq++
	return nil
}

// PeekComplete returns the oldest in-flight slot without retiring it.
func (r *Ring) PeekComplete() (Slot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkLiveLocked(); err != nil {
		return Slot{}, err
	}
	if r.compSeq >= r.pubSeq {
		return Slot{}, ErrRingEmpty
	}
	return r.makeSlotLocked(r.compSeq)
}

// SyncDescriptorForCPU syncs an in-flight slot's descriptor bytes so the CPU
// can safely read device-updated status or ownership bits.
func (r *Ring) SyncDescriptorForCPU(slot Slot) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkLiveLocked(); err != nil {
		return err
	}
	if slot.seq < r.compSeq || slot.seq >= r.pubSeq || slot.Index != uint32(slot.seq&r.mask) {
		return ErrInvalidSlot
	}
	offset := uint64(slot.Index) * r.descSize
	return r.descs.SyncRangeForCPU(offset, r.descSize, FromDevice)
}

// Retire completes the oldest in-flight slot, syncing its data buffer (if
// present) for CPU reads and returning the slot for future Acquire calls.
func (r *Ring) Retire(slot Slot, dir Direction) error {
	if !dir.valid() {
		return ErrInvalidConfig
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkLiveLocked(); err != nil {
		return err
	}
	if r.compSeq >= r.pubSeq || slot.seq != r.compSeq || slot.Index != uint32(slot.seq&r.mask) {
		return ErrInvalidSlot
	}
	if slot.Buf != nil {
		if err := slot.Buf.SyncForCPU(dir); err != nil {
			return err
		}
	}
	r.compSeq++
	return nil
}

// Poison permanently poisons the ring, its descriptor table, and every slot
// data buffer. None of those spans will ever be freed or reused.
func (r *Ring) Poison(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	if err == nil {
		err = ErrPoisoned
	}
	if r.poisonErr == nil {
		r.poisonErr = err
	}
	r.descs.Poison(r.poisonErr)
	for _, b := range r.bufs {
		b.Poison(r.poisonErr)
	}
}

// Close releases the descriptor table and slot buffers back to the pool.
// It refuses if the ring is poisoned or if any slot is still acquired/in-flight.
func (r *Ring) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ErrReleased
	}
	if r.poisonErr != nil {
		return fmt.Errorf("%w: %v", ErrPoisoned, r.poisonErr)
	}
	if err := r.pool.Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrPoisoned, err)
	}
	if r.allocSeq != r.compSeq {
		return ErrInFlight
	}
	all := make([]*Buffer, 0, len(r.bufs)+1)
	all = append(all, r.bufs...)
	all = append(all, r.descs)
	if err := r.pool.freeAll(all); err != nil {
		return err
	}
	r.closed = true
	return nil
}
