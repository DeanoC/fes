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
// Command payloads stay in two register banks; registered head/valid
// state selects the presented command. Controller ready updates only queue
// state. Read responses are registered. hold is synchronous to clk.
module fes_hps_ddr_guard #(
    parameter integer DATA_W = 64,
    parameter integer ADDR_W = 29
) (
    input  wire                clk,
    input  wire                hold,
    // Every locally owned command/write beat/read response has completed.
    // This proof stays meaningful during hold, when responses are hidden.
    output wire                drained,
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
    output wire [ADDR_W-1:0]   m_address,
    output wire [7:0]          m_burstcount,
    input  wire                m_waitrequest,
    input  wire [DATA_W-1:0]   m_readdata,
    input  wire                m_readdatavalid,
    output wire                m_read,
    output wire [DATA_W-1:0]   m_writedata,
    output wire [DATA_W/8-1:0] m_byteenable,
    output wire                m_write
);
    localparam integer BE_W = DATA_W / 8;

    // Two banks retain each command until retirement. The late controller
    // ready signal changes only queue state, never a wide payload enable.
    reg head = 1'b0;
    reg tail = 1'b0;
    reg present = 1'b0;
    reg skid = 1'b0; // both banks occupied
    reg [ADDR_W-1:0] command_address [0:1];
    reg [7:0] command_burstcount [0:1];
    reg command_read [0:1];
    reg command_write [0:1];
    reg [DATA_W-1:0] command_writedata [0:1];
    reg [BE_W-1:0] command_byteenable [0:1];
    // tail points to the next free bank; its opposite retains the most
    // recently accepted header, including after that command retires.
    wire previous = ~tail;
    wire [ADDR_W-1:0] burst_address = command_address[previous];
    wire [7:0] burst_length = command_burstcount[previous];

    assign m_address = command_address[head];
    assign m_burstcount = command_burstcount[head];
    assign m_read = present & command_read[head];
    assign m_write = present & command_write[head];
    assign m_writedata = command_writedata[head];
    assign m_byteenable = command_byteenable[head];

    // Write beats still owed for the burst the core started, read beats
    // not yet returned, and the post-hold states.
    reg [7:0]  beats_owed = 8'd0;
    reg        owed_zero = 1'b1;   // beats_owed == 0
    reg        owed_one = 1'b0;    // beats_owed == 1
    reg [11:0] reads_owed = 12'd0;
    reg        finishing = 1'b0;
    reg        draining = 1'b0;
    // Read data lands here straight from the controller: the route out of
    // the hard block is long, so no logic sits on it.
    reg              returned = 1'b0;
    reg [DATA_W-1:0] returned_data;

    initial begin
        returned_data = {DATA_W{1'b0}};
        readdatavalid = 1'b0;
    end

    assign waitrequest = skid | hold | finishing | draining;

    // A beat enters the guard from the core, or from the guard itself
    // while it finishes a write burst the core abandoned.
    wire from_core = (read | write) & ~waitrequest;
    wire filler = finishing & ~skid & ~owed_zero;
    wire enter = from_core | filler;
    // A hold is containment, not completion. Count reads at core acceptance,
    // including commands not yet taken by the controller. Wait for both return
    // pipeline stages to empty after the last owed response. The persistent
    // `draining` posture is intentionally excluded: hold keeps it asserted even
    // after all old traffic has completed.
    assign drained = !enter && !skid && !m_read && !m_write && owed_zero &&
                     !finishing && reads_owed == 12'd0 && !returned &&
                     !m_readdatavalid && !readdatavalid;
    wire enter_read = from_core & read;
    wire enter_write = (from_core & write) | filler;
    wire [ADDR_W-1:0] enter_address = filler ? burst_address : address;
    wire [7:0] enter_burstcount = filler ? burst_length : burstcount;
    wire [BE_W-1:0] enter_byteenable = filler ? {BE_W{1'b0}} : byteenable;

    // The presented command leaves when the controller takes it.
    wire retire = present & ~m_waitrequest;

    // A first beat only comes from the core (a filler needs beats owed), so
    // its burst length is the core's. The flags keep the counter's adder off
    // the finishing decision.
    wire [7:0] beats_after = ~enter_write ? beats_owed :
        (owed_zero ? burstcount - 8'd1 : beats_owed - 8'd1);
    wire owed_after = ~enter_write ? ~owed_zero :
        (owed_zero ? burstcount != 8'd1 : ~owed_one);
    wire [11:0] reads_after = reads_owed + (enter_read ? {4'd0, burstcount} : 12'd0) -
        (returned ? 12'd1 : 12'd0);

    always @(posedge clk) begin
        if (enter) begin
            command_address[tail] <= enter_address;
            command_burstcount[tail] <= enter_burstcount;
            command_read[tail] <= enter_read;
            command_write[tail] <= enter_write;
            command_writedata[tail] <= writedata;
            command_byteenable[tail] <= enter_byteenable;
            tail <= ~tail;
        end
        if (retire) head <= ~head;
        case ({enter, retire})
            2'b10: begin present <= 1'b1; skid <= present; end
            2'b01: begin present <= skid; skid <= 1'b0; end
            default: begin end
        endcase

        beats_owed <= beats_after;
        owed_zero <= ~owed_after;
        owed_one <= beats_after == 8'd1;
        reads_owed <= reads_after;
        finishing <= (hold | finishing) & owed_after;
        draining <= hold | (draining & (reads_after != 12'd0));

        returned <= m_readdatavalid;
        returned_data <= m_readdata;
        readdata <= returned_data;
        readdatavalid <= returned & ~hold & ~draining;
    end
endmodule
