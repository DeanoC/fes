// SPDX-License-Identifier: MIT
// Machine-mode control and status registers for fes_rv32_cpu.
//
// Implements the Zicsr access rules, the trap/return state (mstatus.MIE/MPIE,
// mtvec, mepc, mcause, mtval, mscratch), interrupt pending/enable bits for
// the machine external, timer and software interrupts, the Zicntr counters
// (mcycle, minstret and their read-only cycle/time/instret shadows) and the
// read-only identity registers. Only machine mode exists; MPP reads as 11.
module fes_rv32_csr (
    input  logic        clk,
    input  logic        reset,

    // One CSR instruction per asserted cycle. `write` is the instruction's
    // intent (CSRRW always; CSRRS/CSRRC only when rs1/uimm is nonzero).
    input  logic        access,
    input  logic [11:0] addr,
    input  logic [1:0]  op,          // 1 read/write, 2 set bits, 3 clear bits
    input  logic        write,
    input  logic [31:0] operand,
    output logic [31:0] rdata,
    output logic        illegal,     // unknown CSR or write to a read-only CSR

    // Trap entry and return. Never asserted together with a legal access.
    input  logic        trap,
    input  logic [31:0] trap_cause,
    input  logic [31:0] trap_pc,
    input  logic [31:0] trap_value,
    input  logic        mret,
    output logic [31:0] mtvec,
    output logic [31:0] mepc,

    // Interrupt sources, synchronous to clk; registered once here.
    input  logic        irq_external,
    input  logic        irq_timer,
    input  logic        irq_software,
    output logic        irq_take,    // an enabled interrupt is pending
    output logic [31:0] irq_cause,   // highest-priority pending interrupt

    input  logic [63:0] mtime,       // memory-mapped timer shadowed by `time`
    input  logic        retired      // one instruction retired this cycle
);
    localparam [31:0] MISA = 32'h4000_0100;   // RV32I

    logic mstatus_mie, mstatus_mpie;
    logic mie_meie, mie_mtie, mie_msie;
    logic mip_meip, mip_mtip, mip_msip;
    logic [31:0] mscratch, mcause, mtval;
    logic [63:0] mcycle, minstret;

    wire [31:0] mstatus = {19'd0, 2'b11, 3'd0, mstatus_mpie, 3'd0, mstatus_mie, 3'd0};
    wire [31:0] mie = {20'd0, mie_meie, 3'd0, mie_mtie, 3'd0, mie_msie, 3'd0};
    wire [31:0] mip = {20'd0, mip_meip, 3'd0, mip_mtip, 3'd0, mip_msip, 3'd0};

    logic known, read_only;
    always_comb begin
        known = 1'b1;
        case (addr)
            12'h300: rdata = mstatus;
            12'h301: rdata = MISA;
            12'h304: rdata = mie;
            12'h305: rdata = mtvec;
            12'h340: rdata = mscratch;
            12'h341: rdata = mepc;
            12'h342: rdata = mcause;
            12'h343: rdata = mtval;
            12'h344: rdata = mip;
            12'hb00: rdata = mcycle[31:0];
            12'hb02: rdata = minstret[31:0];
            12'hb80: rdata = mcycle[63:32];
            12'hb82: rdata = minstret[63:32];
            12'hc00: rdata = mcycle[31:0];
            12'hc01: rdata = mtime[31:0];
            12'hc02: rdata = minstret[31:0];
            12'hc80: rdata = mcycle[63:32];
            12'hc81: rdata = mtime[63:32];
            12'hc82: rdata = minstret[63:32];
            12'hf11, 12'hf12, 12'hf13, 12'hf14: rdata = 32'd0;
            default: begin
                rdata = 32'd0;
                known = 1'b0;
            end
        endcase
        read_only = addr[11:10] == 2'b11;
        illegal = !known || (write && read_only);
    end

    wire do_write = access && write && !illegal;
    logic [31:0] wdata;
    always_comb begin
        case (op)
            2'd2: wdata = rdata | operand;
            2'd3: wdata = rdata & ~operand;
            default: wdata = operand;
        endcase
    end

    // Highest priority first: external, software, timer (RISC-V privileged
    // specification, machine-level interrupt ordering).
    wire pending_meip = mip_meip && mie_meie;
    wire pending_mtip = mip_mtip && mie_mtie;
    wire pending_msip = mip_msip && mie_msie;
    assign irq_take = mstatus_mie && (pending_meip || pending_mtip || pending_msip);
    assign irq_cause = pending_meip ? 32'h8000_000b :
                       pending_msip ? 32'h8000_0003 : 32'h8000_0007;

    always_ff @(posedge clk) begin
        mip_meip <= irq_external;
        mip_mtip <= irq_timer;
        mip_msip <= irq_software;
        if (reset) begin
            mstatus_mie <= 1'b0;
            mstatus_mpie <= 1'b0;
            mie_meie <= 1'b0;
            mie_mtie <= 1'b0;
            mie_msie <= 1'b0;
            mtvec <= 32'd0;
            mscratch <= 32'd0;
            mepc <= 32'd0;
            mcause <= 32'd0;
            mtval <= 32'd0;
            mcycle <= 64'd0;
            minstret <= 64'd0;
        end else begin
            mcycle <= mcycle + 64'd1;
            if (retired)
                minstret <= minstret + 64'd1;
            if (trap) begin
                mepc <= {trap_pc[31:2], 2'b00};
                mcause <= trap_cause;
                mtval <= trap_value;
                mstatus_mpie <= mstatus_mie;
                mstatus_mie <= 1'b0;
            end else if (mret) begin
                mstatus_mie <= mstatus_mpie;
                mstatus_mpie <= 1'b1;
            end else if (do_write) begin
                case (addr)
                    12'h300: begin
                        mstatus_mie <= wdata[3];
                        mstatus_mpie <= wdata[7];
                    end
                    12'h304: begin
                        mie_msie <= wdata[3];
                        mie_mtie <= wdata[7];
                        mie_meie <= wdata[11];
                    end
                    // WARL: direct (0) or vectored (1) modes only. A vectored
                    // BASE is 64-byte aligned so vectors need no adder.
                    12'h305: mtvec <= wdata[0] ? {wdata[31:6], 5'd0, 1'b1} : {wdata[31:2], 2'b00};
                    12'h340: mscratch <= wdata;
                    12'h341: mepc <= {wdata[31:2], 2'b00};
                    12'h342: mcause <= wdata;
                    12'h343: mtval <= wdata;
                    12'hb00: mcycle[31:0] <= wdata;
                    12'hb02: minstret[31:0] <= wdata;
                    12'hb80: mcycle[63:32] <= wdata;
                    12'hb82: minstret[63:32] <= wdata;
                    default: ;    // misa, mip: writes are legal and ignored
                endcase
            end
        end
    end
endmodule
