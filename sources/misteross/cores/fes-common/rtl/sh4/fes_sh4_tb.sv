module fes_sh4_tb;
    logic clk = 0;
    always #5 clk <= ~clk;

    logic reset = 1, bram_reset = 1, load_we = 0;
    logic [8:0] load_word_addr;
    logic [31:0] load_data;
    logic valid, ready, rvalid, retired, bram_retired, bram_irq_ack;
    logic [3:0] irq_level = 0;
    logic [11:0] irq_event = 0;
    logic nmi = 0, nmi_while_blocked = 0, irq_ack;
    logic [31:0] addr, wdata, rdata, retired_pc, fault_pc, bram_retired_pc, bram_fault_pc;
    logic [3:0] fault, bram_fault;
    logic locked, queue_write;
    logic [3:0] wstrb, rmask;
    logic [31:0] image [0:8191];
    logic [31:0] ext_mem [0:8191];
    int cycle, latency;
    logic held;
    logic [73:0] held_request;
    int retire_count, read_count, previous_retire_cycle;
    int retried_word_writes, locked_reads, locked_writes, mapped_operand_reads;
    logic lock_open;
    logic [31:0] lock_addr;
    logic [3:0] lock_lane;
    logic [2:0] queue_beat;
    logic [31:5] queue_line;
    int resident_reads, resident_writes, conflict_reads, cold_store_line_reads, alias_reads;
    logic check_data_cache = 0;
    logic check_qacr_area = 0;
    int qacr_area_writes, queue_zero_writes, mapped_queue_writes;
    logic cache_cancel_done, mmu_done;

    fes_sh4_mmu_tb mmu_test (.clk, .done(mmu_done));
    fes_sh4_icache_tb cache_cancel (.clk, .done(cache_cancel_done));
    logic check_warm_loop = 0, check_alias_load = 1;

    fes_sh4 #(.RESET_PC(0), .RESET_CCR(9'h103)) dut (
        .clk, .reset, .irq_level, .irq_event, .nmi, .nmi_while_blocked, .irq_ack,
        .mem_valid(valid), .mem_lock(locked), .mem_sq(queue_write), .mem_rmask(rmask), .mem_ready(ready),
        .mem_addr(addr), .mem_wstrb(wstrb), .mem_wdata(wdata),
        .mem_rvalid(rvalid), .mem_rdata(rdata),
        .retired, .retired_pc, .fault, .fault_pc
    );

    fes_sh4_bram_system bram (
        .clk, .reset(bram_reset), .irq_level(4'b0), .irq_event(12'b0),
        .nmi(1'b0), .nmi_while_blocked(1'b0), .irq_ack(bram_irq_ack), .load_we, .load_word_addr, .load_data,
        .retired(bram_retired), .retired_pc(bram_retired_pc),
        .fault(bram_fault), .fault_pc(bram_fault_pc)
    );

    assign ready = cycle % 5 == 4;
    always @(posedge clk)
        if (!bram_reset)
            assert (!bram_irq_ack) else $fatal(1, "Unexpected diagnostic interrupt acknowledge");

    always @(posedge clk) begin
        rvalid <= 0;
        if (reset) begin
            cycle <= 0;
            latency <= 0;
            held <= 0;
            retire_count <= 0;
            read_count <= 0;
            retried_word_writes <= 0;
            qacr_area_writes <= 0;
            queue_zero_writes <= 0;
            mapped_queue_writes <= 0;
            locked_reads <= 0;
            mapped_operand_reads <= 0;
            locked_writes <= 0;
            lock_open <= 0;
            queue_beat <= 0;
            resident_reads <= 0;
            resident_writes <= 0;
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
                assert (valid && {addr, wdata, wstrb, locked, queue_write, rmask} == held_request)
                    else $fatal(1, "Request changed while backpressured");
            held <= valid && !ready;
            held_request <= {addr, wdata, wstrb, locked, queue_write, rmask};

            if (latency != 0) begin
                latency <= latency - 1;
                rvalid <= latency == 1;
            end
            if (lock_open)
                assert (locked) else $fatal(1, "Lock dropped before the TAS.B write was accepted");
            if (valid && ready && locked) begin
                if (!lock_open) begin
                    assert (wstrb == 0 && $onehot(rmask))
                        else $fatal(1, "TAS.B did not start with a single-byte read");
                    lock_open <= 1;
                    lock_addr <= addr;
                    lock_lane <= rmask;
                    locked_reads <= locked_reads + 1;
                end else begin
                    assert (addr == lock_addr && wstrb == lock_lane)
                        else $fatal(1, "An unrelated bus request interrupted TAS.B");
                    lock_open <= 0;
                    locked_writes <= locked_writes + 1;
                end
            end
            if (valid && ready) begin
                if (queue_beat != 0)
                    assert (queue_write && queue_line == addr[31:5])
                        else $fatal(1, "A foreign request interrupted a store queue transfer");
                if (queue_write) begin
                    assert (wstrb == 4'hf && addr[4:2] == queue_beat)
                        else $fatal(1, "Invalid store queue write beat");
                    queue_line <= addr[31:5];
                    queue_beat <= queue_beat + 3'd1;
                end
                assert (latency == 0 && addr[1:0] == 0 && addr[25:15] == 0 &&
                        (addr[28:26] == 0 || (check_qacr_area && addr[31:5] == 27'h0600011 && wstrb == 4'hf)))
                    else $fatal(1, "Unexpected memory request at %h", addr);
                if (wstrb == 0) begin
                    rdata <= ext_mem[addr[14:2]];
                    read_count <= read_count + 1;
                    case (addr & 32'hffff_ffe0)
                        32'h0000_0200: resident_reads <= resident_reads + 1;
                        32'h0000_4200: conflict_reads <= conflict_reads + 1;
                        32'h0000_0600: cold_store_line_reads <= cold_store_line_reads + 1;
                        32'h0000_0800: mapped_operand_reads <= mapped_operand_reads + 1;
                        32'ha000_0200: alias_reads <= alias_reads + 1;
                        default: ;
                    endcase
                    latency <= 1 + cycle % 3;
                end else begin
                    if (check_data_cache && addr == 32'h604)
                        assert (cold_store_line_reads == 0)
                            else $fatal(1, "A store miss fetched a line before writing through");
                    if (queue_write && addr[31:5] == 27'h0600011)
                        qacr_area_writes <= qacr_area_writes + 1;
                    if (queue_write && addr[31:5] == 27'h10)
                        queue_zero_writes <= queue_zero_writes + 1;
                    if (queue_write && addr[31:5] == 27'h80)
                        mapped_queue_writes <= mapped_queue_writes + 1;
                    if (addr[31:5] == 27'h10)
                        resident_writes <= resident_writes + 1;
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
        image['h88 >> 2] = 'h0000_016d;
        image['h8c >> 2] = 'h0000_0400;
        image['h90 >> 2] = 'h0000_0407;
        image['h94 >> 2] = 'h0000_096d;
        image['h98 >> 2] = 'h410;
        image['h9c >> 2] = 'h400;
        image['h810 >> 2] = 'h1234_5678;
        emit('h800, 'he92a); // Only reachable by translated instruction fetch.
        start_bus();
        expect_stop(1, 'h402);
        assert (dut.gpr[6] == 'h1234_5678 && dut.gpr[8] == 'h1233_5678 &&
                dut.gpr[9] == 'h2a && ext_mem['h810 >> 2] == 'h1233_5678)
            else $fatal(1, "CPU translation or cache use produced incorrect results");

        // A translated page's WT bit overrides the CCR's direct-area mode.
        image['h94 >> 2] = 'h0000_096c;
        emit('h20, 'he05a);
        emit('h22, 'h2500); // MOV.B R0,@R5
        emit('h24, 'h6852); // Read the updated cache word.
        emit('h26, 'hfffd);
        start_bus();
        expect_stop(1, 'h26);
        assert (dut.gpr[8] == 'h1234_565a && ext_mem['h810 >> 2] == 'h1234_5678 &&
                mapped_operand_reads == 8)
            else $fatal(1, "TLB WT=0 did not select copy-back over CCR.WT");
        emit('h20, 'h6652);
        emit('h22, 'he033);
        emit('h24, 'h8052);
        emit('h26, 'h6852);

        // A TAS.B lookup is a store permission check. It must never issue
        // even the read on a read-only or clean page.
        emit('h20, 'h451b); // TAS.B @R5 (VA 0x410)
        image['h810 >> 2] = 'h1122_3300;
        image['h94 >> 2] = 'h0000_0969; // Writable, D=0
        start_bus();
        expect_stop(8, 'h20);
        assert (dut.expevt == 'h080 && dut.mmu.tea == 'h410 && dut.sr[0] == 0 &&
                mapped_operand_reads == 0 && locked_reads == 0 && ext_mem['h810 >> 2] == 'h1122_3300)
            else $fatal(1, "Initial-write exception issued a TAS.B access or changed T");

        image['h94 >> 2] = 'h0000_094d; // Read-only, D=1
        start_bus();
        expect_stop(7, 'h20);
        assert (dut.expevt == 'h0c0 && dut.mmu.tea == 'h410 && dut.sr[0] == 0 &&
                mapped_operand_reads == 0 && locked_reads == 0 && ext_mem['h810 >> 2] == 'h1122_3300)
            else $fatal(1, "Protected TAS.B issued a memory access or changed T");

        // OCBI uses write permission even on a cache miss. OCBP and
        // OCBWB use read permission and issue no fills on a cache miss.
        emit('h20, 'h0593); // OCBI @R5
        start_bus();
        expect_stop(7, 'h20);
        assert (dut.expevt == 'h0c0 && dut.mmu.tea == 'h410 && mapped_operand_reads == 0)
            else $fatal(1, "OCBI did not check store permission on a cache miss");
        emit('h20, 'h05a3); // OCBP @R5
        emit('h22, 'h05b3); // OCBWB @R5
        emit('h24, 'hfffd);
        start_bus();
        expect_stop(1, 'h24);
        assert (mapped_operand_reads == 0 && ext_mem['h810 >> 2] == 'h1122_3300)
            else $fatal(1, "OCBP or OCBWB rejected a read-only page or performed a refill");

        // Read-only byte test still succeeds through that same mapping.
        emit('h20, 'h451e); // LDC R5,GBR
        emit('h22, 'he000);
        emit('h24, 'hcc80); // TST.B #0x80,@(R0,GBR)
        emit('h26, 'h0829); // MOVT R8
        emit('h28, 'hfffd);
        start_bus();
        expect_stop(1, 'h28);
        assert (dut.gpr[8] == 1 && mapped_operand_reads == 8 && locked_reads == 0 &&
                ext_mem['h810 >> 2] == 'h1122_3300)
            else $fatal(1, "TST.B did not behave as a read");
        emit('h24, 'hcf80); // OR.B #0x80,@(R0,GBR)
        start_bus();
        expect_stop(7, 'h24);
        assert (dut.expevt == 'h0c0 && dut.mmu.tea == 'h410 && mapped_operand_reads == 0 &&
                ext_mem['h810 >> 2] == 'h1122_3300)
            else $fatal(1, "Protected OR.B issued a memory access");

        image['h94 >> 2] = 'h0000_096d;
        emit('h20, 'h6652);
        emit('h22, 'he033);
        emit('h24, 'h8052);
        emit('h26, 'h6852);
        literal('h28, 9, 'h9c);

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
        image['h1444 >> 2] = 'h0000_116d;
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

        // The first MAC.L read hits an existing mapping. The second misses.
        // Inspect the register state from the handler before retrying via RTE.
        emit('h2c, 'he660); // First operand at VA 0x60
        emit('h2e, 'h0028); // CLRMAC
        emit('h30, 'h065f); // MAC.L @R5+,@R6+
        emit('h32, 'hc317);
        image['h60 >> 2] = 32'hffff_fffd;
        image['h1010 >> 2] = 7;
        emit('h1416, 'h021a); // STS MACL,R2 while handling the miss
        start_bus();
        wait (retired && retired_pc == 'ha000_1416);
        assert (dut.gpr_storage[5] == 'hc10 && dut.gpr_storage[6] == 'h60 &&
                dut.gpr[2] == 0 && dut.mach == 0)
            else $fatal(1, "MAC.L changed state before the second read");
        expect_stop(1, 'ha000_1104);
        assert (dut.gpr[8] == 'h30 && dut.gpr[12] == 'h40 && dut.gpr[13] == 'hc10 &&
                dut.gpr_storage[5] == 'hc14 &&
                dut.gpr_storage[6] == 'h64 && {dut.mach, dut.macl} == -64'd21)
            else $fatal(1, "MAC.L failed to retry the second operand fault");

        // User GBR stack access retries its translated read and increments
        // the source only after the handler installs the mapping.
        emit('h2e, 'h4517); // LDC.L @R5+,GBR
        emit('h30, 'h0612); // STC GBR,R6
        start_bus();
        expect_stop(1, 'ha000_1104);
        assert (dut.gpr[8] == 'h2e && dut.gpr[12] == 'h40 && dut.gpr[13] == 'hc10 &&
                dut.gpr_storage[5] == 'hc14 && dut.gpr_storage[6] == 7)
            else $fatal(1, "LDC.L did not retry through a TLB miss");

        emit('h2e, 'h451b); // TAS.B @R5, initially unmapped
        emit('h30, 'h0629); // MOVT R6
        image['h1010 >> 2] = 'h1122_3300;
        start_bus();
        expect_stop(1, 'ha000_1104);
        assert (dut.gpr[8] == 'h2e && dut.gpr[12] == 'h60 && dut.gpr[13] == 'hc10 &&
                dut.gpr_storage[6] == 1 && locked_reads == 1 && locked_writes == 1 &&
                retried_word_writes == 1 && ext_mem['h1010 >> 2] == 'h1122_3380)
            else $fatal(1, "TAS.B write-class miss did not retry exactly once");

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
        emit('h06, 'h001b); // SLEEP is also privileged.
        start_bus();
        expect_stop(1, 'h100);
        assert (dut.spc == 6 && dut.ssr == 0)
            else $fatal(1, "User SLEEP did not take an illegal instruction exception");

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

        // Same-register operands consume two consecutive addresses. The
        // second MAC.W uses separate bases and carries across MACL.
        image = '{default:32'hfffd_fffd};
        emit('h00, 'he140);
        emit('h02, 'he244);
        emit('h04, 'h0028);
        emit('h06, 'h411f); // MAC.W @R1+,@R1+
        emit('h08, 'h080a); // STS MACH,R8
        emit('h0a, 'h091a); // STS MACL,R9
        emit('h0c, 'h412f); // MAC.W @R2+,@R1+
        emit('h0e, 'h0a0a);
        emit('h10, 'h0b1a);
        image['h40 >> 2] = 32'h0003_fffe; // -2 * 3
        image['h44 >> 2] = 32'h0002_0005; // 5 * 5
        start_bus();
        expect_stop(1, 'h12);
        assert (dut.gpr[1] == 'h46 && dut.gpr[2] == 'h46 &&
                dut.gpr[8] == 'hffff_ffff && dut.gpr[9] == 'hffff_fffa &&
                dut.gpr[10] == 0 && dut.gpr[11] == 19)
            else $fatal(1, "MAC.W sign, carry or shared-base postincrement was incorrect");

        emit('h02, 'he245); // Misalign the second MAC.W's other operand
        start_bus();
        expect_stop(4, 'hc);
        assert (dut.mmu.tea == 'h45 && dut.gpr[1] == 'h44 && dut.gpr[2] == 'h45 &&
                {dut.mach, dut.macl} == -64'd6)
            else $fatal(1, "Misaligned second MAC.W operand changed architectural state");

        // Saturation at both limits. The MACH upper half survives MAC.L
        // saturation and the MACH low bit records MAC.W overflow.
        image = '{default:32'hfffd_fffd};
        emit('h00, 'h0058); // SETS
        literal('h02, 3, 'h80);
        emit('h04, 'h430a); // LDS R3,MACH
        literal('h06, 3, 'h84);
        emit('h08, 'h431a); // LDS R3,MACL
        emit('h0a, 'he150);
        emit('h0c, 'h011f); // MAC.L @R1+,@R1+
        emit('h0e, 'h080a);
        emit('h10, 'h091a);
        literal('h12, 3, 'h88);
        emit('h14, 'h430a);
        emit('h16, 'he300);
        emit('h18, 'h431a);
        emit('h1a, 'h011f); // MAC.L @R1+,@R1+
        emit('h1c, 'h0a0a);
        emit('h1e, 'h0b1a);
        literal('h20, 3, 'h8c);
        emit('h22, 'h431a);
        emit('h24, 'h411f); // MAC.W @R1+,@R1+
        emit('h26, 'h0c0a);
        emit('h28, 'h0d1a);
        literal('h2a, 3, 'h90);
        emit('h2c, 'h431a);
        emit('h2e, 'h411f);
        emit('h30, 'h0e1a);
        image['h50 >> 2] = 1;
        image['h54 >> 2] = 2;
        image['h58 >> 2] = 32'hffff_ffff;
        image['h5c >> 2] = 1;
        image['h60 >> 2] = 32'h0002_0001;
        image['h64 >> 2] = 32'h0001_ffff;
        image['h80 >> 2] = 32'hcafe_7fff;
        image['h84 >> 2] = 32'hffff_fffe;
        image['h88 >> 2] = 32'hbeef_8000;
        image['h8c >> 2] = 32'h7fff_ffff;
        image['h90 >> 2] = 32'h8000_0000;
        start_bus();
        expect_stop(1, 'h32);
        assert (dut.gpr[8] == 'hcafe_7fff && dut.gpr[9] == 'hffff_ffff &&
                dut.gpr[10] == 'hbeef_8000 && dut.gpr[11] == 0 &&
                dut.gpr[12] == 'hbeef_8001 && dut.gpr[13] == 'h7fff_ffff &&
                dut.gpr[14] == 'h8000_0000 && dut.gpr[1] == 'h68)
            else $fatal(1, "MAC saturation limits, MACH bits or long postincrement were incorrect");

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

        // TAS.B bypasses a warm operand cache and locks only its addressed
        // byte. The immediate GBR forms use the same byte lane and preserve
        // the other bytes in the word.
        image = '{default:32'hfffd_fffd};
        literal('h00, 1, 'h80);
        literal('h02, 3, 'h84);
        emit('h04, 'h431e); // LDC R3,GBR
        emit('h06, 'he003);
        emit('h08, 'h6410); // Warm D-cache with MOV.B @R1,R4
        emit('h0a, 'h411b); // TAS.B @R1, test zero
        emit('h0c, 'h0829);
        emit('h0e, 'h411b); // TAS.B @R1, test nonzero
        emit('h10, 'h0929);
        emit('h12, 'hcc01); // TST.B #1,@(R0,GBR)
        emit('h14, 'h0a29);
        emit('h16, 'hcf05); // OR.B #5,@(R0,GBR)
        emit('h18, 'hce0f); // XOR.B #15,@(R0,GBR)
        emit('h1a, 'hcd0f); // AND.B #15,@(R0,GBR)
        emit('h1c, 'h0b29); // Capture T, preserved by OR/XOR/AND
        emit('h1e, 'hcc08); // TST.B #8,@(R0,GBR)
        emit('h20, 'h0c29);
        emit('h22, 'h6d10);
        image['h80 >> 2] = 'h203;
        image['h84 >> 2] = 'h200;
        image['h200 >> 2] = 'h0012_3456;
        start_bus();
        expect_stop(1, 'h24);
        assert (dut.gpr[4] == 0 && dut.gpr[8] == 1 && dut.gpr[9] == 0 &&
                dut.gpr[10] == 1 && dut.gpr[11] == 1 && dut.gpr[12] == 0 &&
                dut.gpr[13] == 'h0a && ext_mem['h200 >> 2] == 'h0a12_3456 &&
                locked_reads == 2 && locked_writes == 2 && resident_reads == 18)
            else $fatal(1, "TAS.B or GBR byte logic violated memory or flag semantics");

        // A byte store allocates in copy-back mode and leaves memory
        // untouched until it is evicted. TAS.B first writes back a dirty alias.
        image = '{default:32'hfffd_fffd};
        literal('h00, 1, 'h80); // CCR
        literal('h02, 2, 'h84);
        emit('h04, 'h2122); // CCR=ICE|OCE, direct-area copy-back
        emit('h06, 'h6812); // Read back CCR
        literal('h08, 3, 'h88); // 0x201
        literal('h0a, 4, 'h8c); // 0x4200, same cache index
        emit('h0c, 'he66a);
        emit('h0e, 'h2360); // Copy-back byte store
        emit('h10, 'h6942); // Force a dirty eviction
        emit('h12, 'h7301);
        emit('h14, 'h2360); // Dirty the first line again
        literal('h16, 5, 'h90); // P2 alias of 0x202
        emit('h18, 'h451b); // TAS.B must purge before locking
        emit('h1a, 'h0a29);
        emit('h1c, 'he637);
        emit('h1e, 'h2360); // Dirties the line without touching external memory
        literal('h20, 2, 'h94);
        emit('h22, 'h2122); // OCI discards this dirty line
        emit('h24, 'h6b12);
        emit('h26, 'h6c30);
        image['h80 >> 2] = 'hff00_001c;
        image['h84 >> 2] = 'h101;
        image['h88 >> 2] = 'h201;
        image['h8c >> 2] = 'h4200;
        image['h90 >> 2] = 'ha000_0202;
        image['h94 >> 2] = 'h109;
        image['h200 >> 2] = 'h4433_2211;
        image['h204 >> 2] = 'h5566_7788;
        image['h4200 >> 2] = 'h0bad_cafe;
        start_bus();
        wait (retired && retired_pc == 'h0e);
        assert (ext_mem['h200 >> 2] == 'h4433_2211 && resident_reads == 8)
            else $fatal(1, "Copy-back store did not allocate without writing through");
        wait (retired && retired_pc == 'h10);
        assert (ext_mem['h200 >> 2] == 'h4433_6a11 && ext_mem['h204 >> 2] == 'h5566_7788)
            else $fatal(1, "Dirty eviction wrote incorrect bytes or an incorrect victim address");
        expect_stop(1, 'h28);
        assert (dut.gpr[8] == 'h101 && dut.gpr[9] == 'h0bad_cafe && dut.gpr[10] == 0 &&
                dut.gpr[11] == 'h101 && dut.gpr[12] == 'hffff_ffea &&
                ext_mem['h200 >> 2] == 'h44ea_6a11 && locked_reads == 1 && locked_writes == 1)
            else $fatal(1, "Dirty TAS.B purge, OCI or operand cache mode was incorrect");

        // The P1 copy-back bit is independent of the write-through mode
        // for direct P0. Disabling the caches also bypasses line allocation.
        image = '{default:32'hfffd_fffd};
        literal('h00, 1, 'h80); // CCR
        literal('h02, 2, 'h84);
        emit('h04, 'h2122); // ICE|OCE|WT|CB
        literal('h06, 3, 'h88); // P1 data address
        literal('h08, 4, 'h8c); // Direct P0 address at a different index
        emit('h0a, 'he625);
        emit('h0c, 'h2362); // Copy-back longword store in P1
        emit('h0e, 'h2462); // Write-through longword store in P0
        emit('h10, 'he200);
        emit('h12, 'h2122); // Disable both caches
        emit('h14, 'h6812); // Read CCR
        emit('h16, 'h6932); // Uncached read of the dirty P1 alias
        emit('h18, 'h6a42);
        image['h80 >> 2] = 'hff00_001c;
        image['h84 >> 2] = 'h107;
        image['h88 >> 2] = 'h8000_0200;
        image['h8c >> 2] = 'h600;
        image['h200 >> 2] = 'h77;
        image['h600 >> 2] = 'h11;
        start_bus();
        wait (retired && retired_pc == 'h0e);
        assert (ext_mem['h200 >> 2] == 'h77 && ext_mem['h600 >> 2] == 'h25)
            else $fatal(1, "CCR did not select independent P1 and P0 write modes");
        expect_stop(1, 'h1a);
        assert (dut.gpr[8] == 0 && dut.gpr[9] == 'h25 && dut.gpr[10] == 'h25 &&
                ext_mem['h200 >> 2] == 'h25 && cold_store_line_reads == 1)
            else $fatal(1, "Cache enable bits or dirty-data preservation was incorrect");

        // Line operations accept byte addresses, ignore misses, and
        // distinguish clean write-back from invalidation and purge.
        image = '{default:32'hfffd_fffd};
        literal('h00, 1, 'h80);
        literal('h02, 2, 'h84);
        emit('h04, 'h2122);
        literal('h06, 3, 'h88);
        literal('h08, 4, 'h8c);
        literal('h0a, 5, 'h90);
        emit('h0c, 'he660);
        emit('h0e, 'h2360); // Dirty the 0x200 line
        emit('h10, 'h04a3); // OCBP @R4 misses on a different tag at the same index
        emit('h12, 'h0593); // OCBI @R5 is uncached and must leave the line alone
        emit('h14, 'h03b3); // OCBWB @R3
        emit('h16, 'h6830); // Remains a hit after write-back
        emit('h18, 'h03b3); // No writes for an already clean line
        emit('h1a, 'he627);
        emit('h1c, 'h2360);
        emit('h1e, 'h0393); // OCBI @R3 discards the dirty data
        emit('h20, 'h6930); // Refill should see the old data
        emit('h22, 'he643);
        emit('h24, 'h2360);
        emit('h26, 'h03a3); // OCBP @R3 writes back and invalidates
        emit('h28, 'h6a30);
        image['h80 >> 2] = 'hff00_001c;
        image['h84 >> 2] = 'h101;
        image['h88 >> 2] = 'h203;
        image['h8c >> 2] = 'h4203;
        image['h90 >> 2] = 'ha000_0203;
        image['h200 >> 2] = 'h1122_3344;
        start_bus();
        wait (retired && retired_pc == 'h12);
        assert (ext_mem['h200 >> 2] == 'h1122_3344 && resident_reads == 8)
            else $fatal(1, "Missed or uncached maintenance touched the resident dirty line");
        wait (retired && retired_pc == 'h16);
        assert (ext_mem['h200 >> 2] == 'h6022_3344 && resident_reads == 8)
            else $fatal(1, "OCBWB did not clean the line and keep it resident");
        expect_stop(1, 'h2a);
        assert (dut.gpr[8] == 'h60 && dut.gpr[9] == 'h60 && dut.gpr[10] == 'h43 &&
                ext_mem['h200 >> 2] == 'h4322_3344 && resident_reads == 24 && resident_writes == 16)
            else $fatal(1, "OCBI and OCBP did not discard or purge correctly");

        // MOVCA.L allocates a copy-back line with no fill traffic, even
        // when it must evict a dirty victim. In write-through mode it stores
        // directly to memory without allocating.
        image = '{default:32'hfffd_fffd};
        literal('h00, 1, 'h80);
        literal('h02, 2, 'h84);
        emit('h04, 'h2122);
        literal('h06, 3, 'h88);
        literal('h08, 4, 'h8c);
        literal('h0a, 5, 'h90);
        emit('h0c, 'he017);
        emit('h0e, 'h2302); // Dirty the victim
        emit('h10, 'he02e);
        emit('h12, 'h04c3); // MOVCA.L R0,@R4
        emit('h14, 'h6842);
        emit('h16, 'h04b3); // Write back the allocated line
        literal('h18, 2, 'h94);
        emit('h1a, 'h2122); // Enable WT for direct P0
        emit('h1c, 'h05c3); // Ordinary write-through store behavior
        image['h80 >> 2] = 'hff00_001c;
        image['h84 >> 2] = 'h101;
        image['h88 >> 2] = 'h200;
        image['h8c >> 2] = 'h4204;
        image['h90 >> 2] = 'h600;
        image['h94 >> 2] = 'h103;
        image['h200 >> 2] = 'h1234_5678;
        image['h4204 >> 2] = 'h3b;
        start_bus();
        wait (retired && retired_pc == 'h12);
        assert (ext_mem['h200 >> 2] == 'h17 && ext_mem['h4204 >> 2] == 'h3b &&
                conflict_reads == 0)
            else $fatal(1, "MOVCA.L failed to evict or performed an unwanted refill");
        expect_stop(1, 'h1e);
        assert (dut.gpr[8] == 'h2e && ext_mem['h4204 >> 2] == 'h2e &&
                ext_mem['h4208 >> 2] == 0 && ext_mem['h600 >> 2] == 'h2e &&
                conflict_reads == 0 && cold_store_line_reads == 0)
            else $fatal(1, "MOVCA.L write-back data or write-through fallback was incorrect");

        // Both queues buffer writes without touching the bus. QACR1
        // supplies the external area bits independently from QACR0.
        image = '{default:32'hfffd_fffd};
        literal('h00, 1, 'h80); // QACR0
        literal('h02, 3, 'h84);
        literal('h04, 4, 'h88);
        emit('h06, 'he20c);
        emit('h08, 'h1121); // Write QACR1 (area 3)
        emit('h0a, 'h5811); // Read QACR1
        emit('h0c, 'he010);
        emit('h0e, 'he630);
        for (int word_index = 0; word_index < 8; word_index++) begin
            emit('h10 + 8*word_index, 16'h1300 | 16'(word_index)); // R0 to SQ0
            emit('h12 + 8*word_index, 16'h1460 | 16'(word_index)); // R6 to SQ1
            emit('h14 + 8*word_index, 'h7001);
            emit('h16 + 8*word_index, 'h7601);
        end
        emit('h50, 'h0483); // PREF @R4 drains SQ1
        emit('h52, 'h0383); // PREF @R3 drains SQ0
        literal('h54, 5, 'h8c);
        emit('h56, 'h0583); // Ordinary prefetch of the 0x600 line
        emit('h58, 'h6950);
        image['h80 >> 2] = 'hff00_0038;
        image['h84 >> 2] = 'he000_0200;
        image['h88 >> 2] = 'he000_0220;
        image['h8c >> 2] = 'h603;
        image['h600 >> 2] = 'h2700_0000;
        check_qacr_area = 1;
        start_bus();
        wait (retired && retired_pc == 'h4e);
        assert (qacr_area_writes == 0 && queue_zero_writes == 0)
            else $fatal(1, "Store queue writes reached external memory before PREF");
        expect_stop(1, 'h5a);
        assert (dut.gpr[8] == 'hc && dut.gpr[9] == 'h27 &&
                qacr_area_writes == 8 && queue_zero_writes == 8 && cold_store_line_reads == 8)
            else $fatal(1, "QACR selection, store queue publish, or ordinary PREF was incorrect");
        for (int word_index = 0; word_index < 8; word_index++) begin
            assert (ext_mem[('h200 >> 2) + word_index] == 32'h10 + 32'(word_index) &&
                    ext_mem[('h220 >> 2) + word_index] == 32'h30 + 32'(word_index))
                else $fatal(1, "Store queue order or data differed at word %0d", word_index);
        end
        check_qacr_area = 0;

        // Install executable VA zero and a queue mapping. Unlike an ordinary
        // prefetch, a queue PREF can raise a read-class TLB exception.
        image = '{default:32'hfffd_fffd};
        literal('h00, 1, 'ha0);
        emit('h02, 'he207);
        emit('h04, 'h2122); // PTEH VA zero, ASID 7
        literal('h06, 3, 'ha4);
        emit('h08, 'h1131);
        emit('h0a, 'h0038); // LDTLB
        literal('h0c, 4, 'ha8);
        emit('h0e, 'h1144); // URC=1
        literal('h10, 2, 'hac);
        emit('h12, 'h2122);
        literal('h14, 3, 'hb0);
        emit('h16, 'h1131);
        emit('h18, 'h0038);
        literal('h1a, 6, 'hb4);
        emit('h1c, 'h1163); // Seed TEA
        emit('h1e, 'he401);
        emit('h20, 'h1144); // AT=1
        literal('h22, 3, 'hb8);
        emit('h24, 'h0383); // Ignored ordinary prefetch to an unmapped page
        literal('h26, 3, 'hbc); // SQ0 VA
        emit('h28, 'he025);
        for (int word_index = 0; word_index < 8; word_index++)
            emit('h2a + 2*word_index, 16'h1300 | 16'(word_index));
        literal('h3a, 5, 'hc0);
        emit('h3c, 'h0583); // Translated publish
        image['ha0 >> 2] = 'hff00_0000;
        image['ha4 >> 2] = 'h0000_016d;
        image['ha8 >> 2] = 'h0000_0400;
        image['hac >> 2] = 'he000_0407;
        image['hb0 >> 2] = 'h0000_1164;
        image['hb4 >> 2] = 'hbeef_0000;
        image['hb8 >> 2] = 'h800;
        image['hbc >> 2] = 'he000_0400;
        image['hc0 >> 2] = 'he000_0400;
        start_bus();
        expect_stop(1, 'h3e);
        assert (mapped_queue_writes == 8 && dut.mmu.tea == 'hbeef_0000 &&
                dut.mmu.pteh_vpn == 22'h380001)
            else $fatal(1, "Translated queue PREF failed or ordinary PREF changed exception registers");
        for (int word_index = 0; word_index < 8; word_index++)
            assert (ext_mem[('h1000 >> 2) + word_index] == 'h25)
                else $fatal(1, "Translated queue wrote incorrect word %0d", word_index);

        image['hc0 >> 2] = 'he000_0800; // PREF targets an unmapped queue page
        start_bus();
        expect_stop(5, 'h3c);
        assert (mapped_queue_writes == 0 && dut.mmu.tea == 'he000_0800 && dut.expevt == 'h040)
            else $fatal(1, "Queue PREF did not report a read-class TLB miss");
        image['hb0 >> 2] = 'h0000_1160; // Queue mapping D=0
        start_bus();
        expect_stop(8, 'h2a);
        assert (mapped_queue_writes == 0 && dut.mmu.tea == 'he000_0400 && dut.expevt == 'h080)
            else $fatal(1, "Queue store did not report an initial-write exception");

        // MMUCR.SQMD restricts writes to the P4 queue window even with
        // the MMU off. User attempts must not reach shared memory.
        image = '{default:32'hfffd_fffd};
        literal('h00, 1, 'h80);
        literal('h02, 2, 'h84);
        literal('h04, 8, 'h88);
        emit('h06, 'h2122);
        emit('h08, 'he000);
        emit('h0a, 'h400e); // SR=0, user mode
        emit('h0c, 'h2802); // MOV.L R0,@R8 into the queue window
        literal('h100, 1, 'h120);
        emit('h102, 'h6912);
        image['h80 >> 2] = 'hff00_0010;
        image['h84 >> 2] = 'h200;
        image['h88 >> 2] = 'he000_0200;
        image['h120 >> 2] = 'hff00_0024;
        start_bus();
        expect_stop(1, 'h104);
        assert (dut.spc == 'h0c && dut.mmu.tea == 'he000_0200 && dut.gpr[9] == 'h100 &&
                queue_zero_writes == 0)
            else $fatal(1, "SQMD did not protect the user-mode queue window");

        // Program a bootstrap and operand mapping through the guest's P4
        // arrays. Neither array access may leak onto the shared memory bus.
        image = '{default:32'hfffd_fffd};
        literal('h00, 1, 'h80);
        literal('h02, 2, 'h84);
        emit('h04, 'h2122); // UTLB address array for bootstrap page
        literal('h06, 1, 'h88);
        literal('h08, 2, 'h8c);
        emit('h0a, 'h2122); // UTLB data1 for bootstrap page
        literal('h0c, 1, 'h90);
        literal('h0e, 2, 'h94);
        emit('h10, 'h2122); // UTLB address array for operand page
        emit('h12, 'h6812);
        literal('h14, 1, 'h98);
        literal('h16, 2, 'h9c);
        emit('h18, 'h2122); // UTLB data1 for operand page
        emit('h1a, 'h6912);
        literal('h1c, 1, 'ha0);
        emit('h1e, 'he20f);
        emit('h20, 'h2122); // UTLB data2 for operand page
        emit('h22, 'h6a12);
        literal('h24, 1, 'ha4);
        emit('h26, 'he201);
        emit('h28, 'h2122); // MMUCR.AT
        literal('h2a, 7, 'ha8);
        emit('h2c, 'h6c72);
        image['h80 >> 2] = 'hf600_0000;
        image['h84 >> 2] = 'h100;
        image['h88 >> 2] = 'hf700_0000;
        image['h8c >> 2] = 'h16d;
        image['h90 >> 2] = 'hf600_0100;
        image['h94 >> 2] = 'h500;
        image['h98 >> 2] = 'hf700_0100;
        image['h9c >> 2] = 'h116d;
        image['ha0 >> 2] = 'hf780_0100;
        image['ha4 >> 2] = 'hff00_0010;
        image['ha8 >> 2] = 'h410;
        image['h1010 >> 2] = 'hca5e_0010;
        start_bus();
        expect_stop(1, 'h2e);
        assert (dut.gpr[8] == 'h500 && dut.gpr[9] == 'h116d && dut.gpr[10] == 'hf &&
                dut.gpr[12] == 'hca5e_0010)
            else $fatal(1, "Guest TLB array values did not configure instruction and data translation");

        // Banked control transfers always use the bank opposite SR.RB.
        // When LDC.L restores SR, the postincrement belongs to the old bank.
        image = '{default:32'hfffd_fffd};
        emit('h00, 'he211);
        emit('h02, 'h42be); // LDC R2,R3_BANK
        emit('h04, 'h08b2); // STC R3_BANK,R8
        emit('h06, 'he322);
        literal('h08, 15, 'h80);
        emit('h0a, 'h4fb3); // STC.L R3_BANK,@-R15
        emit('h0c, 'h4fc7); // LDC.L @R15+,R4_BANK
        emit('h0e, 'h42fa); // LDC R2,DBR
        emit('h10, 'h09fa); // STC DBR,R9
        emit('h12, 'h4ff2); // STC.L DBR,@-R15
        emit('h14, 'h4f17); // LDC.L @R15+,GBR
        literal('h16, 1, 'h84);
        emit('h18, 'h4107); // LDC.L @R1+,SR, switching to bank zero
        emit('h1a, 'h0a92); // STC R1_BANK,R10, now looking at bank one
        emit('h1c, 'h0bb2); // STC R3_BANK,R11, the previously active R3
        emit('h1e, 'h0c12); // STC GBR,R12
        emit('h20, 'h0d02); // STC SR,R13
        emit('h22, 'he000);
        emit('h24, 'h400e); // Enter user mode
        emit('h26, 'h4f13); // STC.L GBR,@-R15 is legal in user mode
        emit('h28, 'h401e); // Zero GBR
        emit('h2a, 'h4f17); // Restore GBR from the user stack
        emit('h2c, 'h0e12); // STC GBR,R14
        emit('h2e, 'h4f87); // User LDC.L to a bank register is illegal
        image['h80 >> 2] = 'h300;
        image['h84 >> 2] = 'h204;
        image['h204 >> 2] = 'h4000_0001;
        start_bus();
        expect_stop(1, 'h100);
        assert (dut.gpr_storage[3] == 'h11 && dut.gpr_storage[4] == 'h11 &&
                dut.gpr_storage[19] == 'h22 && dut.gpr[8] == 'h11 && dut.gpr[9] == 'h11 &&
                dut.gpr[10] == 'h208 && dut.gpr[11] == 'h22 && dut.gpr[12] == 'h11 &&
                dut.gpr[13] == 'h4000_0001 && dut.gpr[14] == 'h11 && dut.gpr[15] == 'h300 &&
                dut.spc == 'h2e && dut.ssr == 0)
            else $fatal(1, "Control transfer, privilege, or old-bank postincrement was incorrect");

        // Load the saved context through separate stack slots, then save it
        // in reverse order. DBR has a distinct encoding from R7_BANK.
        image = '{default:32'hfffd_fffd};
        literal('h00, 15, 'h80);
        emit('h02, 'h4f27); // LDC.L @R15+,VBR
        emit('h04, 'h4f37); // LDC.L @R15+,SSR
        emit('h06, 'h4f47); // LDC.L @R15+,SPC
        emit('h08, 'h4ff6); // LDC.L @R15+,DBR
        emit('h0a, 'h7f20); // Save to a different stack area
        emit('h0c, 'h4ff2); // STC.L DBR,@-R15
        emit('h0e, 'h4f43); // STC.L SPC,@-R15
        emit('h10, 'h4f33); // STC.L SSR,@-R15
        emit('h12, 'h4f23); // STC.L VBR,@-R15
        emit('h14, 'h4f03); // STC.L SR,@-R15
        emit('h16, 'h4f07); // LDC.L @R15+,SR
        image['h80 >> 2] = 'h200;
        image['h200 >> 2] = 'ha000_0800;
        image['h204 >> 2] = 'h7000_8303;
        image['h208 >> 2] = 'ha000_1234;
        image['h20c >> 2] = 'h9876_5432;
        start_bus();
        expect_stop(1, 'h18);
        assert (dut.vbr == 'ha000_0800 && dut.ssr == 'h7000_8303 && dut.spc == 'ha000_1234 &&
                dut.dbr == 'h9876_5432 && dut.gpr[15] == 'h220 &&
                ext_mem['h21c >> 2] == 'h7000_00f0 && ext_mem['h220 >> 2] == 'ha000_0800 &&
                ext_mem['h224 >> 2] == 'h7000_8303 && ext_mem['h228 >> 2] == 'ha000_1234 &&
                ext_mem['h22c >> 2] == 'h9876_5432)
            else $fatal(1, "Longword stack transfers did not round trip control state");

        image['h80 >> 2] = 'h201;
        emit('h02, 'h4f07);
        start_bus();
        expect_stop(4, 'h02);
        assert (dut.gpr[15] == 'h201 && dut.sr == 'h7000_00f0)
            else $fatal(1, "Faulting LDC.L changed SR or postincremented its base");
        image['h80 >> 2] = 'h200;
        emit('h02, 'ha002); // BRA with LDC.L in the slot
        emit('h04, 'h4f07);
        start_bus();
        expect_stop(2, 'h04);
        assert (dut.gpr[15] == 'h200 && dut.sr == 'h7000_00f0)
            else $fatal(1, "Slot-illegal LDC.L changed architectural state");

        // Equal-priority interrupts remain masked. Raising the priority as
        // the branch retires must defer acknowledgement until after its slot.
        image = '{default:32'hfffd_fffd};
        literal('h00, 1, 'h80);
        emit('h02, 'h410e); // LDC R1,SR
        emit('h04, 'he20a);
        emit('h06, 'hef50);
        emit('h08, 'hed00);
        emit('h0a, 'ha005); // BRA 0x18
        emit('h0c, 'h7201); // ADD #1,R2 in the slot
        emit('h18, 'h7202);
        emit('h1a, 'h001b); // SLEEP
        emit('h1c, 'h7204);
        emit('h600, 'h0842); // STC SPC,R8
        emit('h602, 'h0932); // STC SSR,R9
        emit('h604, 'h4f32); // STC.L SGR,@-R15
        emit('h606, 'h6af6); // MOV.L @R15+,R10
        literal('h608, 1, 'h640);
        emit('h60a, 'h6b12); // Read INTEVT into R11
        emit('h60c, 'h7d01); // Count entries in shared R13
        emit('h60e, 'h002b); // RTE
        emit('h610, 'h0009);
        image['h80 >> 2] = 32'h4000_0050;
        image['h640 >> 2] = 32'hff00_0028;
        irq_level = 5;
        irq_event = 'h340;
        start_bus();
        wait (retired && retired_pc == 'ha);
        assert (!irq_ack && dut.intevt == 0) else $fatal(1, "Equal-priority IRQ was accepted");
        @(negedge clk);
        irq_level = 6;
        wait (irq_ack);
        assert (dut.spc == 'h18 && dut.gpr_storage[2] == 11 && dut.gpr[13] == 0 &&
                dut.ssr == 'h4000_0050 && dut.sgr == 'h50 && dut.intevt == 'h340)
            else $fatal(1, "IRQ interrupted a delay slot or saved incorrect state");
        @(negedge clk);
        irq_level = 5;
        wait (retired && retired_pc == 'h1a);
        @(negedge clk); // Let the SLEEP retirement pulse complete.
        repeat (20) begin
            @(negedge clk);
            assert (!retired && !irq_ack) else $fatal(1, "Masked IRQ woke SLEEP");
        end
        assert (dut.gpr[8] == 'h18 && dut.gpr[9] == 'h4000_0050 && dut.gpr[10] == 'h50 &&
                dut.gpr[11] == 'h340 && dut.gpr[13] == 1 && dut.gpr[2] == 13)
            else $fatal(1, "First IRQ handler or RTE did not complete");
        irq_level = 7;
        irq_event = 'h420;
        wait (irq_ack);
        @(negedge clk);
        irq_level = 0;
        expect_stop(1, 'h100);
        assert (dut.gpr[8] == 'h1c && dut.gpr[9] == 'h4000_0050 && dut.gpr[11] == 'h420 &&
                dut.gpr[13] == 2 && dut.gpr_storage[2] == 17 && dut.intevt == 'h420)
            else $fatal(1, "IRQ did not wake SLEEP and resume at the next instruction");

        // A newly asserted NMI also wins when both inputs first arrive on
        // the same cycle while asleep.
        start_bus();
        wait (retired && retired_pc == 'h1a);
        @(negedge clk);
        irq_level = 15;
        irq_event = 'h400;
        nmi = 1;
        @(negedge clk);
        assert (!irq_ack && dut.spc == 'h1c && dut.intevt == 'h1c0)
            else $fatal(1, "A new NMI lost priority to a simultaneous IRQ");
        irq_level = 0;
        nmi = 0;
        expect_stop(1, 'h100);
        assert (dut.gpr[11] == 'h1c0 && dut.gpr_storage[2] == 17)
            else $fatal(1, "NMI did not wake SLEEP and return");

        // An NMI edge captured with BL set takes priority over a pending IRQ
        // when BL clears. Holding the NMI input high must yield a single entry.
        emit('h08, 'h0009);
        emit('h0e, 'hfffd);
        emit('h604, 'h0a3a); // STC SGR,R10
        emit('h606, 'h0009);
        emit('h60c, 'h0009);
        irq_level = 15;
        irq_event = 'h400;
        nmi = 1;
        start_bus();
        wait (retired && retired_pc == 'h60a);
        assert (!irq_ack && dut.gpr[8] == 4 && dut.gpr[11] == 'h1c0)
            else $fatal(1, "Latched NMI did not take priority over IRQ");
        wait (irq_ack);
        assert (dut.spc == 4 && dut.intevt == 'h400) else $fatal(1, "Pending IRQ was lost after NMI");
        @(negedge clk);
        irq_level = 0;
        wait (retired && retired_pc == 'h18);
        assert (dut.gpr[11] == 'h400) else $fatal(1, "Held NMI was taken twice");
        nmi = 0;
        reset = 1;

        // The interrupt controller can permit NMI entry with BL still set.
        nmi = 1;
        nmi_while_blocked = 1;
        start_bus();
        wait (retired && retired_pc == 'h60a);
        assert (!irq_ack && dut.gpr[8] == 0 && dut.gpr[9] == 'h7000_00f0 && dut.gpr[11] == 'h1c0)
            else $fatal(1, "NMI override did not enter with BL set");
        nmi = 0;
        nmi_while_blocked = 0;

        assert (mmu_done) else $fatal(1, "MMU unit check did not finish");
        assert (cache_cancel_done) else $fatal(1, "Cache cancellation check did not finish");

        $display("PASS SH-4 integer, stalls, delay slots, caches, SH-4 MMU control, page sizes, permissions, exception return, extended integer execution, MAC, control transfers, interrupts, byte operations, copy-back, cache maintenance, store queues and TLB arrays");
        $finish;
    end

    initial begin
        #250000;
        $fatal(1, "Timed out at PC %h (last retirement %h, fault %d at %h)",
               dut.pc, retired_pc, fault, fault_pc);
    end
endmodule
