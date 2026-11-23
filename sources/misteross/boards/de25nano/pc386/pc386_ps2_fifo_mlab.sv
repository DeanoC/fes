// DE25 pc386: the 16-byte PS/2 FIFO with one write port (#2108).
//
// pc386_kbc.sv's pc386_ps2_fifo writes entry 0 on a clear with a push and
// write_at otherwise: two write ports, so each array becomes 16 registers per
// bit with two write decoders. Here the clear's push and a normal push share
// one address (clear ? 0 : write_at) and one enable, so each array is a small
// MLAB RAM, read asynchronously as before. Its ports are cycle-exact with
// pc386_ps2_fifo's in every clock, including the head outputs while empty
// (docs/validation/2026-11-22-pc386-ps2-fifo-area/diff/).
//
// derive.py retargets pc386_kbc's two FIFO instances to this module for the
// DE25 build only. The DE10 keeps pc386_ps2_fifo, so its netlist and seal
// do not move.
module pc386_ps2_fifo_mlab (
    input wire clk, clear,             // clear empties it before this clock's push
    input wire push,
    input wire push_last,
    input wire [7:0] push_key_bit,
    input wire push_key_make,
    input wire push_key_repeat,
    input wire [7:0] push_byte,
    input wire pop,
    output wire valid,
    output wire [7:0] head,
    output wire head_last,
    output wire [7:0] head_key_bit,
    output wire head_key_make,
    output wire head_key_repeat,
    output wire [4:0] space
);
    // One write port, so each array is a small RAM (MLAB) rather than
    // 16 registers per bit with a write decoder for each of two ports.
    (* ramstyle = "MLAB" *) reg [7:0] bytes [0:15];
    (* ramstyle = "MLAB" *) reg last_bytes [0:15];
    (* ramstyle = "MLAB" *) reg [7:0] key_bits [0:15];
    (* ramstyle = "MLAB" *) reg key_makes [0:15];
    (* ramstyle = "MLAB" *) reg key_repeats [0:15];
    reg [3:0] read_at, write_at;
    reg [4:0] count;
    assign valid = count != 0;
    assign head = bytes[read_at];
    assign head_last = last_bytes[read_at];
    assign head_key_bit = key_bits[read_at];
    assign head_key_make = key_makes[read_at];
    assign head_key_repeat = key_repeats[read_at];
    assign space = 5'd16 - count;
    // A byte pushed with the clear (a reset's ACK) is kept, at entry 0.
    wire store = clear ? push : push && count != 16;
    wire [3:0] store_at = clear ? 4'd0 : write_at;
    always @(posedge clk) if (store) begin
        bytes[store_at] <= push_byte;
        last_bytes[store_at] <= push_last;
        key_bits[store_at] <= push_key_bit;
        key_makes[store_at] <= push_key_make;
        key_repeats[store_at] <= push_key_repeat;
    end
    always @(posedge clk) begin
        if (clear) begin
            read_at <= 0;
            write_at <= {3'b0, push};
            count <= {4'b0, push};
        end else begin
            if (push && count != 16) write_at <= write_at + 1'b1;
            if (pop && count != 0) read_at <= read_at + 1'b1;
            count <= count + {4'b0, push && count != 16} - {4'b0, pop && count != 0};
        end
    end
endmodule
