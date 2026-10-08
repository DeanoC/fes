// SPDX-License-Identifier: MIT
// Original FES Z80 instruction engine. Derived from public instruction/flag
// specifications, never from a CPU implementation. See README.md for sources.
module fes_z80_engine #(
    parameter bit NMOS = 1'b1
) (
    input logic clk, reset, enable, int_n, nmi_n,
    input logic interrupt_blocked, bus_resume,
    input logic bus_ready,
    input logic [7:0] bus_rdata,
    output logic bus_req,
    output logic [2:0] bus_kind,
    output logic [3:0] bus_extra,
    output logic [4:0] bus_delay,
    output logic [15:0] bus_addr,
    output logic [7:0] bus_wdata,
    output logic [15:0] refresh_addr,
    output logic halted, illegal, retired,
    output logic [15:0] retire_pc,
    output logic [15:0] debug_pc, debug_sp, debug_af, debug_bc, debug_de,
                        debug_hl, debug_ix, debug_iy, debug_ir,
    output logic [2:0] debug_iff
);
    // Every NMOS transition writes one constant state bit. Testing that bit
    // avoids wide state equality and transition muxes in the pin-timed core.
    // The documented personality retains its original six-bit state codes.
    localparam integer STATE_WIDTH = NMOS ? 33 : 6;
    typedef enum logic [STATE_WIDTH-1:0] {
        FETCH=STATE_WIDTH'(NMOS ? 33'd1 : 33'd0),
        IMM8=STATE_WIDTH'(NMOS ? 33'd2 : 33'd1),
        IMM_LO=STATE_WIDTH'(NMOS ? 33'd4 : 33'd2),
        IMM_HI=STATE_WIDTH'(NMOS ? 33'd8 : 33'd3),
        DISP=STATE_WIDTH'(NMOS ? 33'd16 : 33'd4),
        INDEX_CB=STATE_WIDTH'(NMOS ? 33'd32 : 33'd5),
        READ8=STATE_WIDTH'(NMOS ? 33'd64 : 33'd6),
        WRITE8=STATE_WIDTH'(NMOS ? 33'd128 : 33'd7),
        READ_LO=STATE_WIDTH'(NMOS ? 33'd256 : 33'd8),
        READ_HI=STATE_WIDTH'(NMOS ? 33'd512 : 33'd9),
        WRITE_LO=STATE_WIDTH'(NMOS ? 33'd1024 : 33'd10),
        WRITE_HI=STATE_WIDTH'(NMOS ? 33'd2048 : 33'd11),
        POP_LO=STATE_WIDTH'(NMOS ? 33'd4096 : 33'd12),
        POP_HI=STATE_WIDTH'(NMOS ? 33'd8192 : 33'd13),
        PUSH_HI=STATE_WIDTH'(NMOS ? 33'd16384 : 33'd14),
        PUSH_LO=STATE_WIDTH'(NMOS ? 33'd32768 : 33'd15),
        IO_READ=STATE_WIDTH'(NMOS ? 33'd65536 : 33'd16),
        IO_WRITE=STATE_WIDTH'(NMOS ? 33'd131072 : 33'd17),
        DELAY=STATE_WIDTH'(NMOS ? 33'd262144 : 33'd18),
        IRQ_ACK=STATE_WIDTH'(NMOS ? 33'd524288 : 33'd19),
        NMI_ACK=STATE_WIDTH'(NMOS ? 33'd1048576 : 33'd20),
        VECTOR_LO=STATE_WIDTH'(NMOS ? 33'd2097152 : 33'd21),
        VECTOR_HI=STATE_WIDTH'(NMOS ? 33'd4194304 : 33'd22),
        EX_READ_LO=STATE_WIDTH'(NMOS ? 33'd8388608 : 33'd23),
        EX_READ_HI=STATE_WIDTH'(NMOS ? 33'd16777216 : 33'd24),
        EX_WRITE_HI=STATE_WIDTH'(NMOS ? 33'd33554432 : 33'd25),
        EX_WRITE_LO=STATE_WIDTH'(NMOS ? 33'd67108864 : 33'd26),
        BLOCK_READ=STATE_WIDTH'(NMOS ? 33'd134217728 : 33'd27),
        BLOCK_WRITE=STATE_WIDTH'(NMOS ? 33'd268435456 : 33'd28),
        BLOCK_IN=STATE_WIDTH'(NMOS ? 33'd536870912 : 33'd29),
        BLOCK_OUT=STATE_WIDTH'(NMOS ? 33'd1073741824 : 33'd30),
        ARITH_TAIL=STATE_WIDTH'(NMOS ? 33'd2147483648 : 33'd31),
        BLOCK_COMPARE_TAIL=STATE_WIDTH'(NMOS ? 33'd4294967296 : 33'd32)
    } state_t;
    typedef enum logic [4:0] {
        LD8, ALU8, INC8, DEC8, CB8, LD16_IMM, LD16_MEM, STORE16,
        LOAD_A, STORE_A, JUMP, CALL, JR, DJNZ, PUSH, POP, RETURN,
        EX_STACK, BLOCK_LD, BLOCK_CP, BLOCK_INPUT, BLOCK_OUTPUT, IN_A,
        OUT_A, IN_REG, OUT_REG, RRD, RLD, INT_PUSH
    } action_t;
    state_t state, after_delay;
    // Reverse cases below use these mutually exclusive NMOS bits; binary
    // cases and comparisons specialize back to their ordinary fast forms.
    function automatic logic state_matches(input state_t value, expected);
        state_matches=NMOS ? |(value & expected) : value==expected;
    endfunction
    action_t action;
    logic [7:0] a_reg, f_reg, b_reg, c_reg, d_reg, e_reg, h_reg, l_reg;
    logic [15:0] af_alt, bc_alt, de_alt, hl_alt, ix, iy, sp, pc, wz;
    logic [7:0] i_reg, r_reg, opcode, tmp8, write_data;
    logic [15:0] ea, idle_addr, tmp16, push_value, jump_target, instruction_pc;
    logic [1:0] index_sel, group_sel, im;
    logic [2:0] dest;
    logic [1:0] pair_sel;
    logic indexed_cb, iff1, iff2, ei_delay, q;
    logic nmi_prev, nmi_pending, ld_air, sampled_int_n, sampled_nmi;
    wire recognition_nmi = NMOS ? sampled_nmi : nmi_pending;
    wire recognition_int_n = NMOS ? sampled_int_n : int_n;
    logic [4:0] delay_count;
    logic branch_taken;
    wire [15:0] bc = {b_reg,c_reg};
    wire [15:0] de = {d_reg,e_reg};
    wire [15:0] hl = {h_reg,l_reg};
    wire [15:0] index_hl = ({16{index_sel==1}} & ix)
                         | ({16{index_sel==2}} & iy)
                         | ({16{index_sel!=1 && index_sel!=2}} & hl);
    wire [7:0] current_op = (state_matches(state,FETCH) || state_matches(state,IRQ_ACK)) ? bus_rdata : opcode;
    wire [2:0] x_z = current_op[2:0];
    logic [4:0] alu_op;
    logic [7:0] alu_a, alu_b, alu_xy, alu_result, alu_flags;
    logic [2:0] alu_bit;

    // Documented indexed byte forms access real H/L; index-byte register
    // encodings trap before writeback. Specializing these helpers removes the
    // IX/IY byte mux from the fast datapath while preserving the NMOS paths.
    function automatic logic [7:0] reg8(input logic [2:0] sel, input logic real_hl);
        // Parallel byte selection retains the zero-valued memory slot.
        reg8 = ({8{sel==3'd0}} & b_reg)
             | ({8{sel==3'd1}} & c_reg)
             | ({8{sel==3'd2}} & d_reg)
             | ({8{sel==3'd3}} & e_reg)
             | ({8{sel==3'd4 && (!NMOS || real_hl || (index_sel!=1 && index_sel!=2))}} & h_reg)
             | ({8{sel==3'd5 && (!NMOS || real_hl || (index_sel!=1 && index_sel!=2))}} & l_reg)
             | ({8{sel==3'd4 && NMOS && !real_hl && index_sel==1}} & ix[15:8])
             | ({8{sel==3'd5 && NMOS && !real_hl && index_sel==1}} & ix[7:0])
             | ({8{sel==3'd4 && NMOS && !real_hl && index_sel==2}} & iy[15:8])
             | ({8{sel==3'd5 && NMOS && !real_hl && index_sel==2}} & iy[7:0])
             | ({8{sel==3'd7}} & a_reg);
    endfunction
    function automatic logic [15:0] pair16(input logic [1:0] sel, input logic af);
        case(sel)
            0: pair16=bc; 1: pair16=de; 2: pair16=index_hl;
            3: pair16=af ? {a_reg,f_reg} : sp;
        endcase
    endfunction
    function automatic logic condition(input logic [2:0] sel);
        case(sel)
            0: condition=~f_reg[6]; 1: condition=f_reg[6];
            2: condition=~f_reg[0]; 3: condition=f_reg[0];
            4: condition=~f_reg[2]; 5: condition=f_reg[2];
            6: condition=~f_reg[7]; 7: condition=f_reg[7];
        endcase
    endfunction
    task automatic set_reg8(input logic [2:0] sel, input logic [7:0] value,
                            input logic real_hl);
        case(sel)
            0: b_reg<=value; 1: c_reg<=value; 2: d_reg<=value; 3: e_reg<=value;
            4: if (!NMOS || real_hl || index_sel==0) h_reg<=value;
               else if(index_sel==1) ix[15:8]<=value; else iy[15:8]<=value;
            5: if (!NMOS || real_hl || index_sel==0) l_reg<=value;
               else if(index_sel==1) ix[7:0]<=value; else iy[7:0]<=value;
            7: a_reg<=value;
            default: ;
        endcase
    endtask
    task automatic set_pair16(input logic [1:0] sel, input logic [15:0] value,
                              input logic af);
        case(sel)
            0: {b_reg,c_reg}<=value; 1: {d_reg,e_reg}<=value;
            2: if(index_sel==1) ix<=value; else if(index_sel==2) iy<=value;
               else {h_reg,l_reg}<=value;
            3: if(af) {a_reg,f_reg}<=value; else sp<=value;
        endcase
    endtask
    task automatic fault;
        illegal<=1; halted<=1;
    endtask
    // Retirement is the interrupt recognition boundary, including each repeat
    // iteration. EI itself never admits INT, even if IFF1 was already set.
    task automatic finish(input logic changed_flags);
        retired<=1; q<=changed_flags;
        index_sel<=0; group_sel<=0; indexed_cb<=0; state<=FETCH;
        ei_delay<=0; ld_air<=group_sel==2 && (current_op==8'h57 || current_op==8'h5f);
        if(recognition_nmi && !interrupt_blocked) begin
            nmi_pending<=0; halted<=0; iff1<=0; state<=NMI_ACK;
        end else if(!interrupt_blocked && !recognition_int_n && iff1 && !(group_sel==0 && (current_op==8'hfb || current_op==8'hf3))) begin
            iff1<=0; iff2<=0; halted<=0; state<=IRQ_ACK;
        end
    endtask
    // Decode operation candidates independently, then select with the original
    // priority, including arbitrary combinations of NMOS state bits.
    wire am_alu_fetch = state_matches(state,FETCH) || state_matches(state,IRQ_ACK);
    wire am_alu_base = am_alu_fetch && group_sel==0;
    wire am_alu_cb = !am_alu_base && ((am_alu_fetch && group_sel==1) ||
                                     (state_matches(state,READ8) && action==CB8));
    wire am_alu_ed = state_matches(state,FETCH) && group_sel==2 &&
                     !(state_matches(state,READ8) && action==CB8);
    wire am_alu_action = !((am_alu_fetch && (group_sel==0 || group_sel==1)) ||
                          (state_matches(state,READ8) && action==CB8) ||
                          (state_matches(state,FETCH) && group_sel==2));
    logic [4:0] am_base_op, am_cb_op, am_action_op;

    function automatic logic [4:0] am_decode_cb_op(input logic [7:3] op);
        if(op[7:6]==1) am_decode_cb_op=5'd19;
        else case(op[5:3])
            0: am_decode_cb_op=5'd11;
            1: am_decode_cb_op=5'd12;
            2: am_decode_cb_op=5'd13;
            3: am_decode_cb_op=5'd14;
            4: am_decode_cb_op=5'd15;
            5: am_decode_cb_op=5'd16;
            6: am_decode_cb_op=5'd17;
            7: am_decode_cb_op=5'd18;
        endcase
    endfunction

    always_comb begin
        am_base_op=0;
        // The base selector guarantees that current_op is bus_rdata.
        if(bus_rdata[7:6]==2) am_base_op={2'b00,bus_rdata[5:3]};
        else if(bus_rdata[7:6]==0 && (bus_rdata[2:0]==4 || bus_rdata[2:0]==5))
            am_base_op=bus_rdata[2:0]==4 ? 5'd8 : 5'd9;
        else if(bus_rdata[7:6]==0 && bus_rdata[2:0]==7) begin
            case(bus_rdata[5:3])
                0: am_base_op=21; 1: am_base_op=22; 2: am_base_op=23; 3: am_base_op=24;
                4: am_base_op=10; 5: am_base_op=25; 6: am_base_op=26; 7: am_base_op=27;
            endcase
        end

        am_cb_op=am_alu_fetch ? am_decode_cb_op(bus_rdata[7:3])
                             : am_decode_cb_op(opcode[7:3]);

        am_action_op=0;
        case(action)
            ALU8: am_action_op={2'b00,opcode[5:3]};
            INC8: am_action_op=8;
            DEC8: am_action_op=9;
            BLOCK_CP: am_action_op=7;
            default: ;
        endcase
    end
    assign alu_op = ({5{am_alu_base}} & am_base_op)
                  | ({5{am_alu_cb}} & am_cb_op)
                  | ({5{am_alu_ed}} & 5'd20)
                  | ({5{am_alu_action}} & am_action_op);

    wire am_a_base_reg = am_alu_base && bus_rdata[7:6]==0 &&
                         (bus_rdata[2:0]==4 || bus_rdata[2:0]==5);
    wire am_a_cb_reg = am_alu_cb && am_alu_fetch;
    wire am_a_bus = (am_alu_cb && !am_alu_fetch) ||
                    (am_alu_action && (action==INC8 || action==DEC8));
    wire am_a_acc = !(am_a_base_reg || am_a_cb_reg || am_a_bus);
    wire am_b_tmp = am_alu_action && action==BLOCK_CP &&
                    state_matches(state,BLOCK_COMPARE_TAIL);
    wire am_b_bus = am_alu_action &&
                    (action==ALU8 || (action==BLOCK_CP && !state_matches(state,BLOCK_COMPARE_TAIL)));

    assign alu_a = ({8{am_a_acc}} & a_reg)
                 | ({8{am_a_base_reg}} & reg8(bus_rdata[5:3],0))
                 | ({8{am_a_cb_reg}} & reg8(bus_rdata[2:0],1))
                 | ({8{am_a_bus}} & bus_rdata);
    assign alu_b = ({8{am_alu_base}} & reg8(bus_rdata[2:0],0))
                 | ({8{am_b_bus}} & bus_rdata)
                 | ({8{am_b_tmp}} & tmp8);
    assign alu_xy = {8{am_alu_cb}} & ((indexed_cb || x_z==6) ? wz[15:8] : alu_a);
    assign alu_bit = current_op[5:3];
    fes_z80_alu #(.NMOS(NMOS)) alu (
        .op(alu_op), .a(alu_a), .b(alu_b), .flags_in(f_reg),
        .xy_source(alu_xy), .bit_index(alu_bit), .q(q),
        .result(alu_result), .flags_out(alu_flags)
    );

    always_comb begin
        bus_req=!illegal; bus_kind=1; bus_extra=0; bus_delay=delay_count; bus_addr=ea; bus_wdata=write_data;
        (* parallel_case *) case(NMOS ? STATE_WIDTH'(1) : state)
            (NMOS ? STATE_WIDTH'(state_matches(state,FETCH)) : FETCH): begin bus_kind=0; bus_addr=pc; end
            (NMOS ? STATE_WIDTH'(state_matches(state,IMM8)) : IMM8), (NMOS ? STATE_WIDTH'(state_matches(state,IMM_LO)) : IMM_LO), (NMOS ? STATE_WIDTH'(state_matches(state,IMM_HI)) : IMM_HI), (NMOS ? STATE_WIDTH'(state_matches(state,DISP)) : DISP), (NMOS ? STATE_WIDTH'(state_matches(state,INDEX_CB)) : INDEX_CB): bus_addr=pc;
            (NMOS ? STATE_WIDTH'(state_matches(state,READ8)) : READ8), (NMOS ? STATE_WIDTH'(state_matches(state,READ_LO)) : READ_LO), (NMOS ? STATE_WIDTH'(state_matches(state,EX_READ_LO)) : EX_READ_LO), (NMOS ? STATE_WIDTH'(state_matches(state,BLOCK_READ)) : BLOCK_READ): bus_addr=ea;
            (NMOS ? STATE_WIDTH'(state_matches(state,READ_HI)) : READ_HI), (NMOS ? STATE_WIDTH'(state_matches(state,EX_READ_HI)) : EX_READ_HI): bus_addr=ea+16'd1;
            (NMOS ? STATE_WIDTH'(state_matches(state,WRITE8)) : WRITE8), (NMOS ? STATE_WIDTH'(state_matches(state,WRITE_LO)) : WRITE_LO): bus_kind=2;
            (NMOS ? STATE_WIDTH'(state_matches(state,WRITE_HI)) : WRITE_HI): begin bus_kind=2; bus_addr=ea+16'd1; bus_wdata=tmp16[15:8]; end
            (NMOS ? STATE_WIDTH'(state_matches(state,POP_LO)) : POP_LO), (NMOS ? STATE_WIDTH'(state_matches(state,POP_HI)) : POP_HI): bus_addr=sp;
            (NMOS ? STATE_WIDTH'(state_matches(state,PUSH_HI)) : PUSH_HI): begin bus_kind=2; bus_addr=sp-16'd1; bus_wdata=push_value[15:8]; end
            (NMOS ? STATE_WIDTH'(state_matches(state,PUSH_LO)) : PUSH_LO): begin bus_kind=2; bus_addr=sp-16'd1; bus_wdata=push_value[7:0]; end
            (NMOS ? STATE_WIDTH'(state_matches(state,IO_READ)) : IO_READ), (NMOS ? STATE_WIDTH'(state_matches(state,BLOCK_IN)) : BLOCK_IN): begin bus_kind=3; bus_addr=ea; end
            (NMOS ? STATE_WIDTH'(state_matches(state,IO_WRITE)) : IO_WRITE), (NMOS ? STATE_WIDTH'(state_matches(state,BLOCK_OUT)) : BLOCK_OUT): begin bus_kind=4; bus_addr=ea; end
            (NMOS ? STATE_WIDTH'(state_matches(state,DELAY)) : DELAY): begin bus_kind=7; bus_addr=idle_addr; end
            (NMOS ? STATE_WIDTH'(state_matches(state,ARITH_TAIL)) : ARITH_TAIL): begin bus_kind=7; bus_addr=idle_addr; bus_delay=3; end
            (NMOS ? STATE_WIDTH'(state_matches(state,BLOCK_COMPARE_TAIL)) : BLOCK_COMPARE_TAIL): begin bus_kind=7; bus_addr=ea; bus_delay=5; end
            (NMOS ? STATE_WIDTH'(state_matches(state,IRQ_ACK)) : IRQ_ACK): begin bus_kind=5; bus_addr=pc; end
            (NMOS ? STATE_WIDTH'(state_matches(state,NMI_ACK)) : NMI_ACK): begin bus_kind=6; bus_addr=pc; end
            (NMOS ? STATE_WIDTH'(state_matches(state,VECTOR_LO)) : VECTOR_LO): bus_addr=ea;
            (NMOS ? STATE_WIDTH'(state_matches(state,VECTOR_HI)) : VECTOR_HI): bus_addr=ea+16'd1;
            (NMOS ? STATE_WIDTH'(state_matches(state,EX_WRITE_HI)) : EX_WRITE_HI): begin bus_kind=2; bus_addr=ea+16'd1; bus_wdata=push_value[15:8]; end
            (NMOS ? STATE_WIDTH'(state_matches(state,EX_WRITE_LO)) : EX_WRITE_LO): begin bus_kind=2; bus_wdata=push_value[7:0]; end
            (NMOS ? STATE_WIDTH'(state_matches(state,BLOCK_WRITE)) : BLOCK_WRITE): begin bus_kind=2; bus_addr=de; end
            default: bus_req=0;
        endcase
        // Extend the actual machine cycle that owns the internal work, rather
        // than emitting dummy bus cycles (e.g. DJNZ's first M1 is five T).
        if((state_matches(state,FETCH) && !halted) || state_matches(state,IRQ_ACK)) begin
            if(group_sel==0) begin
                if(current_op[7:6]==0 && current_op[2:0]==3) bus_extra=2;
                if(current_op==8'hf9) bus_extra=2;
                if(current_op==8'h10 || (current_op[7:6]==3 && current_op[2:0]==0) ||
                   (current_op[7:6]==3 && current_op[2:0]==5 && !current_op[3]) ||
                   (current_op[7:6]==3 && current_op[2:0]==7)) bus_extra=1;
            end else if(group_sel==2) begin
                if(current_op==8'h47 || current_op==8'h4f || current_op==8'h57 || current_op==8'h5f ||
                   (current_op[7:5]==3'b101 && current_op[2:1]==1)) bus_extra=1;
            end
        end
        if(state_matches(state,IRQ_ACK) && im!=0) bus_extra=1;
        (* parallel_case *) case(NMOS ? STATE_WIDTH'(1) : state)
            (NMOS ? STATE_WIDTH'(state_matches(state,INDEX_CB)) : INDEX_CB): bus_extra=2;
            (NMOS ? STATE_WIDTH'(state_matches(state,IMM8)) : IMM8): if(action==LD8 && dest==6 && index_sel!=0) bus_extra=2;
            (NMOS ? STATE_WIDTH'(state_matches(state,IMM_HI)) : IMM_HI): if(action==CALL && branch_taken) bus_extra=1;
            (NMOS ? STATE_WIDTH'(state_matches(state,READ8)) : READ8): if(action==INC8 || action==DEC8 || action==CB8) bus_extra=1;
            (NMOS ? STATE_WIDTH'(state_matches(state,EX_READ_HI)) : EX_READ_HI): bus_extra=1;
            (NMOS ? STATE_WIDTH'(state_matches(state,EX_WRITE_LO)) : EX_WRITE_LO): bus_extra=2;
            (NMOS ? STATE_WIDTH'(state_matches(state,BLOCK_WRITE)) : BLOCK_WRITE): bus_extra=2;
            default: ;
        endcase
        // Refresh exposes R before the M1's increment commits.
        refresh_addr={i_reg,r_reg};
        if(!NMOS) begin bus_extra=0; bus_delay=0; end
        retire_pc=instruction_pc;
        debug_pc=pc; debug_sp=sp; debug_af={a_reg,f_reg}; debug_bc=bc;
        debug_de=de; debug_hl=hl; debug_ix=ix; debug_iy=iy;
        debug_ir={i_reg,r_reg}; debug_iff={iff2,iff1,ei_delay};
    end

    function automatic logic documented_index(input logic [7:0] op);
        logic mem_form;
        begin
            mem_form=(op[7:6]==1 && op!=8'h76 && (op[5:3]==6 || op[2:0]==6)) ||
                     (op[7:6]==2 && op[2:0]==6) ||
                     (op[7:6]==0 && op[5:3]==6 && (op[2:0]==4 || op[2:0]==5 || op[2:0]==6));
            documented_index=mem_form || op==8'hcb || op==8'h21 || op==8'h22 ||
                op==8'h2a || op==8'h23 || op==8'h2b || op==8'h09 || op==8'h19 ||
                op==8'h29 || op==8'h39 || op==8'he1 || op==8'he3 || op==8'he5 ||
                op==8'he9 || op==8'hf9;
        end
    endfunction

    task automatic decode_cb(input logic [7:0] op);
        logic [7:0] v;
        begin
            if(!NMOS && ((op[7:6]==0 && op[5:3]==6) || (indexed_cb && op[2:0]!=6))) fault();
            else if(op[2:0]==6 || indexed_cb) begin
                action<=CB8; ea<=indexed_cb ? ea : hl; state<=READ8;
            end else begin
                v=reg8(op[2:0],1);
                case(op[7:6])
                    0: begin set_reg8(op[2:0],alu_result,1); f_reg<=alu_flags; end
                    1: f_reg<=alu_flags;
                    2: set_reg8(op[2:0],v & ~(8'b1 << op[5:3]),1);
                    3: set_reg8(op[2:0],v | (8'b1 << op[5:3]),1);
                endcase
                finish(op[7:6]<2);
            end
        end
    endtask
    task automatic arithmetic16(input logic [15:0] lhs, rhs,
                                input logic with_carry, subtract);
        logic [16:0] sum;
        logic [7:0] fs;
        begin
            if(subtract) sum={1'b0,lhs}-{1'b0,rhs}-17'(with_carry && f_reg[0]);
            else sum={1'b0,lhs}+{1'b0,rhs}+17'(with_carry && f_reg[0]);
            fs=f_reg;
            fs[5]=NMOS && sum[13]; fs[3]=NMOS && sum[11];
            fs[4]=lhs[12]^rhs[12]^sum[12]; fs[1]=subtract; fs[0]=sum[16];
            if(with_carry) begin
                fs[7]=sum[15]; fs[6]=sum[15:0]==0;
                fs[2]=(lhs[15]^sum[15]) && (subtract ? lhs[15]^rhs[15] : !(lhs[15]^rhs[15]));
            end
            if(with_carry) {h_reg,l_reg}<=sum[15:0];
            else set_pair16(2,sum[15:0],0);
            f_reg<=fs; wz<=lhs+16'd1; idle_addr<=refresh_addr; q<=1;
            if(NMOS) begin
                delay_count<=4; after_delay<=ARITH_TAIL; state<=DELAY;
            end else finish(1);
        end
    endtask

    task automatic decode_ed(input logic [7:0] op);
        logic known;
        logic [7:0] v;
        begin
            known=1;
            if(op[7:6]==1) case(op[2:0])
                0: if(!NMOS && op[5:3]==6) fault(); else begin
                    action<=IN_REG; dest<=op[5:3]; ea<=bc; wz<=bc+16'd1; state<=IO_READ;
                end
                1: if(!NMOS && op[5:3]==6) fault(); else begin
                    action<=OUT_REG; ea<=bc; wz<=bc+16'd1;
                    write_data<=op[5:3]==6 ? 8'd0 : reg8(op[5:3],1); state<=IO_WRITE;
                end
                2: arithmetic16(hl,pair16(op[5:4],0),1,!op[3]);
                3: begin action<=op[3] ? LD16_MEM : STORE16; pair_sel<=op[5:4]; state<=IMM_LO; end
                4: if(!NMOS && op!=8'h44) fault(); else begin
                    a_reg<=alu_result; f_reg<=alu_flags; finish(1);
                end
                5: if(!NMOS && op!=8'h45 && op!=8'h4d) fault(); else begin
                    iff1<=iff2; action<=RETURN; state<=POP_LO;
                end
                6: if(!NMOS && op!=8'h46 && op!=8'h56 && op!=8'h5e) fault(); else begin
                    case(op[5:3])
                        0,1,4,5: im<=0; 2,6: im<=1; 3,7: im<=2;
                    endcase
                    finish(0);
                end
                7: case(op[5:3])
                    0: begin i_reg<=a_reg; finish(0); end
                    1: begin r_reg<=a_reg; finish(0); end
                    2,3: begin
                        v=op[3] ? {r_reg[7],r_reg[6:0]+7'd1} : i_reg;
                        a_reg<=v;
                        f_reg<={v[7],v==0,NMOS && v[5],1'b0,NMOS && v[3],iff2,1'b0,f_reg[0]};
                        ld_air<=1; finish(1);
                        // UM0080 documents PV=0 when a maskable interrupt is admitted.
                        if(!recognition_int_n && iff1 && !recognition_nmi && !interrupt_blocked) f_reg[2]<=0;
                    end
                    4,5: begin action<=op[3] ? RLD : RRD; ea<=hl; wz<=hl+16'd1; state<=READ8; end
                    default: known=0;
                endcase
            endcase
            else if(op[7:5]==3'b101 && op[2:0]<4) begin
                case(op[1:0])
                    0: begin action<=BLOCK_LD; ea<=hl; state<=BLOCK_READ; end
                    1: begin action<=BLOCK_CP; ea<=hl; state<=BLOCK_READ; end
                    2: begin action<=BLOCK_INPUT; ea<=bc; state<=BLOCK_IN; end
                    3: begin action<=BLOCK_OUTPUT; ea<=hl; state<=BLOCK_READ; end
                endcase
            end else known=0;
            if(!known) begin if(NMOS) finish(0); else fault(); end
        end
    endtask

    task automatic decode_base(input logic [7:0] op);
        logic [2:0] y,z;
        logic [15:0] v16;
        begin
            y=op[5:3]; z=op[2:0];
            if(op==8'hdd || op==8'hfd) begin
                if(!NMOS && index_sel!=0) fault();
                else begin index_sel<=op==8'hdd ? 2'd1 : 2'd2; q<=0; end
            end else if(op==8'hed) begin
                if(!NMOS && index_sel!=0) fault();
                else begin group_sel<=2; index_sel<=0; q<=0; end
            end else if(op==8'hcb) begin
                group_sel<=1; q<=0;
                if(index_sel!=0) begin indexed_cb<=1; action<=CB8; state<=DISP; end
            end else if(!NMOS && index_sel!=0 && !documented_index(op)) fault();
            else case(op[7:6])
                0: case(z)
                    0: case(y)
                        0: finish(0);
                        1: begin af_alt<={a_reg,f_reg}; {a_reg,f_reg}<=af_alt; finish(0); end
                        2: begin action<=DJNZ; state<=IMM8; end
                        3,4,5,6,7: begin action<=JR; branch_taken<=y==3 || condition({1'b0,y[1:0]}); state<=IMM8; end
                    endcase
                    1: if(!op[3]) begin action<=LD16_IMM; pair_sel<=op[5:4]; state<=IMM_LO; end
                       else arithmetic16(index_hl,pair16(op[5:4],0),0,0);
                    2: case(op[5:4])
                        0,1: begin
                            ea<=op[4] ? de : bc; action<=op[3] ? LOAD_A : STORE_A;
                            if(op[3]) begin wz<=(op[4] ? de : bc)+16'd1; state<=READ8; end
                            else begin
                                wz<={a_reg,(op[4] ? e_reg : c_reg)+8'd1}; write_data<=a_reg; state<=WRITE8;
                            end
                        end
                        2,3: begin
                            action<=op[4] ? (op[3] ? LOAD_A : STORE_A) : (op[3] ? LD16_MEM : STORE16);
                            pair_sel<=2; state<=IMM_LO;
                        end
                    endcase
                    3: begin
                        v16=pair16(op[5:4],0)+(op[3] ? 16'hffff : 16'd1);
                        set_pair16(op[5:4],v16,0); finish(0);
                    end
                    4,5: if(y==6) begin
                            action<=z==4 ? INC8 : DEC8; dest<=6;
                            if(index_sel!=0) state<=DISP; else begin ea<=hl; state<=READ8; end
                         end else begin
                            set_reg8(y,alu_result,0); f_reg<=alu_flags; finish(1);
                         end
                    6: begin
                        action<=LD8; dest<=y;
                        if(y==6 && index_sel!=0) state<=DISP;
                        else begin ea<=hl; state<=IMM8; end
                    end
                    7: begin a_reg<=alu_result; f_reg<=alu_flags; finish(1); end
                endcase
                1: if(op==8'h76) begin halted<=1; finish(0); end
                   else if(y==6) begin
                        action<=LD8; dest<=6; write_data<=reg8(z,1);
                        if(index_sel!=0) state<=DISP; else begin ea<=hl; state<=WRITE8; end
                   end else if(z==6) begin
                        action<=LD8; dest<=y;
                        if(index_sel!=0) state<=DISP; else begin ea<=hl; state<=READ8; end
                   end else begin set_reg8(y,reg8(z,0),0); finish(0); end
                2: if(z==6) begin
                        action<=ALU8;
                        if(index_sel!=0) state<=DISP; else begin ea<=hl; state<=READ8; end
                   end else begin
                        if(y!=7) a_reg<=alu_result; f_reg<=alu_flags; finish(1);
                   end
                3: case(z)
                    0: begin action<=RETURN; if(condition(y)) state<=POP_LO; else finish(0); end
                    1: if(!op[3]) begin action<=POP; pair_sel<=op[5:4]; state<=POP_LO; end
                       else case(op[5:4])
                            0: begin action<=RETURN; state<=POP_LO; end
                            1: begin
                                bc_alt<=bc; de_alt<=de; hl_alt<=hl;
                                {b_reg,c_reg}<=bc_alt; {d_reg,e_reg}<=de_alt; {h_reg,l_reg}<=hl_alt; finish(0);
                            end
                            2: begin if(NMOS) pc<=index_hl; finish(0); end
                            3: begin sp<=index_hl; finish(0); end
                       endcase
                    2: begin action<=JUMP; branch_taken<=condition(y); state<=IMM_LO; end
                    3: case(y)
                        0: begin action<=JUMP; branch_taken<=1; state<=IMM_LO; end
                        1: ; // CB handled above
                        2: begin action<=OUT_A; state<=IMM8; end
                        3: begin action<=IN_A; state<=IMM8; end
                        4: begin action<=EX_STACK; ea<=sp; push_value<=index_hl; state<=EX_READ_LO; end
                        5: begin {d_reg,e_reg}<=hl; {h_reg,l_reg}<=de; finish(0); end
                        6: begin iff1<=0; iff2<=0; finish(0); end
                        7: begin iff1<=1; iff2<=1; finish(0); ei_delay<=1; end
                    endcase
                    4: begin action<=CALL; branch_taken<=condition(y); state<=IMM_LO; end
                    5: if(!op[3]) begin
                            action<=PUSH; push_value<=pair16(op[5:4],1); q<=0; state<=PUSH_HI;
                       end else begin action<=CALL; branch_taken<=1; state<=IMM_LO; end
                    6: begin action<=ALU8; state<=IMM8; end
                    7: begin
                        action<=CALL; push_value<=state_matches(state,IRQ_ACK) ? pc : pc+16'd1; jump_target<={10'd0,y,3'd0};
                        wz<={10'd0,y,3'd0}; q<=0;
                        state<=PUSH_HI;
                    end
                endcase
            endcase
        end
    endtask

    // Original block-operation flag equations. Repeating operations retire one
    // iteration so interrupts can restart them at their ED prefix.
    task automatic block_finish(input logic [7:0] value);
        logic dec_dir, again;
        logic [15:0] next_hl, next_bc;
        logic [7:0] sum8, adjusted, next_b, fs;
        logic unused_block_bits;
        logic [8:0] io_sum;
        begin
            dec_dir=opcode[3]; next_hl=hl+(dec_dir ? 16'hffff : 16'd1);
            next_bc=bc-16'd1; next_b=b_reg-8'd1; fs=f_reg; again=0;
            {h_reg,l_reg}<=next_hl;
            case(action)
                BLOCK_LD: begin
                    {d_reg,e_reg}<=de+(dec_dir ? 16'hffff : 16'd1);
                    {b_reg,c_reg}<=next_bc; sum8=a_reg+value;
                    fs[5]=NMOS && sum8[1]; fs[3]=NMOS && sum8[3];
                    fs[4]=0; fs[2]=next_bc!=0; fs[1]=0;
                    again=next_bc!=0;
                    delay_count<=5;
                end
                BLOCK_CP: begin
                    {b_reg,c_reg}<=next_bc; adjusted=alu_result-8'(alu_flags[4]);
                    fs=alu_flags; fs[0]=f_reg[0]; fs[2]=next_bc!=0;
                    fs[5]=NMOS && adjusted[1]; fs[3]=NMOS && adjusted[3];
                    wz<=wz+(dec_dir ? 16'hffff : 16'd1);
                    again=next_bc!=0 && !alu_flags[6];
                    delay_count<=5;
                end
                BLOCK_INPUT, BLOCK_OUTPUT: begin
                    b_reg<=next_b;
                    if(action==BLOCK_INPUT) begin
                        io_sum={1'b0,value}+{1'b0,c_reg+(dec_dir ? 8'hff : 8'd1)};
                        wz<=bc+(dec_dir ? 16'hffff : 16'd1);
                    end else begin
                        io_sum={1'b0,value}+{1'b0,next_hl[7:0]};
                        wz<={next_b,c_reg}+(dec_dir ? 16'hffff : 16'd1);
                    end
                    fs={next_b[7],next_b==0,NMOS && next_b[5],io_sum[8],NMOS && next_b[3],
                        ~^((io_sum[7:0]&8'd7)^next_b),value[7],io_sum[8]};
                    // The documented block-I/O manual only defines Z and N.
                    if(!NMOS) fs={1'b0,next_b==0,4'b0000,1'b1,f_reg[0]};
                    again=next_b!=0;
                    delay_count<=5;
                end
                default: ;
            endcase
            unused_block_bits = &{sum8,adjusted};
            f_reg<=fs; q<=1;
            if(opcode[4] && again) begin
                if(NMOS) pc<=pc-16'd2; wz<=pc-16'd1;
                if(NMOS) begin
                    fs[5]=((pc-16'd2) & 16'h2000)!=0;
                    fs[3]=((pc-16'd2) & 16'h0800)!=0;
                    if(action==BLOCK_INPUT || action==BLOCK_OUTPUT) begin
                        if(fs[0]) begin
                            if(value[7]) begin
                                fs[2]=fs[2] ^ (~^((next_b-8'd1)&8'd7)) ^ 1'b1;
                                fs[4]=next_b[3:0]==0;
                            end else begin
                                fs[2]=fs[2] ^ (~^((next_b+8'd1)&8'd7)) ^ 1'b1;
                                fs[4]=next_b[3:0]==15;
                            end
                        end else fs[2]=fs[2] ^ (~^(next_b&8'd7)) ^ 1'b1;
                    end
                    f_reg<=fs;
                end
            end
            after_delay<=FETCH;
            if(NMOS && opcode[4] && again) begin idle_addr<=ea; state<=DELAY; end else finish(1);
        end
    endtask

    // AlphaMister: isolate the fast PC's same-cycle control from other writebacks.
    // NMOS retains its original reverse-case priority for every binary state.
    generate if (!NMOS) begin : am_pc_fast
        wire am_repeat = opcode[4] && (
            (action==BLOCK_LD && bc!=16'd1) ||
            (action==BLOCK_CP && bc!=16'd1 &&
             a_reg!=(state_matches(state,BLOCK_COMPARE_TAIL) ? tmp8 : bus_rdata)) ||
            ((action==BLOCK_INPUT || action==BLOCK_OUTPUT) && b_reg!=8'd1));
        always_ff @(posedge clk) begin
            if(reset) pc<=0;
            else if(enable && !illegal && bus_ready &&
                    !(bus_resume && state_matches(state,FETCH) && group_sel==0 && index_sel==0)) begin
                case(state)
                    FETCH: if(!halted) begin
                        if(group_sel==0 && bus_rdata==8'he9) pc<=index_hl;
                        else pc<=pc+16'd1;
                    end
                    DISP, INDEX_CB, IMM_LO: pc<=pc+16'd1;
                    IMM8: begin
                        if((action==JR && branch_taken) || (action==DJNZ && b_reg!=1))
                            pc<=pc+16'd1+{{8{bus_rdata[7]}},bus_rdata};
                        else pc<=pc+16'd1;
                    end
                    IMM_HI: begin
                        if(action==JUMP && branch_taken) pc<={bus_rdata,tmp8};
                        else pc<=pc+16'd1;
                    end
                    POP_HI: if(action==RETURN) pc<={bus_rdata,tmp8};
                    PUSH_LO: if(action==CALL || action==INT_PUSH) pc<=jump_target;
                    IRQ_ACK: if(im==0 && bus_rdata==8'he9) pc<=index_hl;
                    VECTOR_HI: pc<={bus_rdata,tmp8};
                    WRITE8: if(action==BLOCK_INPUT && am_repeat) pc<=pc-16'd2;
                    BLOCK_READ: if(action==BLOCK_CP && am_repeat) pc<=pc-16'd2;
                    BLOCK_COMPARE_TAIL, BLOCK_WRITE, BLOCK_OUT: if(am_repeat) pc<=pc-16'd2;
                    default: ;
                endcase
            end
        end
    end endgenerate

    always_ff @(posedge clk) begin
        retired<=0;
        nmi_prev<=nmi_n;
        // The NMOS pin bus retires at the next T boundary after its final-T
        // rising-edge sample. DMA release instead recognizes the live INT line.
        if(enable) begin
            sampled_int_n<=int_n;
            sampled_nmi<=nmi_pending || (nmi_prev && !nmi_n);
        end
        if(nmi_prev && !nmi_n) nmi_pending<=1;
        if(reset) begin
            // Zilog NMOS warm RESET leaves the general register banks and SP
            // intact. Their cold power-up values are deliberately unspecified.
            if(!NMOS) begin
                a_reg<=0; f_reg<=0; b_reg<=0; c_reg<=0; d_reg<=0; e_reg<=0; h_reg<=0; l_reg<=0;
                af_alt<=0; bc_alt<=0; de_alt<=0; hl_alt<=0; ix<=0; iy<=0; sp<=16'hffff;
            end
            if(NMOS) pc<=0; wz<=0;
            i_reg<=0; r_reg<=0; opcode<=0; tmp8<=0; tmp16<=0; ea<=0; idle_addr<=0; write_data<=0;
            push_value<=0; jump_target<=0; instruction_pc<=0;
            index_sel<=0; group_sel<=0; im<=0; dest<=0; pair_sel<=0;
            indexed_cb<=0; iff1<=0; iff2<=0; ei_delay<=0; q<=0;
            nmi_prev<=1; nmi_pending<=0; ld_air<=0; sampled_int_n<=1; sampled_nmi<=0; branch_taken<=0;
            delay_count<=0; after_delay<=FETCH; action<=LD8; state<=FETCH;
            halted<=0; illegal<=0;
        end else if(enable && !illegal && bus_resume && state_matches(state,FETCH) && group_sel==0 && index_sel==0) begin
            if(nmi_pending) begin
                nmi_pending<=0; iff1<=0; halted<=0; state<=NMI_ACK;
            end else if(!int_n && iff1 && !ei_delay) begin
                iff1<=0; iff2<=0; halted<=0; state<=IRQ_ACK;
                if(NMOS && ld_air) f_reg[2]<=0;
            end
        end else if(enable && !illegal && bus_ready) begin
            (* parallel_case *) case(NMOS ? STATE_WIDTH'(1) : state)
                (NMOS ? STATE_WIDTH'(state_matches(state,FETCH)) : FETCH): begin
                    r_reg[6:0]<=r_reg[6:0]+7'd1;
                    if(halted) begin
                        if(!interrupt_blocked && recognition_nmi) begin nmi_pending<=0; iff1<=0; halted<=0; state<=NMI_ACK; end
                        else if(!interrupt_blocked && !recognition_int_n && iff1 && !ei_delay) begin iff1<=0; iff2<=0; halted<=0; state<=IRQ_ACK; end
                    end else begin
                        if(index_sel==0 && group_sel==0) instruction_pc<=pc;
                        if(NMOS) pc<=pc+16'd1; opcode<=bus_rdata;
                        case(group_sel)
                            0: decode_base(bus_rdata);
                            1: decode_cb(bus_rdata);
                            2: decode_ed(bus_rdata);
                            default: fault();
                        endcase
                    end
                end
                (NMOS ? STATE_WIDTH'(state_matches(state,DISP)) : DISP): begin
                    if(NMOS) pc<=pc+16'd1; idle_addr<=pc; ea<=index_hl+{{8{bus_rdata[7]}},bus_rdata};
                    wz<=index_hl+{{8{bus_rdata[7]}},bus_rdata};
                    if(indexed_cb) state<=INDEX_CB;
                    else if(action==LD8 && opcode[7:6]==0 && opcode[2:0]==6) state<=IMM8;
                    else if(NMOS) begin
                        delay_count<=5; after_delay<=action==LD8 && dest==6 ? WRITE8 : READ8; state<=DELAY;
                    end else state<=action==LD8 && dest==6 ? WRITE8 : READ8;
                end
                (NMOS ? STATE_WIDTH'(state_matches(state,INDEX_CB)) : INDEX_CB): begin
                    if(NMOS) pc<=pc+16'd1; opcode<=bus_rdata;
                    if(!NMOS && (bus_rdata[2:0]!=6 || (bus_rdata[7:6]==0 && bus_rdata[5:3]==6))) fault();
                    else state<=READ8;
                end
                (NMOS ? STATE_WIDTH'(state_matches(state,IMM8)) : IMM8): begin
                    if(NMOS) pc<=pc+16'd1;
                    case(action)
                        LD8: if(dest==6) begin write_data<=bus_rdata; state<=WRITE8; end
                             else begin set_reg8(dest,bus_rdata,0); finish(0); end
                        ALU8: begin if(opcode[5:3]!=7) a_reg<=alu_result; f_reg<=alu_flags; finish(1); end
                        JR: if(branch_taken) begin
                                if(NMOS) pc<=pc+16'd1+{{8{bus_rdata[7]}},bus_rdata};
                                wz<=pc+16'd1+{{8{bus_rdata[7]}},bus_rdata};
                                if(NMOS) begin idle_addr<=pc; q<=0; delay_count<=5; after_delay<=FETCH; state<=DELAY; end
                                else finish(0);
                            end else finish(0);
                        DJNZ: begin
                            b_reg<=b_reg-8'd1; q<=0;
                            if(b_reg!=1) begin
                                if(NMOS) pc<=pc+16'd1+{{8{bus_rdata[7]}},bus_rdata};
                                wz<=pc+16'd1+{{8{bus_rdata[7]}},bus_rdata};
                                if(NMOS) begin idle_addr<=pc; delay_count<=5; after_delay<=FETCH; state<=DELAY; end
                                else finish(0);
                            end
                            else finish(0);
                        end
                        IN_A, OUT_A: begin
                            ea<={a_reg,bus_rdata}; write_data<=a_reg;
                            if(action==IN_A) begin wz<={a_reg,bus_rdata}+16'd1; state<=IO_READ; end
                            else begin wz<={a_reg,bus_rdata+8'd1}; state<=IO_WRITE; end
                        end
                        default: fault();
                    endcase
                end
                (NMOS ? STATE_WIDTH'(state_matches(state,IMM_LO)) : IMM_LO): begin tmp8<=bus_rdata; if(NMOS) pc<=pc+16'd1; state<=IMM_HI; end
                (NMOS ? STATE_WIDTH'(state_matches(state,IMM_HI)) : IMM_HI): begin
                    tmp16<={bus_rdata,tmp8}; ea<={bus_rdata,tmp8}; if(NMOS) pc<=pc+16'd1;
                    case(action)
                        LD16_IMM: begin set_pair16(pair_sel,{bus_rdata,tmp8},0); finish(0); end
                        LD16_MEM: begin wz<={bus_rdata,tmp8}+16'd1; state<=READ_LO; end
                        STORE16: begin tmp16<=pair16(pair_sel,0); write_data<=8'(pair16(pair_sel,0)); wz<={bus_rdata,tmp8}+16'd1; state<=WRITE_LO; end
                        LOAD_A: begin wz<={bus_rdata,tmp8}+16'd1; state<=READ8; end
                        STORE_A: begin wz<={a_reg,tmp8+8'd1}; write_data<=a_reg; state<=WRITE8; end
                        JUMP: begin wz<={bus_rdata,tmp8}; if(branch_taken) if(NMOS) pc<={bus_rdata,tmp8}; finish(0); end
                        CALL: begin
                            wz<={bus_rdata,tmp8};
                            if(branch_taken) begin push_value<=pc+16'd1; jump_target<={bus_rdata,tmp8}; q<=0; state<=PUSH_HI; end
                            else finish(0);
                        end
                        default: fault();
                    endcase
                end
                (NMOS ? STATE_WIDTH'(state_matches(state,READ8)) : READ8): case(action)
                    LD8: begin set_reg8(dest,bus_rdata,1); finish(0); end
                    LOAD_A: begin a_reg<=bus_rdata; finish(0); end
                    ALU8: begin if(opcode[5:3]!=7) a_reg<=alu_result; f_reg<=alu_flags; finish(1); end
                    INC8, DEC8: begin write_data<=alu_result; f_reg<=alu_flags; q<=1; state<=WRITE8; end
                    CB8: begin
                        if(opcode[7:6]<2) begin f_reg<=alu_flags; q<=1; end else q<=0;
                        case(opcode[7:6])
                            0: write_data<=alu_result;
                            1: ;
                            2: write_data<=bus_rdata & ~(8'b1 << opcode[5:3]);
                            3: write_data<=bus_rdata | (8'b1 << opcode[5:3]);
                        endcase
                        if(indexed_cb && opcode[7:6]!=1 && opcode[2:0]!=6)
                            set_reg8(opcode[2:0],opcode[7:6]==0 ? alu_result :
                                opcode[7:6]==2 ? bus_rdata & ~(8'b1 << opcode[5:3]) : bus_rdata | (8'b1 << opcode[5:3]),1);
                        if(opcode[7:6]==1) finish(1); else state<=WRITE8;
                    end
                    RRD,RLD: begin
                        if(action==RRD) begin a_reg[3:0]<=bus_rdata[3:0]; write_data<={a_reg[3:0],bus_rdata[7:4]}; tmp8<={a_reg[7:4],bus_rdata[3:0]}; end
                        else begin a_reg[3:0]<=bus_rdata[7:4]; write_data<={bus_rdata[3:0],a_reg[3:0]}; tmp8<={a_reg[7:4],bus_rdata[7:4]}; end
                        q<=1;
                        if(NMOS) begin idle_addr<=ea; delay_count<=4; after_delay<=WRITE8; state<=DELAY; end
                        else state<=WRITE8;
                    end
                    default: fault();
                endcase
                (NMOS ? STATE_WIDTH'(state_matches(state,WRITE8)) : WRITE8): if(action==BLOCK_INPUT) block_finish(tmp8); else begin
                    if(action==RRD || action==RLD) f_reg<={tmp8[7],tmp8==0,NMOS&&tmp8[5],1'b0,NMOS&&tmp8[3],~^tmp8,1'b0,f_reg[0]};
                    finish(action==INC8 || action==DEC8 || action==RRD || action==RLD || (action==CB8 && opcode[7:6]<2));
                end
                (NMOS ? STATE_WIDTH'(state_matches(state,READ_LO)) : READ_LO): begin tmp8<=bus_rdata; state<=READ_HI; end
                (NMOS ? STATE_WIDTH'(state_matches(state,READ_HI)) : READ_HI): begin set_pair16(pair_sel,{bus_rdata,tmp8},0); finish(0); end
                (NMOS ? STATE_WIDTH'(state_matches(state,WRITE_LO)) : WRITE_LO): state<=WRITE_HI;
                (NMOS ? STATE_WIDTH'(state_matches(state,WRITE_HI)) : WRITE_HI): finish(0);
                (NMOS ? STATE_WIDTH'(state_matches(state,POP_LO)) : POP_LO): begin tmp8<=bus_rdata; sp<=sp+16'd1; state<=POP_HI; end
                (NMOS ? STATE_WIDTH'(state_matches(state,POP_HI)) : POP_HI): begin
                    sp<=sp+16'd1;
                    if(action==RETURN) begin if(NMOS) pc<={bus_rdata,tmp8}; wz<={bus_rdata,tmp8}; end
                    else set_pair16(pair_sel,{bus_rdata,tmp8},1);
                    finish(0);
                end
                (NMOS ? STATE_WIDTH'(state_matches(state,PUSH_HI)) : PUSH_HI): begin sp<=sp-16'd1; state<=PUSH_LO; end
                (NMOS ? STATE_WIDTH'(state_matches(state,PUSH_LO)) : PUSH_LO): begin
                    sp<=sp-16'd1;
                    if(action==CALL || action==INT_PUSH) if(NMOS) pc<=jump_target;
                    if(action==INT_PUSH && im==2 && group_sel==3) state<=VECTOR_LO;
                    else finish(0);
                end
                (NMOS ? STATE_WIDTH'(state_matches(state,IO_READ)) : IO_READ): begin
                    if(action==IN_A) a_reg<=bus_rdata;
                    else begin
                        if(dest!=6) set_reg8(dest,bus_rdata,1);
                        f_reg<={bus_rdata[7],bus_rdata==0,NMOS&&bus_rdata[5],1'b0,NMOS&&bus_rdata[3],~^bus_rdata,1'b0,f_reg[0]};
                    end
                    finish(action==IN_REG);
                end
                (NMOS ? STATE_WIDTH'(state_matches(state,IO_WRITE)) : IO_WRITE): finish(0);
                (NMOS ? STATE_WIDTH'(state_matches(state,DELAY)) : DELAY): (* parallel_case *) case(NMOS ? STATE_WIDTH'(1) : after_delay)
                    (NMOS ? STATE_WIDTH'(state_matches(after_delay,FETCH)) : FETCH): finish(q);
                    (NMOS ? STATE_WIDTH'(state_matches(after_delay,READ8)) : READ8): state<=READ8;
                    (NMOS ? STATE_WIDTH'(state_matches(after_delay,WRITE8)) : WRITE8): state<=WRITE8;
                    (NMOS ? STATE_WIDTH'(state_matches(after_delay,ARITH_TAIL)) : ARITH_TAIL): state<=ARITH_TAIL;
                    default: fault();
                endcase
                (NMOS ? STATE_WIDTH'(state_matches(state,ARITH_TAIL)) : ARITH_TAIL): finish(1);
                (NMOS ? STATE_WIDTH'(state_matches(state,IRQ_ACK)) : IRQ_ACK): begin
                    r_reg[6:0]<=r_reg[6:0]+7'd1; index_sel<=0;
                    if(im==0) begin group_sel<=0; state<=FETCH; instruction_pc<=pc; opcode<=bus_rdata; decode_base(bus_rdata); end
                    else begin
                        action<=INT_PUSH; push_value<=pc; jump_target<=16'h0038; wz<=16'h0038;
                        ea<={i_reg,bus_rdata}; group_sel<=im==2 ? 2'd3 : 2'd0; state<=PUSH_HI;
                    end
                end
                (NMOS ? STATE_WIDTH'(state_matches(state,NMI_ACK)) : NMI_ACK): begin
                    r_reg[6:0]<=r_reg[6:0]+7'd1; index_sel<=0; group_sel<=0;
                    action<=INT_PUSH; push_value<=pc; jump_target<=16'h0066; wz<=16'h0066; state<=PUSH_HI;
                end
                (NMOS ? STATE_WIDTH'(state_matches(state,VECTOR_LO)) : VECTOR_LO): begin tmp8<=bus_rdata; state<=VECTOR_HI; end
                (NMOS ? STATE_WIDTH'(state_matches(state,VECTOR_HI)) : VECTOR_HI): begin if(NMOS) pc<={bus_rdata,tmp8}; wz<={bus_rdata,tmp8}; finish(0); end
                (NMOS ? STATE_WIDTH'(state_matches(state,EX_READ_LO)) : EX_READ_LO): begin tmp8<=bus_rdata; state<=EX_READ_HI; end
                (NMOS ? STATE_WIDTH'(state_matches(state,EX_READ_HI)) : EX_READ_HI): begin tmp16<={bus_rdata,tmp8}; q<=0; state<=EX_WRITE_HI; end
                (NMOS ? STATE_WIDTH'(state_matches(state,EX_WRITE_HI)) : EX_WRITE_HI): state<=EX_WRITE_LO;
                (NMOS ? STATE_WIDTH'(state_matches(state,EX_WRITE_LO)) : EX_WRITE_LO): begin set_pair16(2,tmp16,0); wz<=tmp16; finish(0); end
                (NMOS ? STATE_WIDTH'(state_matches(state,BLOCK_READ)) : BLOCK_READ): begin
                    tmp8<=bus_rdata; write_data<=bus_rdata;
                    case(action)
                        BLOCK_LD: state<=BLOCK_WRITE;
                        BLOCK_CP: if(NMOS) state<=BLOCK_COMPARE_TAIL; else block_finish(bus_rdata);
                        BLOCK_OUTPUT: begin ea<={b_reg-8'd1,c_reg}; state<=BLOCK_OUT; end
                        default: fault();
                    endcase
                end
                (NMOS ? STATE_WIDTH'(state_matches(state,BLOCK_COMPARE_TAIL)) : BLOCK_COMPARE_TAIL): block_finish(tmp8);
                (NMOS ? STATE_WIDTH'(state_matches(state,BLOCK_WRITE)) : BLOCK_WRITE): begin
                    if(action==BLOCK_INPUT) ea<=hl;
                    block_finish(tmp8);
                end
                (NMOS ? STATE_WIDTH'(state_matches(state,BLOCK_IN)) : BLOCK_IN): begin tmp8<=bus_rdata; write_data<=bus_rdata; ea<=hl; state<=WRITE8; end
                (NMOS ? STATE_WIDTH'(state_matches(state,BLOCK_OUT)) : BLOCK_OUT): block_finish(tmp8);
                default: fault();
            endcase
        end
    end
endmodule
