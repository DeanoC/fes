# DE25 initial owner-kernel physical boot and supplier refusal

Bottom board TRWU8J8G boots the initial candidate image produced by
829cf0353d1ea7a479f95bdbdf3d72e0391ab810 with the board kernel produced by
e6bdd35973cec91ee3ef93b92862680de3b7988f. The preceding physical SD write and
complete readback are recorded in the [media delivery](../2026-10-31-de25-owner-card/README.md).

Physical serial capture includes SPL, U-Boot, Linux and the login prompt.
Linux reports 6.12.11-fes-de25-owner on the DE25 model and mounts the candidate
ext4 root on mmcblk0p2. UART login observes boot UUID
b989f017-f9f3-45dd-811b-0482b1b65421 and the unchanged approved card CID
275048534436344760702d33e0017c00. Automatic networking obtains
192.168.10.165 from DHCP, and Dropbear starts. Its new Ed25519 host key was
created once and printed over the identified physical UART before SSH; no
network key enrollment or ambient SSH identity/configuration was used.

The existing read-only first-boot collector passes kernel, architecture,
board/root/console, seven selected userspace file hashes, Ethernet and pinned-key
SSH checks. UART and SSH report the same boot UUID. The collector retains its
usual limit: selected files match; the entire writable SD is not independently
attested from the running target. This does not prove DMA or a whole normal
FES lifecycle. The new root key/random seed/network configuration are normal
first-boot filesystem writes after the recorded complete card readback.

The permanent DMA owner does not bind. Kernel probe returns EOPNOTSUPP (-95),
so /dev/fes-de25-scanout is absent and the FPGA startup check cannot pass.
The SMMU initializes, the actual owner group is singleton and reports DMA,
and the FPGA service/manager/region eventually bind as built-ins. Read-only
supplier inspection identifies the omitted ancestor: /sys/devices/platform/soc@0
is bound to simple-pm-bus and exposes its unbind attribute. The protected
SMMU is its child. The owner's existing transitive parent/supplier policy
correctly refuses this removable ancestor; the failure is not waived or
converted to an admission pass.

The repair extends the source-bound, default-off supplier patch to
simple-pm-bus and requires its linked probe symbol. The pinned kernel builds
that driver under CONFIG_OF rather than an independent SIMPLE_PM_BUS symbol.
The initial repair's invented configuration check was rejected before its
kernel compile; its failed job is retained separately. The corrected recipe
requires built-in OF. Existing owner/guard/allocator policy and normal runtime
admission remain unchanged. The repaired kernel still requires a new physical
boot and actual userspace-control tests.

After the physical restart the shared HDMI fan-out delivers 90 frames in three
seconds, all with raw hash 82b13e12b17b9b7b4e1b7e6ef4a950de, and the frame is
black. Before this boot it showed ZX81 7/0/1 with hash
6c7ecd1671ca5d6dcb10435de986bc64. The change confirms changed-picture propagation
through the shared feed for this restart. Black is not a working MENU/ZX81
picture or new core acceptance. No raw capture node or producer was changed.

The first candidate is a Linux boot/SSH diagnostic success and an owner-binding
failure. No grant, physical DMA/SMMU/firewall transfer, MMIO publication, FPGA
load, timing signoff, normal runtime, MENU/catalog or ZX81 lifecycle/recovery
pass is claimed. The approved HPS-first QSPI is unchanged. UART sessions are
logged out and closed; the kit claim remains held for the repair trial rather
than being released as restored or promoted to a new baseline. The exact
pretrial SD image remains independently backed up for restoration.
