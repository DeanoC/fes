# Fresh writer using the Nano microSD wiring contract

Base `aa0d94a11`; production repair `a5d6027e9`. The freshly inspected spare
has the same full CID/capacity, MBR and current tail hash as the original plan.
The independent review still places sectors 7,774,200–7,774,207 outside the
sole FAT partition and 144 MiB boot image. The earlier failed writer remains
failed/unknown; its capture and consumed records are not resumed or replayed.
The newer read-only inspection ended SD-contained generation 7 and its full
2,501-file closure/source are retained separately.

The [matched writer](prepared-writer.json), build `e4487fd3d4c10b80`, includes
the explicit Nano socket-wiring policy and separate card CSD protection checks.
Normal storage stays read-only; the dedicated writer alone consumes the
reviewed identity/MBR/original-tail plan. Complete SDK/entry/FIT/SPL/BL31 source
and receipt validation, embedded-SPL decoding and offline file readbacks pass.
The eight production software checks remain applicable to these unchanged
production bytes. The new runner's actual decoder and gate models pass nine
upload/lineage negatives and 25 native/recovery receipt negatives. The frozen
procedure binds 2,409 files and three eligible source trees.

The sequence is a fresh qualified recovery boot, one fixed FIT upload and
fresh SD readback, then one new native boot and bounded eight-sector write.
Every verified/written/contained/resumed/pass record must bind the expected
card, extent, pattern, owner and generation. Only complete native success and
final containment permit a separately consumed recovery boot and independent
read-only tail verification, including protected prefix/MBR/FIT/manifest
checks before and after. The last stage records its limited card summary;
full expected CID was checked by native code. Each programming operation has
its own fresh forced scan and one-use grant.

New receipts call a complete native result `native_sd_writes_verified`.
An incomplete write instead retains `native_sd_write_possible` and
`native_write_outcome_unknown`; no false verification field is interpreted
as evidence that no sector could have committed. Any failed gate stops later
stages with a two-second receive-only tail. There is no retry, rollback or
post-failure recovery gate bypass.

Program/prompt/native bounds remain 90/50/100 seconds; each boot phase is
240 seconds, recovery readback 150 and the outer experiment 850. The normal
user holds the shared Quartus lock, with the root 890-second supervisor inside
it and uid-1000 vendor children. Qualified fixed 3 MHz/auto-off transport,
top-filtered private service and exact clock/auto/USB/server/UART cleanup
remain required. Fresh clean-source/file/tool/claim/UART admission and a
60-second stable connection are mandatory before starting.

Only top TRWU8CBL and its installed disposable spare are selected; the original
factory card stays safe, factory restoration remains waived, the bottom kit
is excluded and old-output cleanup remains paused. This preparation grants no
live authority or native write success. FAT persistence, ordinary installation,
native networking and full appliance acceptance remain unqualified. Helpers
and raw results are under `out/nano-sd-fixed-write-trial-20261110/`.

## Hardware result

[The native writer passed](hardware-result.json) all five stages after full
matched cold/ATF/EL2/EL1 proof. Exactly eight reviewed sectors were written and
read back, SD was contained and resumed, the same card was read again, and
final native containment reached generation 7. The separate qualified recovery
reboot then passed. All three programming operations completed successfully.

The independent recovery verification stopped before reaching the tail. Its
first raw prefix read completed, but U-Boot printed the command summary and
`32768 blocks read: OK` on one line. The host parser expected the latter at
the beginning of a line and rejected it. This is a failed whole attempt with
a successful native write phase, not independent persistence acceptance.
The failed capture and two-second passive tail remain unchanged.

Independent cleanup passes for clock/auto readbacks, exact USB permissions,
private-server termination and free UART; there are no new programmer failures.
The last observed board state is the qualified recovery prompt following that
read-only prefix read. CPU park was not observed. No native SD lease is claimed
to survive that recovery reboot. The next step is to repair the parser using
the captured format, then prepare a new recovery-only readback with fresh
admission. The writer will not be rerun; its old original-tail plan is consumed.
