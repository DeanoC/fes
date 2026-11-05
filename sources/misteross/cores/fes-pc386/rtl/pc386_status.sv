// SPDX-License-Identifier: MIT
`include "fes_application.vh"

// The status ring (docs/pc386-machine-design.md, "HPS DDR window and ports"):
// the system's status words in the DDR window, where the HPS reads them
// through /dev/mem while the core runs (scripts/pc386_cpu_status.py). Every
// 2^PERIOD_LOG2 clocks it latches the six status words in one clock with the
// clock count, writes them as record n mod 256, then writes n + 1 to the head:
//
//   window offset 0x01000000          head: {MAGIC, records written}
//   0x01001000 + 64 x (n mod 256)     record n: word 0 {n, MAGIC}, words 1-6
//                                     the status words 0-5, word 7 the clock count
//
// The reader takes the head, then the newest record, which is not written
// again for 255 periods, and checks its word 0 for n. The clock count runs
// from configuration and never resets; everything else resets with port 2.
// A period that comes while a record is still going out is skipped.
//
// Its beats are single writes on port 2's aux port, in front of pc386_disk,
// which has the port first: a beat takes the port only on a clock the disk
// drives neither read nor write, and the disk sees waitrequest while the beat
// is on the port, which holds it until taken, then gives the port back.
module pc386_status #(
    parameter integer PERIOD_LOG2 = 16
) (
    input wire clk, reset,
    input wire [6*64-1:0] status,
    // pc386_disk's master
    input wire [28:0] s_address,
    input wire [7:0] s_burstcount,
    input wire s_read, s_write,
    input wire [63:0] s_writedata,
    input wire [7:0] s_byteenable,
    output wire s_waitrequest, s_readdatavalid,
    // Port 2's aux port
    output wire [28:0] m_address,
    output wire [7:0] m_burstcount,
    output wire m_read, m_write,
    output wire [63:0] m_writedata,
    output wire [7:0] m_byteenable,
    input wire m_waitrequest, m_readdatavalid
);
    localparam [31:0] MAGIC = 32'h50333836;  // "P386"
    localparam [31:0] HEAD = `FES_APPLICATION_HPS_DDR_WINDOW_BASE + 32'h01000000;
    localparam [28:0] HEAD_WORD = HEAD[31:3];
    localparam [28:0] RING_WORD = HEAD_WORD + 29'd512;

    reg [63:0] clocks = 64'd0;
    always @(posedge clk) clocks <= clocks + 64'd1;

    reg busy, own;
    reg [3:0] beat;                       // 0-7 the record's words, 8 the head
    reg [31:0] count;
    reg [6*64-1:0] latched;
    reg [63:0] stamp;
    wire taken = own && !m_waitrequest;
    always @(posedge clk)
        if (reset) begin
            busy <= 0; own <= 0; beat <= 0; count <= 0;
        end else begin
            if (!busy && clocks[PERIOD_LOG2-1:0] == {PERIOD_LOG2{1'b0}}) begin
                busy <= 1; beat <= 0; latched <= status; stamp <= clocks;
            end
            if (busy && !own && !s_read && !s_write) own <= 1;
            if (taken) begin
                own <= 0;
                beat <= beat + 4'd1;
                if (beat == 4'd8) begin busy <= 0; count <= count + 32'd1; end
            end
        end

    reg [63:0] word;
    always @*
        case (beat)
            4'd0: word = {count, MAGIC};
            4'd1: word = latched[63:0];
            4'd2: word = latched[127:64];
            4'd3: word = latched[191:128];
            4'd4: word = latched[255:192];
            4'd5: word = latched[319:256];
            4'd6: word = latched[383:320];
            4'd7: word = stamp;
            default: word = {MAGIC, count + 32'd1};
        endcase

    assign m_address = !own ? s_address : beat == 4'd8 ? HEAD_WORD : RING_WORD + {18'd0, count[7:0], beat[2:0]};
    assign m_burstcount = own ? 8'd1 : s_burstcount;
    assign m_read = !own && s_read;
    assign m_write = own || s_write;
    assign m_writedata = own ? word : s_writedata;
    assign m_byteenable = own ? 8'hff : s_byteenable;
    assign s_waitrequest = own || m_waitrequest;
    assign s_readdatavalid = m_readdatavalid;
endmodule
