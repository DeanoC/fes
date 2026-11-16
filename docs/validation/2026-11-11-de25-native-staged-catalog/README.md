# Native startup with a retained package catalog

Against base `32449e9ea`, writable native startup reads the new
`nano-staged-program-catalog-v1` envelope from `FES/INSTALL.JSN`, retains all
members before menu construction and preflights/publishes them atomically.
The existing fixed 8 KiB upload/control bound remains unchanged. The additional
ceilings are 32 unique entry/package identities and 64 MiB of upload content;
32 full records need not fit within 8 KiB. Source reads use the remaining content
allowance, never a fresh per-member 64 MiB allowance. Parsed archive/ROM
allocations are excluded from that content count.

The old single-program envelope/API path is removed and rejected. Only absence
of the optional catalog produces `ErrStagedCatalogAbsent`; a missing member is
a source error. Startup still independently checks storage ownership before
ignoring an absent or quarantined optional source. Uncertain storage prevents
startup. A bad later source or failed platform preflight returns/publishes no
partial batch, leaving a healthy installed library independent of that source.

The host-only `cmd/stage-installer` selects explicit entry IDs from an existing
Nano export, validates package/ROM content, creates an empty bootstrap library,
copies exactly hashed inputs and publishes the catalog last in a fresh output
directory. Existing output refuses. Matched boot/menu artifacts must be supplied
separately; the output is staging content, not a complete bootable image.

The race-enabled tests produce two versions of one synthetic core on a real
FAT32 image, retain both under the active lease, install second then first through
the normal client API, alternate launch/return, and discover/launch both using a
fresh store/loader/service without source attachment or reinstall. Independent
mtools reads verify both installed archives. Ten whole-batch cases cover missing
catalog versus missing member, changed digest, duplicate ID/package, empty,
legacy, unknown, oversized and incompatible later input. Producer refusal tests
and the remaining-read-budget tests also pass. The real sealed SDK package
separately passes the actual host producer CLI and existing-output refusal.

All eight selected gates pass, including 1,935 parent regressions (three skips),
generated-consumer consistency, storage lifecycle and actual patched TF-A C plus
Verilated FPGA endpoint integration with modeled bridges/memory/SDM. The final
AArch64 TamaGo SDK cross-build passes with matching selected source hashes;
counts, ELF identity and retained paths are in `result.json`. The first SDK was
superseded when missing-catalog/member handling was corrected. Its evidence is
retained; only `native-sdk-r2` is current for this change.

No board action, matched boot-chain assembly or hardware acceptance took place.
Native Ethernet remains disabled. Next integration is a matched boot chain and
bounded physical multi-package install/discovery/launch qualification. Native
Ethernet, broader cores/devices and full DE10 appliance parity remain open.
