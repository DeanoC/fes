# Old-6526 timer and interrupt reference

`make sim-fes-c64-cia-reference` replays 385 deterministic bus programs at
three schedules: the native fractional 52.224 MHz / 985248 Hz cadence, an
early write/read pair in a 53-clock cycle, and a late pair in a 54-clock cycle.
It uses checked-in expectations and needs no external source or firmware.
The existing `sim-fes-c64-cia` register/port test remains a separate check.

The fixtures cover both timers, zero and short latches, 16-bit reloads,
start/stop/force and one-shot pipelines, latch writes around reload, mask
changes, ICR reads, old timer-B suppression, simultaneous timers, B counting
A underflows, and reset with pending work. Eight additional TOD programs
cover five/six-pulse input division, alarm equality, masked pending alarms,
later mask enable and acknowledgement before/after assertion. Per schedule
they check 55,834 cycle states, 813 register reads and 752 ordered physical IRQ
edges. The
ordered edges retain a rise and fall within the same guest cycle. Assertions
must occur at Phi2; ICR-read deassertions at `read_sample`; reset deassertions
at Phi2.

Each case has a 64-bit FNV-1a fingerprint of its complete state trajectory:
cycle, TA count/latch, TB count/latch, CRA, CRB, ICR, mask and physical IRQ.
Each field is encoded as four little-endian bytes. Register reads and IRQ
edges have explicit expected values and timestamps. A trajectory mismatch
identifies its program; the regeneration tool can emit the full source
trace for diagnosis. These fingerprints are regression checks, not
cryptographic attestations.

## Pinned source and regeneration

The independent oracle directly includes unchanged VICE 3.10 `ciacore.c`
and links unchanged `ciatimer.c` and `alarm.c`. It does not call the RTL or
reimplement the timer equations. Only allocation/logging, interrupt-number
registration, TOD timing randomness and external callbacks are stubbed.
Explicit `T` events invoke the unchanged TOD callback. The automatic power
clock is placed beyond the program horizon; the fixture supplies the pulses
directly, so it does not qualify the motherboard power-clock frequency.

Source: [VICE 3.10 archive](https://github.com/VICE-Team/svn-mirror/releases/download/3.10.0/vice-3.10.tar.gz).
Archive SHA256: `8e5bac18cbcb9f192380ad3ef881f8790f5b75c41d7b3da65d831985d864d6d1`.
`cia_timing_vectors.py` also verifies individual CIA, timer and alarm file
hashes before compiling. On a 64-bit Linux host with a C compiler, from
`sources/misteross`:

```sh
python3 cores/fes-c64/sim/cia_timing_vectors.py \
  --vice-root /path/to/vice-3.10 \
  --output /tmp/cia_timing_expected.h \
  --trace /tmp/cia_timing_oracle.csv
cmp cores/fes-c64/sim/cia_timing_expected.h /tmp/cia_timing_expected.h
make sim-fes-c64-cia sim-fes-c64-cia-reference
```

`cia_timing_program.h` contains the named input programs. Add or change
programs there, then regenerate the expected header. The program SHA256 is
recorded in that header. Each program has at most one bus operation per
cycle: `W` writes, `R` reads with acknowledgement, `X` resets at Phi2, and
`T` supplies a power input pulse at the declared Phi2 boundary.

The source oracle receives actual bus-cycle timestamps, so `write_offset`
is zero rather than the CPU API's default one. Its physical-pin callback
queues future events until their stated timestamps: VICE's internal
`irq_enabled` may change early when scheduling a callback and is not the
sampled pin. Reset cancels a pending same-cycle assertion. The native
schedule writes 27 SYS edges after Phi2 and samples reads 44 edges after
Phi2: the motherboard's comparisons against 26/43 see the previous
`cycle_clock` value. Additional schedules use write/read edges 1/2 and
51/52. All bus actions follow that cycle's timer transition.

## Implementation and limits

The RTL adapts VICE's bounded timer/interrupt transitions into registers
and combinational next-state logic. Each timer keeps delayed count-enable,
force-load and one-shot state. The previous count-enable decrements while
the newly computed enable detects zero. STOP and input-mode changes retain
pending pipeline work. A underflow queues B's step after both timers advance.
Interrupt source flags, bit 7 and the physical pin have separate state;
acknowledgement and mask changes use the old-6526 delay pipeline.

The original VICE timer/CIA implementation is by Andre Fachat, with CIA
improvements by Ettore Perazzoli, Andreas Boose, Alexander Bluhm and Olaf
Seibert. Alarm code is by Ettore Perazzoli and Andreas Boose. The adapted RTL
and source-linked oracle use GPL-2.0-or-later; the complete license is
`../rtl/c64_cia.COPYING`.

This models the old MOS 6526 (`CIA_MODEL_6526`), including the timer-B
suppression corner after an ICR read. The newer 6526A interrupt behavior is
not selected or qualified. TOD alarm cases qualify only their declared
digital input boundary and bus schedules; arbitrary analogue TOD phase,
write/pulse collisions and physical-chip behavior remain unqualified. External
CNT edges, serial shifting, FLAG input and PB timer outputs remain outside
the implemented interface. CNT
is held high, so B's gated and ungated A-underflow modes see the same events.
The fixtures qualify these deterministic digital scenarios and schedules,
not every analog pin subphase or a full-machine hardware artifact.
