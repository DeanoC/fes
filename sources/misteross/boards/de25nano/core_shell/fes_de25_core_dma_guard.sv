// SPDX-License-Identifier: GPL-3.0-or-later
// A shell DMA adapter presents one bounded, contiguous command at a time.
// Its completion must cover every data beat and the downstream response.
module fes_de25_core_dma_guard #(
    parameter logic [31:0] BASE = 32'h95000000,
    parameter logic [31:0] BYTES = 32'h00800000
) (
    input  logic        clk,
    input  logic        reset,
    input  logic        stop,
    input  logic        request_valid,
    output logic        request_ready,
    input  logic [31:0] request_address,
    input  logic [15:0] request_bytes,
    input  logic        request_write,
    output logic        target_valid,
    input  logic        target_ready,
    output logic [31:0] target_address,
    output logic [15:0] target_bytes,
    output logic        target_write,
    input  logic        target_completed,
    output logic        fault,
    output logic        drained
);
    localparam logic [32:0] LIMIT = {1'b0, BASE} + {1'b0, BYTES};
    typedef enum logic [1:0] { Idle, Pending, Outstanding } phase_t;
    phase_t phase;
    logic issued, in_range;

    assign in_range = LIMIT <= 33'h100000000 && request_bytes != 0 && request_address >= BASE &&
                      ({1'b0, request_address} + {17'b0, request_bytes}) <= LIMIT;
    assign request_ready = phase == Idle && !stop && !fault && !reset;
    assign target_valid = phase == Pending && !reset;
    assign issued = target_valid && target_ready;
    assign drained = phase == Idle && (stop || fault) && !reset;

    always_ff @(posedge clk) begin
        if (reset) begin
            phase <= Idle;
            fault <= 0;
            target_address <= 0;
            target_bytes <= 0;
            target_write <= 0;
        end else begin
            case (phase)
                Idle: if (request_valid && request_ready) begin
                    if (!in_range) begin
                        fault <= 1;
                    end else begin
                        target_address <= request_address;
                        target_bytes <= request_bytes;
                        target_write <= request_write;
                        phase <= Pending;
                    end
                end
                // Once staged, preserve the command under backpressure even
                // during Stop. Only a real response may retire issued work.
                Pending: if (issued)
                    phase <= target_completed ? Idle : Outstanding;
                Outstanding: if (target_completed)
                    phase <= Idle;
                default: phase <= Outstanding;
            endcase
        end
    end
endmodule
