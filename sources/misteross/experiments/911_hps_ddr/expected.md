# 911 HPS DDR bridge

`make sim EXP=911_hps_ddr` writes `0xA65A` and `0x1234` through the
application mailbox and reads them from a model of
`cyclonev_hps_interface_fpga2sdram` port 2. `make oss EXP=911_hps_ddr`
requires the fork that places that cell at
`cyclonev_hps_interface_fpga2sdram.52.53.0`. Quartus comparison is not
implemented.

This is the FPGA-to-HPS DDR bridge (on-SoC DDR). It is not the MiSTer
GPIO SDRAM addon. The configuration constants match the MiSTer 64-bit
port-2 command wiring: burst length 1, low halfword, byte enables
`0x03`. The 64-bit data for that command returns on read/write port 3.

GPO/GPI use `fes.application` 1.0 framing. Identity opcode 1 returns
magic `0x4546` / `0x3153` and ABI tag 3. Opcode 18 is local to this
probe and uses the same index 0/1/2 address, write and read sequence as
`910_sdram_addon`.

A development RBF load leaves the FPGA SDRAM port and HPS bridges
contained. The probe releases `FPGAPORTRST` (`0xFFC25080` = `0x3fff`),
the HPS bridge reset (`0xFFD0501C` = `0`), and the L3 remap
(`0xFF800000` = `0x19`), then restores containment. It does not write GPO.
Claim the designated kit with `scripts/kit.py session`, load the
exact OSS RBF, and run `hardware/probe.sh`. Never take over another
owner. Simulation does not prove the HPS memory controller accepted the
port.
