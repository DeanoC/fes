# pc386 DE10 registered product timing, 18 November 2026

The registered `fes.pc386` product producer passed its lockstep, Quartus and
seal checks on a clean detached checkout of main
`5e592f00389fd2bb61194788c917c36f5c880dec` (tree
`ff53605e527f134b939783a94d274f7b115c73e4`). This is the experimental
registered configuration on that source; it is not a release or kit acceptance.
The separately enabled 286 reference-governor diagnostic was not selected, and
this result says nothing about timing with it enabled.

## Seeds and timing

The product recipe declared `[1]` and the retained QSF confirms seed `1`.
The first and only attempt passed: **1 seed to close, of 1 tried**. This is
not a measurement of yield across other seeds.

Quartus Prime Lite 17.0.2 Build 602 returned the following worst slack across
its analyses, with zero total negative slack in every analysis:

| Analysis | Worst slack | TNS |
| --- | ---: | ---: |
| Setup | +0.479 ns | 0.000 ns |
| Hold | +0.114 ns | 0.000 ns |
| Recovery | +5.955 ns | 0.000 ns |
| Removal | +0.268 ns | 0.000 ns |
| Minimum pulse width | +1.122 ns | 0.000 ns |

The worst setup slack is on the 61.44 MHz system clock. Dividing 0.479 ns by
its reported 16.276 ns period gives **2.943%**, near and slightly under the
forward plan's approximate 3% timing-health threshold. TimeQuest separately
reports a same-clock Fmax of 63.30 MHz, 3.03% above 61.44 MHz. Fmax excludes
paths between clocks and does not replace the slack-based checks. This is a
timing-health finding, not a failed timing gate; [#1932](https://github.com/DeanoC/fes/issues/1932)
tracks the remaining margin and coverage questions.

The TimeQuest unconstrained-paths summary lists **zero unconstrained clocks,
2 unconstrained input port paths and 34 unconstrained output port paths** in
both setup and hold. The producer checks passed, but this record does not
claim complete endpoint coverage or whole-board physical timing signoff.

## Build and retained evidence

On Powerboat the clean checkout was at
`out/dev/1902-pc386-main-seed-5e592`; from its `sources/misteross` directory
the bounded supervisor invoked:

```sh
python3 cores/fes-pc386/tools/build_pc386_product.py \
  --root /home/deano/.codex/worktrees/d31c/fes/out/dev/1902-pc386-main-seed-5e592/sources/misteross \
  --cache-root /home/deano/fes/out/cache/misteross-toolchains --with-lockstep
```

The producer selected an authenticated installed toolchain; the pc386 build
did not route on a GPU. Full locked-Yosys netlist lockstep passed all
1,758,699 cases from the 941-file corpus, alongside its directed suites.
The lockstep report SHA-256 is
`05ad9c0452046497bd841e9ac5f7261a5774fac489caa89c1d301f371b00b928`.
A separate DE25 Quartus job was active when the overall job started; it was
no longer observed immediately before this build's Quartus phase. The overall
job had an 11-hour cap and heartbeats; the Quartus phase had a 3-hour cap.

- Build ID: `4eb5a469939ea3548da2a21f77e66afa`
- Package ID: `dd2904dc44abf1815acbbdf896d5fd0065bee06508eb5f8818232350a7a44c49`
- Record ID: `bb04bd9066f9a0194d4a2260cfc5956230cd431995f160f036380838d1856ce1`
- RBF SHA-256: `778c77bbebea2feebbe0340dd40ad75c55a1cca2eff5275436d46c7121ebdbd6`
- Package manifest SHA-256: `48d400efc6e60aa449d1274323300fb000e13163908dbfb2caf4c5ed055f1d00`
- Retained evidence manifest SHA-256: `e109aac607d585843d428af0f22d62a2dca1b4b29c1f315bca52f4e777a65b1e`
- Retained TimeQuest report SHA-256: `3a2efbb5442b477ded1de2713e00e7175056c6e59f5a64b799340c0d7a257083`

The package, build record, QSF, fitter and TimeQuest reports, lockstep report,
receipt and build log are retained locally under
`/home/deano/.codex/worktrees/d31c/fes/.1902-main-product-seed-5e592-20261118/`;
they are not committed or publicly hosted. The source entry and its retained
copy both passed `pc386_seal_evidence.validate`, and every copied member was
compared byte-for-byte with its source. On the host retaining these files:

```sh
cd /home/deano/.codex/worktrees/d31c/fes/.1902-main-product-seed-5e592-20261118
sha256sum -c seal-SHA256SUMS
```

All ten members passed. This result is specific to the recorded source and
artifact; a later source, build or enabled-governor configuration needs its
own measurement. No kit was contacted, and no hardware, game-corpus or pitch
acceptance is claimed.
