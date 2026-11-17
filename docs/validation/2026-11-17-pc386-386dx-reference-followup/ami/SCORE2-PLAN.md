# AMI 386DX-25 score screen with corrected image review

Frozen before the new run. This repeats only the benchmark workflow whose
previous DOS gate misread a known-valid prompt; the independent reason and
retained correction are in CORRECTION.md. Keep the original 47–65 TopBench IQR
selection rule, 25 MHz i386dx, 8 MiB, no dynarec, BIOS 050591, and all frozen
source/config/CMOS/ROM and speed-sampling criteria in the earlier BENCH-PLAN.md.
Do not measure game pace or select a reference based on a score alone.

Use a new isolated private 3090-host root, private copies of the exact previous
pre-run disk, NVR, config, app and ROM tree; hash them before launch. The
previous input disk hash is 802fccc74abf296ed4f96bbe7343bd4685bf5566246a9a481901b6b2ccd3f073,
the NVR hash 4b684a11fce2c469ea8275d02a9ae5caa4d1a487a4907b2152f903d64249b912,
and 86Box executable ae3dc231824cf0c2facd1af4e2164814cbbb15bb1a9fa0603cdd8e9ee0ad2969.
Retain native 800x520 capture plus a lossless 2x nearest-neighbor visual copy
for each independent checkpoint. Parent independently inspects each exact
review image and its corresponding native digest; do not send any input if the
shown state is ambiguous. Sequence: DOS prompt, CACHE.COM result, C:\MARKS,
C:\MARKS\TOPBENCH, then benchmark. Obtain early/final score and 4fps recording;
measure the precommitted full-speed interval only inside visible score run.
Preserve missing/invalid evidence and stop on any gate failure; no repeated
mutation or guessed input. Separate effective-cache flag, C800, game launch,
and pitch acceptance remain open even if the score qualifies.

Use an explicit 480-second job cap, 10-second heartbeat, disk floor 30 GiB,
ROM audit before and after and exact-child cleanup. Never use a kit, IBM
reference disk, GPU or user harness. This is emulator screening only.
