// SPDX-License-Identifier: GPL-2.0-or-later
// ZX Spectrum .tap player for fes.media.spectrum-tape unit 0.
//
// The image is a sequence of blocks: a little-endian uint16 length, then
// that many payload bytes. A block whose first byte is 0 gets the header
// pilot (8063 edges); any other flag gets the data pilot (3223). Edges are
// 2168 T-states, sync pulses are 667 then 735, a 0-bit is two 855 T pulses
// and a 1-bit is two 1710 T pulses. A one-second pause (EAR low) follows
// each block. A trailing partial block is not played. EAR stays low while
// the unit is not ready. Version 1.0 does not capture MIC writes.
module spectrum_tape (
    input  wire        clk,
    input  wire        cen,
    input  wire        reset,
    input  wire [1:0]  unit_state,
    input  wire [31:0] unit_size,
    input  wire [15:0] write_addr,
    input  wire [15:0] write_data,
    input  wire [1:0]  write_enable,
    output reg         ear
);
    localparam [1:0] READY = 2'd3;
    localparam integer BYTES = 65536;
    localparam [2:0] S_IDLE = 3'd0;
    localparam [2:0] S_LEN_LO = 3'd1;
    localparam [2:0] S_LEN_HI = 3'd2;
    localparam [2:0] S_FLAG = 3'd3;
    localparam [2:0] S_PULSE = 3'd4;
    localparam [2:0] S_FETCH = 3'd5;
    localparam [2:0] S_PAUSE = 3'd6;
    localparam [2:0] S_DONE = 3'd7;
    localparam [1:0] P_PILOT = 2'd0;
    localparam [1:0] P_SYNC1 = 2'd1;
    localparam [1:0] P_SYNC2 = 2'd2;
    localparam [1:0] P_BIT = 2'd3;

    // Two M10K ports, same shape as the Apple II disk store. A mailbox
    // write borrows port A for that clock. The player is reset until the
    // unit is ready, and the mailbox leaves READY before the first data word.
    (* ram_style = "m10k_tdp" *) reg [7:0] image [0:BYTES-1];
    reg [7:0] read_data = 8'h00;
    reg [15:0] read_addr = 16'h0000;
    wire [15:0] address_a = write_enable[0] ? write_addr : read_addr;
    wire [15:0] address_b = write_addr + 16'd1;

    always @(posedge clk) begin
        if (write_enable[0])
            image[address_a] <= write_data[7:0];
        read_data <= image[address_a];
    end

    always @(posedge clk)
        if (write_enable[1] && write_addr != 16'hffff)
            image[address_b] <= write_data[15:8];

`ifdef VERILATOR
    integer init_i;
    initial begin
        for (init_i = 0; init_i < BYTES; init_i = init_i + 1)
            image[init_i] = 8'h00;
        $readmemh("build/diagnostics/fes-spectrum/tape.hex", image);
    end
`endif

    reg [2:0] state = S_IDLE;
    reg [1:0] phase = P_PILOT;
    reg [15:0] ptr = 16'h0000;
    reg [15:0] length = 16'h0000;
    reg [15:0] bytes_left = 16'h0000;
    reg [15:0] next_addr = 16'h0000;
    reg [13:0] pilot_left = 14'd0;
    reg [21:0] timer = 22'd0;
    reg [21:0] width = 22'd0;
    reg [7:0] cur = 8'h00;
    reg [2:0] bit_i = 3'd0;
    reg bit_half = 1'b0;
    wire ready = unit_state == READY && unit_size != 32'd0 && unit_size <= 32'd65536;

    always @(posedge clk) begin
        if (reset || !ready) begin
            state <= S_IDLE;
            ear <= 1'b0;
            ptr <= 16'h0000;
            read_addr <= 16'h0000;
        end else if (cen) begin
            case (state)
                S_IDLE: begin
                    ear <= 1'b0;
                    ptr <= 16'h0000;
                    read_addr <= 16'h0000;
                    state <= S_LEN_LO;
                end
                S_LEN_LO: begin
                    // read_data is the length low byte requested last cycle.
                    length[7:0] <= read_data;
                    read_addr <= ptr + 16'd1;
                    state <= S_LEN_HI;
                end
                S_LEN_HI: begin
                    length[15:8] <= read_data;
                    read_addr <= ptr + 16'd2;
                    if ({read_data, length[7:0]} == 16'd0 ||
                        {16'd0, ptr} + 32'd2 + {16'd0, read_data, length[7:0]} > unit_size)
                        state <= S_DONE;
                    else
                        state <= S_FLAG;
                end
                S_FLAG: begin
                    cur <= read_data;
                    bytes_left <= length - 16'd1;
                    next_addr <= ptr + 16'd3;
                    pilot_left <= read_data == 8'h00 ? 14'd8063 : 14'd3223;
                    ear <= 1'b1;
                    timer <= 22'd2168;
                    width <= 22'd2168;
                    phase <= P_PILOT;
                    bit_i <= 3'd7;
                    bit_half <= 1'b0;
                    state <= S_PULSE;
                end
                S_PULSE: begin
                    if (timer == 22'd1) begin
                        ear <= ~ear;
                        case (phase)
                            P_PILOT: begin
                                if (pilot_left == 14'd1) begin
                                    phase <= P_SYNC1;
                                    timer <= 22'd667;
                                    width <= 22'd667;
                                end else begin
                                    pilot_left <= pilot_left - 14'd1;
                                    timer <= 22'd2168;
                                end
                            end
                            P_SYNC1: begin
                                phase <= P_SYNC2;
                                timer <= 22'd735;
                                width <= 22'd735;
                            end
                            P_SYNC2: begin
                                phase <= P_BIT;
                                bit_half <= 1'b0;
                                width <= cur[7] ? 22'd1710 : 22'd855;
                                timer <= cur[7] ? 22'd1710 : 22'd855;
                            end
                            default: begin
                                if (!bit_half) begin
                                    bit_half <= 1'b1;
                                    timer <= width;
                                end else if (bit_i != 3'd0) begin
                                    bit_i <= bit_i - 3'd1;
                                    bit_half <= 1'b0;
                                    width <= cur[bit_i - 3'd1] ? 22'd1710 : 22'd855;
                                    timer <= cur[bit_i - 3'd1] ? 22'd1710 : 22'd855;
                                end else if (bytes_left != 16'd0) begin
                                    read_addr <= next_addr;
                                    state <= S_FETCH;
                                end else begin
                                    ear <= 1'b0;
                                    timer <= 22'd3500000;
                                    ptr <= ptr + 16'd2 + length;
                                    state <= S_PAUSE;
                                end
                            end
                        endcase
                    end else begin
                        timer <= timer - 22'd1;
                    end
                end
                S_FETCH: begin
                    cur <= read_data;
                    bytes_left <= bytes_left - 16'd1;
                    next_addr <= next_addr + 16'd1;
                    bit_i <= 3'd7;
                    bit_half <= 1'b0;
                    phase <= P_BIT;
                    width <= read_data[7] ? 22'd1710 : 22'd855;
                    timer <= read_data[7] ? 22'd1710 : 22'd855;
                    state <= S_PULSE;
                end
                S_PAUSE: begin
                    ear <= 1'b0;
                    if (timer == 22'd1) begin
                        if ({16'd0, ptr} + 32'd2 > unit_size)
                            state <= S_DONE;
                        else begin
                            read_addr <= ptr;
                            state <= S_LEN_LO;
                        end
                    end else begin
                        timer <= timer - 22'd1;
                    end
                end
                default: ear <= 1'b0;
            endcase
        end
    end
endmodule
