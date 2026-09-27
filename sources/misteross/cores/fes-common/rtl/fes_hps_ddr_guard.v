// SPDX-License-Identifier: GPL-2.0-or-later
// Registered Avalon-MM boundary for one fes.memory.hps-ddr port.
//
// The HPS controller cannot recover a burst that stops midway: the port
// stays unusable until the HPS resets. Every command the core hands over
// is therefore held in this guard until the controller takes it. While
// hold is high the guard accepts nothing new, sends the remaining beats of
// a write burst the core started with byte enables cleared, and hides the
// read data of reads issued before the hold. After hold falls the core
// waits until that data has drained.
//
// Signals to and from the controller are registered here, so the HPS
// boundary paths start and end at these flops. hold is synchronous to clk.
module fes_hps_ddr_guard #(
    parameter integer DATA_W = 64,
    parameter integer ADDR_W = 29
) (
    input  wire                clk,
    input  wire                hold,
    // Core side.
    input  wire [ADDR_W-1:0]   address,
    input  wire [7:0]          burstcount,
    output wire                waitrequest,
    output reg  [DATA_W-1:0]   readdata,
    output reg                 readdatavalid,
    input  wire                read,
    input  wire [DATA_W-1:0]   writedata,
    input  wire [DATA_W/8-1:0] byteenable,
    input  wire                write,
    // Controller side.
    output reg  [ADDR_W-1:0]   m_address,
    output reg  [7:0]          m_burstcount,
    input  wire                m_waitrequest,
    input  wire [DATA_W-1:0]   m_readdata,
    input  wire                m_readdatavalid,
    output reg                 m_read,
    output reg  [DATA_W-1:0]   m_writedata,
    output reg  [DATA_W/8-1:0] m_byteenable,
    output reg                 m_write
);
    localparam integer BE_W = DATA_W / 8;

    // Skid entry behind the presented command.
    reg              skid = 1'b0;
    reg [ADDR_W-1:0] skid_address;
    reg [7:0]        skid_burstcount;
    reg              skid_read;
    reg              skid_write;
    reg [DATA_W-1:0] skid_writedata;
    reg [BE_W-1:0]   skid_byteenable;

    // Write beats still owed for the burst the core started, read beats
    // not yet returned, and the post-hold states.
    reg [7:0]  beats_owed = 8'd0;
    reg [11:0] reads_owed = 12'd0;
    reg        finishing = 1'b0;
    reg        draining = 1'b0;
    // Read data lands here straight from the controller: the route out of
    // the hard block is long, so no logic sits on it.
    reg              returned = 1'b0;
    reg [DATA_W-1:0] returned_data;

    initial begin
        returned_data = {DATA_W{1'b0}};
        m_read = 1'b0;
        m_write = 1'b0;
        readdatavalid = 1'b0;
    end

    assign waitrequest = skid | hold | finishing | draining;

    // A beat enters the guard from the core, or from the guard itself
    // while it finishes a write burst the core abandoned.
    wire from_core = (read | write) & ~waitrequest;
    wire filler = finishing & ~skid & (beats_owed != 8'd0);
    wire enter = from_core | filler;
    wire enter_read = from_core & read;
    wire enter_write = (from_core & write) | filler;
    wire [ADDR_W-1:0] enter_address = filler ? m_address : address;
    wire [7:0] enter_burstcount = filler ? m_burstcount : burstcount;
    wire [BE_W-1:0] enter_byteenable = filler ? {BE_W{1'b0}} : byteenable;

    // The presented command leaves when the controller takes it.
    wire slot_free = ~(m_read | m_write) | ~m_waitrequest;

    wire [7:0] beats_after = ~enter_write ? beats_owed :
        (beats_owed == 8'd0 ? enter_burstcount - 8'd1 : beats_owed - 8'd1);
    wire [11:0] reads_after = reads_owed + (enter_read ? {4'd0, burstcount} : 12'd0) -
        (returned ? 12'd1 : 12'd0);

    always @(posedge clk) begin
        if (slot_free) begin
            if (skid) begin
                m_address <= skid_address;
                m_burstcount <= skid_burstcount;
                m_read <= skid_read;
                m_write <= skid_write;
                m_writedata <= skid_writedata;
                m_byteenable <= skid_byteenable;
                skid <= 1'b0;
            end else if (enter) begin
                m_address <= enter_address;
                m_burstcount <= enter_burstcount;
                m_read <= enter_read;
                m_write <= enter_write;
                m_writedata <= writedata;
                m_byteenable <= enter_byteenable;
            end else begin
                m_read <= 1'b0;
                m_write <= 1'b0;
            end
        end else if (enter) begin
            skid_address <= enter_address;
            skid_burstcount <= enter_burstcount;
            skid_read <= enter_read;
            skid_write <= enter_write;
            skid_writedata <= writedata;
            skid_byteenable <= enter_byteenable;
            skid <= 1'b1;
        end

        beats_owed <= beats_after;
        reads_owed <= reads_after;
        finishing <= (hold | finishing) & (beats_after != 8'd0);
        draining <= hold | (draining & (reads_after != 12'd0));

        returned <= m_readdatavalid;
        returned_data <= m_readdata;
        readdata <= returned_data;
        readdatavalid <= returned & ~hold & ~draining;
    end
endmodule
