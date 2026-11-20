// SPDX-License-Identifier: GPL-2.0-or-later
`include "generated/fes_de25_hps_window.vh"
// fes.fabric.de25-hps-window 1.0's control block, in the board's peripheral
// clock (clk_sys): the runtime publishes the kernel's grant here while the
// machine is held, and withdraws it the same way.
//
// The grant registers (base, span, generation) are writable only while no
// grant is held; a write then is refused with SLVERR. CommandGrant is
// accepted only for the exact span, a base aligned to GrantAlignment and
// below 4 GiB, a nonzero generation, and a held, drained machine;
// CommandClear only while held and drained, and it clears the fault record.
// A refused command answers SLVERR and sets FaultCommand.
//
// Crossings into machine_clk: granted is a level into a two-stage
// synchronizer; the base is a held bundle, constant whenever granted is set,
// because it is writable only while granted is clear; Clear is a toggle into
// a two-stage synchronizer. Back into clk_sys: drained, held and the five
// fault bits are levels, each into its own two-stage synchronizer.
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
    input  wire        machine_clk,
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
    (* async_reg = "true", altera_attribute = "-name SYNCHRONIZER_IDENTIFICATION FORCED_IF_ASYNCHRONOUS" *) reg [9:0] fault_sync = 10'd0;
    // Launch registers in machine_clk, so each crossing starts at a flop.
    (* preserve, dont_merge *) reg drained_source = 1'b0, held_source = 1'b0;
    (* preserve, dont_merge *) reg [4:0] fault_source = 5'd0;
    always @(posedge machine_clk) begin
        drained_source <= machine_drained;
        held_source <= machine_held;
        fault_source <= {fault_outside, fault_no_grant, fault_port};
    end
    always @(posedge clk_sys) begin
        drained_sync <= {drained_sync[0], drained_source};
        held_sync <= {held_sync[0], held_source};
        fault_sync <= {fault_sync[4:0], fault_source};
    end
    wire drained = drained_sync[1];
    wire held = held_sync[1];
    wire [4:0] machine_fault = fault_sync[9:5];

    // ---- Registers
    reg granted = 1'b0, command_fault = 1'b0, clear_toggle = 1'b0;
    reg [31:0] base_lo = 32'd0, base_hi = 32'd0, span = 32'd0, gen_lo = 32'd0, gen_hi = 32'd0;
    wire acceptable = span == SPAN && (base_lo & (ALIGN - 1)) == 32'd0 && base_hi == 32'd0 &&
                      (gen_lo | gen_hi) != 32'd0;
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
    reg clear_seen = 1'b0;
    always @(posedge machine_clk) begin
        granted_sync <= {granted_sync[0], granted};
        clear_sync <= {clear_sync[0], clear_toggle};
        clear_seen <= clear_sync[1];
    end
    assign grant_valid = granted_sync[1];
    // The base is a held bundle: it is constant while granted is set, and a
    // grant reaches the machine two machine clocks after it is set.
    assign grant_base = base_lo;
    assign fault_clear = clear_sync[1] != clear_seen;
endmodule
