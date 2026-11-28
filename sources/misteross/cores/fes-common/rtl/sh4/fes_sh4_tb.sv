module fes_sh4_tb;
    logic clk = 0;
    always #5 clk <= ~clk;

    logic reset = 1, bram_reset = 1, load_we = 0;
    logic [8:0] load_word_addr;
    logic [31:0] load_data;
    logic valid, ready, rvalid, retired, bram_retired;
    logic [31:0] addr, wdata, rdata, retired_pc, fault_pc, bram_retired_pc, bram_fault_pc;
    logic [3:0] fault, bram_fault;
    logic [3:0] wstrb;
    logic [31:0] image [0:8191];
    logic [31:0] ext_mem [0:8191];
    int cycle, latency;
    logic held;
    logic [67:0] held_request;
    int retire_count, read_count, previous_retire_cycle;
    int retried_word_writes;
    int resident_reads, conflict_reads, cold_store_line_reads, alias_reads;
    logic check_data_cache = 0;
    logic cache_cancel_done, mmu_done;

    fes_sh4_mmu_tb mmu_test (.clk, .done(mmu_done));
    fes_sh4_icache_tb cache_cancel (.clk, .done(cache_cancel_done));
    logic check_warm_loop = 0, check_alias_load = 1;

    fes_sh4 #(.RESET_PC(0)) dut (
        .clk, .reset, .mem_valid(valid), .mem_ready(ready),
        .mem_addr(addr), .mem_wstrb(wstrb), .mem_wdata(wdata),
        .mem_rvalid(rvalid), .mem_rdata(rdata),
        .retired, .retired_pc, .fault, .fault_pc
    );

    fes_sh4_bram_system bram (
        .clk, .reset(bram_reset), .load_we, .load_word_addr, .load_data,
        .retired(bram_retired), .retired_pc(bram_retired_pc),
        .fault(bram_fault), .fault_pc(bram_fault_pc)
    );

    assign ready = cycle % 5 == 4;

    always @(posedge clk) begin
        rvalid <= 0;
        if (reset) begin
            cycle <= 0;
            latency <= 0;
            held <= 0;
            retire_count <= 0;
            read_count <= 0;
            retried_word_writes <= 0;
            resident_reads <= 0;
            conflict_reads <= 0;
            cold_store_line_reads <= 0;
            alias_reads <= 0;
            previous_retire_cycle <= 0;
        end else begin
            cycle <= cycle + 1;
            if (retired) begin
                assert (retired_pc[0] == 0) else $fatal(1, "Retired an odd PC");
                if (check_alias_load && retired_pc == 32'h14)
                    assert (dut.gpr[6] == 32'hffff_ff80) else $fatal(1, "Aliased load adjusted its base");
                if (check_warm_loop && (retired_pc == 32'h6 || retired_pc == 32'h8))
                    assert (cycle - previous_retire_cycle == 3)
                        else $fatal(1, "Warm sequential instruction did not retire in three clocks");
                previous_retire_cycle <= cycle;
                retire_count <= retire_count + 1;
            end
            if (held)
                assert (valid && {addr, wdata, wstrb} == held_request)
                    else $fatal(1, "Request changed while backpressured");
            held <= valid && !ready;
            held_request <= {addr, wdata, wstrb};

            if (latency != 0) begin
                latency <= latency - 1;
                rvalid <= latency == 1;
            end
            if (valid && ready) begin
                assert (latency == 0 && addr[1:0] == 0 && addr[28:15] == 0)
                    else $fatal(1, "Unexpected memory request at %h", addr);
                if (wstrb == 0) begin
                    rdata <= ext_mem[addr[14:2]];
                    read_count <= read_count + 1;
                    case (addr & 32'hffff_ffe0)
                        32'h0000_0200: resident_reads <= resident_reads + 1;
                        32'h0000_4200: conflict_reads <= conflict_reads + 1;
                        32'h0000_0600: cold_store_line_reads <= cold_store_line_reads + 1;
                        32'ha000_0200: alias_reads <= alias_reads + 1;
                        default: ;
                    endcase
                    latency <= 1 + cycle % 3;
                end else begin
                    if (check_data_cache && addr == 32'h604)
                        assert (cold_store_line_reads == 0)
                            else $fatal(1, "A store miss fetched a line before writing through");
                    if (addr == 32'h1010)
                        retried_word_writes <= retried_word_writes + 1;
                    for (int lane = 0; lane < 4; lane++)
                        if (wstrb[lane])
                            ext_mem[addr[14:2]][8*lane +: 8] <= wdata[8*lane +: 8];
                end
            end
        end
    end

    task automatic emit(input int address, input logic [15:0] opcode);
        image[address >> 2][16*address[1] +: 16] = opcode;
    endtask

    task automatic literal(input int address, input logic [3:0] destination, input int pool);
        emit(address, {4'hd, destination, 8'((pool - ((address + 4) & ~3)) >> 2)});
    endtask

    task automatic start_bus;
        @(negedge clk);
        reset = 1;
        @(negedge clk);
        ext_mem = image;
        reset = 0;
    endtask

    task automatic expect_stop(input logic [3:0] cause, input logic [31:0] pc);
        wait (fault != 0);
        @(negedge clk);
        assert (fault == cause && fault_pc == pc)
            else $fatal(1, "Expected fault %d at %h, got %d at %h", cause, pc, fault, fault_pc);
        repeat (4) begin
            @(negedge clk);
            assert (!retired && fault == cause && fault_pc == pc)
                else $fatal(1, "Fault must stop the core until reset");
        end
    endtask

    initial begin
        image = '{default:32'hfffd_fffd};
        emit('h00, 'hd61d); // MOV.L @(0x74,PC),R6 (literal 0x78)
        emit('h02, 'he180); // MOV #-128,R1
        emit('h04, 'h2610); // MOV.B R1,@R6
        emit('h06, 'h7601); // ADD #1,R6
        emit('h08, 'h2610); // MOV.B R1,@R6
        emit('h0a, 'h6264); // MOV.B @R6+,R2
        emit('h0c, 'h2611); // MOV.W R1,@R6
        emit('h0e, 'h6361); // MOV.W @R6,R3
        emit('h10, 'h6465); // MOV.W @R6+,R4
        emit('h12, 'h2616); // MOV.L R1,@-R6
        emit('h14, 'h6666); // MOV.L @R6+,R6 (suppress postincrement)
        emit('h16, 'hd617); // MOV.L @(0x5c,PC),R6 (literal 0x74)
        emit('h18, 'h1611); // MOV.L R1,@(4,R6)
        emit('h1a, 'h5561); // MOV.L @(4,R6),R5
        emit('h1c, 'he07f); // MOV #127,R0
        emit('h1e, 'h8062); // MOV.B R0,@(2,R6)
        emit('h20, 'h8462); // MOV.B @(2,R6),R0
        emit('h22, 'h8163); // MOV.W R0,@(6,R6)
        emit('h24, 'h8563); // MOV.W @(6,R6),R0
        emit('h26, 'h887f); // CMP/EQ #127,R0
        emit('h28, 'h8d02); // BT/S 0x30
        emit('h2a, 'h0008); // CLRT in taken delay slot
        emit('h30, 'h8d06); // BT/S 0x40, untaken
        emit('h32, 'h1602); // MOV.L R0,@(8,R6), in the untaken delay slot
        emit('h34, 'hb014); // BSR 0x60
        emit('h36, 'he807); // MOV #7,R8
        emit('h38, 'h8800); // CMP/EQ #0,R0
        emit('h3a, 'h8b01); // BF 0x40, skip 0x3c without delay slot
        emit('h40, 'hd90b); // MOV.L @(0x2c,PC),R9 (literal 0x70)
        emit('h42, 'h9a14); // MOV.W @(0x28,PC),R10 (literal 0x6e)
        emit('h44, 'hc70a); // MOVA @(0x28,PC),R0
        emit('h46, 'h0009); // NOP
        emit('h48, 'he7ff); // MOV #-1,R7
        emit('h4a, 'he001); // MOV #1,R0
        emit('h4c, 'h370e); // ADDC R0,R7
        emit('h4e, 'ha003); // BRA 0x58
        emit('h50, 'h0129); // MOVT R1
        emit('h58, 'h370a); // SUBC R0,R7
        emit('h60, 'h7803); // ADD #3,R8
        emit('h62, 'h000b); // RTS
        emit('h64, 'h7802); // ADD #2,R8 in return slot
        emit('h6e, 'h8001);
        image['h70 >> 2] = 32'h1234_5678;
        image['h74 >> 2] = 32'h0000_0100;
        image['h78 >> 2] = 32'h0000_0200;
        image['h200 >> 2] = 32'b0;

        start_bus();
        for (int i = 0; i < 129; i++) begin
            @(negedge clk);
            load_we = 1;
            load_word_addr = 9'(i);
            load_data = image[i];
        end
        @(negedge clk);
        load_we = 0;
        bram_reset = 0;

        expect_stop(1, 'h5a);
        assert (retire_count == 41) else $fatal(1, "Unexpected retirement count %d", retire_count);
        assert (dut.gpr[1] == 1 && dut.gpr[2] == 32'hffff_ff80 &&
                dut.gpr[3] == 32'hffff_ff80 && dut.gpr[4] == 32'hffff_ff80 &&
                dut.gpr[5] == 32'hffff_ff80 && dut.gpr[6] == 'h100 &&
                dut.gpr[7] == 32'hffff_fffe && dut.gpr[8] == 12 &&
                dut.gpr[9] == 32'h1234_5678 && dut.gpr[10] == 32'hffff_8001 && dut.t)
            else $fatal(1, "Unexpected integer results R1=%h R6=%h R7=%h R8=%h R9=%h R10=%h",
                        dut.gpr[1], dut.gpr[6], dut.gpr[7], dut.gpr[8], dut.gpr[9], dut.gpr[10]);
        assert (ext_mem['h200 >> 2] == 32'hffff_ff80 && ext_mem['h100 >> 2][23:16] == 8'h7f &&
                ext_mem['h104 >> 2] == 32'h007f_ff80 && ext_mem['h108 >> 2] == 127)
            else $fatal(1, "Unexpected byte lanes or delayed store");
        assert (bram_fault == 1 && bram_fault_pc == 'h5a && !bram_retired && bram_retired_pc == 'h58 &&
                bram.cpu.gpr[5] == 32'hffff_ff80 && bram.cpu.gpr[7] == dut.gpr[7] &&
                bram.cpu.gpr[9] == dut.gpr[9])
            else $fatal(1, "Synchronous BRAM execution diverged from stalled bus");

        check_alias_load = 0;
        emit('h00, 'h0008);
        emit('h02, 'h8d01); // untaken delayed branch
        emit('h04, 'hc700); // PC-relative instruction is prohibited in either delay slot
        start_bus();
        expect_stop(2, 4);
        assert (retire_count == 2) else $fatal(1, "Faulting slot retired");

        emit('h00, 'he101);
        emit('h02, 'h2115); // MOV.W R1,@-R1, unaligned
        start_bus();
        expect_stop(4, 2);
        assert (dut.gpr[1] == 1 && retire_count == 1) else $fatal(1, "Fault wrote its base");

        emit('h00, 'he103);
        emit('h02, 'h412b); // JMP @R1, odd target
        emit('h04, 'h7102);
        start_bus();
        expect_stop(3, 3);
        assert (dut.gpr[1] == 5 && retire_count == 3) else $fatal(1, "Jump slot did not retire");

        // One cold line fill, followed by hits through forward execution and
        // a taken backward branch. Consecutive ALU ops overlap fetch.
        image = '{default:32'hfffd_fffd};
        emit('h00, 'he000); // MOV #0,R0
        emit('h02, 'h0009); // NOP
        emit('h04, 'h7001); // ADD #1,R0
        emit('h06, 'h8803); // CMP/EQ #3,R0
        emit('h08, 'h8bfc); // BF 0x04
        check_warm_loop = 1;
        start_bus();
        expect_stop(1, 'ha);
        assert (read_count == 8 && retire_count == 11 && dut.gpr[0] == 3)
            else $fatal(1, "Warm loop failed or reread its line (%0d reads)", read_count);
        check_warm_loop = 0;

        // Write the next instruction after it was fetched from the same line.
        image = '{default:32'hfffd_fffd};
        emit('h00, 'he306); // MOV #6,R3
        emit('h02, 'hd103); // MOV.L @(0x0c,PC),R1 (literal at 0x10)
        emit('h04, 'h2311); // MOV.W R1,@R3
        emit('h06, 'he20b); // Replaced by MOV #0x23,R2
        emit('h10, 'he223);
        start_bus();
        expect_stop(1, 'h8);
        assert (dut.gpr[2] == 'h23 && read_count == 24)
            else $fatal(1, "Self-modifying store gave opcode result %h with %0d reads", dut.gpr[2], read_count);

        // The call target shares a cache index with the caller. The return
        // must miss and use the caller tag and instruction bytes again.
        image = '{default:32'hfffd_fffd};
        emit('h00, 'hd303); // MOV.L @(0x0c,PC),R3 (literal at 0x10)
        emit('h02, 'h430b); // JSR @R3
        emit('h04, 'he001); // MOV #1,R0 (call slot)
        emit('h06, 'h7008); // ADD #8,R0 (return address)
        image['h10 >> 2] = 'h2000;
        emit('h2000, 'h7002);
        emit('h2002, 'h000b); // RTS
        emit('h2004, 'h7004); // ADD #4,R0 (return slot)
        start_bus();
        expect_stop(1, 'h8);
        assert (dut.gpr[0] == 15 && read_count == 32)
            else $fatal(1, "Conflicting instruction tags or redirect were mishandled");

        // Fetches to the P2 alias bypass the cache and read a single word.
        image['h10 >> 2] = 'ha000_2000;
        start_bus();
        expect_stop(1, 'h8);
        assert (dut.gpr[0] == 15 && read_count == 19)
            else $fatal(1, "Uncached P2 instruction fetch unexpectedly refilled");

        // Cache one operand line, write two partial words through it, and
        // confirm the modified bytes are served from M20K on subsequent hits.
        image = '{default:32'hfffd_fffd};
        emit('h00, 'hd311); // MOV.L @(0x44,PC),R3 (literal at 0x48)
        emit('h02, 'h6132); // MOV.L @R3,R1 (cold)
        emit('h04, 'h6232); // MOV.L @R3,R2 (warm)
        emit('h06, 'he0af); // MOV #-81,R0
        emit('h08, 'h8032); // MOV.B R0,@(2,R3)
        emit('h0a, 'h6c32); // MOV.L @R3,R12
        emit('h0c, 'he07f); // MOV #127,R0
        emit('h0e, 'h8130); // MOV.W R0,@(0,R3)
        emit('h10, 'h6532); // MOV.L @R3,R5
        emit('h12, 'hd60e); // MOV.L literal into R6 (0x4c)
        emit('h14, 'h6d62); // MOV.L @R6,R13, conflicting data index
        emit('h16, 'h6832); // MOV.L @R3,R8, refill original line
        emit('h18, 'hd70d); // MOV.L literal into R7 (0x50)
        emit('h1a, 'h6972); // MOV.L @R7,R9, uncached alias
        emit('h1c, 'he02b); // MOV #43,R0
        emit('h1e, 'h2702); // MOV.L R0,@R7, invalidate cached copy
        emit('h20, 'h6a32); // MOV.L @R3,R10, reload after alias write
        emit('h22, 'hd40c); // MOV.L literal into R4 (0x54)
        emit('h24, 'h2402); // MOV.L R0,@R4, cold write without allocation
        emit('h26, 'h6b42); // MOV.L @R4,R11, first fill of that line
        image['h48 >> 2] = 'h204;
        image['h4c >> 2] = 'h4204;
        image['h50 >> 2] = 'ha000_0204;
        image['h54 >> 2] = 'h604;
        image['h204 >> 2] = 'h1234_5678;
        image['h4204 >> 2] = 'h8765_4321;
        check_data_cache = 1;
        start_bus();
        expect_stop(1, 'h28);
        assert (dut.gpr[1] == 'h1234_5678 && dut.gpr[2] == 'h1234_5678 &&
                dut.gpr[12] == 'h12af_5678 && dut.gpr[5] == 'h12af_007f &&
                dut.gpr[13] == 'h8765_4321 && dut.gpr[8] == 'h12af_007f &&
                dut.gpr[9] == 'h12af_007f && dut.gpr[10] == 'h2b && dut.gpr[11] == 'h2b)
            else $fatal(1, "Operand cache lane update, conflict, or alias result was incorrect");
        assert (resident_reads == 24 && conflict_reads == 8 &&
                cold_store_line_reads == 8 && alias_reads == 1 &&
                ext_mem['h204 >> 2] == 'h2b && ext_mem['h604 >> 2] == 'h2b)
            else $fatal(1, "Unexpected external traffic: resident=%0d conflict=%0d cold=%0d alias=%0d",
                        resident_reads, conflict_reads, cold_store_line_reads, alias_reads);

        // Install entries through the CPU's P4 registers and LDTLB. Enable
        // translation, access a remapped operand, and jump into the mapped page.
        check_data_cache = 0;
        image = '{default:32'hfffd_fffd};
        literal('h00, 1, 'h80); // PTEH address
        literal('h02, 2, 'h84); // Virtual page zero, ASID 7
        emit('h04, 'h2122); // MOV.L R2,@R1
        literal('h06, 3, 'h88); // Physical page zero
        emit('h08, 'h1131); // MOV.L R3,@(4,R1) (PTEL)
        emit('h0a, 'h0038); // LDTLB
        literal('h0c, 4, 'h8c); // URC=1
        emit('h0e, 'h1144); // MOV.L R4,@(16,R1) (MMUCR)
        literal('h10, 2, 'h90); // Virtual page one
        emit('h12, 'h2122);
        literal('h14, 3, 'h94); // Physical page two
        emit('h16, 'h1131);
        emit('h18, 'h0038);
        emit('h1a, 'he401); // MOV #1,R4 (AT)
        emit('h1c, 'h1144);
        literal('h1e, 5, 'h98); // Operand VA
        emit('h20, 'h6652); // MOV.L @R5,R6
        emit('h22, 'he033);
        emit('h24, 'h8052); // MOV.B R0,@(2,R5)
        emit('h26, 'h6852);
        literal('h28, 9, 'h9c); // Target VA
        emit('h2a, 'h492b);
        emit('h2c, 'h0009);
        image['h80 >> 2] = 'hff00_0000;
        image['h84 >> 2] = 7;
        image['h88 >> 2] = 'h0000_016c;
        image['h8c >> 2] = 'h0000_0400;
        image['h90 >> 2] = 'h0000_0407;
        image['h94 >> 2] = 'h0000_096c;
        image['h98 >> 2] = 'h410;
        image['h9c >> 2] = 'h400;
        image['h810 >> 2] = 'h1234_5678;
        emit('h800, 'he92a); // Only reachable by translated instruction fetch.
        start_bus();
        expect_stop(1, 'h402);
        assert (dut.gpr[6] == 'h1234_5678 && dut.gpr[8] == 'h1233_5678 &&
                dut.gpr[9] == 'h2a && ext_mem['h810 >> 2] == 'h1233_5678)
            else $fatal(1, "CPU translation or cache use produced incorrect results");

        image['h98 >> 2] = 'hc10; // Leave this virtual page unmapped.
        start_bus();
        expect_stop(5, 'h20);
        assert (dut.mmu.tea == 'hc10) else $fatal(1, "Data miss did not retain its virtual address");

        image['h98 >> 2] = 'h410;
        image['h9c >> 2] = 'hc00;
        start_bus();
        expect_stop(5, 'hc00);
        assert (dut.mmu.tea == 'hc00) else $fatal(1, "Fetch miss did not retain its virtual address");

        // Keep the first page mapping from the MMU boot fixture. The handler
        // runs from a direct P2 alias and installs a mapping using exception PTEH.
        literal('h1e, 10, 'ha0);
        emit('h20, 'h4a2e); // LDC R10,VBR
        emit('h22, 'h0b22); // STC VBR,R11
        emit('h24, 'hef7c); // MOV #0x7c,R15, for SGR verification
        emit('h26, 'he001); // User SR with T set
        emit('h28, 'h400e); // LDC R0,SR (enable exceptions, select bank zero)
        literal('h2a, 5, 'h98);
        emit('h2c, 'he015); // Distinct user R0
        emit('h2e, 'h6652); // MOV.L @R5,R6, initially faults
        emit('h30, 'h0729); // MOVT R7
        emit('h32, 'hc317); // TRAPA #0x17, general vector
        image['h98 >> 2] = 'hc10;
        image['ha0 >> 2] = 'ha000_1000;
        image['h1010 >> 2] = 'h1357_2468;

        emit('h1400, 'h0842); // STC SPC,R8
        emit('h1402, 'h0932); // STC SSR,R9
        emit('h1404, 'h0a3a); // STC SGR,R10
        literal('h1406, 1, 'h1440);
        emit('h1408, 'h5c19); // MOV.L @(0x24,R1),R12 (EXPEVT)
        emit('h140a, 'h5d13); // MOV.L @(0x0c,R1),R13 (TEA)
        literal('h140c, 2, 'h1444);
        emit('h140e, 'h1121); // MOV.L R2,@(4,R1) (PTEL)
        emit('h1410, 'h0038); // LDTLB
        emit('h1412, 'h493e); // LDC R9,SSR
        emit('h1414, 'h484e); // LDC R8,SPC
        emit('h1416, 'he0ff); // Handler R0 is in bank one
        emit('h1418, 'h002b); // RTE
        emit('h141a, 'h7001); // ADD #1,R0 using the restored bank
        image['h1440 >> 2] = 'hff00_0000;
        image['h1444 >> 2] = 'h0000_116c;
        literal('h1100, 1, 'h1110);
        emit('h1102, 'h5e18); // MOV.L @(0x20,R1),R14 (TRA)
        image['h1110 >> 2] = 'hff00_0000;

        start_bus();
        expect_stop(1, 'ha000_1104);
        assert (dut.gpr_storage[0] == 'h16 && dut.gpr_storage[16] == 'hffff_ffff &&
                dut.gpr_storage[6] == 'h1357_2468 && dut.gpr_storage[7] == 1 &&
                dut.gpr[8] == 'h2e && dut.gpr[9] == 1 && dut.gpr[10] == 'h7c &&
                dut.gpr[11] == 'ha000_1000 && dut.gpr[12] == 'h40 && dut.gpr[13] == 'hc10 &&
                dut.gpr[14] == 'h5c && dut.spc == 'h34 && dut.ssr == 1)
            else $fatal(1, "Read miss, RTE slot, banked registers or TRAPA did not resume correctly");

        emit('h2e, 'h452b); // JMP @R5, missing instruction page
        emit('h30, 'h0009);
        emit('h1010, 'he64d); // First opcode at the translated target
        emit('h1012, 'hc317);
        start_bus();
        expect_stop(1, 'ha000_1104);
        assert (dut.gpr_storage[6] == 'h4d && dut.gpr[8] == 'hc10 && dut.gpr[12] == 'h40 &&
                dut.gpr[13] == 'hc10 && dut.spc == 'hc14)
            else $fatal(1, "Instruction miss did not restart the remapped fetch");

        emit('h2e, 'hb007); // BSR 0x40
        emit('h30, 'h2502); // MOV.L R0,@R5 in the delay slot (store miss)
        emit('h40, 'hc317);
        start_bus();
        expect_stop(1, 'ha000_1104);
        assert (dut.gpr[8] == 'h2e && dut.gpr[12] == 'h60 && dut.gpr[13] == 'hc10 &&
                dut.pr == 'h32 && dut.spc == 'h42 && retried_word_writes == 1 &&
                ext_mem['h1010 >> 2] == 'h16)
            else $fatal(1, "Store miss did not replay the branch and store exactly once");

        // Supervisor can read SR and clear MD. A user execution of LDTLB
        // must fault before changing the TLB state.
        image = '{default:32'hfffd_fffd};
        emit('h00, 'h0102); // STC SR,R1
        emit('h02, 'he000); // MOV #0,R0
        emit('h04, 'h400e); // LDC R0,SR
        emit('h06, 'h0038); // LDTLB
        start_bus();
        expect_stop(1, 'h100);
        assert (dut.gpr_storage[17] == 'h7000_00f0 && dut.spc == 6 &&
                dut.ssr == 0 && dut.mmu.utlb_valid == 0)
            else $fatal(1, "Supervisor-only MMU control was not enforced");

        // Signed overflow, subtract-with-borrow, rotate through T, and the
        // negative multiple-of-32 register-controlled shift special case.
        image = '{default:32'hfffd_fffd};
        literal('h00, 1, 'h80);
        emit('h02, 'he201); // MOV #1,R2
        emit('h04, 'h312f); // ADDV R2,R1
        emit('h06, 'h0829); // MOVT R8
        emit('h08, 'h312b); // SUBV R2,R1
        emit('h0a, 'h0929); // MOVT R9
        emit('h0c, 'h632a); // NEGC R2,R3
        emit('h0e, 'h4324); // ROTCL R3
        emit('h10, 'he4e0); // MOV #-32,R4
        emit('h12, 'h6533); // MOV R3,R5
        emit('h14, 'h454c); // SHAD R4,R5
        emit('h16, 'h6633); // MOV R3,R6
        emit('h18, 'h464d); // SHLD R4,R6
        emit('h1a, 'h673c); // EXTU.B R3,R7
        emit('h1c, 'h6a3f); // EXTS.W R3,R10
        emit('h1e, 'he000); // MOV #0,R0
        emit('h20, 'hcb80); // OR #0x80,R0
        emit('h22, 'hca01); // XOR #1,R0
        emit('h24, 'hc981); // AND #0x81,R0
        emit('h26, 'hc802); // TST #2,R0
        emit('h28, 'h0b29); // MOVT R11
        emit('h2a, 'h4005); // ROTR R0
        emit('h2c, 'h6c08); // SWAP.B R0,R12
        emit('h2e, 'h6d09); // SWAP.W R0,R13
        emit('h30, 'h2dcd); // XTRCT R12,R13
        emit('h32, 'he000); // MOV #0,R0
        emit('h34, 'h4010); // DT R0 (wrap)
        emit('h36, 'h4015); // CMP/PL R0
        emit('h38, 'h0e29); // MOVT R14
        image['h80 >> 2] = 'h7fff_ffff;
        start_bus();
        expect_stop(1, 'h3a);
        assert (dut.gpr[1] == 'h7fff_ffff && dut.gpr[8] == 1 && dut.gpr[9] == 1 &&
                dut.gpr[3] == 'hffff_fffd && dut.gpr[5] == 'hffff_ffff && dut.gpr[6] == 0 &&
                dut.gpr[7] == 253 && dut.gpr[10] == 'hffff_fffd && dut.gpr[11] == 1 &&
                dut.gpr[12] == 'h8000_4000 && dut.gpr[13] == 'h4000_0040 &&
                dut.gpr[0] == 'hffff_ffff && dut.gpr[14] == 0)
            else $fatal(1, "Extended integer flags, shifts or register destinations were incorrect");

        // The software division sequences use Q/M/T across 32 DIV1 steps.
        image = '{default:32'hfffd_fffd};
        emit('h00, 'he007); // Divisor 7 in R0
        emit('h02, 'he100); // High dividend in R1
        emit('h04, 'he264); // Low dividend 100
        emit('h06, 'h0019); // DIV0U
        for (int step = 0; step < 32; step++) begin
            emit('h08 + 4*step, 'h4224); // ROTCL R2
            emit('h0a + 4*step, 'h3104); // DIV1 R0,R1
        end
        emit('h88, 'h4224);
        emit('h8a, 'he300); // Zero for signed-quotient correction
        emit('h8c, 'h323e); // ADDC R3,R2
        start_bus();
        expect_stop(1, 'h8e);
        assert (dut.gpr[2] == 14) else $fatal(1, "Unsigned DIV1 quotient is %h", dut.gpr[2]);

        emit('h02, 'he1ff); // Sign extension for a negative dividend
        emit('h04, 'he29b); // One's complement representation of -100
        emit('h06, 'h2107); // DIV0S R0,R1
        start_bus();
        expect_stop(1, 'h8e);
        assert (dut.gpr[2] == 'hffff_fff2) else $fatal(1, "Signed DIV1 quotient is %h", dut.gpr[2]);

        // Exercise DSP partial products with opposite signed/unsigned high
        // words, and verify short multiplies leave MACH intact.
        image = '{default:32'hfffd_fffd};
        literal('h00, 1, 'h80);
        emit('h02, 'he2ff);
        emit('h04, 'h312d); // DMULS.L R2,R1
        emit('h06, 'h080a); // STS MACH,R8
        emit('h08, 'h091a); // STS MACL,R9
        emit('h0a, 'h3125); // DMULU.L R2,R1
        emit('h0c, 'h0a0a);
        emit('h0e, 'h0b1a);
        literal('h10, 1, 'h84);
        emit('h12, 'h212f); // MULS.W R2,R1
        emit('h14, 'h0c1a);
        emit('h16, 'h212e); // MULU.W R2,R1
        emit('h18, 'h0d1a);
        emit('h1a, 'h0127); // MUL.L R2,R1
        emit('h1c, 'h0e1a);
        emit('h1e, 'h0f0a);
        image['h80 >> 2] = 'h8000_0000;
        image['h84 >> 2] = 'h0000_8000;
        start_bus();
        expect_stop(1, 'h20);
        assert (dut.gpr[8] == 0 && dut.gpr[9] == 'h8000_0000 &&
                dut.gpr[10] == 'h7fff_ffff && dut.gpr[11] == 'h8000_0000 &&
                dut.gpr[12] == 'h0000_8000 && dut.gpr[13] == 'h7fff_8000 &&
                dut.gpr[14] == 'hffff_8000 && dut.gpr[15] == 'h7fff_ffff)
            else $fatal(1, "Multiply signedness, width, or MACH preservation was incorrect");

        // A nested call preserves PR via its normal stack instructions.
        // Its callee uses both indexed and GBR-relative data addressing.
        image = '{default:32'hfffd_fffd};
        literal('h0, 15, 'h70);
        emit('h02, 'he308); // Offset from PC+4 to outer callee
        emit('h04, 'h0303); // BSRF R3
        emit('h06, 'h0009);
        emit('h10, 'h4f22); // STS.L PR,@-R15
        emit('h12, 'hb009); // BSR 0x28
        emit('h14, 'h0009);
        emit('h16, 'h4f26); // LDS.L @R15+,PR
        emit('h18, 'h000b); // RTS
        emit('h1a, 'h0009);
        literal('h28, 3, 'h74);
        emit('h2a, 'h431e); // LDC R3,GBR
        emit('h2c, 'h0412); // STC GBR,R4
        emit('h2e, 'he03d);
        emit('h30, 'hc201); // MOV.L R0,@(4,GBR)
        emit('h32, 'he004);
        emit('h34, 'h073e); // MOV.L @(R0,R3),R7
        emit('h36, 'he659);
        emit('h38, 'h0366); // MOV.L R6,@(R0,R3)
        emit('h3a, 'hc601); // MOV.L @(4,GBR),R0
        emit('h3c, 'h000b);
        emit('h3e, 'h0009);
        image['h70 >> 2] = 'h300;
        image['h74 >> 2] = 'h200;
        start_bus();
        expect_stop(1, 'h8);
        assert (dut.gpr[4] == 'h200 && dut.gpr[7] == 'h3d && dut.gpr[0] == 'h59 &&
                dut.gpr[15] == 'h300 && ext_mem['h2fc >> 2] == 8)
            else $fatal(1, "Call linkage or indexed and GBR-relative memory was incorrect");

        assert (mmu_done) else $fatal(1, "MMU unit check did not finish");
        assert (cache_cancel_done) else $fatal(1, "Cache cancellation check did not finish");

        $display("PASS SH-4 integer, stalls, delay slots, caches, SH-4 MMU control, page sizes, permissions, exception return and extended integer execution");
        $finish;
    end

    initial begin
        #180000;
        $fatal(1, "Timed out");
    end
endmodule
