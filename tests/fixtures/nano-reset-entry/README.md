# Pinned recovery SPL fixture

`spl_agilex5.c` is the unmodified GPL-2.0 board source from Terasic U-Boot
`d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413`, path
`arch/arm/mach-socfpga/spl_agilex5.c`. SHA-256:
`e3f42e5c2d8a97a9183343090b8296b3b7fe63b1d1f057c31a53a33e4774750e`.
The reset-entry producer checks this source before inserting the separate
read-only SPL diagnostic. The fixture grants no hardware or reset authority.
