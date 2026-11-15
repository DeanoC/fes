# Native computer access cost isolated on TRWU8CBL

Compiled source `dd4c6aebebe039478852d846f93b74f4a89c9c2b`; operator publication `a9e825839`. One bounded attempt; no retry.

Recovery and seven fixed Ethernet uploads/fresh SD readbacks passed. Native startup reached all 14 CPU checkpoints and the expected gamepad descriptor, with seven idle reports before owned generation-2 Pause. The first ZX81 FPGA transfer completed all 1,650,688 bytes with data held. ARM-to-COMPLETE measured 13.2754164625 seconds. These observations do not prove accepted core launch or physical controls.

Computer identity exceeded its unchanged one-second limit at word index 2, after 52 reads and three writes: elapsed 1.003052055 seconds, last checked read 18.244892 ms. The new saved timing isolates that read:

| Phase | Elapsed |
| --- | ---: |
| Secure ownership snapshot before register access | 9.090945 ms |
| Register access including CPU mapping verification | 52.125 microseconds |
| Secure ownership snapshot after register access | 9.100302 ms |

Over 99% of this access is in the two ownership snapshot calls; register/mapping access is under 0.3%. The phase values identify the secure-call path, not individual functions inside it. They do not justify removing CPU mapping checks, changing permission, or extending deadlines. Next inspect and reduce nested secure ownership observations while preserving fresh containment before and after each CSR access.

No full identity, Run, USB resume, return, Quit or completed core cycle was accepted. Native entered reboot_required with resources retained. Master-stop reported complete/data-held; CPU halt remains unproved. Full panic tail captured without secondary error. Independent decoding and cleanup passed: unchanged 15M, exact USB permissions restored, private JTAG process gone, UART free. Claim and test card retained; factory card and other kit untouched. No further board command.

All eight selected software checks and the matched boot chain passed before the attempt. Candidate/source bundles are at `/home/deano/fes-retained/de25-access-timing-20261105-r1`; frozen first-failure traces/procedure are at `/home/deano/fes-retained/de25-access-timing-stop-20261105-r1`. Checksums and source/publication restoration are verified. This remains a failed hardware diagnostic; normal package/ROM/media admission, physical video/controls, native Ethernet and DE10-equivalent core-load readiness remain open.
