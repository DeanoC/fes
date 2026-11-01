# DE25 trace-enabled Linux, stable networking and allocation-only observation

The verified image `7eb6266188823c9cd4810259fc7cc73eb70ec8f58129e5718f127fd17dd32a0b`
now boots on bottom TRWU8J8G from the user's new 32 GB card. This is opt-in
owner-kernel candidate evidence, not normal FES factory/MENU or physical DMA
frame acceptance. The previous working FES card is kept aside for rollback.

## Card, actual kernel and network

The first [card-write attempt](first-card-write-failure.json) completes the
write/flush but fails during verification after the USB reader disconnects.
It remains a failure. Following reconnection, a fresh read-only check reads all
3,984,588,800 bytes and [matches the expected SHA256](full-card-readback.json)
in 177.28 seconds; no image rewrite or weakened verification is used. The
user swaps that verified card after clean shutdown of the working system.

[Actual first boot](first-boot.json) verifies kernel producer `3dab0b06b`, linked
notes `e06e399b…` and matched root receipt `53a17111…`. The permanent owner is
root:root0600, major10/minor124. Its group0 translated-DMA attachment is a kernel
configuration observation, not physical translation acceptance. Initial boot
retains the missing-modules modprobe message, built-in-driver readiness and
service-channel probe deferral followed by FPGA-manager/region registration.

The observed SD CID is `1234564153544300200000000a019300`; the actual address
`02:8e:2a:b1:d3:bd` equals the SD-CID-derived rule installed before DHCP.
DHCP obtains `192.168.10.168`. Dropbear's standard `-R` generates host keys on
first connection. The key is read independently over UART before authenticated
pinned-key SSH; it is preserved through reboot. An initial filesystem check
stops because the fresh image has no firmware directory; the programmer creates
that directory when needed. This is retained separately from the successful
[actual tracefs event check](tracing-events.log).

An [ordinary software reboot](software-reboot.json) changes boot `22750e00…`
to `e5e896ba…`, preserving CID, MAC, IP, host key, kernel notes, root receipt and
owner permissions. UART records shutdown, U-Boot and Linux startup; the
[guarded SSH identity](software-reboot.log) verifies the new boot. No physical
power cycle is needed for this reboot. No FPGA image is loaded during that
network/reboot test.

## Actual allocation-only observation

The existing [bound artifacts](allocation-artifacts.json) are unchanged:
programmer `3d9410e75`/binary `68a88ea8…`, client `5a99f2073`/binary `ad0b7ad3…`,
and FPGA producer `d4c6544e3`/RBF `bc4290c1…`. The source-bound scanout-only
compiler/interconnect checks were already passed and retained in the
[earlier failed-frame record](../2026-11-01-de25-allocated-scanout/README.md).
The same FPGA is loaded with its permanent-owner marker, then the client is
invoked once with `--allocation-only`. It never maps pixels, publishes a frame
or issues POLL. Its one unexposed owner REAP succeeds before close.

A separate trace instance enables only `iommu:map` and `iommu:unmap`, filtered
to the diagnostic's actual PID270. The child is stopped before exec while that
filter is configured. The [actual trace](mapping.trace) contains exactly two
events, with [zero overruns, commit overruns or dropped events](mapping-stats.log)
on all four CPUs:

- One 8 MiB map from IOVA `[0xff800000, 0x100000000)` to physical
  `[0xbcd00000, 0xbd500000)`.
- One complete unmap of the same IOVA range, size and unmapped size 8 MiB.

The [parsed result](allocation-result.json) is kernel mapping/lifetime evidence.
It does not show that FPGA traffic uses that translation. The backing range
fits the previously observed firewall window, but that window is from the
previous boot and is not re-read here. The cause of the original AXI response
error remains unresolved. No third frame test is attempted on this image.

Under the exclusive lease, [known-success cleanup](cleanup.log) removes the
trace instance/mount, own overlay/firmware/configfs mount and temporary files.
[Final identity](final-identity.log) retains the same boot/MAC/owner permissions;
two vendor OF property-leak warnings remain. All SSH/UART clients are closed.
SRAM still contains the idle scanout diagnostic; no factory-FPGA restoration,
baseline promotion or board release is claimed. The candidate claim remains
retained. No QSPI or production bundle is changed.

Still required: physical SID/SMMU/firewall/DDR and full-frame acceptance; managed
idle/MENU, the single production factory and normal agent/catalog lifecycle;
safe restart/adoption, tape SAVE, failure/reset/whole-load bounds and timing
#1284. These positive Linux and mapping checks do not meet those gates.
