# DE25 owner-candidate physical SD preparation

After the verified candidate was ready, Deano moved the bottom kit's working
card to Powerboat's reader for this trial. The immediately preceding claimed
UART preflight identified bottom board TRWU8J8G, boot
e8963972-c3e9-405c-a2ef-93eb09f691e6 and SD CID
275048534436344760702d33e0017c00. The USB reader cannot itself attest that CID;
card selection additionally relies on Deano's physical handoff, the observed
capacity/USB path, exact MBR/partition layout and locked vendor boot payloads.

The writer verified the new removable USB card at /dev/sda:
62,534,975,488 bytes, Generic_STORAGE_DEVICE-0:0,
pci-0000:80:14.0-usb-0:11.4.1:1.0-scsi-0:0:0:0. It checked the expected device
number, removability, capacity, USB properties, no mounted partitions and the
locked vendor MBR before taking an exclusive block-device descriptor. The
original partition geometry ends at byte 3,984,588,800.

Before writing, it backed up that entire overwritten extent and independently
hashed the retained backup against the original read stream. Independent FAT
readback verifies the original four vendor payloads. Backup SHA256:
`9daa6788aff7e07857e3e7a0c349b56b1ae8af1793437837c16169942f63ccfb`.
The actual before-write image is retained in the private rollback directory
/home/deano/kit-status/rollback/de25-TRWU8J8G-before-owner-20261031/baseline.img.
This is a real writable-card snapshot, not a re-created factory image or a
filesystem-cleanliness/whole-card-tail attestation. Backup bytes are read-only
and the enclosing directory is private. The card's unused tail was not written.

The exact verified [owner SD candidate](../2026-10-31-de25-owner-image/README.md)
was written through the same validated descriptor. The writer waited for fsync,
flushed the block cache, reopened the device read-only and hashed all
3,984,588,800 bytes. Device readback matches candidate SHA256
`de2a9a4ca35214ab75230d5807a0a8900d99f7869e1346c83f4e41bce0853138`.
Partition-table reread succeeds. The complete preparation takes 779.5 seconds.
All block descriptors are closed. The observer records the existing writer PID
and advancing kernel I/O counters during the blocking flush; no writer was
restarted on an observation timeout.

The writer used repository head d64ced1eabf3e514626cd8563e6cb68dba68e3d0 and
reverified the unchanged image recipe before accessing the card. Image producer
remains 829cf0353d1ea7a479f95bdbdf3d72e0391ab810; kernel producer remains
e6bdd35973cec91ee3ef93b92862680de3b7988f. The later userspace test preparation
changes neither kernel nor image bytes. Exact local operation source, terminal
receipt and observer log are retained here. QSPI and FPGA programming were not
performed.

This establishes physical media write/readback only. The candidate board boot,
actual owner syscalls and translated DMA/HDMI, loader/concurrency, timing,
managed MENU/runtime/catalog and ZX81 lifecycle/recovery remain unverified.
The claim is retained for the pending first boot; this is not baseline promotion.
Passive UART capture was started before requesting card insertion/power-on.
