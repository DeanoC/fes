// Differential top: the DE10 pc386_ps2_fifo (pc386_kbc.sv) beside the DE25
// pc386_ps2_fifo_mlab, on the same inputs. The bench compares every output.
module fifo_diff_top (
    input wire clk, clear, push, push_last, push_key_make, push_key_repeat, pop,
    input wire [7:0] push_key_bit, push_byte,
    output wire valid_r, valid_n,
    output wire [7:0] head_r, head_n, key_bit_r, key_bit_n,
    output wire last_r, last_n, make_r, make_n, repeat_r, repeat_n,
    output wire [4:0] space_r, space_n
);
    pc386_ps2_fifo r (.clk(clk), .clear(clear), .push(push), .push_last(push_last), .push_key_bit(push_key_bit),
        .push_key_make(push_key_make), .push_key_repeat(push_key_repeat), .push_byte(push_byte), .pop(pop),
        .valid(valid_r), .head(head_r), .head_last(last_r), .head_key_bit(key_bit_r), .head_key_make(make_r),
        .head_key_repeat(repeat_r), .space(space_r));
    pc386_ps2_fifo_mlab n (.clk(clk), .clear(clear), .push(push), .push_last(push_last), .push_key_bit(push_key_bit),
        .push_key_make(push_key_make), .push_key_repeat(push_key_repeat), .push_byte(push_byte), .pop(pop),
        .valid(valid_n), .head(head_n), .head_last(last_n), .head_key_bit(key_bit_n), .head_key_make(make_n),
        .head_key_repeat(repeat_n), .space(space_n));
endmodule
