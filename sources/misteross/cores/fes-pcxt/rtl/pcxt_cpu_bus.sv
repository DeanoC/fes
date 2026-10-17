// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_pcxt_isa.vh"

// MCL86 maximum-mode pins to one outstanding byte transaction. S6_3_MUX
// separates the address and data phases; status goes passive BEFORE the BIU
// samples data. Keep READY and read data held through that passive interval.
// This is a virtual bus adapter, not a pin-compatible 8288 controller.
module pcxt_cpu_bus (
    input wire clk, reset,
    input wire [19:0] ad,
    input wire [2:0] status,
    input wire data_phase, lock_n,
    output reg cpu_ready,
    output reg [7:0] cpu_rdata,
    output wire cycle_valid,
    input wire cycle_ready,
    output reg [19:0] cycle_address,
    output reg [7:0] cycle_wdata,
    output reg [1:0] cycle_kind,
    output reg cycle_write,
    output reg cycle_lock,
    input wire cycle_done,
    input wire [7:0] cycle_rdata,
    input wire [1:0] cycle_fault,
    output reg [1:0] fault
);
    localparam [2:0] IDLE=0, DATA=1, ISSUE=2, WAIT=3, RETIRE=4, LOCK=5;
    reg [2:0] state;
    wire transfer = status != 3'b111 && status != 3'b011;
    assign cycle_valid = !reset && fault == 0 && state == ISSUE;

    always @(posedge clk) begin
        if (reset) begin
            state <= IDLE;
            cpu_ready <= 0;
            cpu_rdata <= 8'hff;
            cycle_address <= 0;
            cycle_wdata <= 0;
            cycle_kind <= 0;
            cycle_write <= 0;
            cycle_lock <= 0;
            fault <= 0;
        end else if (fault == 0) begin
            case (state)
                IDLE: if (transfer && !data_phase) begin
                    cpu_ready <= 0;
                    cycle_address <= status == 0 ? 20'b0 : ad;
                    cycle_kind <= status == 0 ? 2'(`FES_PCXT_ISA_KIND_INTA) :
                        status[2] ? 2'(`FES_PCXT_ISA_KIND_MEMORY) : 2'(`FES_PCXT_ISA_KIND_IO);
                    cycle_write <= status == 3'b010 || status == 3'b110;
                    state <= DATA;
                end
                DATA: if (data_phase) begin
                    cycle_wdata <= ad[7:0];
                    // LOCK_n is registered one fabric edge after the mux.
                    state <= LOCK;
                end
                LOCK: begin
                    cycle_lock <= !lock_n;
                    state <= ISSUE;
                end
                ISSUE: begin
                    if (cycle_ready) state <= WAIT;
                end
                WAIT: if (cycle_done) begin
                    fault <= cycle_fault;
                    cpu_rdata <= cycle_rdata;
                    cpu_ready <= cycle_fault == 0;
                    state <= RETIRE;
                end
                RETIRE: if (status == 3'b111) state <= IDLE;
                default: state <= IDLE;
            endcase
        end
    end
endmodule
