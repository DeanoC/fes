# DE25-Nano R1 factory recovery and native reinstall

On 27 November 2026, DE25-Nano `TRWU8CBL` cold-booted the Terasic factory system after a deliberate QSPI restore. After reinstalling native R1, the same board cold-booted Tenfoot from a fresh launcher card, installed the staged ZX81 package, launched it using the physical USB keyboard, returned to the menu and parked. This is hardware evidence for the retained R1 artifacts built from `90040d7602a217c87131ef4ce72ac467eb30e448`. [R1 packaging PR](https://github.com/DeanoC/fes/pull/2026) · [DE25 lifecycle qualification](https://github.com/DeanoC/fes/issues/1867)

| Stage | Physical evidence |
|---|---|
| Native baseline | Two independent reads of the 16 MiB QSPI main array matched the retained native R1 reference byte for byte. Quartus then verified the native JIC against the board. |
| Factory restore | Programming the board-specific factory recovery JIC passed Quartus's integrated verification. The first standalone verify stopped with JTAG error 18939 during loader configuration. A separate read-only SDM check stopped at `CONFIG_STATUS` before opening flash and returned zero array bytes. |
| Factory cold boot | After a confirmed power cycle with a separate SD card containing the verified Terasic image, the board reached the Ubuntu 22.04.3 LTS login. The console identified kernel `6.12.11-gd7d192a9ddd9` and an ext3 root at `/dev/mmcblk0p2`. Factory Linux shut down cleanly. A fresh standalone Quartus verify then passed against the factory JIC. |
| Native reinstall | Native JIC programming passed integrated verification. A separate Quartus verify also passed. Both operations passed their independent host cleanup audits. |
| Native cold boot and play | Passive UART recorded the matching R1 SPL `a2450c0987e7bf1f` and power-on reset. From an empty library, the physical keyboard installed the local ZX81 source at generation 1, launched package `03c8fa017f5fcfb40d23d54a449e8464d89fa9919dc6bae301fd8c833955290a` at generation 2, returned to Tenfoot at generation 3 and parked at generation 4. |

The earlier R1 power-on trial used a populated card with **ZX81 trial**, **ZX81** and **ZX80 trial** already installed. This fresh launcher image has empty bootstrap lists and a single local **ZX81** source in `FES/INSTALL.JSN`. The old recorder marked its preinstalled-title check `incomplete`. A separate install-aware verifier replayed the complete 633,665-byte capture, including the physical input, package identity, USB pause/resume and final Park. The recorder transmitted zero UART bytes. The launcher read the package and firmware from the card.

| Retained artifact | SHA-256 |
|---|---|
| Factory QSPI JIC | `fb61f70dd4c44908833bef88dfaec4a084b4bbae06aeb64a39d3a85b411971f1` |
| Native R1 QSPI JIC | `a906ce1cdc691f5066f59a9da1cc9d6790f85aa6ccd10e8962c00e1d0cdf5c47` |
| Terasic factory SD image | `047ed8f702d076d7ad0062517ac2b2316b948c564fc8e07f393c1b78ec07baaf` |
| Fresh native launcher SD image | `9115dd8a5b371e00b3fb265355fd2dd15bccf660c1c078f9eafa07d4dcb1473f` |
| Factory boot UART | `e0fb20231ef46620ccd34f18bd74ff35df74230d5f4364d28201fddf1a9e5333` |
| Native boot, install, launch and Park UART | `a5b973468e117773cc57806e31626e15fb98311056985f633a4a027853675d5b` |
| Restored C64 startup and Park UART | `4c9d4ce6ae9630d78732b8a8ae3ba15c566f92b337865db1acd2d199357e505c` |

## Original card and handback

Before staging R1, the operator verified the complete 3,980,394,496-byte backup of the original C64 card against SHA-256 `6380e2cee3c3caf37e06893e8dc93adcf8c7bed2b660ec8698d6d3a0bbd8587d`. Staging verified the 150,994,944-byte launcher prefix by direct readback and preserved the remaining bytes.

After the ZX81 run, a direct readback compared all 3,980,394,496 bytes of the restored card with the original backup. The card went back into powered-off CBL. On the next startup, the retained volatile image from [the C64 selector trial](https://github.com/DeanoC/fes/pull/2277) booted as `af4407e48b21634e`. Tenfoot reported **Commodore 64 PAL** ready at generation 0. A single UART Quit parked the board at generation 1. This handback exercised startup and Park.

The final evidence archive has SHA-256 `7f91bba940c14d4d7d37fd8b9ce63263e7635989ab13a44b0177e3747bd34fdf`. All 27 members passed local verification. Both serial ports and the JTAG device were free when the kit lease was released at 12:14:22 UTC.
