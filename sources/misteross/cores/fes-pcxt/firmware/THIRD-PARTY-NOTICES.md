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
[`LICENSE-CC-BY-SA-4.0.txt`](https://creativecommons.org/licenses/by-sa/4.0/legalcode.txt).

`cga-font-8x8.bin` is the CGA character ROM: 256 CP437 characters of 8 rows,
2048 bytes, SHA-256
`75c79a7e7fa423dda67ec6d6d76cec86b63f85677726368750c75b0920ddf319`. It is the
`vgafont8` table of the LGPL VGABios 0.9d `vgafonts.h`
([`bochs-emu/VGABIOS`](https://github.com/bochs-emu/VGABIOS) at
`12d978bd86fe88ee6b8620b0f1e91c7fb81bc2e6`), whose header credits the fonts to
Joseph Gil's fntcol16 collection and states that the individual fonts are public
domain. Its characters 00h–7Fh are byte-identical to the GLaBIOS graphics table.
