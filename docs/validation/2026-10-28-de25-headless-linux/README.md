# DE25-Nano headless Linux: host validation, 2026-10-28

The standalone AArch64 image and SDK built successfully. Structural media/rootfs
verification, QEMU userspace probes and an exported-SDK compile/run passed.
This is host-only evidence. No block device, FPGA or QSPI was programmed and
no physical DE25 boot was performed.

## Source and exact artifacts

- Base: `c7d08235308562964f875d2dbb3fb95f6c9c91ec`.
- Tested producer: `e949e5db427841cc912ab090043a04bc646e7df8`.
- Recipe SHA-256: `0f73479fe7e3c03df5a0fb99a0f7c105d9e350f99fbf10339ad3906a23a0cde6`.
- Builder image: `sha256:ea200655b92ab1211bc8b326c6e799815f1d648b2d5e5a6c982f597cb2b9650d`.

[image.json](image.json), [verify.json](verify.json) and [smoke.json](smoke.json)
are exact copies of the completed build's receipts. Both verification reports
bind the same recipe, rootfs and card digests. Build inputs came from the clean
committed snapshot; the dedicated AArch64 compiler cache was reused. This was
not an independent reproducibility campaign.

| Artifact | Bytes | SHA-256 |
| --- | ---: | --- |
| `fes-de25-nano.img` | 3984588800 | `92d058cd6c15fe25854e4cb129ba14a57593f407aad18a0c7fb2d80a530164cc` |
| `rootfs.ext4` | 134217728 | `a656a7cd9cbe5db7f43a0f18cfe3969f434e194f6018f54aa24ffb80ca4555c5` |
| `rootfs.tar` | 4341760 | `f04912f5c5badf3b0cfee0ce3c2351336a76032015b3ca8f42dc30a4760c7dd6` |
| `aarch64-sdk.tar.gz` | 64922403 | `1a0a1de7b9be748090c72a096a5c601dbf837e3cb18ecc158c1fc9f9b2ad2132` |

The card preserves the locked Terasic BSP MBR/FAT prefix and replaces Linux
partition 2 with the FES rootfs. The receipt also records every extracted vendor
boot artifact and the separate factory firmware dependency. DE10 boot/media
pins, first-party modules and shared contracts are unchanged.

## Completed checks

- 18 focused DE25 build/boot/rootfs tests passed, including output-lock,
  receipt/hash, malformed-input and SSH identification framing cases.
- 67 affected-test, test-mode and HIL planner tests passed.
- `make check` passed: 21 generated consumers and 37 fixture copies matched.
  Documentation links and whitespace checks passed.
- `make de25-linux`, `make verify-de25-linux` and `make smoke-de25-linux`
  completed successfully for the producer and artifacts above. The rootfs
  checks include AArch64 BusyBox/Dropbear, serial getty, DHCP and the selected
  development login. `/etc/dropbear` was inspected as a real root-owned
  directory with mode 0700 and no baked host keys.
- QEMU user emulation reported `aarch64` and served
  `SSH-2.0-dropbear_2020.81` with a temporary key. SSH authentication,
  Linux kernel boot, Ethernet and FPGA behavior were not tested by that probe.
- The exported SDK was unpacked to a new directory, relocated, and used to
  compile a C program that requires `__aarch64__`. QEMU ran it successfully:
  `FES DE25 AArch64 SDK probe passed`. The exact
  [probe](sdk-probe.sh) and [output](sdk-smoke.log) are retained.

The local `make test-changed` run passed 1,040 parent tests with two skips, then
failed in unchanged `TestHomeLoungeNativeRender/settings`: its room draw took
50.65 ms against a 48 ms wall deadline, with 2.25 ms thread CPU and three
involuntary switches while the image build was running. The isolated tenfoot
race-enabled retry of that test passed in 1.779 s ([output](ui-retry.log)). The
aggregate affected run remains failed; the retry is separate evidence. FogCast
has the same tree as the base, `d9306de9a3b6f9225bc17c1ccd814a0526574dbf`.

## Local packaging observations

Inspecting the fakeroot startup source and its process state showed descriptor
enumeration under this host's roughly billion-descriptor container limit. The
DE25 container now caps `nofile` at 65,536, allowing fakeroot packaging to finish.

The earlier QEMU probe failed when one socket read included binary SSH key
exchange after the identification line. The tested producer reads one bounded
line; its regression covers fragmented identification followed by binary data.

## Remaining physical gate

Use the exact card digest above on an identified DE25-Nano, following the
applicable claim and authorized media-delivery procedure. Confirm board
revision, factory HPS QSPI dependency and MSEL AS mode before boot; the vendor
instructions require at least an 8 GB microSD card. Retain serial boot/kernel
and device-tree identity, root partition, observed MAC/DHCP address, an actual
SSH login, and a clean reboot with successful serial/SSH startup and the same
persisted SSH host key. If testing both boards, record distinct MAC identities.

Runtime programming, MENU/display, HPS DDR, playable cores and managed appliance
updates remain outside this milestone. Physical acceptance stays `not-run`.
