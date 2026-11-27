# Jitter RNG source provenance

FES vendors the public header and the C sources and headers in `arch/` and `src/` from commit `fbbe36ed459a9f4eeb6caecfe66efe581a638fba`. Its signed history describes it as `v3.7.0-353-gfbbe36e`. This is a development revision. Its architecture layer includes a freestanding AArch64 timer and memory interface introduced after v3.7.0. The firmware build reads the local files and verifies the SHA-256 manifest in `sources.json` before compilation. [Upstream tree](https://github.com/smuellerDD/jitterentropy-library/tree/fbbe36ed459a9f4eeb6caecfe66efe581a638fba)

The annotated v3.7.0 tag object is `8b3874a980f5f43bb79390bbf94885fa8d18103d`, pointing to `e783cf1c450bce4d72f95c9f9c84546a6094976a`. The selected commit and tag both verified with Stephan Müller's published OpenPGP fingerprint `342C 4E3A 39EA 5F19 909B E38A AE5D 0DA3 FD09 2353`. The vendored source files and licenses are byte-identical to Git blobs in the selected commit. [Upstream signing key](https://www.chronox.de/about/index.html)

The FES allocator, bounded clock wrapper, compiler flags and the cold-boot adapter live in `../nano_jitter_port/`. For the shared runtime noise source, the diagnostic uses the prescribed counter divisor of 1 and records the low byte of each raw sampling interval, including stuck intervals. The serving code checks that upstream measured the same divisor and accepts exactly the selected operating profile. The physical entropy estimate applies to the measured deployment and its assessment. [Upstream raw recording procedure](https://github.com/smuellerDD/jitterentropy-library/tree/fbbe36ed459a9f4eeb6caecfe66efe581a638fba/tests/raw-entropy)

The original distribution terms and notices are included in `LICENSE`, `LICENSE.bsd` and `LICENSE.gplv2`.
