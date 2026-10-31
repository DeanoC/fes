// SPDX-License-Identifier: MIT
// Bounded system-clock CSR proxy. A timeout releases only the CPU transaction;
// the downstream command/response lifetime survives until completion or reset.
`include "generated/fes_de25_scanout.vh"
module fes_de25_scanout_guard #(
    parameter integer TIMEOUT_CYCLES = `FES_DE25_SCANOUT_GUARD_TIMEOUT_CYCLES
) (
    input wire clk, reset_n,
    input wire [7:0] address,
    input wire read, write,
    input wire [3:0] byteenable,
    input wire [31:0] writedata,
    output reg [31:0] readdata,
    output reg readdatavalid, writeresponsevalid,
    output wire waitrequest,
    output wire [1:0] response,
    output reg [6:0] avm_address,
    output wire avm_read, avm_write,
    output reg [3:0] avm_byteenable,
    output reg [31:0] avm_writedata,
    input wire avm_waitrequest,
    input wire [31:0] avm_readdata,
    input wire avm_readdatavalid, avm_writeresponsevalid,
    input wire [1:0] avm_response
);
    localparam integer TIMER_BITS = (TIMEOUT_CYCLES < 2) ? 1 : $clog2(TIMEOUT_CYCLES);
    reg [TIMER_BITS-1:0] timer;
    reg pending, accepted, pending_write, host_waiting;
    reg [31:0] faults, epoch;
    wire poisoned = |faults;
    wire command_accepted = pending && !accepted && !avm_waitrequest;
    wire response_present = avm_readdatavalid || avm_writeresponsevalid;
    wire response_expected = pending && (accepted || command_accepted) &&
        (pending_write ? (avm_writeresponsevalid && !avm_readdatavalid) :
                         (avm_readdatavalid && !avm_writeresponsevalid));
    wire [31:0] response_faults =
        ((response_present && !response_expected) ? `FES_DE25_SCANOUT_GUARD_FAULT_UNEXPECTED_RESPONSE : 32'b0) |
        ((response_expected && avm_response != 0) ? `FES_DE25_SCANOUT_GUARD_FAULT_RESPONSE : 32'b0);
    wire timeout_expired = host_waiting && !response_expected && timer == TIMER_BITS'(TIMEOUT_CYCLES - 1);
    wire host_request = (read || write) && !waitrequest;
    wire valid_access = (read != write) && byteenable == 4'hf && address[1:0] == 0;
    wire [31:0] status = (poisoned ? `FES_DE25_SCANOUT_GUARD_STATUS_POISONED : 32'b0) |
        (pending ? `FES_DE25_SCANOUT_GUARD_STATUS_PENDING : 32'b0) |
        (accepted ? `FES_DE25_SCANOUT_GUARD_STATUS_ACCEPTED : 32'b0) |
        ((epoch != 0) ? `FES_DE25_SCANOUT_GUARD_STATUS_EPOCH_SET : 32'b0);
    reg [31:0] local_data;
    reg local_valid;
    always @* begin
        local_valid = 1'b1;
        case ({24'b0, address})
            `FES_DE25_SCANOUT_GUARD_ID_OFFSET: local_data = `FES_DE25_SCANOUT_GUARD_IDENTITY;
            `FES_DE25_SCANOUT_GUARD_VERSION_OFFSET: local_data = `FES_DE25_SCANOUT_GUARD_VERSION;
            `FES_DE25_SCANOUT_GUARD_STATUS_OFFSET: local_data = status;
            `FES_DE25_SCANOUT_GUARD_FAULT_OFFSET: local_data = faults;
            `FES_DE25_SCANOUT_GUARD_EPOCH_OFFSET: local_data = epoch;
            `FES_DE25_SCANOUT_GUARD_TIMEOUT_OFFSET: local_data = 32'(TIMEOUT_CYCLES);
            default: begin local_data = 0; local_valid = 1'b0; end
        endcase
    end
    // Never return SLVERR on the CPU bus: Linux must check local fault/epoch
    // before AND after endpoint access. Dummy zero data is not endpoint proof.
    assign response = 2'b00;
    assign waitrequest = host_waiting;
    assign avm_read = pending && !accepted && !pending_write;
    assign avm_write = pending && !accepted && pending_write;
    initial begin
        if (TIMEOUT_CYCLES < 2) $fatal(1, "guard timeout must be at least two system cycles");
    end
    always @(posedge clk or negedge reset_n) begin
        if (!reset_n) begin
            timer <= 0;
            pending <= 0; accepted <= 0; pending_write <= 0; host_waiting <= 0;
            faults <= 0; epoch <= 0;
            readdata <= 0; readdatavalid <= 0; writeresponsevalid <= 0;
            avm_address <= 0; avm_byteenable <= 0; avm_writedata <= 0;
        end else begin
            readdatavalid <= 0; writeresponsevalid <= 0;
            faults <= faults | response_faults |
                (timeout_expired ? `FES_DE25_SCANOUT_GUARD_FAULT_TIMEOUT : 32'b0);
            if (command_accepted) accepted <= 1;
            if (response_expected) begin
                pending <= 0; accepted <= 0;
            end
            if (host_waiting) begin
                if (response_expected || timeout_expired || |response_faults) begin
                    host_waiting <= 0;
                    readdata <= (!poisoned && response_faults == 0 && response_expected) ? avm_readdata : 32'b0;
                    readdatavalid <= !pending_write;
                    writeresponsevalid <= pending_write;
                end else timer <= timer + 1'b1;
            end
            if (host_request) begin
                if (!valid_access || (address[7] && (!local_valid ||
                    (write && (address != 8'(`FES_DE25_SCANOUT_GUARD_EPOCH_OFFSET) || epoch != 0 || writedata == 0))))) begin
                    faults <= faults | response_faults | `FES_DE25_SCANOUT_GUARD_FAULT_ACCESS;
                    readdata <= 0; readdatavalid <= read; writeresponsevalid <= write && !read;
                end else if (address[7]) begin
                    readdata <= local_data; readdatavalid <= read; writeresponsevalid <= write;
                    if (write && !poisoned && response_faults == 0) epoch <= writedata;
                end else if (poisoned || |response_faults) begin
                    readdata <= 0; readdatavalid <= read; writeresponsevalid <= write;
                end else begin
                    avm_address <= address[6:0]; avm_byteenable <= byteenable; avm_writedata <= writedata;
                    pending <= 1; accepted <= 0; pending_write <= write; host_waiting <= 1; timer <= 0;
                end
            end
        end
    end
endmodule
