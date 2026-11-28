module fes_sh4 #(
    parameter logic [31:0] RESET_PC = 32'ha000_0000
) (
    input  logic        clk,
    input  logic        reset,
    output logic        mem_valid,
    input  logic        mem_ready,
    output logic [31:0] mem_addr,
    output logic [3:0]  mem_wstrb,
    output logic [31:0] mem_wdata,
    input  logic        mem_rvalid,
    input  logic [31:0] mem_rdata,
    output logic        retired,
    output logic [31:0] retired_pc,
    output logic [2:0]  fault,
    output logic [31:0] fault_pc
);
    typedef enum logic [3:0] {
        FETCH, FETCH_WAIT, DECODE, EXECUTE, DATA_REQ, DATA_WAIT, COMMIT, STOP
    } stage_t;
    typedef enum logic [5:0] {
        BAD, NOP, MOV, ADD, SUB, ADDC, SUBC, AND_OP, OR_OP, XOR_OP, NOT_OP,
        EQ, HS, GE, HI, GT, TST, SET_T, MOV_T, SHLL, SHLR, SHAR,
        JMP, CALL, BR, BSR, RETURN_OP, BT, BF, BTS, BFS, LOAD, STORE, MOVA
    } op_t;
    typedef enum logic [1:0] {BYTE, WORD, LONGWORD} size_t;
    localparam logic [2:0] ILLEGAL = 3'd1, SLOT_ILLEGAL = 3'd2,
                           FETCH_ALIGN = 3'd3, DATA_ALIGN = 3'd4;

    stage_t stage;
    logic [31:0] gpr [0:15];
    logic [31:0] pc, pr, delay_target;
    logic t, in_slot;
    logic [15:0] ir;

    logic [3:0] n, m, dst, base_reg;
    op_t decoded, op;
    size_t dec_size, size;
    logic dec_pre, dec_post, dec_pc_relative, pre, post, pc_relative;
    logic [31:0] dec_a, dec_b, dec_offset, a, b, offset, store_value;

    logic [31:0] value, target, effective_addr;
    logic [32:0] arithmetic;
    logic write_gpr, write_t, next_t, write_pr, control, delayed, taken, aligned;
    logic [31:0] wb_value, next_pc, link_value, writeback_base;
    logic wb_gpr, wb_t, wb_t_value, wb_pr, wb_base, start_slot;
    logic [31:0] data_addr, data_wdata;
    logic [3:0] data_wstrb;
    logic [31:0] load_word;
    logic [1:0] data_addr_lane;

    assign n = ir[11:8];
    assign m = ir[7:4];

    always_comb begin
        decoded = BAD;
        dec_size = BYTE;
        dec_pre = 1'b0;
        dec_post = 1'b0;
        dec_pc_relative = 1'b0;
        dec_a = gpr[n];
        dec_b = gpr[m];
        dec_offset = 32'b0;

        casez (ir)
            16'h0009: decoded = NOP;
            16'h0008, 16'h0018: begin decoded = SET_T; dec_b = {31'b0, ir[4]}; end
            16'h000b: decoded = RETURN_OP;
            16'h0?29: decoded = MOV_T;

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
            16'h1???, 16'h5???: begin
                decoded = ir[14] ? LOAD : STORE;
                dec_size = LONGWORD;
                dec_a = ir[14] ? gpr[m] : gpr[n];
                dec_offset = {26'b0, ir[3:0], 2'b00};
            end
            default: ;
        endcase

        // Byte and word displacement forms place the register in a different field.
        casez (ir)
            16'h80??, 16'h81??, 16'h84??, 16'h85??: begin
                decoded = ir[10] ? LOAD : STORE;
                dec_size = ir[8] ? WORD : BYTE;
                dec_a = gpr[m];
                dec_b = gpr[0];
                dec_offset = ir[8] ? {27'b0, ir[3:0], 1'b0} : {28'b0, ir[3:0]};
            end
            16'h9???, 16'hd???, 16'hc7??: begin
                decoded = ir[15:12] == 4'hc ? MOVA : LOAD;
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
            MOV, MOV_T, MOVA, ADD, SUB, ADDC, SUBC, AND_OP, OR_OP, XOR_OP, NOT_OP, SHLL, SHLR, SHAR: begin
                write_gpr = 1'b1;
                case (op)
                    MOV: value = b;
                    MOV_T: value = {31'b0, t};
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
            JMP, CALL, RETURN_OP, BR, BSR, BT, BF, BTS, BFS: begin
                control = 1'b1;
                delayed = op != BT && op != BF;
                taken = (op == BT || op == BTS) ? t : (op == BF || op == BFS) ? !t : 1'b1;
                write_pr = op == CALL || op == BSR;
                if (op == JMP || op == CALL)
                    target = a;
                if (op == RETURN_OP)
                    target = pr;
            end
            default: ;
        endcase
    end

    assign mem_valid = !reset && ((stage == FETCH && !pc[0]) || stage == DATA_REQ);
    assign mem_addr = stage == DATA_REQ ? data_addr : {pc[31:2], 2'b00};
    assign mem_wstrb = stage == DATA_REQ ? data_wstrb : 4'b0;
    assign mem_wdata = data_wdata;
    assign load_word = mem_rdata >> {data_addr_lane, 3'b000};

    always_ff @(posedge clk) begin
        retired <= 1'b0;
        if (reset) begin
            stage <= FETCH;
            pc <= RESET_PC;
            pr <= 32'b0;
            t <= 1'b0;
            in_slot <= 1'b0;
            fault <= 3'b0;
            fault_pc <= 32'b0;
            retired_pc <= 32'b0;
        end else begin
            case (stage)
                FETCH: begin
                    if (pc[0]) begin
                        fault <= FETCH_ALIGN;
                        fault_pc <= pc;
                        stage <= STOP;
                    end else if (mem_ready) begin
                        stage <= FETCH_WAIT;
                    end
                end
                FETCH_WAIT: if (mem_rvalid) begin
                    ir <= pc[1] ? mem_rdata[31:16] : mem_rdata[15:0];
                    stage <= DECODE;
                end
                DECODE: begin
                    op <= decoded;
                    size <= dec_size;
                    pre <= dec_pre;
                    post <= dec_post && n != m;
                    pc_relative <= dec_pc_relative;
                    a <= dec_a;
                    b <= dec_b;
                    offset <= dec_offset;
                    store_value <= dec_b;
                    dst <= decoded == MOVA || ((ir[15:8] == 8'h84 || ir[15:8] == 8'h85) && decoded == LOAD) ? 4'd0 : n;
                    base_reg <= decoded == LOAD ? m : n;
                    stage <= EXECUTE;
                end
                EXECUTE: begin
                    next_pc <= in_slot ? delay_target : control && taken && !delayed ? target : pc + 32'd2;
                    start_slot <= control && delayed;
                    if (control && delayed)
                        delay_target <= taken ? target : pc + 32'd4;
                    wb_gpr <= write_gpr;
                    wb_value <= value;
                    wb_t <= write_t;
                    wb_t_value <= next_t;
                    wb_pr <= write_pr;
                    link_value <= pc + 32'd4;
                    wb_base <= (op == STORE && pre) || (op == LOAD && post);
                    writeback_base <= pre ? effective_addr : a + (32'd1 << size);
                    if (op == BAD || (in_slot && (control || pc_relative))) begin
                        fault <= in_slot ? SLOT_ILLEGAL : ILLEGAL;
                        fault_pc <= pc;
                        stage <= STOP;
                    end else if (op == LOAD || op == STORE) begin
                        if (!aligned) begin
                            fault <= DATA_ALIGN;
                            fault_pc <= pc;
                            stage <= STOP;
                        end else begin
                            data_addr <= {effective_addr[31:2], 2'b00};
                            data_addr_lane <= effective_addr[1:0];
                            data_wdata <= store_value << {effective_addr[1:0], 3'b000};
                            case (size)
                                BYTE: data_wstrb <= op == STORE ? 4'b0001 << effective_addr[1:0] : 4'b0;
                                WORD: data_wstrb <= op == STORE ? 4'b0011 << effective_addr[1:0] : 4'b0;
                                default: data_wstrb <= op == STORE ? 4'b1111 : 4'b0;
                            endcase
                            stage <= DATA_REQ;
                        end
                    end else begin
                        stage <= COMMIT;
                    end
                end
                DATA_REQ: if (mem_ready)
                    stage <= op == LOAD ? DATA_WAIT : COMMIT;
                DATA_WAIT: if (mem_rvalid) begin
                    wb_gpr <= 1'b1;
                    case (size)
                        BYTE: wb_value <= {{24{load_word[7]}}, load_word[7:0]};
                        WORD: wb_value <= {{16{load_word[15]}}, load_word[15:0]};
                        default: wb_value <= load_word;
                    endcase
                    stage <= COMMIT;
                end
                COMMIT: begin
                    if (wb_base)
                        gpr[base_reg] <= writeback_base;
                    if (wb_gpr)
                        gpr[dst] <= wb_value;
                    if (wb_t)
                        t <= wb_t_value;
                    if (wb_pr)
                        pr <= link_value;
                    pc <= next_pc;
                    in_slot <= start_slot;
                    retired <= 1'b1;
                    retired_pc <= pc;
                    stage <= FETCH;
                end
                STOP: ;
                default: stage <= STOP;
            endcase
        end
    end
endmodule
