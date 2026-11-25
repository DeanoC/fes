// SPDX-License-Identifier: GPL-2.0-or-later
`include "generated/fes_de25_hps_window.vh"
// fes.fabric.de25-hps-window 1.0's control block, in the board's peripheral
// clock (clk_sys): the runtime publishes the kernel's grant here while the
// machine is held, and withdraws it the same way.
//
// The grant registers (base, span, generation) are writable only while no
// grant is held; a write then is refused with SLVERR. CommandGrant is
// accepted only for the exact span, a base aligned to GrantAlignment whose
// whole span lies below 4 GiB, a nonzero generation, and a held, drained
// machine;
// CommandClear only while held and drained, and it clears the fault record.
// A refused command answers SLVERR and sets FaultCommand.
//
// Crossings into machine_clk: granted is a level into a two-stage
// synchronizer, and the base is a held bundle copied in machine_clk when
// that synchronized level rises. The base is writable only while granted is
// clear, so it has been still since before granted was set. grant_valid
// rises with the copy, one machine clock after the synchronizer's output.
// Clear is a toggle into a two-stage synchronizer.
//
// Back into clk_sys: drained and held are levels, each into its own
// two-stage synchronizer. The fault record (port bits and reason) crosses by
// request and acknowledge, as pc386_snapshot does: when the live record
// differs from the held copy and no request is open, the machine side copies
// it and flips the request. The held copy launches through fault_launch, a
// register that copies it every clock (so the crossing leaves a register whose
// only input is the record), and the request is delayed one clock with it, so
// the data never launches after its request. clk_sys copies fault_launch on the clock its
// synchronizer's output moves, marked by fault_request_seen (that output one
// clock later); fault_ack, one clock after that, is the acknowledge, so the
// copy's enable comes from the synchronizer chain alone; the machine side
// copies again only once the acknowledge has crossed back. The held copy is therefore still from before
// the request leaves until after clk_sys has copied it, and FAULT follows
// the record whenever it changes, whatever the machine is doing.
module fes_de25_hps_window_control (
    input  wire        clk_sys, reset_n,
    input  wire [5:0]  address,
    input  wire        read, write,
    input  wire [3:0]  byteenable,
    input  wire [31:0] writedata,
    output reg  [31:0] readdata = 32'd0,
    output reg         readdatavalid = 1'b0,
    output reg         writeresponsevalid = 1'b0,
    output reg  [1:0]  response = 2'b00,
    output wire        waitrequest,
    // Machine clock side.
    input  wire        machine_clk, machine_reset_n,
    output wire        grant_valid,
    output wire [31:0] grant_base,
    output wire        fault_clear,
    input  wire        machine_drained, machine_held,
    input  wire [2:0]  fault_port,
    input  wire        fault_no_grant, fault_outside
);
    localparam [31:0] SPAN = `FES_DE25_HPS_WINDOW_SPAN;
    localparam [31:0] ALIGN = `FES_DE25_HPS_WINDOW_GRANT_ALIGNMENT;
    assign waitrequest = 1'b0;

    // ---- Machine -> clk_sys: levels, one synchronizer each
    (* async_reg = "true", altera_attribute = "-name SYNCHRONIZER_IDENTIFICATION FORCED_IF_ASYNCHRONOUS" *) reg [1:0] drained_sync = 2'b00;
    (* async_reg = "true", altera_attribute = "-name SYNCHRONIZER_IDENTIFICATION FORCED_IF_ASYNCHRONOUS" *) reg [1:0] held_sync = 2'b00;
    // Launch registers in machine_clk, so each crossing starts at a flop.
    (* preserve, dont_merge *) reg drained_source = 1'b0, held_source = 1'b0;
    always @(posedge machine_clk or negedge machine_reset_n) begin
        if (!machine_reset_n) begin
            drained_source <= 1'b0; held_source <= 1'b0;
        end else begin
            drained_source <= machine_drained;
            held_source <= machine_held;
        end
    end
    always @(posedge clk_sys or negedge reset_n) begin
        if (!reset_n) begin
            drained_sync <= 2'b00; held_sync <= 2'b00;
        end else begin
            drained_sync <= {drained_sync[0], drained_source};
            held_sync <= {held_sync[0], held_source};
        end
    end
    wire drained = drained_sync[1];
    wire held = held_sync[1];

    // ---- Machine -> clk_sys: the fault record, by request and acknowledge
    (* async_reg = "true", altera_attribute = "-name SYNCHRONIZER_IDENTIFICATION FORCED_IF_ASYNCHRONOUS" *) reg [1:0] fault_request_sync = 2'b00;
    (* async_reg = "true", altera_attribute = "-name SYNCHRONIZER_IDENTIFICATION FORCED_IF_ASYNCHRONOUS" *) reg [1:0] fault_ack_sync = 2'b00;
    (* preserve, dont_merge *) reg [4:0] fault_hold = 5'd0;
    (* preserve, dont_merge *) reg fault_request = 1'b0, fault_ack = 1'b0;
    (* preserve, dont_merge *) reg [4:0] fault_launch = 5'd0;
    reg request_pre = 1'b0;
    reg fault_request_seen = 1'b0;
    // Copy only, enabled from the synchronizer chain alone: register to register from fault_launch.
    reg [4:0] machine_fault = 5'd0;
    wire [4:0] fault_live = {fault_outside, fault_no_grant, fault_port};
    // Both sides start a fresh handshake when the window's bus epoch ends.
    always @(posedge machine_clk or negedge machine_reset_n) begin
        if (!machine_reset_n) begin
            fault_ack_sync <= 2'b00;
            fault_hold <= 5'd0; fault_launch <= 5'd0;
            request_pre <= 1'b0; fault_request <= 1'b0;
        end else begin
            fault_ack_sync <= {fault_ack_sync[0], fault_ack};
            if (fault_ack_sync[1] == request_pre && fault_live != fault_hold) begin
                fault_hold <= fault_live;
                request_pre <= !request_pre;
            end
            fault_launch <= fault_hold;
            fault_request <= request_pre;
        end
    end
    always @(posedge clk_sys or negedge reset_n) begin
        if (!reset_n) begin
            fault_request_sync <= 2'b00;
            fault_request_seen <= 1'b0; fault_ack <= 1'b0;
            machine_fault <= 5'd0;
        end else begin
            fault_request_sync <= {fault_request_sync[0], fault_request};
            fault_request_seen <= fault_request_sync[1];
            fault_ack <= fault_request_seen;
            if (fault_request_sync[1] != fault_request_seen) machine_fault <= fault_launch;
        end
    end

    // ---- Registers
    reg granted = 1'b0, command_fault = 1'b0, clear_toggle = 1'b0;
    reg [31:0] base_lo = 32'd0, base_hi = 32'd0, span = 32'd0, gen_lo = 32'd0, gen_hi = 32'd0;
    // The whole span lies below 4 GiB: the rebase adds 32-bit offsets, so a
    // span that crossed it would wrap into low memory.
    wire [32:0] grant_end = {1'b0, base_lo} + {1'b0, SPAN};
    wire acceptable = span == SPAN && (base_lo & (ALIGN - 1)) == 32'd0 && base_hi == 32'd0 &&
                      grant_end <= 33'h1_0000_0000 && (gen_lo | gen_hi) != 32'd0;
    wire [31:0] status = {28'd0, command_fault || machine_fault != 5'd0, held, drained, granted};
    wire [31:0] fault_word = {21'd0, command_fault, machine_fault[4], machine_fault[3], 5'd0, machine_fault[2:0]};
    wire full = byteenable == 4'b1111;
    always @(posedge clk_sys or negedge reset_n) begin
        if (!reset_n) begin
            granted <= 1'b0; command_fault <= 1'b0; clear_toggle <= 1'b0;
            base_lo <= 32'd0; base_hi <= 32'd0; span <= 32'd0; gen_lo <= 32'd0; gen_hi <= 32'd0;
            readdatavalid <= 1'b0; writeresponsevalid <= 1'b0; response <= 2'b00; readdata <= 32'd0;
        end else begin
            readdatavalid <= read;
            writeresponsevalid <= write;
            response <= 2'b00;
            if (read) begin
                case (address)
                    6'h00: readdata <= `FES_DE25_HPS_WINDOW_IDENTITY;
                    6'h04: readdata <= `FES_DE25_HPS_WINDOW_VERSION;
                    6'h08: readdata <= status;
                    6'h10: readdata <= base_lo;
                    6'h14: readdata <= base_hi;
                    6'h18: readdata <= span;
                    6'h1c: readdata <= gen_lo;
                    6'h20: readdata <= gen_hi;
                    6'h24: readdata <= fault_word;
                    default: readdata <= 32'd0;
                endcase
            end
            if (write) begin
                if (!full) response <= 2'b10;
                else case (address)
                    6'h0c: begin
                        if (writedata == `FES_DE25_HPS_WINDOW_COMMAND_GRANT && !granted && held && drained && acceptable)
                            granted <= 1'b1;
                        else if (writedata == `FES_DE25_HPS_WINDOW_COMMAND_CLEAR && held && drained) begin
                            granted <= 1'b0; command_fault <= 1'b0; clear_toggle <= ~clear_toggle;
                        end else begin
                            response <= 2'b10; command_fault <= 1'b1;
                        end
                    end
                    6'h10: if (granted) response <= 2'b10; else base_lo <= writedata;
                    6'h14: if (granted) response <= 2'b10; else base_hi <= writedata;
                    6'h18: if (granted) response <= 2'b10; else span <= writedata;
                    6'h1c: if (granted) response <= 2'b10; else gen_lo <= writedata;
                    6'h20: if (granted) response <= 2'b10; else gen_hi <= writedata;
                    default: response <= 2'b10;
                endcase
            end
        end
    end

    // ---- clk_sys -> machine
    (* async_reg = "true", altera_attribute = "-name SYNCHRONIZER_IDENTIFICATION FORCED_IF_ASYNCHRONOUS" *) reg [1:0] granted_sync = 2'b00;
    (* async_reg = "true", altera_attribute = "-name SYNCHRONIZER_IDENTIFICATION FORCED_IF_ASYNCHRONOUS" *) reg [1:0] clear_sync = 2'b00;
    reg clear_seen = 1'b0, grant_seen = 1'b0;
    reg [31:0] base_machine = 32'd0;
    // Reset the receiving grant too, including when the machine clock stops.
    // The retained base is unusable until a new synchronized grant arrives.
    always @(posedge machine_clk or negedge machine_reset_n) begin
        if (!machine_reset_n) begin
            granted_sync <= 2'b00;
            grant_seen <= 1'b0;
            clear_sync <= 2'b00; clear_seen <= 1'b0;
        end else begin
            granted_sync <= {granted_sync[0], granted};
            grant_seen <= granted_sync[1];
            clear_sync <= {clear_sync[0], clear_toggle};
            clear_seen <= clear_sync[1];
        end
    end
    always @(posedge machine_clk)
        if (granted_sync[1] && !grant_seen) base_machine <= base_lo;
    assign grant_valid = grant_seen;
    assign grant_base = base_machine;
    assign fault_clear = clear_sync[1] != clear_seen;
endmodule
