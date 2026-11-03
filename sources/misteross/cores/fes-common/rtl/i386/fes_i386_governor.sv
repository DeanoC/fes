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
// The data sheets' counts assume a zero-wait-state local bus, so a uop's I/O
// and MMIO (the machine's VGA memory) accesses also charge the period AT's
// ISA bus time (governor.toml, [isa]). An I/O access takes 8-bit cycles, a
// byte each, or at a port16() port 16-bit ones, a word each; it charges them
// less the two clocks of the bus cycle its instruction's count holds. An
// MMIO access takes a VGA cycle per 16-bit half it touches, less what its
// table count and wait-state charge already give it. Neither goes below
// zero. A cycle's reference clocks follow the frequency the governor runs
// at, worked out at reset as the step is.
//
// Each step is a register: WB's retirement, the ROM address and the memory
// and ISA charges, the ROM's word, the charge, the debt, hold. So the classes and the
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
    // ... its I/O access (io: the port and size, 0 a byte, 1 a word, 2 a
    // dword), and which of its memory accesses are MMIO: mmio of those at its
    // operand (a read-modify-write's two), mmio_st2 its st2 store, with the
    // 16-bit halves each touches.
    input  logic               io,
    input  logic [15:0]        io_port,
    input  logic [1:0]         io_size,
    input  logic [1:0]         mmio,
    input  logic [2:0]         mmio_halves,
    input  logic               mmio_st2,
    input  logic [2:0]         mmio_st2_halves,
    output logic               hold,
    output logic [11:0]        debt_clocks,  // the debt's whole reference clocks
    output logic [12:0]        charge        // what this clock adds to the debt, in reference clocks
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

    // The ISA cycles in reference clocks at freq, rounded, halves up. They
    // settle two clocks after freq, long before WB first retires.
    localparam int SW = $bits(fes_i386_gov_pkg::ISA_STEP) + 10, PW = SW + 4;
    logic [SW-1:0] isa_clock;  // an ISA clock, in 1/2**ISA_FRAC reference clock
    logic [7:0]    isa8, isa16, isa_vga;
    function automatic logic [7:0] cycle(input logic [3:0] clocks, input logic [SW-1:0] one);
        cycle = 8'((PW'(clocks) * PW'(one) + (PW'(1) << (fes_i386_gov_pkg::ISA_FRAC - 1))) >> fes_i386_gov_pkg::ISA_FRAC);
    endfunction
    always_ff @(posedge clk) begin
        isa_clock <= SW'(freq) * SW'(fes_i386_gov_pkg::ISA_STEP);
        isa8 <= cycle(fes_i386_gov_pkg::ISA_IO8, isa_clock);
        isa16 <= cycle(fes_i386_gov_pkg::ISA_IO16, isa_clock);
        isa_vga <= cycle(fes_i386_gov_pkg::ISA_VGA, isa_clock);
    end

    // WB's retirement, as it happens.
    logic                  s1_boundary, s1_eoi, s1_element, s1_taken, s1_dword;
    fes_i386_pkg::gov_t    s1_gov;
    logic [1:0]            s1_accesses;
    logic                  s1_io, s1_mmio_st2;
    logic [15:0]           s1_io_port;
    logic [1:0]            s1_io_size, s1_mmio;
    logic [2:0]            s1_mmio_halves, s1_mmio_st2_halves;
    always_ff @(posedge clk) begin
        s1_boundary <= on && boundary;
        s1_eoi <= eoi;
        s1_element <= element;
        s1_taken <= taken;
        s1_gov <= gov;
        s1_accesses <= on ? accesses : 2'd0;
        s1_dword <= dword;
        s1_io <= on && io;
        s1_io_port <= io_port;
        s1_io_size <= io_size;
        s1_mmio <= on ? mmio : 2'd0;
        s1_mmio_halves <= mmio_halves;
        s1_mmio_st2 <= on && mmio_st2;
        s1_mmio_st2_halves <= mmio_st2_halves;
    end

    // The class addresses the ROM; the memory charge: each bus cycle's wait
    // states, and a dword's second cycle on a 16-bit bus.
    fes_i386_gov_pkg::class_t s1_class;
    assign s1_class = fes_i386_gov_pkg::classify(s1_gov);
    logic [3:0]  per_access;
    assign per_access = {2'd0, p.wait_states} + (p.bus16 && s1_dword ? {2'd0, p.wait_states} + 4'd2 : 4'd0);
    logic [15:0] word;
    fes_i386_gov_rom costs (.clk, .addr(fes_i386_gov_pkg::cost_rom_index(p.tbl, s1_class.cls)), .q(word));
    // The ISA charges: the I/O access's cycles less the data sheet's bus
    // cycle; an MMIO access's VGA cycles less its bus cycle and per_access.
    logic [9:0]  io_clocks, io_extra;
    assign io_clocks = fes_i386_gov_pkg::port16(s1_io_port) ? (s1_io_size == 2'd2 ? {1'b0, isa16, 1'b0} : {2'd0, isa16})
                     : {2'd0, isa8} << s1_io_size;
    assign io_extra = io_clocks > 10'd2 ? io_clocks - 10'd2 : 10'd0;
    function automatic logic [10:0] mmio_extra(input logic [2:0] halves, input logic [7:0] vga, input logic [3:0] local_bus);
        logic [10:0] clocks;
        clocks = 11'(halves) * 11'(vga);
        mmio_extra = clocks > 11'(local_bus) ? clocks - 11'(local_bus) : 11'd0;
    endfunction
    logic [3:0]  local_bus;
    assign local_bus = 4'd2 + per_access;
    logic [12:0] isa;
    assign isa = (s1_io ? {3'd0, io_extra} : 13'd0) + 13'(s1_mmio) * {2'd0, mmio_extra(s1_mmio_halves, isa_vga, local_bus)}
               + (s1_mmio_st2 ? {2'd0, mmio_extra(s1_mmio_st2_halves, isa_vga, local_bus)} : 13'd0);

    logic        s2_ends, s2_column1, s2_element, s2_ea;
    logic [4:0]  s2_memory;
    logic [12:0] s2_isa;
    always_ff @(posedge clk) begin
        s2_ends <= s1_boundary && s1_eoi;
        s2_column1 <= s1_class.branch && s1_taken;
        s2_element <= s1_boundary && s1_element && s1_class.elem;
        s2_ea <= s1_boundary && s1_eoi && s1_class.memory && s1_gov.ea3 && !s1_gov.rm_reg;
        s2_memory <= 5'(s1_accesses) * 5'(per_access);
        s2_isa <= isa;
    end

    always_ff @(posedge clk)
        charge <= (s2_ends ? {5'd0, s2_column1 ? word[15:8] : word[7:0]} : 13'd0)
                + (s2_element ? {5'd0, word[15:8]} : 13'd0) + {12'd0, s2_ea} + {8'd0, s2_memory} + s2_isa;

    // The debt, paid down by step each clock and never below zero.
    logic [27:0] debt;
    logic signed [31:0] next;
    assign next = $signed({4'd0, debt}) + $signed({3'd0, charge, 16'd0}) - $signed({12'd0, product[27:8]});
    always_ff @(posedge clk) begin
        if (reset || warm != 2'd0 || !on) debt <= 28'd0;
        else debt <= next < 0 ? 28'd0 : next > 32'sh0fff_ffff ? 28'hfff_ffff : next[27:0];
        hold <= on && debt > WINDOW;
    end
    assign debt_clocks = debt[27:16];
endmodule
