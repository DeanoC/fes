// SPDX-License-Identifier: MIT
// fes_i386_governor: the speed governor (docs/i386-cpu-design.md, "Speed
// control"). Each instruction WB retires adds its reference cost, in clocks
// of the preset's machine, to a debt, and a fractional enable at the
// preset's frequency pays it down. While the debt is over the window of 64
// reference clocks, hold asks D not to start another instruction or REP
// element. It sees only what WB retires and changes no architectural state,
// timer or I/O timing; under max (preset 0) or turbo, hold stays low.
//
// The charges, from fes_i386_gov_pkg (governor.toml): as an instruction
// ends, its class's cost, or the taken cost of a branch class WB took; as a
// REP element ends, the class's per-element cost; a clock more for an
// address of base, index and displacement; and for every memory bus cycle of
// a uop WB commits, the preset's wait states, and on a 16-bit bus a dword's
// second cycle, two clocks and its wait states. A delivery (an exception or
// interrupt) ends as INT does (fes_i386_cpu).
//
// Each step is a register: WB's retirement, the ROM address and the memory
// charge, the ROM's word, the charge, the debt, hold. So the classes and the
// compare stay off D's and WB's paths, and hold follows a retirement by five
// clocks. The debt counts in 1/65536 reference clock and stops at zero: time
// the CPU spends waiting (I/O, memory, HLT) banks nothing, so a fast CPU runs
// at most a window ahead of the reference machine and a slow one never
// catches up by bursts. A REP string charges element by element, and D's
// hold applies between elements, so the debt stays within the window and
// what is in flight, and interrupts (taken between elements) wait no longer.
//
// The settings are sampled while reset is high, as the self-modifying-code
// profile is: preset (fes.pc386.machine word 0), frequency (word 1, in
// 100 kHz units; 0 the preset's own) and turbo (word 3: 1 runs at max).
// GOV_N/GOV_D is 100 kHz as a fraction of clk, which the producer sets from
// the sealed clock with the guest rates.
module fes_i386_governor #(
    parameter integer GOV_N = 1, GOV_D = 1000
) (
    input  logic               clk,
    input  logic               reset,
    input  logic [2:0]         preset,
    input  logic [9:0]         frequency,
    input  logic               turbo,
    // What WB does on this edge (fes_i386_cpu): an instruction or a REP
    // element ends (boundary, with eoi or element), WB took a branch other
    // than a far jump (taken), the instruction's fields; and the memory bus
    // cycles of the uop it commits (accesses), dword when each is four bytes
    // or more.
    input  logic               boundary,
    input  logic               eoi,
    input  logic               element,
    input  logic               taken,
    input  fes_i386_pkg::gov_t gov,
    input  logic [1:0]         accesses,
    input  logic               dword,
    output logic               hold,
    output logic [11:0]        debt_clocks,  // the debt's whole reference clocks
    output logic [8:0]         charge        // what this clock adds to the debt, in reference clocks
);
    // step: reference clocks per clk, in 1/65536: frequency * 100 kHz / clk.
    // SCALE holds 8 more fraction bits, so a step is within 1/65536 of it.
    localparam logic [63:0] SCALE = ((64'd1 << 24) * 64'(GOV_N) + 64'(GOV_D / 2)) / 64'(GOV_D);
    localparam logic [17:0] K = 18'(SCALE);
    localparam logic [27:0] WINDOW = 28'(64) << 16;

    logic [2:0]  p_id;
    logic [9:0]  p_frequency;
    logic        on;
    fes_i386_gov_pkg::preset_t p;
    assign p = fes_i386_gov_pkg::preset(p_id);
    logic [9:0]  freq;
    logic [27:0] product;
    logic [1:0]  warm;  // clocks until step follows the settings
    always_ff @(posedge clk) begin
        if (reset) begin
            p_id <= preset;
            p_frequency <= frequency;
            on <= preset != 3'd0 && !turbo;
        end
        warm <= reset ? 2'd2 : warm == 2'd0 ? 2'd0 : warm - 2'd1;
        freq <= p_frequency != 10'd0 ? p_frequency : p.frequency;
        product <= 28'(freq) * 28'(K);
    end

    // WB's retirement, as it happens.
    logic                  s1_boundary, s1_eoi, s1_element, s1_taken, s1_dword;
    fes_i386_pkg::gov_t    s1_gov;
    logic [1:0]            s1_accesses;
    always_ff @(posedge clk) begin
        s1_boundary <= on && boundary;
        s1_eoi <= eoi;
        s1_element <= element;
        s1_taken <= taken;
        s1_gov <= gov;
        s1_accesses <= on ? accesses : 2'd0;
        s1_dword <= dword;
    end

    // The class addresses the ROM; the memory charge: each bus cycle's wait
    // states, and a dword's second cycle on a 16-bit bus.
    fes_i386_gov_pkg::class_t s1_class;
    assign s1_class = fes_i386_gov_pkg::classify(s1_gov);
    logic [3:0]  per_access;
    assign per_access = {2'd0, p.wait_states} + (p.bus16 && s1_dword ? {2'd0, p.wait_states} + 4'd2 : 4'd0);
    logic [15:0] word;
    fes_i386_gov_rom costs (.clk, .addr(fes_i386_gov_pkg::cost_rom_index(p.tbl, s1_class.cls)), .q(word));
    logic        s2_ends, s2_column1, s2_element, s2_ea;
    logic [4:0]  s2_memory;
    always_ff @(posedge clk) begin
        s2_ends <= s1_boundary && s1_eoi;
        s2_column1 <= s1_class.branch && s1_taken;
        s2_element <= s1_boundary && s1_element && s1_class.elem;
        s2_ea <= s1_boundary && s1_eoi && s1_class.memory && s1_gov.ea3 && !s1_gov.rm_reg;
        s2_memory <= 5'(s1_accesses) * 5'(per_access);
    end

    always_ff @(posedge clk)
        charge <= (s2_ends ? {1'b0, s2_column1 ? word[15:8] : word[7:0]} : 9'd0)
                + (s2_element ? {1'b0, word[15:8]} : 9'd0) + {8'd0, s2_ea} + {4'd0, s2_memory};

    // The debt, paid down by step each clock and never below zero.
    logic [27:0] debt;
    logic signed [29:0] next;
    assign next = $signed({2'b00, debt}) + $signed({5'd0, charge, 16'd0}) - $signed({10'd0, product[27:8]});
    always_ff @(posedge clk) begin
        if (reset || warm != 2'd0 || !on) debt <= 28'd0;
        else debt <= next < 0 ? 28'd0 : next > 30'sh0fff_ffff ? 28'hfff_ffff : next[27:0];
        hold <= on && debt > WINDOW;
    end
    assign debt_clocks = debt[27:16];
endmodule
