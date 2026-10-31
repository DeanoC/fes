/* SPDX-License-Identifier: GPL-3.0-or-later */
ENTRY(_start)
SECTIONS
{
    . = FES_ENTRY_IMAGE_ADDRESS;
    __image_start = .;
    .text : { KEEP(*(.text.header)) *(.text.start) *(.text.vectors) *(.text .text.*) }
    .rodata : { *(.rodata .rodata.*) }
    .data : { *(.data .data.*) }
    .payload : ALIGN(8) { KEEP(*(.payload)) }
    /* Stored in the image: no uninitialized memory or firmware stack required. */
    .stack : ALIGN(16) { . += 16384; __stack_top = .; BYTE(0); }
    __image_end = .;
    __image_size = __image_end - _start;
    .init_array : { *(.init_array .init_array.*) }
    ASSERT(SIZEOF(.init_array) == 0, "native entry does not run constructors")
    /DISCARD/ : { *(.comment) *(.note*) *(.eh_frame*) }
}
