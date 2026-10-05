# fes.riscv demonstration firmware (RV32I, machine mode).
# Draws a gradient, a border and a banner into the 160x120 RGB332
# framebuffer, then moves a box with the gamepad every frame. A timer
# interrupt four times a second cycles the box colour.

.equ FB,            0x10000000
.equ MMIO,          0x20000000
.equ R_BUTTONS,     0x00
.equ R_FRAME,       0x04
.equ R_MTIME,       0x08
.equ R_MTIMEH,      0x0c
.equ R_MTIMECMP,    0x10
.equ R_MTIMECMPH,   0x14
.equ R_VBLANK,      0x18
.equ R_VBLANK_IE,   0x1c
.equ R_ID,          0x20
.equ R_SOFTIRQ,     0x24

.equ BTN_UP,        1
.equ BTN_DOWN,      2
.equ BTN_LEFT,      4
.equ BTN_RIGHT,     8

.equ SAVE_T0,       0x30          # trap handler register save slots
.equ SAVE_T1,       0x34
.equ SAVE_T2,       0x38
.equ VARS,          0x7e00
.equ V_TICK,        0             # timer ticks since reset
.equ V_BOXX,        4
.equ V_BOXY,        8
.equ V_FAULT,       12            # unexpected exceptions
.equ STACK,         0x7f00
.equ TICK_CYCLES,   18562500      # 74.25 MHz / 4
.equ WIDTH,         160
.equ HEIGHT,        120
.equ BOX,           12
.equ WHITE,         0xff

_start:
    j reset_entry

.org 0x80
trap_vector:
    sw t0, SAVE_T0(zero)
    sw t1, SAVE_T1(zero)
    sw t2, SAVE_T2(zero)
    csrr t0, mcause
    bgez t0, exception
    # Timer interrupt: count the tick and move mtimecmp one period on.
    li t1, VARS
    lw t0, V_TICK(t1)
    addi t0, t0, 1
    sw t0, V_TICK(t1)
    li t1, MMIO
    lw t0, R_MTIMECMP(t1)
    lw t2, R_MTIMECMPH(t1)
    li t1, TICK_CYCLES
    add t0, t0, t1
    sltu t1, t0, t1
    add t2, t2, t1
    li t1, MMIO
    sw t2, R_MTIMECMPH(t1)        # high word first keeps the compare in the future
    sw t0, R_MTIMECMP(t1)
    j trap_exit
exception:
    # Not expected from this firmware: count it and skip the instruction.
    li t1, VARS
    lw t2, V_FAULT(t1)
    addi t2, t2, 1
    sw t2, V_FAULT(t1)
    csrr t0, mepc
    addi t0, t0, 4
    csrw mepc, t0
trap_exit:
    lw t2, SAVE_T2(zero)
    lw t1, SAVE_T1(zero)
    lw t0, SAVE_T0(zero)
    mret

reset_entry:
    li sp, STACK
    la t0, trap_vector
    csrw mtvec, t0
    li s0, VARS
    sw zero, V_TICK(s0)
    sw zero, V_FAULT(s0)
    li t0, (WIDTH - BOX) / 2
    sw t0, V_BOXX(s0)
    li t0, (HEIGHT - BOX) / 2
    sw t0, V_BOXY(s0)
    li s1, MMIO
    li s2, FB
    call draw_background
    call draw_banner
    # Arm the periodic timer and enable its interrupt.
    lw t0, R_MTIME(s1)
    lw t1, R_MTIMEH(s1)
    li t2, TICK_CYCLES
    add t0, t0, t2
    sltu t2, t0, t2
    add t1, t1, t2
    sw t1, R_MTIMECMPH(s1)
    sw t0, R_MTIMECMP(s1)
    li t0, 0x80
    csrs mie, t0
    csrsi mstatus, 8

main_loop:
    lw t0, R_VBLANK(s1)
    beqz t0, main_loop
    sw zero, R_VBLANK(s1)
    # Erase the box by redrawing the gradient under it.
    lw a0, V_BOXX(s0)
    lw a1, V_BOXY(s0)
    li a2, BOX
    li a3, BOX
    call fill_background
    # Move one pixel per frame in the held directions, inside the border.
    lw t1, R_BUTTONS(s1)
    lw a0, V_BOXX(s0)
    lw a1, V_BOXY(s0)
    andi t0, t1, BTN_LEFT
    beqz t0, not_left
    addi a0, a0, -1
not_left:
    andi t0, t1, BTN_RIGHT
    beqz t0, not_right
    addi a0, a0, 1
not_right:
    andi t0, t1, BTN_UP
    beqz t0, not_up
    addi a1, a1, -1
not_up:
    andi t0, t1, BTN_DOWN
    beqz t0, not_down
    addi a1, a1, 1
not_down:
    li t0, 1
    bge a0, t0, x_min_ok
    mv a0, t0
x_min_ok:
    bge a1, t0, y_min_ok
    mv a1, t0
y_min_ok:
    li t0, WIDTH - BOX - 1
    ble a0, t0, x_max_ok
    mv a0, t0
x_max_ok:
    li t0, HEIGHT - BOX - 1
    ble a1, t0, y_max_ok
    mv a1, t0
y_max_ok:
    sw a0, V_BOXX(s0)
    sw a1, V_BOXY(s0)
    # Draw the box in the colour selected by the timer tick.
    lw t0, V_TICK(s0)
    andi t0, t0, 7
    la t1, palette
    add t1, t1, t0
    lbu a4, 0(t1)
    li a2, BOX
    li a3, BOX
    call fill_rect
    j main_loop

# fill_rect(a0 = x, a1 = y, a2 = width, a3 = height, a4 = colour). Uses t0-t4.
fill_rect:
    slli t0, a1, 7
    slli t1, a1, 5
    add t0, t0, t1
    add t0, t0, a0
    add t0, t0, s2
    mv t2, a3
fill_rect_row:
    beqz t2, fill_rect_done
    mv t3, a2
    mv t4, t0
fill_rect_column:
    beqz t3, fill_rect_next
    sb a4, 0(t4)
    addi t4, t4, 1
    addi t3, t3, -1
    j fill_rect_column
fill_rect_next:
    addi t0, t0, WIDTH
    addi t2, t2, -1
    j fill_rect_row
fill_rect_done:
    ret

# fill_background(a0 = x, a1 = y, a2 = width, a3 = height): gradient
# colour ((x >> 5) << 5) | ((y >> 4) << 2) | 1. Uses t0-t6 and a5.
fill_background:
    slli t0, a1, 7
    slli t1, a1, 5
    add t0, t0, t1
    add t0, t0, a0
    add t0, t0, s2
    mv t2, a3
    mv t5, a1
fill_background_row:
    beqz t2, fill_background_done
    mv t3, a2
    mv t4, t0
    mv t6, a0
    srli t1, t5, 4
    slli t1, t1, 2
    ori t1, t1, 1
fill_background_column:
    beqz t3, fill_background_next
    srli a5, t6, 5
    slli a5, a5, 5
    or a5, a5, t1
    sb a5, 0(t4)
    addi t4, t4, 1
    addi t6, t6, 1
    addi t3, t3, -1
    j fill_background_column
fill_background_next:
    addi t0, t0, WIDTH
    addi t5, t5, 1
    addi t2, t2, -1
    j fill_background_row
fill_background_done:
    ret

draw_background:
    addi sp, sp, -4
    sw ra, 0(sp)
    li a0, 0
    li a1, 0
    li a2, WIDTH
    li a3, HEIGHT
    call fill_background
    li a4, WHITE
    li a0, 0
    li a1, 0
    li a2, WIDTH
    li a3, 1
    call fill_rect
    li a0, 0
    li a1, HEIGHT - 1
    li a2, WIDTH
    li a3, 1
    call fill_rect
    li a0, 0
    li a1, 0
    li a2, 1
    li a3, HEIGHT
    call fill_rect
    li a0, WIDTH - 1
    li a1, 0
    li a2, 1
    li a3, HEIGHT
    call fill_rect
    lw ra, 0(sp)
    addi sp, sp, 4
    ret

# draw_banner: the glyph string at (26, 8), 5x7 glyphs scaled 2x, white.
draw_banner:
    addi sp, sp, -20
    sw ra, 0(sp)
    sw s3, 4(sp)
    sw s4, 8(sp)
    sw s5, 12(sp)
    sw s6, 16(sp)
    la s3, banner
    li s4, 26
banner_loop:
    lbu t0, 0(s3)
    li t1, 0xff
    beq t0, t1, banner_done
    addi s3, s3, 1
    slli t1, t0, 3
    sub t1, t1, t0                 # glyph index * 7
    la t2, glyphs
    add s5, t2, t1
    li s6, 0
glyph_row:
    li t0, 7
    beq s6, t0, glyph_done
    add t1, s5, s6
    lbu a5, 0(t1)
    li a6, 0
glyph_column:
    li t0, 5
    beq a6, t0, glyph_row_next
    li t0, 4
    sub t0, t0, a6
    srl t0, a5, t0
    andi t0, t0, 1
    beqz t0, glyph_column_next
    slli a0, a6, 1
    add a0, a0, s4
    slli a1, s6, 1
    addi a1, a1, 8
    li a2, 2
    li a3, 2
    li a4, WHITE
    call fill_rect
glyph_column_next:
    addi a6, a6, 1
    j glyph_column
glyph_row_next:
    addi s6, s6, 1
    j glyph_row
glyph_done:
    addi s4, s4, 12
    j banner_loop
banner_done:
    lw ra, 0(sp)
    lw s3, 4(sp)
    lw s4, 8(sp)
    lw s5, 12(sp)
    lw s6, 16(sp)
    addi sp, sp, 20
    ret

.align 2
palette:
    .byte 0xe0, 0xfc, 0x1c, 0x1f, 0x03, 0xe3, 0xff, 0x92
banner:
    .byte 0, 1, 2, 9, 3, 4, 5, 6, 7, 0xff      # F E S _ R V 3 2 I
glyphs:
    .byte 0x1f, 0x10, 0x1e, 0x10, 0x10, 0x10, 0x10   # 0 F
    .byte 0x1f, 0x10, 0x1e, 0x10, 0x10, 0x10, 0x1f   # 1 E
    .byte 0x0f, 0x10, 0x10, 0x0e, 0x01, 0x01, 0x1e   # 2 S
    .byte 0x1e, 0x11, 0x11, 0x1e, 0x14, 0x12, 0x11   # 3 R
    .byte 0x11, 0x11, 0x11, 0x11, 0x11, 0x0a, 0x04   # 4 V
    .byte 0x1e, 0x01, 0x01, 0x0e, 0x01, 0x01, 0x1e   # 5 3
    .byte 0x0e, 0x11, 0x01, 0x02, 0x04, 0x08, 0x1f   # 6 2
    .byte 0x1f, 0x04, 0x04, 0x04, 0x04, 0x04, 0x1f   # 7 I
    .byte 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00   # 8 unused
    .byte 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00   # 9 space
