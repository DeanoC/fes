# Optimizer-generated shared 720p video integration

AlphaMister generated and selected this RTL without manual source repairs.
Candidate 0003 from `pong-video-area-v3b` reduces complete Pong pilot area
from 1138 to 1118 ALUTs (20, 1.76%), with 233 flip-flops unchanged.
The initialized named-state transition proof covers arbitrary binary current
counter states and all inputs. Fatal-warning lint passes, including the
explicit automatic-lifetime admission rule needed by the separate CI compiler.

The [compact evidence snapshot](2026-10-10-optimizer-video-area/evidence.json)
records source SHA256, panel seeds, receipt hashes and sealed package identity.
It is advisory and does not amend original campaign verdicts.
[AlphaMister PR22](https://github.com/DeanoC/alphamister/pull/22) contains the
optimizer implementation and gate regressions.

## Frozen pilot

FES source `8f5c5b598`; nextpnr
`1656e473e1442f9b734ff5f4cdfddfd013846b9e`; AlphaMister `371b985`.
Development seeds 30001–30004 passed 4/4. Private paired seeds 426–429
passed 4/4 for both sources. Final seeds 402, 406, 408, 430–434 passed 8/8,
with minimum reported pixel Fmax 77.208 MHz against 74.25 MHz.

The integrated source is byte-identical to the qualified generated source:
`6d92673855a4930908ae258d847ee99e6984c5ff18110a1131624b9294d6e901`.
The Pong game, shell, board constraints and shared video baseline did not change
between the pilot source and current-main integration.

## Current production context

The normal producer built and sealed from clean committed source
`5969e0f0911dbad351bcf16205b382b0fe9ab4d4`, using current pinned nextpnr
`9128800fa6e77c4138088a7107a95376ba47ff15`. Build ID:
`0f3ac9f20d9eac225f7014a912d42e55`. Package ID:
`403795ffb14593b192d44f2a0734050e26df94e9643ad1862ef9f1fa353a2836`.
Archive SHA256:
`8d6108e84657347092ca3ba13eaa46fda60b56a3d77c10f9e221ffde8e7cb05b`.

Seed 1 completes routing and meets the pixel requirement, with reported Fmax
91.550 MHz. Production synthesis uses 1138 ALUTs and 233 flip-flops.
The production build ID and tool context differ from the zero-build-ID pilot;
this count is not a matched comparison of the pilot's 20-ALUT reduction.
The stricter single-PLL Pong evidence check binds the raw achieved frequency,
error and packed M/N/K coefficients. Existing multi-PLL consumers retain their
current evidence behavior.

All four affected current-main simulations pass: Pong, demo/Catch, RAM Tester
and RISC-V, using CI Verilator 5.032 at
`8ff77e9d47351b0a59114929880687839a51840b`. The FES producer suite passes
237 tests (six skips), package-export tests pass, and independent review found
no remaining issues.

## Hardware status and limits

The contained kit A diagnostic is pending availability. Another interactive
operator holds the renewable lease; no takeover or hardware load was performed.
The PR remains draft. No factory/image hardware acceptance is claimed, and
Pong qualification does not qualify routing or timing in the other consumers.
Later documentation commits do not change the sealed package's recorded source
revision. Historical campaign and earlier sealed package receipts are unchanged.
