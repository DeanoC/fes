# Byte-exact factory restore files prepared

Quartus Pro26.3 generated a factory-main-array JIC/RPD/map from the two complete
matching TRWU8CBL reads. Both read results, source/evidence hashes and cleanup
records must pass before the file generator creates output. An independent audit
compares all16,777,216 JIC-decoded and RPD bytes with both original backups and
checks target A5EB013BB23B, flash MT25QU128, range0..0xffffff and vendor info
parser acceptance. All generated file modes are600. Original backups are unchanged.

The direct BIN CLI probe had failed with a tool segment violation. The successful
path uses a real saved .pfg layout: the isolated vendor GUI supplied raw-file,
bit-order and partition XML, followed by a separate raw-only CLI generation.
The native16MiB fixture first passed the same complete JIC/RPD comparison.
Thirteen format/input cases and nine source-receipt cases reject wrong target,
flash, corrupted/truncated data, unsafe names/paths, partial/one-pass evidence,
changed receipts, mismatched arrays/sessions and duplicate keys. These model cases
remain separate from physical read evidence. Outer JIC checksum generation is
owned by Quartus; the custom verifier does not claim to independently calculate it.

The initial package guard assumed the read manifest included PFG, which it does
not use, and stopped before creating output. The corrected file generator pins
the independently validated native-candidate producer hashes. Initial/corrected
source, failed log and successful file audit are retained. The output is a
vendor-produced byte-exact restore image; it has not been programmed or booted.
No flash write, reset or boot-config change is authorized by this file-only work.

The main-array backup excludes OTP/security. The physical die/package remains
ambiguous between the compatible16MiB Micron identities. Before any persistent
boot-flash change, prepare a separate bounded write/boot/recovery operator and
obtain explicit approval. Native Ethernet, broader packages/devices, current
physical HDMI/input checks and full DE10-equivalent appliance acceptance remain
open. The existing matched native HPS-first candidate remains a separate artifact.
