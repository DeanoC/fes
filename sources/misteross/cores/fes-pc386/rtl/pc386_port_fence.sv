// SPDX-License-Identifier: MIT
// pc386 DDR port fence for the M4 bus master's machine reset
// (docs/pc386-machine-design.md, phase M4).
//
// The HPS controller cannot recover a burst that stops midway, and
// fes_hps_ddr's guards finish one only during an execution hold. The bus
// master resets the machine without that hold. While hold is high this fence
// takes no new command from the machine and finishes a write burst the
// machine started, with byte enables cleared, even if hold falls first. It
// hides the read data the machine was owed until all of it has returned.
// Otherwise it passes the port through, adding no clock. reset is the port's
// execution reset, under which fes_hps_ddr's guards do the same.
module pc386_port_fence #(
    parameter integer DATA_W = 64,
    parameter integer ADDR_W = 29
) (
    input wire clk, reset, hold,
    // The machine's master
    input wire [ADDR_W-1:0] address,
    input wire [7:0] burstcount,
    input wire read, write,
    input wire [DATA_W-1:0] writedata,
    input wire [DATA_W/8-1:0] byteenable,
    output wire waitrequest,
    output wire [DATA_W-1:0] readdata,
    output wire readdatavalid,
    // The port
    output wire [ADDR_W-1:0] m_address,
    output wire [7:0] m_burstcount,
    output wire m_read, m_write,
    output wire [DATA_W-1:0] m_writedata,
    output wire [DATA_W/8-1:0] m_byteenable,
    input wire m_waitrequest,
    input wire [DATA_W-1:0] m_readdata,
    input wire m_readdatavalid
);
    reg [7:0] beats_owed = 8'd0;     // write beats left in the burst the machine started
    reg [11:0] reads_owed = 12'd0;   // read beats not yet returned
    reg finishing = 1'b0;            // filling the burst after hold fell
    reg hiding = 1'b0;               // read data owed from before a hold
    wire fencing = hold || finishing;
    wire filling = fencing && beats_owed != 8'd0;
    wire blocked = fencing || hiding;

    // Burst address and length matter only on a first beat; a filler is never one.
    assign m_address = address;
    assign m_burstcount = burstcount;
    assign m_read = read && !blocked;
    assign m_write = filling || (write && !blocked);
    assign m_writedata = writedata;
    assign m_byteenable = filling ? {(DATA_W / 8){1'b0}} : byteenable;
    assign waitrequest = m_waitrequest || blocked;
    assign readdata = m_readdata;
    assign readdatavalid = m_readdatavalid && !blocked;

    wire beat_taken = m_write && !m_waitrequest;
    wire read_taken = m_read && !m_waitrequest;
    wire [7:0] beats_after = !beat_taken ? beats_owed :
                             beats_owed == 8'd0 ? burstcount - 8'd1 : beats_owed - 8'd1;
    wire [11:0] reads_after = reads_owed + (read_taken ? {4'd0, burstcount} : 12'd0) -
                              {11'd0, m_readdatavalid};
    always @(posedge clk) begin
        if (reset) begin
            beats_owed <= 0; reads_owed <= 0; finishing <= 0; hiding <= 0;
        end else begin
            beats_owed <= beats_after;
            reads_owed <= reads_after;
            finishing <= fencing && beats_after != 8'd0;
            hiding <= hold || (hiding && reads_after != 12'd0);
        end
    end
endmodule
