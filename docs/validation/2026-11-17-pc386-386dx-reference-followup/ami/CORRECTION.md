# Correction to the retained AMI benchmark DOS-gate interpretation

The retained failed attempt's `bench-run/dos.png` SHA-256 is
`90facef6c3d31d5158961c5657b9fc3a245c594e05c56b36115769e0f3dd159c`.
The earlier result called its guest region black, and the unattended process
therefore timed out without sending any benchmark input. That interpretation was
incorrect: the PNG contains a complete FreeDOS screen and `C:\>` prompt. The
native-scale rendering available to the prior reviewer displayed the guest
region black, while a nearest-neighbor 2x transform of that exact PNG,
`dos-2x.png` SHA-256
`7b1c40d62c26a65e1f5ee75cc197aaff0fa0f33aa19adb430f4707ae6b4548c1`,
is visibly legible. `tesseract dos.png stdout` independently extracts the DOS
screen and prompt. The parent independently inspected the original and 2x
rendering. Both show the guest's `no RAM found at C800` warning, which is a
separate game-compatibility concern.

For comparison, `config2-run/reboot-40.png` has the same original digest;
`reboot-20.png` SHA-256
`6205066305dddff18d13c113c429187b5bd92c4310f170d48999da3a84695d81`
visibly shows DOS. The prior 110-second 4fps recording has a legible DOS prompt
in its decoded frame at t=20 seconds and t=40 seconds. No claim is made about
why the native-scale viewer rendered this image incorrectly. This correction
does not retroactively authorize an input, establish a benchmark score, prove
cache state, or change the prior terminated run. The original result and bytes
are preserved; a separately frozen new run can address the review-display
failure using native screenshots plus losslessly enlarged visual review copies.
