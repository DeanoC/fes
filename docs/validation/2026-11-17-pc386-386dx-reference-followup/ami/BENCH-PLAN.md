# AMI 386DX-25 TopBench screen

The selection rule remains the precommitted 47–65 scaled real-board IQR at
386DX 25 MHz with effective cache enabled. This is a score screen only, not a
game pace or reference qualification. A score outside the band rejects this
configuration; inside the band remains only a candidate until effective cache,
uninterrupted full-speed execution, C800 mapping and the game path are
independently established.

Use the private AMI 050591 setup's saved CMOS
`4b684a11fce2c469ea8275d02a9ae5caa4d1a487a4907b2152f903d64249b912`,
the post-setup 386DX-25 config and a fresh private copy of known benchmark
disk `279ac36b8cbf0a77c99899e60be45e975fd33a828e725dd13568e451eee90027`
with only the small `CACHE.COM` probe added. The original source remains
unchanged. Verify added binary and original TOPBENCH.EXE SHA-256
`4428fc27d5c2987e5bd177540843190ebb33f31ca6ddacc6e9f8fb994076e63d`, as
well as 86Box executable, config, NVR and ROM tree before/after.

The probe reads the OPTi 391 indexed register 21h via ports 22h/24h and
reports `REG=xx`. This selector write and read reset the emulated index latch;
port 22h reads 0xff in the inspected 86Box source, so restoring it does not
prove the earlier latch value. The probe does not write the data register, but
its selector side effect is disclosed. The v6 source maps bit 0x10 on a
register-21h write to `cpu_cache_ext_enabled`; the reading is only same-boot
chipset state, not direct observation of the effective CPU flag or proof this
unrebuilt binary matches source. BIOS `Cache Memory: Enabled` was observed
before saving, not re-read in the benchmark boot. Report those limitations.

Capture 800×520 at 4 fps continuously; after a visually reviewed DOS prompt,
send `CACHE`, review its displayed REG result and prompt, then reviewed
`cd marks`, `cd topbench`, and finally `topbench -l`. Each action is gated by
fresh captured screens and a bounded independent visual review; if any
screen is missing, unexpected or ambiguous, stop without further input. Run
60 seconds after launching TopBench, take early/final score captures and
report visual score. Audit all 4 fps 100% indicator samples over a contiguous
38-second interval wholly inside visible TopBench execution; between-sample
speed and emulator indicator accuracy remain unknown. Preserve video,
screens, exact input hashes, logs, ROM audit, and process outcomes.

Use a 60-second deadline per review, 480-second overall cap, 10-second
heartbeat and exact-child cleanup, detached from SSH so transport loss cannot
orphan an unsupervised job. No IBM reference disk, kit, GPU or harness use.
