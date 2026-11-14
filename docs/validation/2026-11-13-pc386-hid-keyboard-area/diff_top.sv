// Differential bench (2026-11-13 pc386 HID keyboard area record): the base model (a)
// against the working tree (b), each behind its own pc386_ps2_keyboard; run_diff.sh builds it.
module diff_top (
    input wire clk, reset, us_tick,
    input wire [143:0] rows,
    input wire rx_valid, input wire [7:0] rx_byte,
    input wire take_a, take_b,
    output wire valid_a, valid_b, output wire [7:0] byte_a, byte_b,
    output wire scanning_a, scanning_b,
    output wire busy_a, busy_b
);
`define SIDE(S, MODEL) \
    wire key_valid_``S, key_ready_``S, scanning_``S``_w, tclear_``S, key_last_``S, key_complete_``S, key_cancel_``S; \
    wire [7:0] key_report_bit_``S, complete_bit_``S, key_code_``S; \
    wire key_report_make_``S, key_report_repeat_``S, complete_make_``S, complete_repeat_``S; \
    wire [6:0] typematic_``S; \
    MODEL hid_``S (.clk(clk), .reset(reset), .us_tick(us_tick), .rows(rows), .typematic(typematic_``S), \
        .typematic_clear(tclear_``S), .key_valid(key_valid_``S), .key_code(key_code_``S), .key_last(key_last_``S), \
        .key_report_bit(key_report_bit_``S), .key_report_make(key_report_make_``S), .key_report_repeat(key_report_repeat_``S), \
        .key_ready(key_ready_``S), .key_scanning(scanning_``S``_w), .key_complete(key_complete_``S), \
        .complete_bit(complete_bit_``S), .complete_make(complete_make_``S), .complete_repeat(complete_repeat_``S), \
        .key_cancel(key_cancel_``S)); \
    pc386_ps2_keyboard #(.BAT_US(500)) kb_``S (.clk(clk), .reset(reset), .us_tick(us_tick), \
        .rx_valid(rx_valid), .rx_byte(rx_byte), .tx_valid(valid_``S), .tx_byte(byte_``S), .tx_take(take_``S), \
        .key_valid(key_valid_``S), .key_code(key_code_``S), .key_last(key_last_``S), \
        .key_report_bit(key_report_bit_``S), .key_report_make(key_report_make_``S), .key_report_repeat(key_report_repeat_``S), \
        .key_ready(key_ready_``S), .key_complete(key_complete_``S), .complete_bit(complete_bit_``S), \
        .complete_make(complete_make_``S), .complete_repeat(complete_repeat_``S), .key_cancel(key_cancel_``S), \
        .typematic(typematic_``S), .typematic_clear(tclear_``S), .scanning(scanning_``S``_w)); \
    assign scanning_``S = scanning_``S``_w; \
    assign busy_``S = key_valid_``S;
    `SIDE(a, pc386_hid_keyboard_ref)
    `SIDE(b, pc386_hid_keyboard)
endmodule
