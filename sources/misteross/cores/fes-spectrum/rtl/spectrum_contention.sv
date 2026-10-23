// SPDX-License-Identifier: GPL-2.0-or-later
// Which Z80 T states the ULA may stretch, from the presented bus cycle.
//
// The ULA holds the CPU clock high at the start of a T state while it owns
// the bus. A Sinclair ULA (48K, 128K, +2) checks every T state in which the
// address bus holds a contended address without MREQ: T1 of each memory or
// fetch cycle, and the internal T states that keep an address on the bus
// (fetch states after refresh, read/write extensions and internal cycles).
// I/O cycles check T1, T2, and T3/T4 when A0 is high; a ULA port (A0 low) is
// checked at T2 whatever its high byte. These are Fuse's contend_read,
// contend_read_no_mreq and contend_port_early/late points. The +2A/+3 gate
// array contends only MREQ cycles, at their T1. Pentagon never contends.
module spectrum_contention #(
    parameter [1:0] STYLE = 2'd1  // 0 none, 1 Sinclair ULA, 2 Amstrad gate array
) (
    input  wire        cycle_present,
    input  wire [2:0]  cycle_kind,
    input  wire [5:0]  cycle_t,
    input  wire [15:0] a,
    input  wire        a_contended,
    // Memory-style point: stretch if the address is contended.
    output reg         mem_point,
    // ULA port point: stretch regardless of the address.
    output reg         ula_io_point,
    output wire        request
);
    localparam [2:0] FETCH = 3'd0, MEM_RD = 3'd1, MEM_WR = 3'd2,
        IO_RD = 3'd3, IO_WR = 3'd4, INTERNAL = 3'd7;

    always @* begin
        mem_point = 1'b0;
        ula_io_point = 1'b0;
        if (cycle_present) begin
            case (cycle_kind)
                FETCH: mem_point = cycle_t == 6'd1 || (STYLE == 2'd1 && cycle_t >= 6'd5);
                MEM_RD, MEM_WR: mem_point = cycle_t == 6'd1 || (STYLE == 2'd1 && cycle_t >= 6'd4);
                IO_RD, IO_WR: if (STYLE == 2'd1) begin
                    mem_point = cycle_t == 6'd1 || cycle_t == 6'd2 || (a[0] && cycle_t >= 6'd3);
                    ula_io_point = cycle_t == 6'd2 && !a[0];
                end
                INTERNAL: mem_point = STYLE == 2'd1;
                default: ;
            endcase
        end
    end

    assign request = STYLE != 2'd0 && ((mem_point && a_contended) || ula_io_point);
endmodule
