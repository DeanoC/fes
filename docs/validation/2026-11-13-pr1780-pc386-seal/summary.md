# pc386 Quartus seal for PR #1780, 13 November 2026

This is a fresh normal pc386 Quartus seal of commit
`3bc8449a590d9ee8841eb79f65963c6e348f0ed3`, whose first parent is the PR head
`2e9b086e20a2009c7049526a5ed701671b37f56a`. It records the BIOS digest
changed by PR #1780 and closes the missing-seal evidence gap from r1.

## Result

**PASS.** `make build-fes-pc386-quartus CACHE_ROOT=/home/deano/fes/out/cache/misteross-toolchains SEED=1`
completed with Quartus Prime Lite 17.0.2 Build 602. The build used the
authenticated pc386 Yosys HIP slot (`e4d761ef3827204b8315bd07d93817dd4b9a60acd9dc7a039a49bf56e51711a5`);
Quartus performed the fit and timing analysis.

- Build ID: `8295bebf93eb90e7c7b9846231de7c07`
- Seed: `1`
- BIOS SHA-256: `d11a805ed1cbbb00e8d764035f467c60c4fd22a941fa09ece51cf4095ba8541a`, matching `firmware.lock`
- VGA BIOS SHA-256: `8522568e32a33b2f5649109260fec70b0b2cd0d80a6216ac7af6a1041c102e02`
- RBF SHA-256: `96faa87d9570e720e86c1a9b2dc7a92bb501d8bd42acefa51244b85166167156` (3,911,628 bytes)
- Timing: setup +0.977 ns, hold +0.107 ns, recovery +7.664 ns, removal +0.496 ns, minimum pulse width +1.122 ns; every analysis has 0.0 ns TNS.
- Clocks: system 61.44 MHz, pixel 74.27 MHz, audio 12.29 MHz.

The full build summary and exact input receipt are retained here as
[`seal-summary.json`](seal-summary.json) and
[`build-inputs.json`](build-inputs.json). The committed result checker also
validated status, timing, locked BIOS digest and exact input revision.

The earlier 08:14 attempt failed closed because that HIP slot was absent. The
current seal used the existing authenticated slot; this fix did not create or
modify cache contents. If the slot is absent, the sealer now reports the exact
`make toolchain-fes-pc386 CACHE_ROOT=... GPU_ROUTER=HIP` provisioning command.

This record is Quartus build and timing evidence only. No kit or hardware test
was run.
