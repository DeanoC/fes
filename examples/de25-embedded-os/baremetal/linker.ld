/* SPDX-License-Identifier: GPL-3.0-or-later */
/* QEMU virt RAM address, not the DE25 memory map. */
ENTRY(_start)
MEMORY { RAM (rwx) : ORIGIN = 0x40080000, LENGTH = 1M }
SECTIONS
{
    .text : { KEEP(*(.text.boot)) *(.text .text.*) } > RAM
    .rodata : { *(.rodata .rodata.*) } > RAM
    .data : { *(.data .data.*) } > RAM
    .init_array : { *(.init_array .init_array.*) } > RAM
    ASSERT(SIZEOF(.init_array) == 0, "Bare-metal probe has no constructor runner")
    .bss (NOLOAD) : ALIGN(8) {
        __bss_start = .;
        *(.bss .bss.* COMMON)
        . = ALIGN(8);
        __bss_end = .;
    } > RAM
    .stack (NOLOAD) : ALIGN(16) {
        . += 16384;
        __stack_top = .;
    } > RAM
    /DISCARD/ : { *(.comment) *(.note*) *(.eh_frame*) }
}
