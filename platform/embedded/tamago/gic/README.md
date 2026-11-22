# Bare-metal GICv3 interrupts

`platform/embedded/tamago/gic` provides a small, direct ARM GICv3 interrupt
controller for single-core bare-metal EL1 code on both QEMU `virt,gic-version=3`
and DE25-Nano (Agilex 5 GIC-600).

## Model

- **Non-Secure Group 1 only:** uses `ICC_SRE_EL1`, `ICC_CTLR_EL1` (`EOImode=0`),
  `ICC_PMR_EL1`, `ICC_BPR1_EL1`, `ICC_IGRPEN1_EL1`, `ICC_IAR1_EL1`,
  `ICC_EOIR1_EL1` and `GICD_CTLR` (`ARE_NS | EnableGrp1NS`). This works
  identically under TF-A BL31 on DE25-Nano (`DS=0`, Non-Secure EL1) and on
  QEMU `virt,gic-version=3` (`DS=1`).
- **Direct hardware access:** fixed `[640]func()` handler table indexed by
  `INTID`, direct MMIO helpers, and named board IRQ constants (`board/de25nano`
  and `board/qemuvirt`). No ITS/LPIs, IRQ domains, or capability layers.
- **TamaGo vector integration:** TamaGo's `arm64.CPU.Init()` installs `VBAR_EL1`
  and relays IRQ exceptions through `arm64.IRQ_SIGNAL`. `(*Controller).Serve`
  runs a single Go goroutine that unmasks `DAIF.I`, waits for the signal, drains
  pending interrupts via `Dispatch()`, and re-enables IRQs. Existing polling
  drivers are unaffected unless a program calls `board.GIC().Init()`.

## Usage

```go
g := board.GIC()
if err := g.Init(); err != nil {
    panic(err)
}
stop := make(chan struct{})
go g.Serve(stop)

g.Register(board.IRQPhysTimer, func() {
    gic.DisarmPhysicalTimer()
    // handle timer tick
})
g.Enable(board.IRQPhysTimer)
gic.ArmPhysicalTimer(uint32(board.CounterFrequency() / 1000))
```

## WFI idle governor

Call `g.EnableIdle()` after `g.Init()` to replace the polling `runtime/goos.Idle`
stub with an ARMv8 `WFI` idle governor backed by the EL1 physical timer (PPI 30).
`time.Sleep` and channel waits sleep in `WFI` until either the next Go timer
deadline or any enabled interrupt fires; `g.DisableIdle()` restores the polling
stub and disables PPI 30.
