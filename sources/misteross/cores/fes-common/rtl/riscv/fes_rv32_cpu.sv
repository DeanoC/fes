// SPDX-License-Identifier: MIT
// Original RV32I machine-mode CPU for FES Cyclone V cores.
//
// Multicycle, non-pipelined: FETCH issues the instruction read and, when it
// completes, captures the instruction, its decoded class and the source
// operands; EXEC performs the instruction or starts a data access; MEM waits
// for that access. One shared bus carries instruction and data requests; a
// request is held until bus_ready. Traps and the three machine interrupts
// are handled through fes_rv32_csr.
module fes_rv32_cpu #(
    parameter [31:0] RESET_VECTOR = 32'h0000_0000
) (
    input  logic        clk,
    input  logic        reset,         // synchronous, active high

    // Shared instruction/data bus. bus_wstrb is zero for reads. Address,
    // data, strobes and bus_instr stay stable until bus_ready is sampled.
    output logic        bus_valid,
    output logic        bus_instr,
    output logic [31:0] bus_addr,
    output logic [31:0] bus_wdata,
    output logic [3:0]  bus_wstrb,
    input  logic        bus_ready,
    input  logic        bus_error,     // sampled with bus_ready: access fault
    input  logic [31:0] bus_rdata,

    // Interrupt sources and the memory-mapped timer, synchronous to clk.
    input  logic        irq_external,
    input  logic        irq_timer,
    input  logic        irq_software,
    input  logic [63:0] mtime,

    // Observation only; nothing here feeds back into execution.
    output logic        step,          // an instruction finished: retired or trapped
    output logic        retired,       // ...and completed without a trap
    output logic [31:0] step_pc,       // address of that instruction
    output logic        trap,          // trap entry this cycle (with step: exception; alone: interrupt)
    output logic [31:0] trap_cause,
    output logic        wb_valid,      // integer register written this cycle (rd != x0)
    output logic [4:0]  wb_rd,
    output logic [31:0] wb_data,
    output logic [31:0] debug_pc
);
    typedef enum logic [1:0] {S_FETCH, S_EXEC, S_MEM} state_t;

    localparam [6:0] OP_LUI    = 7'b0110111;
    localparam [6:0] OP_AUIPC  = 7'b0010111;
    localparam [6:0] OP_JAL    = 7'b1101111;
    localparam [6:0] OP_JALR   = 7'b1100111;
    localparam [6:0] OP_BRANCH = 7'b1100011;
    localparam [6:0] OP_LOAD   = 7'b0000011;
    localparam [6:0] OP_STORE  = 7'b0100011;
    localparam [6:0] OP_IMM    = 7'b0010011;
    localparam [6:0] OP_OP     = 7'b0110011;
    localparam [6:0] OP_MISC   = 7'b0001111;
    localparam [6:0] OP_SYSTEM = 7'b1110011;

    localparam [31:0] CAUSE_IADDR_MISALIGNED = 32'd0;
    localparam [31:0] CAUSE_IACCESS_FAULT    = 32'd1;
    localparam [31:0] CAUSE_ILLEGAL          = 32'd2;
    localparam [31:0] CAUSE_BREAKPOINT       = 32'd3;
    localparam [31:0] CAUSE_LADDR_MISALIGNED = 32'd4;
    localparam [31:0] CAUSE_LACCESS_FAULT    = 32'd5;
    localparam [31:0] CAUSE_SADDR_MISALIGNED = 32'd6;
    localparam [31:0] CAUSE_SACCESS_FAULT    = 32'd7;
    localparam [31:0] CAUSE_ECALL_M          = 32'd11;

    state_t state;
    logic [31:0] pc, ir;
    logic fetch_issued;
    logic [31:0] mem_addr, mem_wdata;
    logic [3:0] mem_wstrb;

    // Register file: two synchronous read ports sampled when the fetch
    // completes, one write port. x0 is entry 0: initialised to zero with the
    // rest of the array and never written, so no output mux is needed. The
    // array is never read and written at one address in the same cycle for
    // a value that matters, so the read-during-write result is free.
    (* no_rw_check *) logic [31:0] regs [0:31];
    logic [31:0] rs1, rs2;

    integer init_index;
    initial begin
        for (init_index = 0; init_index < 32; init_index = init_index + 1)
            regs[init_index] = 32'd0;
    end

    // Instruction classes are decoded from the fetched word and registered
    // beside it, so EXEC starts from flip-flops rather than decode logic.
    wire [6:0] w_opcode = bus_rdata[6:0];
    wire [2:0] w_funct3 = bus_rdata[14:12];
    wire [6:0] w_funct7 = bus_rdata[31:25];
    wire w_shift_funct7_ok = w_funct7 == 7'd0 || (w_funct7 == 7'b0100000 && w_funct3 == 3'd5);
    wire d_lui = w_opcode == OP_LUI;
    wire d_auipc = w_opcode == OP_AUIPC;
    wire d_jal = w_opcode == OP_JAL;
    wire d_jalr = w_opcode == OP_JALR && w_funct3 == 3'd0;
    wire d_branch = w_opcode == OP_BRANCH && w_funct3 != 3'd2 && w_funct3 != 3'd3;
    wire d_load = w_opcode == OP_LOAD && w_funct3 != 3'd3 && w_funct3 != 3'd6 && w_funct3 != 3'd7;
    wire d_store = w_opcode == OP_STORE && w_funct3 <= 3'd2;
    wire d_op_imm = w_opcode == OP_IMM &&
        (w_funct3 == 3'd1 ? w_funct7 == 7'd0 : w_funct3 == 3'd5 ? w_shift_funct7_ok : 1'b1);
    wire d_op = w_opcode == OP_OP &&
        (w_funct7 == 7'd0 || (w_funct7 == 7'b0100000 && (w_funct3 == 3'd0 || w_funct3 == 3'd5)));
    wire d_fence = w_opcode == OP_MISC && (w_funct3 == 3'd0 || w_funct3 == 3'd1);
    wire d_csr = w_opcode == OP_SYSTEM && w_funct3 != 3'd0 && w_funct3 != 3'd4;
    wire d_ecall = bus_rdata == 32'h0000_0073;
    wire d_ebreak = bus_rdata == 32'h0010_0073;
    wire d_mret = bus_rdata == 32'h3020_0073;
    wire d_wfi = bus_rdata == 32'h1050_0073;
    wire d_legal = d_lui || d_auipc || d_jal || d_jalr || d_branch || d_load || d_store ||
        d_op_imm || d_op || d_fence || d_csr || d_ecall || d_ebreak || d_mret || d_wfi;

    logic is_lui, is_auipc, is_jal, is_jalr, is_branch, is_load, is_store, is_op_imm, is_op;
    logic is_csr, is_ecall, is_ebreak, is_mret, is_legal;
    wire is_jump = is_jal || is_jalr;

    // Fields of the instruction register.
    wire [4:0] rd = ir[11:7];
    wire [2:0] funct3 = ir[14:12];
    wire [4:0] rs1_field = ir[19:15];
    wire [31:0] imm_i = {{20{ir[31]}}, ir[31:20]};
    wire [31:0] imm_s = {{20{ir[31]}}, ir[31:25], ir[11:7]};
    wire [31:0] imm_b = {{19{ir[31]}}, ir[31], ir[7], ir[30:25], ir[11:8], 1'b0};
    wire [31:0] imm_u = {ir[31:12], 12'd0};
    wire [31:0] imm_j = {{11{ir[31]}}, ir[31], ir[19:12], ir[20], ir[30:21], 1'b0};

    // Arithmetic.
    wire alu_alt = is_op ? ir[30] : (funct3 == 3'd5 && ir[30]);
    wire [31:0] alu_b = (is_op || is_branch) ? rs2 : imm_i;
    wire [31:0] alu_result;
    wire alu_eq, alu_lt, alu_ltu;
    fes_rv32_alu alu (
        .a(rs1), .b(alu_b), .funct3(funct3), .alt(alu_alt),
        .result(alu_result), .eq(alu_eq), .lt(alu_lt), .ltu(alu_ltu)
    );
    wire [31:0] pc_plus_4 = pc + 32'd4;
    wire [31:0] pc_offset = is_jal ? imm_j : is_branch ? imm_b : imm_u;
    wire [31:0] pc_plus_offset = pc + pc_offset;
    wire [31:0] access_imm = is_store ? imm_s : imm_i;
    wire [31:0] access_addr = rs1 + access_imm;
    // Alignment needs only the two low bits; keep them off the full carry chain.
    wire [1:0] access_low = rs1[1:0] + access_imm[1:0];
    wire [31:0] jalr_target = {access_addr[31:1], 1'b0};

    logic branch_taken;
    always_comb begin
        case (funct3)
            3'd0: branch_taken = alu_eq;
            3'd1: branch_taken = !alu_eq;
            3'd4: branch_taken = alu_lt;
            3'd5: branch_taken = !alu_lt;
            3'd6: branch_taken = alu_ltu;
            default: branch_taken = !alu_ltu;
        endcase
    end

    wire access_misaligned = (funct3[1:0] == 2'd1 && access_low[0]) ||
                             (funct3[1:0] == 2'd2 && access_low != 2'd0);
    wire jump_misaligned = is_jalr ? access_low[1] : pc_plus_offset[1];

    // CSR unit.
    wire csr_write = funct3[1:0] == 2'd1 || rs1_field != 5'd0;
    wire [31:0] csr_operand = funct3[2] ? {27'd0, rs1_field} : rs1;
    wire [31:0] csr_rdata, csr_mtvec, csr_mepc, irq_cause;
    wire csr_illegal, irq_take;
    logic trap_now, mret_now;
    logic [31:0] trap_cause_now, trap_value_now;
    fes_rv32_csr csr (
        .clk(clk), .reset(reset),
        .access(state == S_EXEC && is_csr), .addr(ir[31:20]), .op(funct3[1:0]),
        .write(csr_write), .operand(csr_operand), .rdata(csr_rdata), .illegal(csr_illegal),
        .trap(trap_now), .trap_cause(trap_cause_now), .trap_pc(pc),
        .trap_value(trap_value_now), .mret(mret_now), .mtvec(csr_mtvec), .mepc(csr_mepc),
        .irq_external(irq_external), .irq_timer(irq_timer), .irq_software(irq_software),
        .irq_take(irq_take), .irq_cause(irq_cause), .mtime(mtime), .retired(retired)
    );
    // Vectored mode keeps BASE 64-byte aligned (WARL in fes_rv32_csr), so the
    // interrupt vector is a bit field, not an addition.
    wire [31:0] trap_vector = (csr_mtvec[0] && trap_cause_now[31]) ?
        {csr_mtvec[31:6], trap_cause_now[3:0], 2'b00} : {csr_mtvec[31:2], 2'b00};

    // Load data alignment.
    logic [31:0] load_data;
    wire [15:0] load_half = mem_addr[1] ? bus_rdata[31:16] : bus_rdata[15:0];
    wire [7:0] load_byte = mem_addr[0] ? load_half[15:8] : load_half[7:0];
    always_comb begin
        case (funct3)
            3'd0: load_data = {{24{load_byte[7]}}, load_byte};
            3'd1: load_data = {{16{load_half[15]}}, load_half};
            3'd4: load_data = {24'd0, load_byte};
            3'd5: load_data = {16'd0, load_half};
            default: load_data = bus_rdata;
        endcase
    end

    // Store data alignment, registered at the end of EXEC.
    logic [31:0] store_data;
    logic [3:0] store_strb;
    always_comb begin
        case (funct3[1:0])
            2'd0: begin
                store_data = {4{rs2[7:0]}};
                store_strb = 4'b0001 << access_low;
            end
            2'd1: begin
                store_data = {2{rs2[15:0]}};
                store_strb = access_low[1] ? 4'b1100 : 4'b0011;
            end
            default: begin
                store_data = rs2;
                store_strb = 4'b1111;
            end
        endcase
    end

    // Bus request.
    wire fetch_valid = state == S_FETCH && (fetch_issued || !irq_take);
    assign bus_valid = fetch_valid || state == S_MEM;
    assign bus_instr = state == S_FETCH;
    assign bus_addr = state == S_MEM ? mem_addr : pc;
    assign bus_wdata = mem_wdata;
    assign bus_wstrb = state == S_MEM ? mem_wstrb : 4'd0;
    wire fetch_done = fetch_valid && bus_ready;
    wire mem_done = state == S_MEM && bus_ready;
    assign debug_pc = pc;

    // Next-state and write-back decisions. flow_pc is the architectural
    // successor without a trap; a trap redirects to the vector instead.
    logic [31:0] flow_pc;
    wire [31:0] next_pc = trap_now ? trap_vector : flow_pc;
    logic wb_now;
    logic [31:0] wb_value;
    logic start_mem, finish;
    always_comb begin
        flow_pc = pc_plus_4;
        wb_now = 1'b0;
        wb_value = alu_result;
        trap_now = 1'b0;
        trap_cause_now = CAUSE_ILLEGAL;
        trap_value_now = 32'd0;
        mret_now = 1'b0;
        start_mem = 1'b0;
        finish = 1'b0;
        case (state)
            S_FETCH: begin
                if (!fetch_issued && irq_take) begin
                    trap_now = 1'b1;
                    trap_cause_now = irq_cause;
                end else if (fetch_done && bus_error) begin
                    trap_now = 1'b1;
                    trap_cause_now = CAUSE_IACCESS_FAULT;
                    trap_value_now = pc;
                    finish = 1'b1;
                end
            end
            S_EXEC: begin
                finish = 1'b1;
                if (!is_legal) begin
                    trap_now = 1'b1;
                    trap_value_now = ir;
                end else if (is_lui) begin
                    wb_now = 1'b1;
                    wb_value = imm_u;
                end else if (is_auipc) begin
                    wb_now = 1'b1;
                    wb_value = pc_plus_offset;
                end else if (is_jump || is_branch) begin
                    if (is_jump || branch_taken) begin
                        flow_pc = is_jalr ? jalr_target : pc_plus_offset;
                        if (jump_misaligned) begin
                            trap_now = 1'b1;
                            trap_cause_now = CAUSE_IADDR_MISALIGNED;
                            trap_value_now = flow_pc;
                        end else begin
                            wb_now = is_jump;
                            wb_value = pc_plus_4;
                        end
                    end
                end else if (is_load || is_store) begin
                    finish = 1'b0;
                    if (access_misaligned) begin
                        finish = 1'b1;
                        trap_now = 1'b1;
                        trap_cause_now = is_load ? CAUSE_LADDR_MISALIGNED : CAUSE_SADDR_MISALIGNED;
                        trap_value_now = access_addr;
                    end else begin
                        start_mem = 1'b1;
                    end
                end else if (is_op || is_op_imm) begin
                    wb_now = 1'b1;
                end else if (is_csr) begin
                    if (csr_illegal) begin
                        trap_now = 1'b1;
                        trap_value_now = ir;
                    end else begin
                        wb_now = 1'b1;
                        wb_value = csr_rdata;
                    end
                end else if (is_ecall) begin
                    trap_now = 1'b1;
                    trap_cause_now = CAUSE_ECALL_M;
                end else if (is_ebreak) begin
                    trap_now = 1'b1;
                    trap_cause_now = CAUSE_BREAKPOINT;
                    trap_value_now = pc;
                end else if (is_mret) begin
                    mret_now = 1'b1;
                    flow_pc = csr_mepc;
                end
                // FENCE, FENCE.I and WFI complete as no-operations.
            end
            default: begin   // S_MEM
                if (mem_done) begin
                    finish = 1'b1;
                    if (bus_error) begin
                        trap_now = 1'b1;
                        trap_cause_now = is_load ? CAUSE_LACCESS_FAULT : CAUSE_SACCESS_FAULT;
                        trap_value_now = mem_addr;
                    end else if (is_load) begin
                        wb_now = 1'b1;
                        wb_value = load_data;
                    end
                end
            end
        endcase
    end

    assign step = finish && !reset;
    assign retired = finish && !trap_now && !reset;
    assign step_pc = pc;
    assign trap = trap_now && !reset;
    assign trap_cause = trap_cause_now;
    assign wb_valid = wb_now && !trap_now && rd != 5'd0 && !reset;
    assign wb_rd = rd;
    assign wb_data = wb_value;

    always_ff @(posedge clk) begin
        if (wb_valid)
            regs[rd] <= wb_value;
        rs1 <= regs[bus_rdata[19:15]];
        rs2 <= regs[bus_rdata[24:20]];
        if (reset) begin
            state <= S_FETCH;
            pc <= RESET_VECTOR;
            ir <= 32'h0000_0013;   // NOP
            {is_lui, is_auipc, is_jal, is_jalr, is_branch, is_load, is_store, is_op} <= 8'd0;
            {is_csr, is_ecall, is_ebreak, is_mret} <= 4'd0;
            is_op_imm <= 1'b1;
            is_legal <= 1'b1;
            fetch_issued <= 1'b0;
            mem_addr <= 32'd0;
            mem_wdata <= 32'd0;
            mem_wstrb <= 4'd0;
        end else begin
            case (state)
                S_FETCH: begin
                    if (trap_now) begin
                        pc <= next_pc;
                        fetch_issued <= 1'b0;
                    end else if (fetch_done) begin
                        ir <= bus_rdata;
                        is_lui <= d_lui;
                        is_auipc <= d_auipc;
                        is_jal <= d_jal;
                        is_jalr <= d_jalr;
                        is_branch <= d_branch;
                        is_load <= d_load;
                        is_store <= d_store;
                        is_op_imm <= d_op_imm;
                        is_op <= d_op;
                        is_csr <= d_csr;
                        is_ecall <= d_ecall;
                        is_ebreak <= d_ebreak;
                        is_mret <= d_mret;
                        is_legal <= d_legal;
                        fetch_issued <= 1'b0;
                        state <= S_EXEC;
                    end else if (fetch_valid) begin
                        fetch_issued <= 1'b1;
                    end
                end
                S_EXEC: begin
                    if (start_mem) begin
                        mem_addr <= access_addr;
                        mem_wdata <= store_data;
                        mem_wstrb <= is_store ? store_strb : 4'd0;
                        state <= S_MEM;
                    end else begin
                        pc <= next_pc;
                        state <= S_FETCH;
                    end
                end
                default: begin
                    if (mem_done) begin
                        pc <= next_pc;
                        state <= S_FETCH;
                    end
                end
            endcase
        end
    end
endmodule
