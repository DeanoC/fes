// SPDX-License-Identifier: GPL-2.0-or-later
// Disk II controller card logic for one slot of the FES Apple II bus.
//
// The card owns what the original controller board owns: the $C0n0-$C0nF
// switches (four stepper phases, motor with its ~1 s off delay, drive select,
// Q6/Q7 mode) and the read data latch. The drive cable carries phases, motor
// and drive select to apple2_disk2_drive and brings back one read bit per
// 4 us bit cell plus write protect. The $Cn00 boot PROM is not in this module;
// the shell serves it from the linked firmware window.
//
// Read latch model: bits shift into an assembly register; leading zeros do
// not shift, which is the self-sync behaviour of the logic state sequencer.
// When a one reaches bit 7 the nibble is complete. The completed nibble is
// held in the latch for eight CPU cycles (two bit cells), long enough for the
// seven-cycle `LDA $C08C,X / BPL` poll and short enough that the next read of
// a well-formed routine sees the following nibble. Outside the hold window a
// read returns the partially assembled next nibble (bit 7 clear). This is a
// behavioural model, not the P6 PROM state table. Write mode is not
// implemented: the drive always reports write protect, so DOS refuses writes.
`include "apple2_bus.vh"

module apple2_disk2_card #(
    parameter integer MOTOR_OFF_DELAY_CYCLES = 1_000_000
) (
    input  wire clk,
    input  wire [`A2_BUS_REQ-1:0] request,
    input  wire devsel,
    output wire [`A2_BUS_RSP-1:0] response,

    // Drive cable.
    output reg  [3:0] phases,
    output wire motor_on,
    output reg  drive2,
    input  wire bit_ce,
    input  wire read_bit,
    input  wire write_protect
);
    wire [15:0] addr = request[`A2_BUS_A];
    wire bus_read = request[`A2_BUS_READ];
    wire strobe = request[`A2_BUS_STROBE];
    wire bus_reset = request[`A2_BUS_RESET];
    wire access = strobe && devsel;

    reg motor = 1'b0;
    reg [19:0] motor_delay = 20'd0;
    reg q6 = 1'b0;
    reg q7 = 1'b0;
    reg [7:0] assembly = 8'd0;
    reg [7:0] latch = 8'd0;
    reg [3:0] hold = 4'd0;

    assign motor_on = motor || motor_delay != 20'd0;

    always @(posedge clk) begin
        if (bus_reset) begin
            phases <= 4'b0000;
            motor <= 1'b0;
            motor_delay <= 20'd0;
            drive2 <= 1'b0;
            q6 <= 1'b0;
            q7 <= 1'b0;
            assembly <= 8'd0;
            latch <= 8'd0;
            hold <= 4'd0;
        end else begin
            if (strobe) begin
                if (hold != 4'd0)
                    hold <= hold - 4'd1;
                if (!motor && motor_delay != 20'd0)
                    motor_delay <= motor_delay - 20'd1;
            end
            if (access) begin
                case (addr[3:1])
                    3'd0, 3'd1, 3'd2, 3'd3: phases[addr[2:1]] <= addr[0];
                    3'd4: begin
                        if (addr[0]) begin
                            motor <= 1'b1;
                        end else if (motor) begin
                            motor <= 1'b0;
                            motor_delay <= MOTOR_OFF_DELAY_CYCLES[19:0];
                        end
                    end
                    3'd5: drive2 <= addr[0];
                    3'd6: q6 <= addr[0];
                    default: q7 <= addr[0];
                endcase
            end
            if (bit_ce && motor_on && !q7) begin
                if (assembly[6]) begin
                    latch <= {assembly[6:0], read_bit};
                    hold <= 4'd8;
                    assembly <= 8'd0;
                end else if (assembly != 8'd0 || read_bit) begin
                    assembly <= {assembly[6:0], read_bit};
                end
            end
        end
    end

    // Any even switch address drives the latch onto the bus, as on the card.
    // Q6 high with Q7 low senses write protect in bit 7.
    wire [7:0] read_value = q6 ? {write_protect, 7'd0} :
                            (hold != 4'd0 ? latch : assembly);
    assign response = {16'd0, 1'b0, 1'b0, 1'b0,
                       devsel && bus_read && !addr[0], read_value};
endmodule
