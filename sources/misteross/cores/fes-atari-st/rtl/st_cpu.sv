// SPDX-License-Identifier: GPL-3.0-or-later
// Atari ST's 16-bit external bus, backed by the shared full 68000 core.
// The fractional clock enables keep all logic in the system clock domain.
module st_cpu #(
    parameter integer SYSTEM_CLOCK_HZ = 52224000,
    parameter integer CPU_CLOCK_HZ = 8000000
) (
    input  wire         clk,
    input  wire         reset,
    output wire [23:1]  addr,
    output wire [15:0]  wdata,
    input  wire [15:0]  rdata,
    output wire         as_n,
    output wire         uds_n,
    output wire         lds_n,
    output wire         rw,
    input  wire         dtack_n,
    input  wire         berr_n,
    input  wire         vpa_n,
    input  wire [2:0]   ipl_n,
    output wire [2:0]   fc,
    output wire         phi1_enable,
    output wire         phi2_enable,
    output wire         peripheral_reset_n,
    output wire         halted_n
);
    localparam integer ACCUMULATOR_BITS = $clog2(SYSTEM_CLOCK_HZ);
    localparam logic [ACCUMULATOR_BITS:0] PHASE_STEP =
        (ACCUMULATOR_BITS + 1)'(2 * CPU_CLOCK_HZ);
    localparam logic [ACCUMULATOR_BITS:0] SYSTEM_TICKS =
        (ACCUMULATOR_BITS + 1)'(SYSTEM_CLOCK_HZ);

    logic [ACCUMULATOR_BITS-1:0] phase_accumulator;
    logic next_phase;
    wire phase_due;
    wire [ACCUMULATOR_BITS-1:0] phase_next;
    generate if (PHASE_STEP > 0 && PHASE_STEP <= SYSTEM_TICKS) begin : direct_phase
        // Compare the retained accumulator before adding. The two next-state
        // additions run in parallel; the selected value has the same residue.
        localparam logic [ACCUMULATOR_BITS-1:0] PHASE_BOUND =
            ACCUMULATOR_BITS'(SYSTEM_TICKS - PHASE_STEP);
        assign phase_due = phase_accumulator >= PHASE_BOUND;
        assign phase_next = phase_due ? phase_accumulator - PHASE_BOUND :
            phase_accumulator + ACCUMULATOR_BITS'(PHASE_STEP);
    end else begin : original_phase
        wire [ACCUMULATOR_BITS:0] phase_sum =
            {1'b0, phase_accumulator} + PHASE_STEP;
        assign phase_due = phase_sum >= SYSTEM_TICKS;
        assign phase_next = phase_due ? ACCUMULATOR_BITS'(phase_sum - SYSTEM_TICKS) :
            phase_sum[ACCUMULATOR_BITS-1:0];
    end endgenerate

    assign phi1_enable = !reset && phase_due && !next_phase;
    assign phi2_enable = !reset && phase_due && next_phase;

    always_ff @(posedge clk) begin
        if (reset) begin
            phase_accumulator <= '0;
            next_phase <= 1'b0;
        end else if (phase_due) begin
            phase_accumulator <= phase_next;
            next_phase <= !next_phase;
        end else begin
            phase_accumulator <= phase_next;
        end
    end

    // These unused outputs remain named so bus arbitration, peripheral E
    // timing and the RESET instruction are explicit boundaries of this slice.
    wire unused_e, unused_vma_n, unused_bg_n;
    fx68k cpu (
        .clk(clk),
        .extReset(reset),
        .pwrUp(reset),
        .enPhi1(phi1_enable),
        .enPhi2(phi2_enable),
        .HALTn(1'b1),
        .BRn(1'b1),
        .BGACKn(1'b1),
        .VPAn(vpa_n),
        .DTACKn(dtack_n),
        .BERRn(berr_n),
        .IPL0n(ipl_n[0]),
        .IPL1n(ipl_n[1]),
        .IPL2n(ipl_n[2]),
        .iEdb(rdata),
        .oEdb(wdata),
        .eab(addr),
        .ASn(as_n),
        .UDSn(uds_n),
        .LDSn(lds_n),
        .eRWn(rw),
        .FC0(fc[0]),
        .FC1(fc[1]),
        .FC2(fc[2]),
        .E(unused_e),
        .VMAn(unused_vma_n),
        .BGn(unused_bg_n),
        .oRESETn(peripheral_reset_n),
        .oHALTEDn(halted_n)
    );

    initial begin
        if (CPU_CLOCK_HZ <= 0 || SYSTEM_CLOCK_HZ < 2 * CPU_CLOCK_HZ)
            $fatal(1, "st_cpu needs a positive CPU clock and system clock >= twice CPU clock");
    end
endmodule
