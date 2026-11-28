module fes_sh4 #(
    parameter logic [31:0] RESET_PC = 32'ha000_0000,
    parameter logic [8:0] RESET_CCR = 9'b0
) (
    input  logic        clk,
    input  logic        reset,
    input  logic [3:0]  irq_level,
    input  logic [11:0] irq_event,
    input  logic        nmi,
    input  logic        nmi_while_blocked,
    output logic        irq_ack,
    output logic        mem_valid,
    output logic        mem_lock,
    output logic [3:0]  mem_rmask,
    input  logic        mem_ready,
    output logic [31:0] mem_addr,
    output logic [3:0]  mem_wstrb,
    output logic [31:0] mem_wdata,
    input  logic        mem_rvalid,
    input  logic [31:0] mem_rdata,
    output logic        retired,
    output logic [31:0] retired_pc,
    output logic [3:0]  fault,
    output logic [31:0] fault_pc
);
    typedef enum logic [4:0] {
        ISSUE, DECODE, EXECUTE, DATA_TRANSLATE, DATA_TLB_WAIT, DATA_REQ, DATA_WAIT,
        COMMIT, EXCEPTION, INTEGER_RESULT, MUL_PRODUCTS, MUL_SUM, MUL_RESULT,
        MAC_READ, MAC_LOW, MAC_HIGH, BYTE_MODIFY, ASLEEP, STOP
    } stage_t;
    typedef enum logic [1:0] {BUS_IDLE, BUS_SEND, BUS_REPLY} bus_state_t;
    typedef enum logic [5:0] {
        BAD, NOP, MOV, ADD, SUB, ADDC, SUBC, AND_OP, OR_OP, XOR_OP, NOT_OP,
        EQ, HS, GE, HI, GT, TST, SET_T, MOV_T, SHLL, SHLR, SHAR,
        JMP, CALL, BR, BSR, RETURN_OP, BT, BF, BTS, BFS, LOAD, STORE, MOVA, LDTLB, LDC,
        RTE, TRAPA, INTEGER, MULTIPLY, CLRMAC, LDS, STS, BRAF, BSRF, MAC, SLEEP, MEMORY_BYTE, TAS, OCBI, OCBWB, OCBP, MOVCA
    } op_t;
    typedef enum logic [1:0] {BYTE, WORD, LONGWORD} size_t;
    typedef enum logic [2:0] {CR_SR, CR_GBR, CR_VBR, CR_SSR, CR_SPC, CR_SGR, CR_DBR, CR_BANK} control_reg_t;
    localparam logic [3:0] ILLEGAL = 4'd1, SLOT_ILLEGAL = 4'd2,
                           FETCH_ALIGN = 4'd3, DATA_ALIGN = 4'd4, TLB_MISS = 4'd5, TLB_MULTIPLE = 4'd6,
                           TLB_PROTECT = 4'd7, TLB_INITIAL_WRITE = 4'd8,
                           ADDRESS_ERROR = 4'd9, TRAP = 4'd10;
    localparam logic [31:0] SR_MASK = 32'h7000_83f3;

    stage_t stage;
    logic [31:0] gpr [0:15];
    logic [31:0] gpr_storage [0:23];
    logic [31:0] pc, pr, delay_target, delay_branch_pc, pre_slot_pr;
    logic [31:0] sr, ssr, spc, sgr, vbr, vector_pc;
    logic [31:0] gbr, dbr, mach, macl, sys_value;
    control_reg_t dec_control_reg, control_reg;
    logic dec_control_transfer, control_transfer, control_write, writes_sr, load_gpr;
    logic [31:0] control_read_value, control_write_value;
    logic [63:0] product, mac_result;
    logic mac_second, mac_word;
    logic [31:0] mac_second_addr;
    logic [32:0] mac_low_sum, mac_high_sum, mac_addend_hi, mac_product_hi;
    logic [1:0] mac_word_signs;
    logic mac_word_overflow, mac_long_overflow;
    logic dec_sys_load, sys_load, dest_r0;
    logic alu_recognized, alu_r0, alu_write;
    logic [31:0] alu_result;
    logic [3:0] alu_flag_mask, alu_flag_value;
    logic [11:0] expevt, intevt, exception_event;
    logic nmi_previous, nmi_pending, nmi_edge, nmi_ready, irq_ready, take_interrupt;
    logic [7:0] tra, trap_imm;
    logic [3:0] exception_cause;
    logic take_exception, address_exception, privileged_op, is_store;
    logic [31:0] fault_address;
    logic t, in_slot, supervisor, fetch_supervisor, bank_one, rte_slot, slot_wrote_pr;
    logic [15:0] ir;

    logic [3:0] n, m, dst, base_reg;
    logic [4:0] dst_index, base_index;
    op_t decoded, op;
    size_t dec_size, size;
    logic dec_pre, dec_post, dec_pc_relative, pre, post, pc_relative;
    logic [31:0] dec_a, dec_b, dec_offset, a, b, offset, store_value;

    logic [31:0] value, target, effective_addr;
    logic [32:0] arithmetic;
    logic write_gpr, write_t, next_t, write_pr, control, delayed, taken, aligned;
    logic [31:0] wb_value, next_pc, link_value, writeback_base;
    logic wb_gpr, wb_t, wb_t_value, wb_pr, wb_base, start_slot;
    logic [31:2] data_addr;
    logic [31:0] data_wdata;
    logic [3:0] data_wstrb, operand_read_mask;
    logic byte_access, byte_writes, ordinary_store, cache_maintenance;
    logic [1:0] cache_command;
    logic [7:0] byte_result;
    logic [31:0] load_word, load_value, local_read_value;
    logic [1:0] data_addr_lane;

    bus_state_t bus_state;
    logic bus_fetch;
    logic [31:0] fetch_pc, fetch_read_addr;
    logic [15:0] fetch_instruction;
    logic fetch_valid, fetch_consume, fetch_read, fetch_read_ready, fetch_response;
    logic fetch_lookup, fetch_cacheable, fetch_flush, redirect, store_accepted;
    logic data_ready, data_complete, data_bus_request, data_bus_ready, data_bus_response;
    logic [31:0] data_read_word, data_bus_addr, data_bus_wdata;
    logic [3:0] data_bus_wstrb;

    logic mmu_ready, mmu_response, mmu_enabled, mmu_cacheable, mmu_request;
    logic [31:0] mmu_va, mmu_pa, mmu_reg_rdata;
    logic [3:0] mmu_fault, mmu_register_index;
    logic mmu_register, event_register, mmu_reg_write, mmu_ldtlb, privilege_write;
    logic ccr_register, ccr_write, mmu_write_through, data_copyback, data_bus_writeback;
    logic ic_enable, oc_enable, p1_copyback, direct_write_through;
    logic fetch_mapped, data_mapped, fetch_tlb_pending, fetch_tlb_valid;
    logic bad_fetch_region, bad_data_region, fetch_page_cacheable, data_cacheable;
    logic [31:0] physical_fetch_pc;

    // R8–R15 are shared. R0–R7 select the second bank only in privileged mode with RB set.
    function automatic logic [4:0] gpr_index(input logic [3:0] number, input logic bank);
        return bank && !number[3] ? {2'b10, number[2:0]} : {1'b0, number};
    endfunction

    assign supervisor = sr[30];
    assign bank_one = supervisor && sr[29];
    assign dst_index = gpr_index(dst, bank_one ^ (control_write && control_reg == CR_BANK));
    assign base_index = gpr_index(base_reg, bank_one);
    assign t = sr[0];
    // RTE restores SR for executing its slot, but the fetch uses the old MD.
    assign fetch_supervisor = supervisor || rte_slot;
    for (genvar r = 0; r < 16; r++) begin : visible_regs
        assign gpr[r] = gpr_storage[gpr_index(4'(r), bank_one)];
    end

    fes_sh4_integer integer_extra (
        .clk, .capture(stage == DECODE), .ir, .recognized(alu_recognized), .use_r0(alu_r0),
        .lhs(a), .rhs(b), .flags({sr[9:8], sr[0]}),
        .result(alu_result), .write_gpr(alu_write), .flag_mask(alu_flag_mask), .flag_value(alu_flag_value)
    );

    fes_sh4_multiplier multiplier (
        .clk, .launch(stage == MUL_PRODUCTS), .sum_products(stage == MUL_SUM), .finish(stage == MUL_RESULT),
        .word_operands(ir[15:12] == 4'h2 || (op == MAC && mac_word)),
        .signed_operands(ir[3:0] == 4'hd || ir[3:0] == 4'hf),
        .lhs(a), .rhs(b), .product
    );

    assign mac_word = size == WORD;
    assign mac_second_addr = n == m ? writeback_base : b;
    assign mac_addend_hi = sr[1] && !mac_word ? {{17{mach[15]}}, mach[15:0]} : {mach[31], mach};
    assign mac_product_hi = mac_word ? {33{product[31]}} : {product[63], product[63:32]};
    assign mac_word_overflow = mac_word_signs[0] == mac_word_signs[1] &&
                               mac_low_sum[31] != mac_word_signs[0];
    assign mac_long_overflow = mac_high_sum[32:15] != {18{mac_high_sum[32]}};

    always_comb begin
        mac_result = {mac_high_sum[31:0], mac_low_sum[31:0]};
        if (sr[1]) begin
            if (mac_word) begin
                mac_result[63:32] = mach;
                if (mac_word_overflow) begin
                    mac_result[32] = 1'b1;
                    mac_result[31:0] = mac_word_signs[0] ? 32'h8000_0000 : 32'h7fff_ffff;
                end
            end else begin
                mac_result[63:48] = mach[31:16];
                if (mac_long_overflow)
                    mac_result[47:0] = mac_high_sum[32] ? 48'h8000_0000_0000 : 48'h7fff_ffff_ffff;
            end
        end
    end

    assign n = ir[11:8];
    assign m = ir[7:4];

    always_comb begin
        case (ir[5:4])
            0: sys_value = mach;
            1: sys_value = macl;
            default: sys_value = pr;
        endcase
    end

    always_comb begin
        case (ir[7:4])
            0: dec_control_reg = CR_SR;
            1: dec_control_reg = CR_GBR;
            2: dec_control_reg = CR_VBR;
            3: dec_control_reg = (ir[15:12] == 4'h0 && ir[3:0] == 4'ha) ||
                                 (ir[15:12] == 4'h4 && ir[3:0] == 4'h2) ? CR_SGR : CR_SSR;
            4: dec_control_reg = CR_SPC;
            15: dec_control_reg = ir[3:0] inside {4'ha, 4'h6} ||
                                  (ir[15:12] == 4'h4 && ir[3:0] == 4'h2) ? CR_DBR : CR_BANK;
            default: dec_control_reg = CR_BANK;
        endcase
        case (dec_control_reg)
            CR_SR:   control_read_value = sr;
            CR_GBR:  control_read_value = gbr;
            CR_VBR:  control_read_value = vbr;
            CR_SSR:  control_read_value = ssr;
            CR_SPC:  control_read_value = spc;
            CR_SGR:  control_read_value = sgr;
            CR_DBR:  control_read_value = dbr;
            CR_BANK: control_read_value = gpr_storage[gpr_index({1'b0, ir[6:4]}, !bank_one)];
        endcase
    end

    always_comb begin
        decoded = alu_recognized ? INTEGER : BAD;
        dest_r0 = alu_r0;
        dec_sys_load = 1'b0;
        dec_control_transfer = 1'b0;
        dec_size = BYTE;
        dec_pre = 1'b0;
        dec_post = 1'b0;
        dec_pc_relative = 1'b0;
        dec_a = gpr[n];
        dec_b = gpr[m];
        dec_offset = 32'b0;

        casez (ir)
            16'h0009: decoded = NOP;
            16'h001b: decoded = SLEEP;
            16'h0008, 16'h0018: begin decoded = SET_T; dec_b = {31'b0, ir[4]}; end
            16'h000b: decoded = RETURN_OP;
            16'h0038: decoded = LDTLB;
            16'h4?0e, 16'h4?1e, 16'h4?2e, 16'h4?3e, 16'h4?4e, 16'h4?fa,
            16'b0100_????_1???_1110: begin
                decoded = LDC;
                dec_control_transfer = 1'b1;
            end
            16'h0?02, 16'h0?12, 16'h0?22, 16'h0?32, 16'h0?42, 16'h0?3a, 16'h0?fa,
            16'b0000_????_1???_0010: begin
                decoded = MOV;
                dec_control_transfer = 1'b1;
                dec_b = control_read_value;
            end
            16'h4?07, 16'h4?17, 16'h4?27, 16'h4?37, 16'h4?47, 16'h4?f6,
            16'b0100_????_1???_0111: begin
                decoded = LOAD;
                dec_control_transfer = 1'b1;
                dec_size = LONGWORD;
                dec_post = 1'b1;
            end
            16'h4?03, 16'h4?13, 16'h4?23, 16'h4?33, 16'h4?43, 16'h4?32, 16'h4?f2,
            16'b0100_????_1???_0011: begin
                decoded = STORE;
                dec_control_transfer = 1'b1;
                dec_size = LONGWORD;
                dec_pre = 1'b1;
                dec_b = control_read_value;
            end
            16'h002b: decoded = RTE;
            16'hc3??: decoded = TRAPA;
            16'h0?29: decoded = MOV_T;
            16'h0028: decoded = CLRMAC;
            16'h0??f, 16'h4??f: begin
                decoded = MAC;
                dec_size = ir[14] ? WORD : LONGWORD;
            end
            16'h3??5, 16'h3??d, 16'h0??7, 16'h2??e, 16'h2??f: decoded = MULTIPLY;
            16'h4?0a, 16'h4?1a, 16'h4?2a: decoded = LDS;
            16'h0?0a, 16'h0?1a, 16'h0?2a: decoded = STS;
            16'h4?06, 16'h4?16, 16'h4?26: begin
                decoded = LOAD;
                dec_size = LONGWORD;
                dec_post = 1'b1;
                dec_sys_load = 1'b1;
            end
            16'h4?02, 16'h4?12, 16'h4?22: begin
                decoded = STORE;
                dec_size = LONGWORD;
                dec_pre = 1'b1;
                dec_b = sys_value;
            end

            16'he???: begin decoded = MOV; dec_b = {{24{ir[7]}}, ir[7:0]}; end
            16'h7???: begin decoded = ADD; dec_b = {{24{ir[7]}}, ir[7:0]}; end
            16'h6??3: decoded = MOV;
            16'h3??c: decoded = ADD;
            16'h3??8: decoded = SUB;
            16'h3??e: decoded = ADDC;
            16'h3??a: decoded = SUBC;
            16'h2??9: decoded = AND_OP;
            16'h2??b: decoded = OR_OP;
            16'h2??a: decoded = XOR_OP;
            16'h6??7: decoded = NOT_OP;
            16'h3??0: decoded = EQ;
            16'h3??2: decoded = HS;
            16'h3??3: decoded = GE;
            16'h3??6: decoded = HI;
            16'h3??7: decoded = GT;
            16'h2??8: decoded = TST;
            16'h88??: begin decoded = EQ; dec_a = gpr[0]; dec_b = {{24{ir[7]}}, ir[7:0]}; end
            16'h4?00, 16'h4?20: decoded = SHLL;
            16'h4?01: decoded = SHLR;
            16'h4?21: decoded = SHAR;

            16'h4?2b: decoded = JMP;
            16'h4?0b: decoded = CALL;
            16'h0?23: begin decoded = BRAF; dec_offset = gpr[n]; end
            16'h0?03: begin decoded = BSRF; dec_offset = gpr[n]; end
            16'ha???, 16'hb???: begin
                decoded = ir[12] ? BSR : BR;
                dec_offset = {{19{ir[11]}}, ir[11:0], 1'b0};
            end
            16'h89??, 16'h8b??, 16'h8d??, 16'h8f??: begin
                case (ir[11:8])
                    4'h9: decoded = BT;
                    4'hb: decoded = BF;
                    4'hd: decoded = BTS;
                    default: decoded = BFS;
                endcase
                dec_offset = {{23{ir[7]}}, ir[7:0], 1'b0};
            end

            16'h6??0, 16'h6??1, 16'h6??2, 16'h6??4, 16'h6??5, 16'h6??6: begin
                decoded = LOAD;
                dec_size = size_t'(ir[1:0]);
                dec_post = ir[2];
                dec_a = gpr[m];
            end
            16'h2??0, 16'h2??1, 16'h2??2, 16'h2??4, 16'h2??5, 16'h2??6: begin
                decoded = STORE;
                dec_size = size_t'(ir[1:0]);
                dec_pre = ir[2];
            end
            16'h0??4, 16'h0??5, 16'h0??6, 16'h0??c, 16'h0??d, 16'h0??e: begin
                decoded = ir[3] ? LOAD : STORE;
                dec_size = size_t'(ir[1:0]);
                dec_a = ir[3] ? gpr[m] : gpr[n];
                dec_offset = gpr[0];
            end
            16'h0?93: decoded = OCBI;
            16'h0?a3: decoded = OCBP;
            16'h0?b3: decoded = OCBWB;
            16'h0?c3: begin
                decoded = MOVCA;
                dec_size = LONGWORD;
                dec_b = gpr[0];
            end
            16'h4?1b: decoded = TAS;
            16'hcc??, 16'hcd??, 16'hce??, 16'hcf??: begin
                decoded = MEMORY_BYTE;
                dec_a = gbr;
                dec_offset = gpr[0];
            end
            16'hc0??, 16'hc1??, 16'hc2??, 16'hc4??, 16'hc5??, 16'hc6??: begin
                decoded = ir[10] ? LOAD : STORE;
                dec_size = size_t'(ir[9:8]);
                dest_r0 = ir[10];
                dec_a = gbr;
                dec_b = gpr[0];
                dec_offset = {24'b0, ir[7:0]} << dec_size;
            end
            16'h1???, 16'h5???: begin
                decoded = ir[14] ? LOAD : STORE;
                dec_size = LONGWORD;
                dec_a = ir[14] ? gpr[m] : gpr[n];
                dec_offset = {26'b0, ir[3:0], 2'b00};
            end
            default: ;
        endcase

        if (alu_recognized && alu_r0) begin
            dec_a = gpr[0];
            dec_b = {24'b0, ir[7:0]};
        end

        // Byte and word displacement forms place the register in a different field.
        casez (ir)
            16'h80??, 16'h81??, 16'h84??, 16'h85??: begin
                decoded = ir[10] ? LOAD : STORE;
                dest_r0 = ir[10];
                dec_size = ir[8] ? WORD : BYTE;
                dec_a = gpr[m];
                dec_b = gpr[0];
                dec_offset = ir[8] ? {27'b0, ir[3:0], 1'b0} : {28'b0, ir[3:0]};
            end
            16'h9???, 16'hd???, 16'hc7??: begin
                dest_r0 = ir[15:12] == 4'hc;
                decoded = dest_r0 ? MOVA : LOAD;
                dec_size = ir[15:12] == 4'h9 ? WORD : LONGWORD;
                dec_pc_relative = 1'b1;
                dec_offset = dec_size == WORD ? {23'b0, ir[7:0], 1'b0} : {22'b0, ir[7:0], 2'b00};
            end
            default: ;
        endcase
    end

    // These are the only arithmetic operations on the shared register operands.
    always_comb begin
        case (op)
            ADDC: arithmetic = {1'b0, a} + {1'b0, b} + {32'b0, t};
            SUBC: arithmetic = {1'b0, a} - {1'b0, b} - {32'b0, t};
            SUB:  arithmetic = {1'b0, a} - {1'b0, b};
            default: arithmetic = {1'b0, a} + {1'b0, b};
        endcase
        value = 32'b0;
        target = pc + 32'd4 + offset;
        effective_addr = a + offset;
        if (pre)
            effective_addr = a - (32'd1 << size);
        if (pc_relative)
            effective_addr = (size == WORD ? pc + 32'd4 : (pc + 32'd4) & 32'hffff_fffc) + offset;
        aligned = (size != LONGWORD || effective_addr[1:0] == 2'b00) &&
                  (size != WORD || !effective_addr[0]);

        write_gpr = 1'b0;
        write_t = 1'b0;
        next_t = 1'b0;
        write_pr = 1'b0;
        control = 1'b0;
        delayed = 1'b0;
        taken = 1'b0;

        case (op)
            MOV, MOV_T, MOVA, STS,
            ADD, SUB, ADDC, SUBC, AND_OP, OR_OP, XOR_OP, NOT_OP, SHLL, SHLR, SHAR: begin
                write_gpr = 1'b1;
                case (op)
                    MOV: value = b;
                    MOV_T: value = {31'b0, t};
                    STS: value = sys_value;
                    MOVA: value = effective_addr;
                    ADD, SUB, ADDC, SUBC: begin
                        value = arithmetic[31:0];
                        write_t = op == ADDC || op == SUBC;
                        next_t = arithmetic[32];
                    end
                    AND_OP: value = a & b;
                    OR_OP: value = a | b;
                    XOR_OP: value = a ^ b;
                    NOT_OP: value = ~b;
                    default: begin
                        value = op == SHLL ? {a[30:0], 1'b0} : {op == SHAR && a[31], a[31:1]};
                        write_t = 1'b1;
                        next_t = op == SHLL ? a[31] : a[0];
                    end
                endcase
            end
            EQ, HS, GE, HI, GT, TST, SET_T: begin
                write_t = 1'b1;
                case (op)
                    EQ: next_t = a == b;
                    HS: next_t = a >= b;
                    GE: next_t = $signed(a) >= $signed(b);
                    HI: next_t = a > b;
                    GT: next_t = $signed(a) > $signed(b);
                    TST: next_t = (a & b) == 32'b0;
                    default: next_t = b[0];
                endcase
            end
            JMP, CALL, RETURN_OP, BR, BSR, BRAF, BSRF, BT, BF, BTS, BFS, RTE: begin
                control = 1'b1;
                delayed = op != BT && op != BF;
                taken = (op == BT || op == BTS) ? t : (op == BF || op == BFS) ? !t : 1'b1;
                write_pr = op inside {CALL, BSR, BSRF};
                if (op == JMP || op == CALL)
                    target = a;
                if (op == RETURN_OP)
                    target = pr;
                if (op == RTE)
                    target = spc;
            end
            default: ;
        endcase
    end

    assign byte_access = op == TAS || op == MEMORY_BYTE;
    assign byte_writes = op == TAS || (op == MEMORY_BYTE && ir[9:8] != 0);
    assign ordinary_store = op == STORE || op == MOVCA;
    assign is_store = ordinary_store || byte_writes || op == OCBI;
    // Bit 1 writes back a dirty line. Bit 0 invalidates a hit.
    assign cache_command = op == OCBI ? 2'b01 : op == OCBWB ? 2'b10 : op == OCBP ? 2'b11 : 2'b00;
    assign cache_maintenance = cache_command != 0;
    assign operand_read_mask = size == BYTE ? 4'b0001 << data_addr_lane :
                               size == WORD ? 4'b0011 << data_addr_lane : 4'b1111;

    always_comb begin
        if (op == TAS)
            byte_result = wb_value[7:0] | 8'h80;
        else case (ir[9:8])
            0, 1: byte_result = wb_value[7:0] & ir[7:0];
            2: byte_result = wb_value[7:0] ^ ir[7:0];
            3: byte_result = wb_value[7:0] | ir[7:0];
        endcase
    end
    assign privileged_op = (op inside {LDTLB, RTE, SLEEP}) ||
                           (control_transfer && control_reg != CR_GBR);
    assign control_write = control_transfer && (op == LDC || op == LOAD);
    assign control_write_value = op == LDC ? a : wb_value;
    assign writes_sr = control_write && control_reg == CR_SR;
    assign load_gpr = !sys_load && (!control_transfer || control_reg == CR_BANK);

    always_comb begin
        exception_cause = 4'b0;
        address_exception = 1'b0;
        fault_address = pc;
        case (stage)
            ISSUE: begin
                if (pc[0]) begin
                    exception_cause = FETCH_ALIGN;
                    address_exception = 1'b1;
                end else if (bad_fetch_region) begin
                    exception_cause = ADDRESS_ERROR;
                    address_exception = 1'b1;
                end else if (fetch_tlb_pending && mmu_response)
                    exception_cause = mmu_fault;
            end
            EXECUTE: begin
                if (op == BAD || (!supervisor && privileged_op) ||
                    (in_slot && (control || pc_relative || writes_sr || op == TRAPA)))
                    exception_cause = in_slot ? SLOT_ILLEGAL : ILLEGAL;
                else if ((op == LOAD || op == MAC || is_store) && !aligned) begin
                    exception_cause = DATA_ALIGN;
                    address_exception = 1'b1;
                    fault_address = effective_addr;
                end else if (op == TRAPA)
                    exception_cause = TRAP;
            end
            MAC_READ: if (!mac_second && (mac_second_addr[0] || (!mac_word && mac_second_addr[1]))) begin
                exception_cause = DATA_ALIGN;
                address_exception = 1'b1;
                fault_address = mac_second_addr;
            end
            DATA_TRANSLATE: if (bad_data_region || ((mmu_register || event_register || ccr_register) && size != LONGWORD)) begin
                exception_cause = ADDRESS_ERROR;
                address_exception = 1'b1;
                fault_address = {data_addr, data_addr_lane};
            end
            DATA_TLB_WAIT: if (mmu_response)
                exception_cause = mmu_fault;
            default: ;
        endcase

        case (exception_cause)
            ILLEGAL:           exception_event = 12'h180;
            SLOT_ILLEGAL:      exception_event = 12'h1a0;
            FETCH_ALIGN:       exception_event = 12'h0e0;
            DATA_ALIGN,
            ADDRESS_ERROR:     exception_event = stage != ISSUE && is_store ? 12'h100 : 12'h0e0;
            TLB_MISS:          exception_event = stage != ISSUE && is_store ? 12'h060 : 12'h040;
            TLB_MULTIPLE:      exception_event = 12'h140;
            TLB_PROTECT:       exception_event = stage != ISSUE && is_store ? 12'h0c0 : 12'h0a0;
            TLB_INITIAL_WRITE: exception_event = 12'h080;
            TRAP:              exception_event = 12'h160;
            default:           exception_event = 12'h000;
        endcase
    end

    // Reset-style multiple-hit and blocked/nested cases remain diagnostic stops.
    assign take_exception = exception_cause != 0 && exception_cause != TLB_MULTIPLE && !sr[28] && !rte_slot;

    // Acknowledge between instructions, after any delayed branch slot and
    // after an outstanding instruction translation has supplied its result.
    assign nmi_edge = nmi && !nmi_previous;
    assign nmi_ready = (nmi_pending || nmi_edge) && (!sr[28] || nmi_while_blocked);
    assign irq_ready = !sr[28] && irq_level > sr[7:4];
    assign take_interrupt = (stage == ISSUE || stage == ASLEEP) && !in_slot &&
                            !fetch_tlb_pending && exception_cause == 0 && (nmi_ready || irq_ready);

    assign redirect = stage == COMMIT && fetch_pc != next_pc;
    assign privilege_write = stage == COMMIT && writes_sr;
    assign mmu_ldtlb = stage == COMMIT && op == LDTLB;
    assign bad_fetch_region = (!fetch_supervisor && fetch_pc[31]) || fetch_pc[31:29] == 3'b111;
    assign bad_data_region = !supervisor && data_addr[31];
    assign fetch_mapped = mmu_enabled && (!fetch_pc[31] || fetch_pc[31:29] == 3'b110);
    assign data_mapped = mmu_enabled && (!data_addr[31] || data_addr[31:29] == 3'b110);
    assign fetch_cacheable = ic_enable && (fetch_mapped ? fetch_page_cacheable : fetch_pc[31:29] != 3'b101);
    assign fetch_lookup = stage != STOP && stage != EXCEPTION && stage != ASLEEP && !fetch_pc[0] && !bad_fetch_region &&
                          (fetch_mapped ? fetch_tlb_valid : fetch_cacheable || stage == ISSUE);
    assign fetch_flush = redirect || store_accepted || mmu_reg_write || mmu_ldtlb || ccr_write ||
                         privilege_write || exception_cause != 0 || take_interrupt ||
                         (stage == COMMIT && op == SLEEP) || stage == EXCEPTION || stage == STOP;
    assign fetch_consume = fetch_valid && ((stage == ISSUE && exception_cause == 0 && !take_interrupt) ||
                                           (stage == COMMIT && !redirect && !privilege_write && !mmu_ldtlb &&
                                            !irq_ready && !nmi_ready && op != SLEEP));
    assign store_accepted = data_bus_ready && mem_wstrb != 0;

    assign mmu_register_index = data_addr[5:2];
    assign mmu_register = {data_addr[31:6], 6'b0} == 32'hff00_0000 &&
                          (mmu_register_index inside {4'd0, 4'd1, 4'd2, 4'd3, 4'd4, 4'd13});
    assign ccr_register = data_addr == 30'h3fc0_0007;
    assign ccr_write = stage == DATA_TRANSLATE && !bad_data_region && ccr_register &&
                       size == LONGWORD && is_store;
    assign event_register = {data_addr[31:6], 6'b0} == 32'hff00_0000 &&
                            (mmu_register_index inside {4'd8, 4'd9, 4'd10});
    assign mmu_reg_write = stage == DATA_TRANSLATE && !bad_data_region && mmu_register &&
                           size == LONGWORD && is_store;
    assign mmu_request = (stage == DATA_TRANSLATE && !bad_data_region && data_mapped && !mmu_register) ||
                         (stage == ISSUE && fetch_mapped && !fetch_tlb_pending && !fetch_tlb_valid && !take_interrupt &&
                          !bad_fetch_region && !fetch_pc[0]);
    assign mmu_va = stage == DATA_TRANSLATE ? {data_addr, data_addr_lane} : fetch_pc;

    fes_sh4_mmu mmu (
        .clk, .reset, .request(mmu_request), .ready(mmu_ready),
        .instruction(stage == ISSUE), .write(stage == DATA_TRANSLATE && is_store),
        .supervisor(stage == ISSUE ? fetch_supervisor : supervisor),
        .address_exception, .fault_address, .virtual_addr(mmu_va), .response(mmu_response), .physical_addr(mmu_pa),
        .cacheable(mmu_cacheable), .write_through(mmu_write_through), .fault(mmu_fault), .enabled(mmu_enabled),
        .reg_select(mmu_register_index), .reg_write(mmu_reg_write), .reg_wdata(data_wdata),
        .reg_rdata(mmu_reg_rdata), .ldtlb(mmu_ldtlb)
    );

    fes_sh4_icache icache (
        .clk, .reset, .flush(fetch_flush), .invalidate_all(ccr_write && data_wdata[11]),
        .invalidate(store_accepted), .invalidate_index(mem_addr[12:5]),
        .lookup(fetch_lookup), .lookup_pc(fetch_mapped ? physical_fetch_pc : fetch_pc), .cacheable(fetch_cacheable),
        .instruction_valid(fetch_valid), .instruction(fetch_instruction), .consume(fetch_consume),
        .read_valid(fetch_read), .read_addr(fetch_read_addr), .read_ready(fetch_read_ready),
        .response_valid(fetch_response), .response_data(mem_rdata)
    );

    fes_sh4_dcache dcache (
        .clk, .reset, .invalidate_all(ccr_write && data_wdata[3]), .request(stage == DATA_REQ), .ready(data_ready),
        .address(data_addr), .address_cacheable(data_cacheable), .address_copyback(data_copyback),
        .purge_before_read(op == TAS), .maintenance(cache_command), .allocate_without_read(op == MOVCA),
        .wstrb(data_wstrb), .wdata(data_wdata),
        .complete(data_complete), .rdata(data_read_word),
        .bus_request(data_bus_request), .bus_writeback(data_bus_writeback), .bus_ready(data_bus_ready),
        .bus_address(data_bus_addr), .bus_wstrb(data_bus_wstrb), .bus_wdata(data_bus_wdata),
        .bus_response(data_bus_response), .bus_rdata(mem_rdata)
    );

    // Data gets priority between refill beats. The bus owns a registered
    // request until acceptance and tags the read until its response arrives.
    assign mem_valid = !reset && bus_state == BUS_SEND;
    assign data_bus_ready = mem_valid && mem_ready && !bus_fetch;
    assign data_bus_response = bus_state == BUS_REPLY && mem_rvalid && !bus_fetch;
    assign fetch_read_ready = bus_state == BUS_IDLE && !data_bus_request && !mem_lock;
    assign fetch_response = bus_state == BUS_REPLY && mem_rvalid && bus_fetch;
    assign load_word = data_read_word >> {data_addr_lane, 3'b000};
    always_comb begin
        case (mmu_register_index)
            8: local_read_value = {22'b0, tra, 2'b00};
            9: local_read_value = {20'b0, expevt};
            10: local_read_value = {20'b0, intevt};
            default: local_read_value = mmu_reg_rdata;
        endcase
        if (!event_register)
            local_read_value = mmu_reg_rdata;
    end

    always_comb begin
        case (size)
            BYTE: load_value = {{24{load_word[7]}}, load_word[7:0]};
            WORD: load_value = {{16{load_word[15]}}, load_word[15:0]};
            default: load_value = load_word;
        endcase
    end

    always_ff @(posedge clk) begin
        if (reset) begin
            bus_state <= BUS_IDLE;
            mem_lock <= 1'b0;
        end else begin
            case (bus_state)
                BUS_IDLE: begin
                    if (data_bus_request) begin
                        bus_fetch <= 1'b0;
                        mem_addr <= data_bus_addr;
                        mem_wstrb <= data_bus_wstrb;
                        mem_rmask <= data_cacheable ? 4'b1111 : operand_read_mask;
                        mem_wdata <= data_bus_wdata;
                        if (op == TAS && !data_bus_writeback)
                            mem_lock <= 1'b1;
                        bus_state <= BUS_SEND;
                    end else if (fetch_read && fetch_read_ready) begin
                        bus_fetch <= 1'b1;
                        mem_addr <= fetch_read_addr;
                        mem_wstrb <= 4'b0;
                        mem_rmask <= 4'b1111;
                        mem_wdata <= 32'b0;
                        bus_state <= BUS_SEND;
                    end
                end
                BUS_SEND: if (mem_ready) begin
                    bus_state <= mem_wstrb == 0 ? BUS_REPLY : BUS_IDLE;
                    if (mem_wstrb != 0)
                        mem_lock <= 1'b0;
                end
                BUS_REPLY: if (mem_rvalid)
                    bus_state <= BUS_IDLE;
                default: bus_state <= BUS_IDLE;
            endcase
        end
    end

    always_ff @(posedge clk) begin
        retired <= 1'b0;
        if (reset) begin
            stage <= ISSUE;
            {ic_enable, p1_copyback, direct_write_through, oc_enable} <= {RESET_CCR[8], RESET_CCR[2:0]};
            pc <= RESET_PC;
            fetch_pc <= RESET_PC;
            pr <= 32'b0;
            gbr <= 32'b0;
            dbr <= 32'b0;
            mach <= 32'b0;
            macl <= 32'b0;
            sr <= 32'h7000_00f0;
            ssr <= '0;
            spc <= '0;
            sgr <= '0;
            vbr <= '0;
            expevt <= '0;
            intevt <= '0;
            nmi_previous <= 1'b0;
            nmi_pending <= 1'b0;
            irq_ack <= 1'b0;
            tra <= '0;
            rte_slot <= 1'b0;
            slot_wrote_pr <= 1'b0;
            fetch_tlb_pending <= 1'b0;
            fetch_tlb_valid <= 1'b0;
            in_slot <= 1'b0;
            fault <= 4'b0;
            fault_pc <= 32'b0;
            retired_pc <= 32'b0;
        end else begin
            nmi_previous <= nmi;
            nmi_pending <= (nmi_pending || nmi_edge) && !(take_interrupt && nmi_ready);
            irq_ack <= take_interrupt && !nmi_ready;

            if (ccr_write)
                {ic_enable, p1_copyback, direct_write_through, oc_enable} <= {data_wdata[8], data_wdata[2:0]};

            if (redirect)
                fetch_pc <= next_pc;
            else if (fetch_consume) begin
                fetch_pc <= fetch_pc + 32'd2;
                ir <= fetch_instruction;
            end

            if (fetch_flush || fetch_consume)
                fetch_tlb_valid <= 1'b0;
            else if (stage == ISSUE && fetch_tlb_pending && mmu_response && mmu_fault == 0) begin
                physical_fetch_pc <= mmu_pa;
                fetch_page_cacheable <= mmu_cacheable;
                fetch_tlb_valid <= 1'b1;
            end
            if (stage == ISSUE) begin
                if (mmu_request && mmu_ready)
                    fetch_tlb_pending <= 1'b1;
                else if (mmu_response)
                    fetch_tlb_pending <= 1'b0;
            end

            if (exception_cause != 0 || take_interrupt) begin
                if (take_interrupt)
                    intevt <= nmi_ready ? 12'h1c0 : irq_event;
                else
                    expevt <= exception_event;
                if (take_exception || take_interrupt) begin
                    ssr <= sr;
                    spc <= exception_cause == TRAP ? pc + 32'd2 : in_slot ? delay_branch_pc : pc;
                    sgr <= gpr[15];
                    sr[30:28] <= 3'b111;
                    vector_pc <= vbr + (take_interrupt ? 32'h600 :
                                        exception_cause == TLB_MISS ? 32'h400 : 32'h100);
                    if (exception_cause == TRAP)
                        tra <= trap_imm;
                    if (in_slot && slot_wrote_pr)
                        pr <= pre_slot_pr;
                    in_slot <= 1'b0;
                    stage <= EXCEPTION;
                end else begin
                    fault <= exception_cause;
                    fault_pc <= pc;
                    stage <= STOP;
                end
            end else case (stage)
                ISSUE: if (fetch_consume)
                    stage <= DECODE;
                DECODE: begin
                    op <= decoded;
                    trap_imm <= ir[7:0];
                    size <= dec_size;
                    pre <= dec_pre;
                    post <= dec_post && (dec_sys_load || dec_control_transfer || n != m);
                    sys_load <= dec_sys_load;
                    control_reg <= dec_control_reg;
                    control_transfer <= dec_control_transfer;
                    pc_relative <= dec_pc_relative;
                    a <= dec_a;
                    b <= dec_b;
                    offset <= dec_offset;
                    store_value <= dec_b;
                    dst <= decoded == MAC ? m :
                           dec_control_transfer && dec_control_reg == CR_BANK && (decoded inside {LDC, LOAD}) ?
                               {1'b0, ir[6:4]} : dest_r0 ? 4'd0 : n;
                    base_reg <= decoded == LOAD && !dec_sys_load && !dec_control_transfer ? m : n;
                    stage <= EXECUTE;
                end
                EXECUTE: begin
                    next_pc <= in_slot ? delay_target : control && taken && !delayed ? target : pc + 32'd2;
                    start_slot <= control && delayed;
                    if (control && delayed)
                        delay_target <= taken ? target : pc + 32'd4;
                    wb_gpr <= write_gpr || (op == LDC && control_reg == CR_BANK);
                    wb_value <= op == LDC ? a : value;
                    wb_t <= write_t;
                    wb_t_value <= next_t;
                    wb_pr <= write_pr;
                    link_value <= pc + 32'd4;
                    wb_base <= (op == STORE && pre) || (op == LOAD && post) || op == MAC;
                    writeback_base <= pre ? effective_addr : a + (32'd1 << size);
                    mac_second <= 1'b0;
                    if (op == LOAD || op == MAC || byte_access || ordinary_store || cache_maintenance) begin
                        data_addr <= effective_addr[31:2];
                        data_addr_lane <= effective_addr[1:0];
                        data_wdata <= store_value << {effective_addr[1:0], 3'b000};
                        case (size)
                            BYTE: data_wstrb <= ordinary_store ? 4'b0001 << effective_addr[1:0] : 4'b0;
                            WORD: data_wstrb <= ordinary_store ? 4'b0011 << effective_addr[1:0] : 4'b0;
                            default: data_wstrb <= ordinary_store ? 4'b1111 : 4'b0;
                        endcase
                        stage <= DATA_TRANSLATE;
                    end else if (op == INTEGER) begin
                        stage <= INTEGER_RESULT;
                    end else if (op == MULTIPLY) begin
                        stage <= MUL_PRODUCTS;
                    end else begin
                        stage <= COMMIT;
                    end
                end
                INTEGER_RESULT: begin
                    wb_value <= alu_result;
                    wb_gpr <= alu_write;
                    wb_t <= alu_flag_mask[0];
                    wb_t_value <= alu_flag_value[0];
                    stage <= COMMIT;
                end
                MUL_PRODUCTS: stage <= MUL_SUM;
                MUL_SUM: stage <= MUL_RESULT;
                MUL_RESULT: stage <= op == MAC ? MAC_LOW : COMMIT;
                MAC_READ: begin
                    if (!mac_second) begin
                        a <= wb_value;
                        {data_addr, data_addr_lane} <= mac_second_addr;
                        offset <= mac_second_addr + (32'd1 << size);
                        mac_second <= 1'b1;
                        stage <= DATA_TRANSLATE;
                    end else begin
                        b <= wb_value;
                        stage <= MUL_PRODUCTS;
                    end
                end
                MAC_LOW: begin
                    mac_low_sum <= {1'b0, macl} + {1'b0, product[31:0]};
                    mac_word_signs <= {macl[31], product[31]};
                    stage <= MAC_HIGH;
                end
                MAC_HIGH: begin
                    mac_high_sum <= mac_addend_hi + mac_product_hi + {32'b0, mac_low_sum[32]};
                    if (n == m)
                        writeback_base <= offset;
                    else begin
                        wb_value <= offset;
                        wb_gpr <= 1'b1;
                    end
                    stage <= COMMIT;
                end
                DATA_TRANSLATE: begin
                    if (mmu_register || event_register || ccr_register) begin
                        wb_gpr <= op == LOAD && load_gpr;
                        wb_value <= ccr_register ? {23'b0, ic_enable, 5'b0, p1_copyback,
                                                     direct_write_through, oc_enable} : local_read_value;
                        if (event_register && is_store) begin
                            case (mmu_register_index)
                                8: tra <= data_wdata[9:2];
                                9: expevt <= data_wdata[11:0];
                                10: intevt <= data_wdata[11:0];
                                default: ;
                            endcase
                        end
                        stage <= op == MAC ? MAC_READ : COMMIT;
                    end else if (data_mapped) begin
                        if (mmu_ready)
                            stage <= DATA_TLB_WAIT;
                    end else begin
                        data_cacheable <= oc_enable && op != TAS && data_addr[31:29] != 3'b101 &&
                                          data_addr[31:29] != 3'b111;
                        data_copyback <= data_addr[31:29] == 3'b100 ? p1_copyback : !direct_write_through;
                        stage <= DATA_REQ;
                    end
                end
                DATA_TLB_WAIT: if (mmu_response) begin
                    data_addr <= mmu_pa[31:2];
                    data_cacheable <= oc_enable && op != TAS && mmu_cacheable;
                    data_copyback <= !mmu_write_through;
                    stage <= DATA_REQ;
                end
                DATA_REQ: if (data_ready)
                    stage <= DATA_WAIT;
                DATA_WAIT: if (data_complete) begin
                    if (op == LOAD || op == MAC || (byte_access && data_wstrb == 0)) begin
                        wb_gpr <= op == LOAD && load_gpr;
                        wb_value <= load_value;
                    end
                    stage <= op == MAC ? MAC_READ :
                             byte_access && data_wstrb == 0 ? BYTE_MODIFY : COMMIT;
                end
                BYTE_MODIFY: begin
                    if (op == TAS || !byte_writes) begin
                        wb_t <= 1'b1;
                        wb_t_value <= (op == TAS ? wb_value[7:0] : byte_result) == 0;
                    end
                    if (byte_writes) begin
                        data_wstrb <= operand_read_mask;
                        data_wdata <= {4{byte_result}};
                        stage <= DATA_REQ;
                    end else
                        stage <= COMMIT;
                end
                COMMIT: begin
                    if (wb_base)
                        gpr_storage[base_index] <= writeback_base;
                    if (wb_gpr)
                        gpr_storage[dst_index] <= wb_value;
                    if (wb_t)
                        sr[0] <= wb_t_value;
                    if (wb_pr) begin
                        pre_slot_pr <= pr;
                        pr <= link_value;
                    end
                    if (control_write) begin
                        case (control_reg)
                            CR_SR:  sr <= control_write_value & SR_MASK;
                            CR_GBR: gbr <= control_write_value;
                            CR_VBR: vbr <= control_write_value;
                            CR_SSR: ssr <= control_write_value & SR_MASK;
                            CR_SPC: spc <= control_write_value;
                            CR_DBR: dbr <= control_write_value;
                            default: ;
                        endcase
                    end
                    case (op)
                        RTE: sr <= ssr;
                        INTEGER: begin
                            if (alu_flag_mask[3]) sr[9] <= alu_flag_value[3];
                            if (alu_flag_mask[2]) sr[8] <= alu_flag_value[2];
                            if (alu_flag_mask[1]) sr[1] <= alu_flag_value[1];
                        end
                        CLRMAC: {mach, macl} <= 64'b0;
                        MAC: {mach, macl} <= mac_result;
                        MULTIPLY: begin
                            macl <= product[31:0];
                            if (ir[15:12] == 4'h3)
                                mach <= product[63:32];
                        end
                        LDS, LOAD: if (op == LDS || sys_load) begin
                            case (ir[5:4])
                                0: mach <= op == LDS ? a : wb_value;
                                1: macl <= op == LDS ? a : wb_value;
                                default: pr <= op == LDS ? a : wb_value;
                            endcase
                        end
                        default: ;
                    endcase
                    if (start_slot)
                        delay_branch_pc <= pc;
                    slot_wrote_pr <= wb_pr;
                    rte_slot <= op == RTE;
                    pc <= next_pc;
                    in_slot <= start_slot;
                    retired <= 1'b1;
                    retired_pc <= pc;
                    stage <= op == SLEEP ? ASLEEP : fetch_consume ? DECODE : ISSUE;
                end
                EXCEPTION: begin
                    pc <= vector_pc;
                    fetch_pc <= vector_pc;
                    stage <= ISSUE;
                end
                ASLEEP, STOP: ;
                default: stage <= STOP;
            endcase
        end
    end
endmodule
