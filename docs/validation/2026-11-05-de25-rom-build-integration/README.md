# Nano held-ROM producer and native build connection

The explicit ROM producer now seals a format-three package, with one firmware
requirement and a map bound to the unchanged FPGA payload and 4/8 KiB model
size. Its HPS-first pair identity includes that map. Verification reconstructs
the expected complete manifest/map from the sealed inputs. Other diagnostic
variants retain format two. Earlier format-two ROM artifacts are not relabeled.

The native builder requires the explicit ROM source profile, selected computer
program mode, an exact firmware image and a host Go compiler. A small host
adapter calls the existing retained package parser/linker to derive the private
activation receipt. It checks the independently expected package identity and
never encodes another kind of ROM link. The builder exports the firmware
snapshot, receipt observation and source/receipt-bearing catalog, then links
the sealed ROM resource map into the command. The command explicitly chooses
the ROM verifier. Image size, metadata receipt and profile mismatches reject.
The final build rechecks the source resource, firmware digest and ELF binding.

The actual host linker runs for both ROM sizes: changing firmware bytes changes
the receipt, while a foreign package identity or wrong image size rejects.
ROM metadata tests require an explicit nonzero receipt and reject a receipt on
ordinary profiles. Producer tests round-trip the actual package bytes through
the shared reader and verify unchanged payload, model sizes and distinct IDs.
All 56 selected boot/entry/ROM/USB composition checks and six producer checks
passed. The ARM64 native command compiles using an existing fixture profile,
without linked boot facts; this establishes compilation only. Initial test
preparation errors remain in their logs. Source/log hashes are in result.json.

No Quartus build, matched ROM boot chain or board operation belongs to these
checks. A fresh source-bound FPGA build is required before deriving the actual
native ELF and matching entry/BL31/SPL. Physical ROM loading, ordinary package
admission and full DE10-equivalent behavior remain unproven.
