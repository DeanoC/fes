# i486 slice 1: generated results

Written by `generate.py` from the reports in `fits/`, `elaboration/` and `lockstep/`. Do not edit by hand.

## DE10

### Every fit

| Arm | Seed | ALMs needed | % of device | Registers | RAM blocks | DSP | CPU clock worst setup (ns) | Worst hold (ns) | Closes |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| a | 1 | 36,455 | 86.98 | 31,861 | 282 | 40 | +0.338 | +0.075 | yes |
| a | 2 | 36,463 | 87.00 | 31,823 | 282 | 40 | +0.404 | +0.076 | yes |
| a | 3 | 36,433 | 86.93 | 31,994 | 282 | 40 | +0.720 | +0.075 | yes |
| a | 4 | 36,488 | 87.06 | 31,895 | 282 | 40 | +0.560 | +0.056 | yes |
| b | 1 | 36,389 | 86.83 | 31,852 | 282 | 40 | +0.118 | +0.031 | yes |
| b | 2 | 36,428 | 86.92 | 31,970 | 282 | 40 | +0.322 | +0.089 | yes |
| b | 3 | 36,422 | 86.91 | 31,997 | 282 | 40 | +0.796 | +0.049 | yes |
| b | 4 | 36,378 | 86.80 | 32,001 | 282 | 40 | +0.967 | +0.091 | yes |
| c | 1 | 36,430 | 86.92 | 32,130 | 282 | 40 | +0.951 | +0.114 | yes |
| c | 2 | 36,489 | 87.07 | 31,997 | 282 | 40 | +0.474 | +0.086 | yes |
| c | 3 | 36,454 | 86.98 | 31,795 | 282 | 40 | +0.478 | +0.102 | yes |
| c | 4 | 36,432 | 86.93 | 31,991 | 282 | 40 | +0.685 | +0.099 | yes |

### By arm

| Arm | Seeds | Median ALMs | Spread | Seeds closing | Median CPU-clock setup (ns) | Worst CPU-clock setup (ns) |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| a, baseline | 1, 2, 3, 4 | 36,459 | 55 | 4 of 4 | +0.482 | +0.338 |
| b, build parameter | 1, 2, 3, 4 | 36,405.5 | 50 | 4 of 4 | +0.559 | +0.118 |
| c, runtime strap | 1, 2, 3, 4 | 36,443 | 59 | 4 of 4 | +0.582 | +0.474 |

### Per-module fits (seed 1, no flatten)

| Arm | Total ALMs | `fes_i386_cpu` | Everything else |
| --- | ---: | ---: | ---: |
| a | 36,398.3 | 24,926.4 | 11,471.9 |
| b | 36,697.3 | 25,235.7 | 11,461.6 |
| c | 36,568.7 | 25,075.2 | 11,493.5 |

Inside `fes_i386_cpu` (one level down; ALMs needed):

| Module | a | b | c | b - a | c - b |
| --- | ---: | ---: | ---: | ---: | ---: |
| `(own logic)` | 9,231.3 | 9,354.5 | 9,369.2 | +123.2 | +14.7 |
| `fes_i386_ex` | 810.3 | 850.6 | 841.4 | +40.3 | -9.2 |
| `fes_i386_l1d` | 2,138.7 | 2,172.6 | 2,120.6 | +33.9 | -52.0 |
| `fes_i386_biu` | 1,285.7 | 1,318.5 | 1,267.9 | +32.8 | -50.6 |
| `fes_i386_decode` | 718.4 | 748.4 | 736.3 | +30.0 | -12.1 |
| `fes_i386_seq` | 179.3 | 202.9 | 197.5 | +23.6 | -5.4 |
| `fes_i386_tlb` | 807.3 | 787.3 | 786.6 | -20.0 | -0.7 |
| `fes_i386_frontend` | 5,268.9 | 5,282.0 | 5,287.2 | +13.1 | +5.2 |
| `fes_i386_l1i` | 1,751.0 | 1,762.1 | 1,742.8 | +11.1 | -19.3 |
| `fes_i386_dops` | 408.2 | 415.9 | 408.3 | +7.7 | -7.6 |
| `fes_i386_dfwd` | 815.1 | 821.2 | 804.1 | +6.1 | -17.1 |
| `fes_i386_muldiv` | 931.2 | 936.9 | 931.0 | +5.7 | -5.9 |
| `fes_i386_exctl` | 79.6 | 82.4 | 81.2 | +2.8 | -1.2 |
| `altshift_taps` | 13.3 | 13.9 | 13.9 | +0.6 | -0.0 |
| `fes_i386_amode` | 6.7 | 7.3 | 7.3 | +0.6 | +0.0 |
| `fes_i386_agu` | 147.5 | 147.0 | 148.6 | -0.5 | +1.6 |
| `fes_i386_governor` | 329.1 | 329.3 | 324.5 | +0.2 | -4.8 |
| `fes_i386_shflags` | 2.7 | 2.7 | 2.9 | +0.0 | +0.2 |
| `fes_i386_ucode_rom` | 0.0 | 0.0 | 0.0 | +0.0 | +0.0 |

### The SX delta, arm b minus arm a

- Delta between medians: **-53.5 ALMs**
- Seed spread (larger arm's): 55 ALMs; netlist floor (logic outside the CPU, per-module fits): 10.3 ALMs; uncertainty used: 55 ALMs
- CPU subtree delta in the per-module fits: 309.3 ALMs
- Reading: not resolved: under 55 ALMs
- Arm b's median against the working ceiling: at or under 36,880 (88%): inside the range the machine has closed at before

### The strap, arm c minus arm b

- Delta between medians: **37.5 ALMs**
- Seed spread (larger arm's): 59 ALMs; netlist floor (logic outside the CPU, per-module fits): 31.9 ALMs; uncertainty used: 59 ALMs
- CPU subtree delta in the per-module fits: -160.5 ALMs
- Reading: not resolved, timing equal within a seed: the strap costs under 59 ALMs
- DE10 timing reading: no timing cost the spike can resolve

## DE25

### Every fit

| Arm | Seed | ALMs needed | % of device | Registers | RAM blocks | DSP | CPU clock worst setup (ns) | Worst hold (ns) | Closes | clk Fmax worst corner (MHz) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- | ---: |
| a | 1 | 35,594 | 76.06 | 33,991 | 134 | 24 | +8.268 | +0.013 | yes | 124.88 |
| a | 2 | 35,537 | 75.93 | 34,017 | 134 | 24 | +7.973 | +0.020 | yes | 120.44 |
| a | 3 | 35,581 | 76.03 | 34,001 | 134 | 24 | +8.109 | +0.022 | yes | 122.44 |
| a | 4 | 35,512 | 75.88 | 34,002 | 134 | 24 | +8.175 | +0.026 | yes | 123.44 |
| b | 1 | 35,746 | 76.38 | 33,798 | 134 | 24 | +7.619 | +0.039 | yes | 115.51 |
| b | 2 | 35,822 | 76.54 | 33,922 | 134 | 24 | +7.768 | +0.038 | yes | 117.54 |
| b | 3 | 35,714 | 76.31 | 33,978 | 134 | 24 | +7.707 | +0.023 | yes | 116.70 |
| b | 4 | 35,781 | 76.46 | 33,868 | 134 | 24 | +7.804 | +0.034 | yes | 118.04 |
| c | 1 | 35,703 | 76.29 | 33,950 | 134 | 24 | +8.078 | +0.035 | yes | 121.98 |
| c | 2 | 35,698 | 76.28 | 33,977 | 134 | 24 | +8.045 | +0.029 | yes | 121.49 |
| c | 3 | 35,643 | 76.16 | 34,098 | 134 | 24 | +8.227 | +0.014 | yes | 124.24 |
| c | 4 | 35,640 | 76.15 | 34,081 | 134 | 24 | +7.836 | +0.032 | yes | 118.48 |

### By arm

| Arm | Seeds | Median ALMs | Spread | Seeds closing | Median CPU-clock setup (ns) | Worst CPU-clock setup (ns) |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| a, baseline | 1, 2, 3, 4 | 35,559 | 82 | 4 of 4 | +8.142 | +7.973 |
| b, build parameter | 1, 2, 3, 4 | 35,763.5 | 108 | 4 of 4 | +7.737 | +7.619 |
| c, runtime strap | 1, 2, 3, 4 | 35,670.5 | 63 | 4 of 4 | +8.061 | +7.836 |

### The SX delta, arm b minus arm a

- Delta between medians: **204.5 ALMs**
- Seed spread (larger arm's): 108 ALMs; netlist floor (logic outside the CPU, per-module fits): not measured ALMs; uncertainty used: 108 ALMs
- CPU subtree delta in the per-module fits: not measured ALMs
- Reading: reported beside the DE10 band, not held to it (method.md)
- Arm b's median against the 92% line: at or under 41,184 (88%)

### The strap, arm c minus arm b

- Delta between medians: **-93 ALMs**
- Seed spread (larger arm's): 108 ALMs; netlist floor (logic outside the CPU, per-module fits): not measured ALMs; uncertainty used: 108 ALMs
- CPU subtree delta in the per-module fits: not measured ALMs
- Reading: not resolved, timing equal within a seed: the strap costs under 108 ALMs

## Netlists

| Arm | Commit | Netlist SHA-256 | Every seed, both boards, fitted it | Files changed from the base |
| --- | --- | --- | --- | --- |
| a | `1ec59267039b` | `5d6e76b8fdf11597…` | yes | 0 |
| b | `15d53ac41016` | `597b13cac310ff67…` | yes | 8 |
| c | `c4d5cb71b0e4` | `07ed0737e31d744c…` | yes | 8 |

## Lockstep (48 directed 486 cases, level-4 oracle)

- identity-off: 6 of 48 agree; the stops list equals slice 0's committed list
- identity-on: 45 of 48 agree; the stops list differs from slice 0's committed list
  - cpuid-1: `stop @1006.eax 480 427`
  - mov-cr4-read: `stop @1000.unexpected-exception 0 6`
  - mov-cr4-write: `stop @1006.vector d 6`
