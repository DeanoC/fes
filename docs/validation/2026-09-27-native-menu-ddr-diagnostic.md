# Native menu DDR diagnostic — 2026-09-27

## Scope and source

Owner: misteross, integrated in FES branch `feat/native-menu-scanout`.
Main dependency: `e21c49b9b77d9ba888eedaa3dd4bbc9d0df6fc51`.
Both sealed artifacts below were built from clean committed source
`4ba67c6d46728f685841c9f120a90887f232709c`.
Subsequent changes update a source assertion and this evidence; they do not
change the artifact inputs. Local build records remain under
`sources/misteross/build/oss/fes-menu{,-pattern}/`.

This slice connects the menu scanout to the actual shared `fes_hps_ddr`
port 0 and generated reserved-window base. It preserves the independent
DDR-free pattern diagnostic. It adds no described package, GP presentation
ABI, runtime presenter, boot selection or image policy.

## Authenticated synthesis and routing

Both producers enforce clean sources, Python read auditing, pinned tools,
execution provenance, legal routing, board electrical policy and 74.25 MHz
pixel timing. GPU 0 was used exclusively.

| Diagnostic | Seed | Pixel Fmax | RBF bytes | RBF SHA-256 |
| --- | --- | --- | --- | --- |
| DDR slot 0 | 4 | 97.2668 MHz | 2002846 | `065298d721b58d65218011dfec83257d0050fe77b2e52a5b89a79824d12a864a` |
| Independent pattern | 1 | 99.4728 MHz | 1997943 | `bae9d3207de74fd25193cb35fe885cbbadf396acbf42ae3e9497ceac36895c3a` |

DDR selects `toolchains/ramtest.lock`: Yosys
`b27035fcc1be6ec040df35a3adbe6d4149297cd8`, nextpnr
`f60b33aa977b237d0762fdef90de42987671b21d`, Mistral
`7ed06e21c18b047ec5c6d6a7e85e5ea2c8827039`.
Pattern retains `toolchain.lock`; its exact pins and executable hashes
are retained in its closed build record.

DDR layout passes both synthesized and routed structural validation:
width `0x16`, type `0x3f`, CPORT FIFO maps `0xd0`, FIFO CPORT maps
`0x2100`, AXI-MM select `0`. Unused command ports and all writes are
structurally constant zero. Routed CFG constants are resolved only from
explicit `MISTRAL_CONST` 32-bit zero/one drivers; dynamic nets remain invalid.

Seeds 1–3 on the preceding netlist left one overused wire. Their illegal
routes did not publish an artifact. The reproducer is recorded in
[routing issue #266](https://github.com/DeanoC/fes/issues/266).
The later read-only specialization changed the netlist; its legal seed-4
result does not resolve the earlier router defect. Seed selection is bounded
1–8 and recorded in provenance.

## Verification and review

All seven menu simulations pass: reader, video, pattern, board, DDR,
DDR board and disabled DDR board. Assertions cover full-frame pixels,
slot/sequence changes, bounded reads, inactive ports/writes, stalls,
PLL/startup gating, quiesce/drain, ordered hold/restart and unexpected hold.
The default full read/write RAM tester also passes SDRAM and all three HPS
DDR scans, signatures, holds, status and button Stop.

The final misteross unit suite passes: 877 tests, one skip. Parent `make check`
and the 22 affected/CI-gate tests pass. The broad affected-component pipeline
passes all 34 selected commands (planned at `ef9ffa05`). It spans
parent, shared contracts, runtime, FogCast and all selected core simulations.
It is not an exact-final-source receipt: later shared/producer changes are
covered by the final 877-test suite, menu/default RAM simulations and both
sealed builds recorded above.

Independent reviews found and verified the Python source-audit correction,
and found no correctness issues in the shared specialization or routed
constant validation. Tests reject excluded Markdown reads in both producer
modes and reject asserted or unknown routed command/configuration nets.

Shared wrapper parameters default to the original full read/write behavior.
Menu disables only operations whose master inputs are tied low. There is no
shared wire/schema or generated-consumer change.

## Containment and next integration

An unexpected hold during active traffic fails closed until reprogramming:
the shared guard hides responses and exports no drain-completion indication.
Ordered reader quiesce/drain before hold permits restart. This deliberately
trades recovery availability for containment in the diagnostic slice.

Classification: host simulations and sealed synthesis/routing only. No kit
programming was performed for these DDR bytes. Earlier physical pattern
checks do not establish acceptance of these rebuilt artifacts.

Next: described menu capability and GP protocol, runtime reserved-window
presentation with generation and SCM_RIGHTS ownership, then exact-artifact
kit scanout acceptance under the existing lease. Tenfoot/mesh integration
and image/boot policy follow that proof.
