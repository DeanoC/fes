# RX diagnostic: hardware snapshot obtained, USB still failed

One new matched USB-only image built from `31de9947a` ran on TRWU8CBL, with one
qualified recovery boot, one boot-file upload/readback and fresh readback, one
volatile native programming and one `fesboot`. The earlier `aaa45acfc` image was
not retried. This image adds non-destructive register observations and still
rejects status zero. Neither image passed USB startup or containment.

The SDK, entry, BL31, loader and composition checks passed. Every selected
native source matched committed `31de9947a` bytes, and the linked native ELF
contained the new snapshot path. The chain SHA256 is
`07c27217b4168b76267f3bebd39a7ffe769022d4b9911e647d7c93226b20fe02`.
The unchanged FPGA producer was `7808f74c2`; no full FPGA compile was run.

The matched cold boot, secure USB setup, nine CPU startup checkpoints, source
context and 328 sealed HPS pin samples passed. The driver then reported RX
status zero and these complete snapshot values:

| Register observation | Value |
| --- | --- |
| GINTSTS before pop | `07000031` |
| GRXSTSR peek | `00000000` |
| GRXSTSP popped status | `00000000` |
| GINTSTS after pop | `07000031` |
| HCCHAR before containment | `80128008` |
| HCINT | `00000020` |
| HCTSIZ | `80000008` |
| HPRT | `0004140f` |
| GRSTCTL | `80000400` |

RX-level remained asserted across zero peek/pop observations. This is evidence
about the first invalid status, not a proof of an empty FIFO, unsupported PIO,
bad PHY or root cause. The subsequent containment wait failed with channel
value `c0128008`. The saved error tail ends during that wait message; no complete
stop receipt or runtime halt was observed. The trace remains rejected. The
operator's passive failure drain can stop on the already recorded failure
marker before capturing the entire tail; that collection issue must be repaired
for the next procedure without changing this frozen failed run.

The test stopped at its first failure. JTAG15M, exact USB permissions, private
JTAG shutdown and free UART passed independent cleanup verification. The claim
and spare remain held, the factory card is safe outside the board, and no
factory restore or further board command followed the failure. This does not
prove USB is contained. The other kit and shared daemon were untouched.

`out/nano-usb-rx-trial-20261104/` retains the raw console, frozen operator and
preflight, upload/readback and cleanup receipts, and independent snapshot replay.
`out/nano-usb-rx-diagnostic-20261104/` retains the matched build and model/race
checks. Independent replay verifies all nine snapshot fields, the matched
prefix, the rejected USB proof, and the upload and cleanup references.

For the next repair, compare the observed state with the primary
[Agilex 5 USB host initialization](https://docs.altera.com/r/docs/814346/26.1.1/hard-processor-system-technical-reference-manual-agilex-5-socs/host-initialization?contentId=nQVPZATxavLoOIsS3Civng),
[target interface](https://docs.altera.com/r/docs/814346/26.1.1/hard-processor-system-technical-reference-manual-agilex-5-socs/target-interface?contentId=RzL6g0b3yGY0Y4cV1g~EdQ)
and [USB2 DMA model](https://docs.altera.com/r/docs/814346/26.1.1/hard-processor-system-technical-reference-manual-agilex-5-socs/dma?contentId=BiLUEVao9zgfjawAvKBZUw).
Those pages document register access, initialization and DMA choices; they do
not establish the cause of this PIO failure. In particular, DMA support alone
is not evidence that PIO is unavailable. No status acceptance rule or ownership
failure gate has been weakened.
