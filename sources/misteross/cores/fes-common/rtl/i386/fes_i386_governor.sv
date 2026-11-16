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
// ISA bus time (governor.toml, [isa]). An I/O access takes the cycles the
// machine's pc386_io splits it into: one 16-bit cycle if word16() says so,
// and an 8-bit cycle for each of its other bytes; it charges them less the
// two clocks of the bus cycle its instruction's count holds. An
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
    parameter integer GOV_N = 1, GOV_D = 1000,
    parameter bit REFERENCE_DIAGNOSTIC = 1'b0
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
    input  logic [3:0]         ilen,
    input  logic               redirect,
    input  logic               abandon,
    input  logic               preserve_de,
    input  logic               replay,
    input  logic [1:0]         accesses,
    input  logic               dword,
    input  logic               ordinary_ram,  // this uop has no access or is wholly RAM
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
    logic                  s1_redirect, s1_abandon, s1_preserve_de, s1_replay, s1_ordinary_ram;
    logic [3:0]            s1_ilen;
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
        s1_redirect <= redirect;
        s1_abandon <= abandon;
        s1_preserve_de <= preserve_de;
        s1_replay <= replay;
        s1_ilen <= ilen;
        s1_gov <= gov;
        s1_accesses <= on ? accesses : 2'd0;
        s1_dword <= dword;
        s1_ordinary_ram <= ordinary_ram;
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
    assign io_clocks = fes_i386_gov_pkg::word16(s1_io_port, s1_io_size)
                     ? {2'd0, isa16} + (s1_io_size == 2'd2 ? {1'b0, isa8, 1'b0} : 10'd0)
                     : {2'd0, isa8} << s1_io_size;
    assign io_extra = io_clocks > 10'd2 ? io_clocks - 10'd2 : 10'd0;
    function automatic logic [10:0] mmio_extra(input logic [2:0] halves, input logic [7:0] vga, input logic [3:0] local_bus);
        logic [10:0] clocks;
        clocks = 11'(halves) * 11'(vga);
        mmio_extra = clocks > 11'(local_bus) ? clocks - 11'(local_bus) : 11'd0;
    endfunction
    logic [3:0]  local_bus;
    assign local_bus = 4'd2 + (REFERENCE_DIAGNOSTIC && p_id == 3'd1 ? 4'd0 : per_access);
    logic [12:0] isa;
    assign isa = (s1_io ? {3'd0, io_extra} : 13'd0) + 13'(s1_mmio) * {2'd0, mmio_extra(s1_mmio_halves, isa_vga, local_bus)}
               + (s1_mmio_st2 ? {2'd0, mmio_extra(s1_mmio_st2_halves, isa_vga, local_bus)} : 13'd0);

    logic        s2_ends, s2_column1, s2_element, s2_ea;
    logic        s2_boundary, s2_redirect, s2_abandon, s2_preserve_de, s2_ordinary_ram;
    logic [3:0]  s2_ilen;
    fes_i386_pkg::gov_t s2_gov;
    logic [7:0] data_bus_acc, s2_data_bus;
    logic ordinary_ram_acc;
    logic [9:0] data_bus_sum;
    assign data_bus_sum = 10'(data_bus_acc) + (10'(s1_accesses) << (s1_dword ? 2 : 1));
    logic [4:0]  s2_memory;
    logic [12:0] s2_isa;
    always_ff @(posedge clk) begin
        s2_ends <= s1_boundary && s1_eoi;
        s2_column1 <= s1_class.branch && s1_taken;
        s2_element <= s1_boundary && s1_element && s1_class.elem;
        s2_ea <= s1_boundary && s1_eoi && s1_class.memory && s1_gov.ea3 && !s1_gov.rm_reg;
        s2_memory <= REFERENCE_DIAGNOSTIC && p_id == 3'd1 ? 5'd0 : 5'(s1_accesses) * 5'(per_access);
        s2_isa <= isa;
        s2_boundary <= s1_boundary;
        s2_redirect <= s1_redirect && s1_boundary;
        s2_abandon <= s1_abandon;
        s2_preserve_de <= s1_preserve_de;
        s2_ilen <= s1_ilen;
        s2_ordinary_ram <= ordinary_ram_acc && (s1_accesses == 0 || s1_ordinary_ram);
        s2_gov <= s1_gov;
        s2_data_bus <= data_bus_sum > 10'd255 ? 8'hff : data_bus_sum[7:0];
        if (reset || s1_abandon || s1_replay || !on || s1_boundary) begin
            data_bus_acc <= 8'd0;
            ordinary_ram_acc <= 1'b1;
        end else begin
            data_bus_acc <= data_bus_sum > 10'd255 ? 8'hff : data_bus_sum[7:0];
            if (s1_accesses != 0) ordinary_ram_acc <= ordinary_ram_acc && s1_ordinary_ram;
        end
    end

    // A default-off experiment for the 286 reference only. Its state is
    // updated on committed boundaries, never by speculative front-end work.
    // This is a diagnostic surrogate, not a physical 286 or a production
    // calibration. In particular, full REP/fault and opcode coverage remain
    // to be validated before enabling it in a package.
    logic [8:0] execution_clocks;
    logic [7:0] refill_execution;
    logic [12:0] reference_fetch;
    logic [5:0] reference_prefix;
    logic       rep_active, reference_on, reference_first, reference_chunkable, reference_chunk_restart;
    logic [7:0] reference_element;
    logic [7:0] reference_chunk_clocks;
    assign reference_on = REFERENCE_DIAGNOSTIC && on && p_id == 3'd1;
    // Reference leaves its fetch queue unchanged for zero CL-count group 2.
    // Restrict this experiment to known RAM or register operands.
    logic reference_zero_shift, reference_zero_kind;
    logic [12:0] reference_pending_sum;
    logic [11:0] reference_pending;
    // A sampled real-mode #DE that is not during delivery preserves the
    // committed queue until a real handler instruction consumes it. This
    // diagnostic does not distinguish nested handlers or qualify their timing.
    logic de_preserve_now, de_wait_synthetic, de_synthetic_boundary;
    assign de_preserve_now = reference_on && s2_abandon && s2_preserve_de;
    assign de_synthetic_boundary = reference_on && de_wait_synthetic && s2_boundary && s2_ilen == 0;
    always_ff @(posedge clk) begin
        if (reset || !reference_on) de_wait_synthetic <= 0;
        else if (s2_abandon) de_wait_synthetic <= de_preserve_now;
        else if (s2_boundary) de_wait_synthetic <= 0;
    end
    assign reference_pending_sum = {1'b0, reference_pending} + {9'd0, s2_gov.ref_prefix_count} +
                                   13'(s2_gov.op == 8'hc0 || s2_gov.op == 8'hc1);
    // The 4095-byte bound keeps a later fetch charge within the existing
    // charge representation. At the bound, use the prior diagnostic path.
    assign reference_zero_kind = reference_on && s2_boundary && s2_ends && !s2_gov.two &&
        !s2_gov.o32 && !s2_gov.ref_zero_unsafe && s2_ordinary_ram && !s2_gov.ref_shift_nonzero &&
        (s2_gov.op == 8'hc0 || s2_gov.op == 8'hc1 || s2_gov.op == 8'hd2 || s2_gov.op == 8'hd3);
    assign reference_zero_shift = reference_zero_kind && !reference_pending_sum[12];
    always_ff @(posedge clk) begin
        if (reset || !reference_on || (s2_abandon && !de_preserve_now)) reference_pending <= 0;
        else if (s2_boundary && !de_synthetic_boundary) reference_pending <= reference_zero_shift ? reference_pending_sum[11:0] : 0;
    end
    // This emulator uses a bounded execution budget for each ordinary REP
    // string chunk. CMPS is decoded per element, not by this chunk mechanism.
    assign reference_chunkable = s2_gov.rep && !s2_gov.two &&
        (s2_gov.op == 8'ha4 || s2_gov.op == 8'ha5 ||
         s2_gov.op == 8'haa || s2_gov.op == 8'hab ||
         s2_gov.op == 8'hac || s2_gov.op == 8'had ||
         s2_gov.op == 8'hae || s2_gov.op == 8'haf);
    assign reference_chunk_restart = rep_active && reference_chunkable && reference_chunk_clocks > 8'd100;
    assign reference_first = !rep_active || reference_chunk_restart || s2_gov.rep && !s2_gov.two &&
                             (s2_gov.op == 8'ha6 || s2_gov.op == 8'ha7);
    always_comb begin
        reference_element = word[15:8];
        if (reference_on && s2_gov.rep && !s2_gov.two &&
            (s2_gov.op == 8'haa || s2_gov.op == 8'hab || s2_gov.op == 8'hac || s2_gov.op == 8'had))
            reference_element = 8'd5;
    end
    always_comb begin
        execution_clocks = s2_ends ? {1'b0, s2_column1 ? word[15:8] : word[7:0]} : 9'd0;
        if (reference_on && s2_ends && !s2_gov.two) begin
            // General instruction-form costs from the 286 reference, not
            // individual game loops. Unsupported forms retain the table.
            if (s2_gov.op >= 8'h70 && s2_gov.op <= 8'h7f)
                execution_clocks = s2_column1 ? 9'd7 : 9'd3;
            if (s2_gov.op == 8'he2) execution_clocks = 9'd11;
            if (s2_gov.rep && (s2_gov.op == 8'ha4 || s2_gov.op == 8'ha5)) execution_clocks = 9'd0;
            if (s2_gov.rep && (s2_gov.op == 8'ha6 || s2_gov.op == 8'ha7 ||
                               s2_gov.op == 8'haa || s2_gov.op == 8'hab ||
                               s2_gov.op == 8'hac || s2_gov.op == 8'had ||
                               s2_gov.op == 8'hae || s2_gov.op == 8'haf)) execution_clocks = 9'd0;
            // Reference instruction-form costs. Restrict these to ordinary
            // effective 16-bit operands. Other encoding and memory contexts
            // remain diagnostic and require separate validation.
            if (!s2_gov.o32) begin
                // Experimental reference-calibrated PUSH ES/CS/DS and POP
                // ES/DS only: unprefixed real-mode operations on a 16-bit
                // RAM stack. SS, FS, GS and other forms keep the prior path.
                if (s2_gov.ref_real_mode && !s2_gov.ref_stack32 && !s2_gov.ref_test_excluded && s2_ordinary_ram) begin
                    if (s2_gov.op == 8'h06 || s2_gov.op == 8'h0e || s2_gov.op == 8'h1e) execution_clocks = 9'd2;
                    if (s2_gov.op == 8'h07 || s2_gov.op == 8'h1f) execution_clocks = 9'd7;
                end
                // Measured-reference experiment, limited to unprefixed 16-bit forms.
                if (!s2_gov.ref_test_excluded) begin
                    // Reference costs for unprefixed ordinary 16-bit forms.
                    if (s2_gov.op == 8'ha8 || s2_gov.op == 8'ha9) execution_clocks = 9'd2;
                    // Experiment: CMP r/m,reg has a shorter execution budget
                    // than the overlap budget on measured ordinary 286 RAM.
                    if (s2_ordinary_ram && !s2_gov.rm_reg &&
                        (s2_gov.op == 8'h38 || s2_gov.op == 8'h39)) execution_clocks = 9'd5;
                    if (s2_ordinary_ram && !s2_gov.rep && (s2_gov.op == 8'haa || s2_gov.op == 8'hab))
                        execution_clocks = 9'd4;
                    if (s2_ordinary_ram && !s2_gov.ref_stack32 && s2_gov.op == 8'hc8 && s2_gov.ref_enter_valid)
                        execution_clocks = s2_gov.ref_enter_level == 0 ? 9'd10 : 9'd11 + ({4'd0, s2_gov.ref_enter_level} << 2);
                    if (s2_ordinary_ram && !s2_gov.ref_stack32 && s2_gov.op == 8'hc9) execution_clocks = 9'd4;
                    // Count zero keeps the prior diagnostic path pending validation.
                    if (s2_ordinary_ram && !s2_gov.ea3 && s2_gov.ref_shift_nonzero &&
                        (s2_gov.op == 8'hc0 || s2_gov.op == 8'hc1 ||
                         (s2_gov.op >= 8'hd0 && s2_gov.op <= 8'hd3))) begin
                        if (s2_gov.mreg == 3'd2 || s2_gov.mreg == 3'd3)
                            execution_clocks = s2_gov.rm_reg ? 9'd9 : 9'd10;
                        else execution_clocks = s2_gov.rm_reg ? 9'd3 : 9'd7;
                    end
                    if (s2_ordinary_ram && s2_gov.op == 8'h60) execution_clocks = 9'd18;
                    if (s2_ordinary_ram && s2_gov.op == 8'h61) execution_clocks = 9'd24;
                    if (s2_gov.op == 8'h8d) execution_clocks = 9'd2;
                    if (s2_ordinary_ram && s2_gov.op == 8'hf6) begin
                        case (s2_gov.mreg)
                            3'd4: execution_clocks = 9'd13;
                            3'd5, 3'd6: execution_clocks = 9'd14;
                            3'd7: execution_clocks = 9'd19;
                            default: ;
                        endcase
                    end
                    if (s2_ordinary_ram && s2_gov.op == 8'hf7) begin
                        case (s2_gov.mreg)
                            3'd4: execution_clocks = 9'd21;
                            3'd5, 3'd6: execution_clocks = 9'd22;
                            3'd7: execution_clocks = 9'd27;
                            default: ;
                        endcase
                    end
                end
                if (!s2_gov.ref_test_excluded && !s2_gov.rm_reg && (s2_gov.op == 8'h84 || s2_gov.op == 8'h85 ||
                    ((s2_gov.op == 8'hf6 || s2_gov.op == 8'hf7) && s2_gov.mreg == 3'd0)))
                    execution_clocks = 9'd5;
                if (!s2_gov.rm_reg && (s2_gov.op == 8'h3a || s2_gov.op == 8'h3b)) execution_clocks = 9'd7;
                if (s2_gov.op == 8'h9c) execution_clocks = 9'd4;
                if (s2_gov.op == 8'he8 || s2_gov.op == 8'he9 || s2_gov.op == 8'heb) execution_clocks = 9'd7;
                if (s2_gov.op == 8'hc2 || s2_gov.op == 8'hc3) execution_clocks = 9'd10;
                if (s2_gov.op <= 8'h3d && (s2_gov.op[2:0] == 3'b100 || s2_gov.op[2:0] == 3'b101))
                    execution_clocks = 9'd2;
                if (s2_gov.rm_reg && (s2_gov.op == 8'h80 || s2_gov.op == 8'h81 || s2_gov.op == 8'h82 || s2_gov.op == 8'h83))
                    execution_clocks = 9'd2;
                if (s2_gov.op == 8'ha0 || s2_gov.op == 8'ha1 ||
                    (!s2_gov.rm_reg && (s2_gov.op == 8'h8a || s2_gov.op == 8'h8b)))
                    execution_clocks = 9'd4;
                if (s2_gov.op == 8'ha2 || s2_gov.op == 8'ha3 ||
                    (!s2_gov.rm_reg && (s2_gov.op == 8'h88 || s2_gov.op == 8'h89)))
                    execution_clocks = 9'd2;
                if (s2_gov.rm_reg && (s2_gov.op == 8'hd0 || s2_gov.op == 8'hd1) &&
                    (s2_gov.mreg == 3'd0 || s2_gov.mreg == 3'd1 || s2_gov.mreg >= 3'd4))
                    execution_clocks = 9'd3;
                if (s2_gov.op >= 8'h50 && s2_gov.op <= 8'h57 || s2_gov.op == 8'h68 || s2_gov.op == 8'h6a)
                    execution_clocks = 9'd2;
                if (s2_gov.op >= 8'h58 && s2_gov.op <= 8'h5f) execution_clocks = 9'd4;
            end
            if (!s2_gov.rm_reg && s2_gov.mreg == 3'd7 &&
                (s2_gov.op == 8'h80 || s2_gov.op == 8'h81 || s2_gov.op == 8'h82 || s2_gov.op == 8'h83))
                execution_clocks = 9'd7;
        end
        if (reference_zero_shift) execution_clocks = 9'd0;
        if (s2_element) execution_clocks = execution_clocks + {1'b0, reference_element};
    end
    always_comb begin
        refill_execution = execution_clocks[8] ? 8'hff : execution_clocks[7:0];
        if (reference_on && s2_ends && !s2_gov.two && !s2_gov.o32 && !s2_gov.ref_test_excluded && !s2_gov.rm_reg &&
            (s2_gov.op == 8'h84 || s2_gov.op == 8'h85)) refill_execution = 8'd7;
        if (reference_on && s2_ends && !s2_gov.two && !s2_gov.o32 && !s2_gov.ref_test_excluded &&
            s2_ordinary_ram && !s2_gov.rm_reg && (s2_gov.op == 8'h38 || s2_gov.op == 8'h39))
            refill_execution = 8'd7;
        // The reference's word ALU memory-destination forms have a different
        // overlap budget from their execution charge. Keep this diagnostic
        // adjustment by opcode/form rather than by a program or loop.
        if (reference_on && s2_ends && !s2_gov.two && !s2_gov.o32 && !s2_gov.rm_reg &&
            s2_gov.op[2:0] == 3'b001 && s2_gov.op <= 8'h31)
            refill_execution = 8'd2;
    end
    assign reference_prefix = reference_on && s2_boundary && reference_first ? s2_gov.prefix286 : 6'd0;
    always_ff @(posedge clk) begin
        if (reset || !reference_on || s2_abandon) begin
            rep_active <= 1'b0;
            reference_chunk_clocks <= 0;
        end else if (s2_boundary) begin
            if (s2_ends) begin rep_active <= 1'b0; reference_chunk_clocks <= 0; end
            else if (s2_element) begin
                rep_active <= 1'b1;
                if (reference_chunkable)
                    reference_chunk_clocks <= reference_first ? reference_element : reference_chunk_clocks + reference_element;
                else reference_chunk_clocks <= 0;
            end
        end
    end
    generate if (REFERENCE_DIAGNOSTIC) begin : gen_reference_queue
        // A 16-byte cap matches this emulator's surrogate, not the six-byte
        // instruction queue of a physical 286.
        logic [4:0] queue_bytes;
        logic [12:0] length, missing, words, present;
        logic [5:0] next_queue;
        logic [7:0] spare, spare_raw;
        logic [8:0] spare_total;
        logic spare_odd;
        // The reference's supported ENTER forms consume three modeled fetch
        // bytes; its fourth encoded byte is not charged to its queue.
        assign length = {1'b0, reference_pending} + (reference_first ?
            {9'd0, s2_ilen} - 13'(reference_on && s2_ends && !s2_gov.two && !s2_gov.o32 &&
                !s2_gov.ref_test_excluded && s2_ordinary_ram && !s2_gov.ref_stack32 && s2_gov.op == 8'hc8 &&
                s2_gov.ref_enter_valid && s2_ilen != 0) : 13'd0);
        always_comb begin
            missing = length > {8'd0, queue_bytes} ? length - {8'd0, queue_bytes} : 0;
            words = (missing + 13'd1) >> 1;
            present = {8'd0, queue_bytes} + (words << 1) - length;
            spare_raw = refill_execution > s2_data_bus ? refill_execution - s2_data_bus : 0;
            spare_total = {1'b0, spare_raw} + 9'(reference_chunkable && !reference_first && spare_odd);
            spare = spare_total[8:1];
            next_queue = spare >= 8 ? 6'd16 : 6'(present) + (6'(spare) << 1);
            if (next_queue > 16) next_queue = 16;
        end
        assign reference_fetch = reference_on && s2_boundary && !reference_zero_shift && !de_synthetic_boundary ? words << 1 : 0;
        always_ff @(posedge clk) begin
            if (reset || !reference_on || (s2_abandon && !de_preserve_now)) begin queue_bytes <= 0; spare_odd <= 0; end
            else if (s2_boundary && !reference_zero_shift && !de_synthetic_boundary) begin
                queue_bytes <= s2_redirect ? 0 : next_queue[4:0];
                spare_odd <= reference_chunkable && !s2_ends ? spare_total[0] : 0;
            end
        end
    end else begin : gen_no_reference_queue
        assign reference_fetch = 13'd0;
    end endgenerate
    always_ff @(posedge clk)
        charge <= {4'd0, execution_clocks} + reference_fetch + {7'd0, reference_prefix}
                + {12'd0, s2_ea && !reference_zero_shift && !(reference_on && !s2_gov.ref_test_excluded && !s2_gov.two && !s2_gov.o32 && s2_gov.op == 8'h8d)} + {8'd0, s2_memory} + s2_isa;

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
