# Tape oracle

`oracle.json` records libspectrum 1.5 (the tape engine of Fuse 1.6.0) playing
each oracle fixture that `fixtures()` in `convert_test.go` builds. For every
fixture it holds the SHA-256 of the image and, for a 48K and a 128K, the line
count and SHA-256 of the run list that `tapedump` prints: one `level T` line
per run of one EAR level, merged across zero-length edges, and `stop level`
where Fuse stops the tape. Edges are applied as `fuse/tape.c` applies them.

`tapedump` links the libspectrum objects of a `libretro/fuse-libretro`
`e997e2bc` build. To record the file again:

1. Build `tapedump.c` and `stubs.c` (kept with the FES Spectrum Fuse oracle
   harness) against that build's `libspectrum/*.o`, `libspectrum/myglib/*.o`
   and `bzip2/*.o`, with `-lz -lm`.
2. `SPECTRUMTAPES_FIXTURES=DIR go test ./internal/spectrumtapes -run TestWriteOracleFixtures`
3. For each fixture `F` in `DIR`: `tapedump F 48 > F.48.runs` and
   `tapedump F 128 > F.128.runs`.
4. Write each fixture's image and run-list hashes into `oracle.json`.

Fixtures that only this package reads (TZX CSW, call sequences and skipped
blocks, and a PZX general STOP, which libspectrum 1.5 reads as a pause of an
unset length) are checked against the package's own engine instead.
