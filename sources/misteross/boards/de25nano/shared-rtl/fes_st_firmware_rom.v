// SPDX-License-Identifier: GPL-3.0-or-later
// 192 KiB Atari ST ROM filled at launch (fes.fabric.firmware-upload), with
// st_rom's request port and its latency: ready rises five clocks after a
// request is seen and holds until the request drops. Uploaded byte pairs
// start at even addresses, so the image is one RAM of big-endian words in two
// byte lanes: the even byte is the upper half of the 68000 word. The contents
// power up undefined; the firmware front end keeps the machine held until an
// image is committed. No Atari ROM bytes are in this repository or a build.
module fes_st_firmware_rom (
    input  wire        clk, reset, req,
    input  wire [17:1] address,
    output reg  [15:0] rdata,
    output reg         ready,
    input  wire [17:0] write_addr,
    input  wire [15:0] write_data,
    input  wire [1:0]  write_enable
);
    localparam integer WORDS = 98304;
    localparam [2:0] IDLE=0, EVEN_READ=1, ODD_READ=2, EVEN_GROUP=3, ODD_GROUP=4, COMPLETE=5;
    (* ramstyle = "M20K" *) reg [7:0] upper [0:WORDS-1];
    (* ramstyle = "M20K" *) reg [7:0] lower [0:WORDS-1];
    reg [2:0] state;
    reg [17:1] word_address;
    reg [7:0] upper_q, lower_q, upper_stage, lower_stage;
    always @(posedge clk) begin
        if (write_enable[0]) upper[write_addr[17:1]] <= write_data[7:0];
        if (write_enable[1]) lower[write_addr[17:1]] <= write_data[15:8];
        upper_q <= upper[word_address];
        lower_q <= lower[word_address];
        upper_stage <= upper_q;
        lower_stage <= lower_q;
    end
    always @(posedge clk) begin
        if (reset) begin
            state<=IDLE; word_address<=0; ready<=0; rdata<=0;
        end else case (state)
            IDLE: if (req) begin word_address<=address; state<=EVEN_READ; end
            EVEN_READ: state<=ODD_READ;
            ODD_READ: state<=EVEN_GROUP;
            EVEN_GROUP: state<=ODD_GROUP;
            ODD_GROUP: begin rdata<={upper_stage,lower_stage}; ready<=1; state<=COMPLETE; end
            COMPLETE: if (!req) begin ready<=0; state<=IDLE; end
            default: state<=IDLE;
        endcase
    end
endmodule
