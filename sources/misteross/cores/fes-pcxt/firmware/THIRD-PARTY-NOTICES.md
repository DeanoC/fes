# PC/XT hardware DOS package notices

`freedos-1.4-xt-360.img` is a 360 KiB FAT12 image made from the FreeDOS 1.4
kernel 2043 and FreeCOM 0.86a. The kernel boot sector and `KERNEL.SYS` come from
[`FreeDOS/base/kernel`](https://gitlab.com/FreeDOS/base/kernel) at
`9afeadbdf7e38b5b988152944a96331d13868ad8` under GPL-2.0-or-later.
`COMMAND.COM` comes from [`FreeDOS/base/freecom`](https://gitlab.com/FreeDOS/base/freecom)
at `5df932d608cc9ba6a4bc830627cad7bd2da70d8e` under GPL-2.0. Their upstream
license texts are included beside the image. The filesystem boot-sector BPB,
FAT12 directory, `FDCONFIG.SYS`, `AUTOEXEC.BAT`, and `FESXT.TXT` marker are
assembled by the tracked `build_pcxt_freedos_image.py` script. The exact image
is SHA-256 `140a2d0a507f7452790e73416dd4dab2c66a89e09a71e26b2ca8f95d1f3c5e5c`.

`GLABIOS_0.4.2_8X.ROM` is the pinned GLaBIOS 0.4.2 8088 XT/5160 ROM from
[the upstream release](https://github.com/640-KB/GLaBIOS/releases/tag/v0.4.2),
licensed GPL-3.0. The ROM includes font bitmaps credited upstream to VileR
under CC-BY-SA-4.0. See `LICENSE-GLaBIOS.txt` and
[`LICENSE-CC-BY-SA-4.0.txt`](https://creativecommons.org/licenses/by-sa/4.0/legalcode).
