// SPDX-License-Identifier: MIT
// pc386 DDR port fence for the M4 bus master's machine reset
// (docs/pc386-machine-design.md, phase M4).
//
// The HPS controller cannot recover a burst that stops midway, and
// fes_hps_ddr's guards finish one only during an execution hold. The bus
// master resets the machine without that hold. While hold is high this fence
// takes no new command from the machine and finishes a write burst the port
// started, with byte enables cleared, even if hold falls first. Each filler
// beat carries its burst's first-beat address and length. A command the port
// refused stays presented, unchanged, until the port takes it, as Avalon-MM
// requires, though the machine resets underneath it. The fence hides the read
// data the machine was owed until all of it has returned. Otherwise it passes
// the port through, adding no clock. reset is the port's execution reset,
// under which fes_hps_ddr's guards do the same.
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
    reg [7:0] beats_owed = 8'd0;     // write beats left in the burst the port took a first beat of
    reg [11:0] reads_owed = 12'd0;   // read beats not yet returned
    reg finishing = 1'b0;            // filling that burst after hold fell
    reg hiding = 1'b0;               // read data owed from before a hold
    reg refused = 1'b0;              // the port refused last clock's command
    reg owned = 1'b0;                // ... and it is the fence's to present again
    // The burst's first beat, and last clock's command.
    reg [ADDR_W-1:0] burst_address = {ADDR_W{1'b0}}, last_address = {ADDR_W{1'b0}};
    reg [7:0] burst_burstcount = 8'd0, last_burstcount = 8'd0;
    reg last_read = 1'b0, last_write = 1'b0;
    reg [DATA_W-1:0] last_writedata = {DATA_W{1'b0}};
    reg [DATA_W/8-1:0] last_byteenable = {(DATA_W / 8){1'b0}};
    wire fencing = hold || finishing || owned;
    // A command the port refused when hold rose, or while fencing, goes
    // again; the machine may already be in reset.
    wire again = refused && fencing;
    wire filling = fencing && beats_owed != 8'd0;
    wire blocked = fencing || hiding;

    assign m_read = again ? last_read : read && !blocked;
    assign m_write = again ? last_write : filling || (write && !blocked);
    assign m_address = again ? last_address : filling ? burst_address : address;
    assign m_burstcount = again ? last_burstcount : filling ? burst_burstcount : burstcount;
    assign m_writedata = again ? last_writedata : writedata;
    assign m_byteenable = again ? last_byteenable : filling ? {(DATA_W / 8){1'b0}} : byteenable;
    assign waitrequest = m_waitrequest || blocked;
    assign readdata = m_readdata;
    assign readdatavalid = m_readdatavalid && !blocked;

    wire refusing = (m_read || m_write) && m_waitrequest;
    wire beat_taken = m_write && !m_waitrequest;
    wire read_taken = m_read && !m_waitrequest;
    wire first_beat = beat_taken && beats_owed == 8'd0;
    wire [7:0] beats_after = !beat_taken ? beats_owed : first_beat ? m_burstcount - 8'd1 : beats_owed - 8'd1;
    wire [11:0] reads_after = reads_owed + (read_taken ? {4'd0, m_burstcount} : 12'd0) -
                              {11'd0, m_readdatavalid};
    always @(posedge clk) begin
        last_address <= m_address; last_burstcount <= m_burstcount; last_read <= m_read; last_write <= m_write;
        last_writedata <= m_writedata; last_byteenable <= m_byteenable;
        if (first_beat) begin burst_address <= m_address; burst_burstcount <= m_burstcount; end
        if (reset) begin
            beats_owed <= 0; reads_owed <= 0; finishing <= 0; hiding <= 0; refused <= 0; owned <= 0;
        end else begin
            beats_owed <= beats_after;
            reads_owed <= reads_after;
            refused <= refusing;
            owned <= refusing && fencing;
            finishing <= fencing && beats_after != 8'd0;
            // A read the fence still presents after hold falls is hidden too.
            hiding <= hold || (refusing && fencing) || (hiding && reads_after != 12'd0);
        end
    end
endmodule
