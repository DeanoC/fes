# DE25 scanout visibility routed check

This full Quartus compile checks the registered HDMI data/DE gate and the
board's split reset SDC collections against the generated scanout netlist.
It is a scanout-shell diagnostic, separate from the ZX81 HPS SOF/operator
receipt, and does not qualify a board or hardware behavior.

| Input or result | Value |
| --- | --- |
| FES source | `4ddbb89a4d39d15b6f0694c974dfdf1a84452fe0` |
| Device | `A5EB013BB23BE4SCS` |
| Quartus | Prime Pro 26.3.0 Build 114 |
| Terasic rev. B archive SHA-256 | `f79f0eff3b0f9d042fdb61ddcf43efec77b0ebc13248eea100b94e47a1189b66` |
| Reset/visibility clear pins | 11 matched, 11 required |
| Fault synchronizer ingress | 1 source and 1 stage-0 D pin matched |
| Visibility synchronizer ingress | 1 stage-0 D pin matched |
| Full compilation | 0 errors, 308 warnings |
| Timing Analyzer | Successful; timing requirements met |
| SOF | 1,766,040 bytes; SHA-256 `822c292df42b23b5fcab91c5f262e1dd580ba98d9d14475194b6f4ced777cb8e` |

The design is still not fully constrained for setup or hold. Design Assistant
reported four high, two medium and one low severity findings with zero waivers;
the build receipt marks timing acceptance and hardware acceptance as not
qualified/not run. External HDMI delays, physical CDC/MTBF and exact-artifact
board acceptance remain open. The SOF was not programmed.

The generated project and reports were retained in the task workspace at
`sources/misteross/build/diagnostic/de25nano-scanout-pr1232-4ddbb89a/combined/hps/`.
