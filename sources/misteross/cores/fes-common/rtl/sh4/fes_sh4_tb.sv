module fes_sh4_tb;
    logic clk = 0;
    always #5 clk <= ~clk;

    logic reset = 1, bram_reset = 1, load_we = 0, bram_nmi = 0, bram_rtc_tick = 0;
    logic [8:0] load_word_addr;
    logic [31:0] load_data;
    logic valid, ready, rvalid, retired, bram_retired, bram_irq_ack;
    logic [3:0] irq_level = 0;
    logic [11:0] irq_event = 0;
    logic bram_sci_txd, unused_sci_txd_enable, unused_sci_sck_out, unused_sci_sck_enable;
    logic [19:0] bram_gpio_in = '1, bram_gpio_out, bram_gpio_oe, bram_gpio_pullup;
    logic unused_sleeping, unused_standby_wake;
    logic [11:0] bram_frequency_control;
    logic [1:0] bram_processor_status;
    logic bram_freq_change, bram_standby, bram_deep_sleep, bram_high_z, bram_peripheral_pullup;
    logic unused_nmi_ack, unused_scif_txd, unused_scif_txd_enable, unused_scif_rts_n;
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
    logic check_data_cache = 0, check_operand_ram = 0, check_index_modes = 0;
    logic check_qacr_area = 0;
    int qacr_area_writes, queue_zero_writes, mapped_queue_writes;
    logic cache_cancel_done, mmu_done, fp_arith_finished, bsc_finished, tmu_finished;
    logic dma_finished, rtc_finished, scif_finished, sci_finished, gpio_finished, cpg_finished;
    fes_sh7091_cpg_tb cpg_test (.clk, .finished(cpg_finished));
    fes_sh7091_gpio_tb gpio_test (.clk, .finished(gpio_finished));
    fes_sh7091_sci_tb sci_test (.clk, .finished(sci_finished));
    fes_sh7091_scif_tb scif_test (.clk, .finished(scif_finished));
    fes_sh7091_rtc_tb rtc_test (.clk, .finished(rtc_finished));
    fes_sh7091_dmac_tb dma_test (.clk, .finished(dma_finished));
    fes_sh7091_tmu_tb tmu_test (.clk, .finished(tmu_finished));
    fes_sh7091_bsc_tb bsc_test (.clk, .finished(bsc_finished));
    fes_sh4_fp_arithmetic_tb fp_arith_test (.clk, .finished(fp_arith_finished));

    fes_sh4_mmu_tb mmu_test (.clk, .done(mmu_done));
    fes_sh4_icache_tb cache_cancel (.clk, .done(cache_cancel_done));
    logic check_warm_loop = 0, check_alias_load = 1;

    fes_sh4 #(.RESET_PC(0), .RESET_CCR(16'h0103)) dut (
        .clk, .reset, .standby(1'b0), .standby_ready(1'b0),
        .sleeping(unused_sleeping), .standby_wake(unused_standby_wake), .manual_reset(1'b0), .irq_level, .irq_event, .nmi, .nmi_while_blocked, .irq_ack, .nmi_ack(unused_nmi_ack),
        .mem_valid(valid), .mem_lock(locked), .mem_sq(queue_write), .mem_rmask(rmask), .mem_ready(ready),
        .mem_addr(addr), .mem_wstrb(wstrb), .mem_wdata(wdata),
        .mem_rvalid(rvalid), .mem_rdata(rdata),
        .retired, .retired_pc, .fault, .fault_pc
    );

    fes_sh4_bram_system bram (
        .clk, .reset(bram_reset), .rtc_tick(bram_rtc_tick), .irq_level(4'b0), .irq_event(12'b0),
        .sci_rxd(bram_sci_txd), .sci_sck(1'b1), .sci_txd(bram_sci_txd), .sci_txd_enable(unused_sci_txd_enable),
        .sci_sck_out(unused_sci_sck_out), .sci_sck_enable(unused_sci_sck_enable),
        .frequency_control(bram_frequency_control), .frequency_change(bram_freq_change),
        .processor_status(bram_processor_status), .standby(bram_standby), .deep_sleep(bram_deep_sleep),
        .peripheral_high_z(bram_high_z), .peripheral_pullup(bram_peripheral_pullup),
        .gpio_in(bram_gpio_in), .gpio_out(bram_gpio_out), .gpio_oe(bram_gpio_oe), .gpio_pullup(bram_gpio_pullup),
        .scif_rxd(1'b1), .scif_cts_n(1'b0), .scif_tick(1'b0),
        .scif_txd(unused_scif_txd), .scif_txd_enable(unused_scif_txd_enable), .scif_rts_n(unused_scif_rts_n),
        .nmi(bram_nmi), .nmi_while_blocked(1'b0), .irq_ack(bram_irq_ack), .load_we, .load_word_addr, .load_data,
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
                if (check_operand_ram)
                    assert (addr[31:26] != 6'b011111)
                        else $fatal(1, "Internal operand RAM accessed the external bus");
                if (queue_beat != 0)
                    assert (queue_write && queue_line == addr[31:5])
                        else $fatal(1, "A foreign request interrupted a store queue transfer");
                if (queue_write) begin
                    assert (wstrb == 4'hf && addr[4:2] == queue_beat)
                        else $fatal(1, "Invalid store queue write beat");
                    queue_line <= addr[31:5];
                    queue_beat <= queue_beat + 3'd1;
                end
                assert (latency == 0 && addr[1:0] == 0 &&
                        (addr[25:15] == 0 || (check_index_modes &&
                         (addr[31:5] inside {27'h0100010, 27'h0100080}) && wstrb == 0)) &&
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
                bram.system_cpu.cpu.gpr[5] == 32'hffff_ff80 && bram.system_cpu.cpu.gpr[7] == dut.gpr[7] &&
                bram.system_cpu.cpu.gpr[9] == dut.gpr[9])
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

        emit('h2e, 'hf3fd); // FSCHG
        emit('h30, 'hf059); // FMOV @R5+,DR0, initially unmapped
        image['h1010 >> 2] = 'h89ab_cdef;
        image['h1014 >> 2] = 'h1234_5678;
        start_bus();
        expect_stop(1, 'ha000_1104);
        assert (dut.gpr[8] == 'h30 && dut.gpr[12] == 'h40 && dut.gpr[13] == 'hc10 &&
                dut.gpr_storage[5] == 'hc18 && dut.fpu_regs.fr_even[0] == 'h1234_5678 &&
                dut.fpu_regs.fr_odd[0] == 'h89ab_cdef)
            else $fatal(1, "Paired FMOV did not restart through a TLB miss");

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
        literal('h2e, 1, 'hac);
        emit('h30, 'h6d12); // Read the OC tag at the virtual cache index.
        literal('h32, 2, 'hb0);
        emit('h34, 'h1122); // Associative clear using virtual VA 0x400
        emit('h36, 'h6e12);
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
        image['hac >> 2] = 'hf400_0400;
        image['hb0 >> 2] = 'h400;
        image['h1010 >> 2] = 'hca5e_0010;
        start_bus();
        expect_stop(1, 'h38);
        assert (dut.gpr[8] == 'h500 && dut.gpr[9] == 'h116d && dut.gpr[10] == 'hf &&
                dut.gpr[12] == 'hca5e_0010 && dut.gpr[13] == 'h1001 && dut.gpr[14] == 'h1000)
            else $fatal(1, "Guest TLB array values did not configure instruction and data translation");

        // The index modes substitute virtual address bit 25 for the
        // high index bit while the cache array ports keep their raw indexes.
        image = '{default:32'hfffd_fffd};
        literal('h00, 1, 'h80);
        literal('h02, 2, 'h84);
        emit('h04, 'h2122); // IIX | OIX | ICE | WT | OCE
        literal('h06, 3, 'h88);
        literal('h08, 4, 'h8c);
        emit('h0a, 'h6a32);
        emit('h0c, 'h6b42);
        literal('h0e, 5, 'h90);
        literal('h10, 6, 'h94);
        emit('h12, 'h450b); // Call VA with bit 12 set and bit 25 clear.
        emit('h14, 'h0009);
        emit('h16, 'h6893);
        emit('h18, 'h460b); // Call VA with bit 12 clear and bit 25 set.
        emit('h1a, 'h0009);
        literal('h1c, 7, 'h98);
        emit('h1e, 'h6c72); // Read tag from OC array entry 128.
        literal('h20, 7, 'h9c);
        emit('h22, 'h6d72);
        image['h80 >> 2] = 'hff00_001c;
        image['h84 >> 2] = 'h0000_8183;
        image['h88 >> 2] = 'h0000_3000;
        image['h8c >> 2] = 'h0200_1000;
        image['h90 >> 2] = 'h0000_1200;
        image['h94 >> 2] = 'h0200_0200;
        image['h98 >> 2] = 'hf400_1000;
        image['h9c >> 2] = 'hf400_3000;
        image['h3000 >> 2] = 'h1234_5678;
        image['h1000 >> 2] = 'h0123_4567;
        image['h1200 >> 2] = 'h000b_e920;
        emit('h1204, 'h0009);
        image['h200 >> 2] = 'h000b_e941;
        emit('h204, 'h0009);
        check_index_modes = 1;
        start_bus();
        expect_stop(1, 'h24);
        check_index_modes = 0;
        assert (dut.gpr[8] == 'h20 && dut.gpr[9] == 'h41 &&
                dut.gpr[10] == 'h1234_5678 && dut.gpr[11] == 'h0123_4567 &&
                dut.gpr[12] == 'h3001 && dut.gpr[13] == 'h0200_1001 &&
                dut.icache.valid_lines[16] && dut.icache.valid_lines[144])
            else $fatal(1, "IC or OC index mode did not select the expected virtual index");

        // RAM banks alias differently in the two index modes. Normal cache
        // fills must leave RAM intact, even if their address has bit 12 set.
        image = '{default:32'hfffd_fffd};
        literal('h00, 1, 'h80);
        literal('h02, 2, 'h84);
        emit('h04, 'h2122); // ICE | ORA | OCE
        literal('h06, 3, 'h88);
        literal('h08, 4, 'h8c);
        literal('h0a, 5, 'h90);
        literal('h0c, 6, 'h94);
        emit('h0e, 'h2362);
        emit('h10, 'he05a);
        emit('h12, 'h2400); // Byte write through a RAM bank 0 alias.
        emit('h14, 'he047);
        emit('h16, 'h2502);
        emit('h18, 'h1361);
        literal('h1a, 7, 'ha0);
        emit('h1c, 'h6b72); // Ordinary cache fill at a RAM-reserved raw index.
        emit('h1e, 'hf3fd);
        emit('h20, 'hf238); // Paired load from operand RAM.
        emit('h22, 'h6832);
        emit('h24, 'h6952);
        literal('h26, 2, 'h98);
        emit('h28, 'h2122); // Turn on OIX. Bit 25 selects the RAM bank.
        literal('h2a, 7, 'h9c);
        emit('h2c, 'h6a72);
        literal('h2e, 0, 'ha4);
        emit('h30, 'h402b);
        emit('h32, 'h0009);
        literal('h60, 0, 'ha8); // P2 code can run with AT set and no entries.
        emit('h62, 'he201);
        emit('h64, 'h2022);
        emit('h66, 'h6c32); // Operand RAM bypasses translation.
        emit('h68, 'h6d12);
        image['h80 >> 2] = 'hff00_001c;
        image['h84 >> 2] = 'h0000_0121;
        image['h88 >> 2] = 'h7c00_0000;
        image['h8c >> 2] = 'h7c00_1001;
        image['h90 >> 2] = 'h7c00_2000;
        image['h94 >> 2] = 'h1122_3344;
        image['h98 >> 2] = 'h0000_01a1;
        image['h9c >> 2] = 'h7e00_0000;
        image['ha0 >> 2] = 'h1000;
        image['ha4 >> 2] = 'ha000_0060;
        image['ha8 >> 2] = 'hff00_0010;
        image['h1000 >> 2] = 'h1234_5678;
        check_operand_ram = 1;
        start_bus();
        expect_stop(1, 'ha000_006a);
        check_operand_ram = 0;
        assert (dut.gpr[8] == 'h1122_5a44 && dut.gpr[9] == 'h47 &&
                dut.gpr[10] == 'h47 && dut.gpr[11] == 'h1234_5678 &&
                dut.gpr[12] == 'h1122_5a44 && dut.gpr[13] == 'h01a1 &&
                dut.fpu_regs.fr_even[1] == 'h1122_3344 &&
                dut.fpu_regs.fr_odd[1] == 'h1122_5a44)
            else $fatal(1, "Operand RAM bank selection, byte mask, paired data or MMU bypass failed");

        // Area 7 register aliases use the UTLB's permissions and preserve
        // the original virtual address on an invalid access.
        image = '{default:32'hfffd_fffd};
        literal('h00, 1, 'h40);
        emit('h02, 'h412b);
        emit('h04, 'h0009);
        image['h40 >> 2] = 'ha000_0200;
        literal('h200, 1, 'h300);
        literal('h202, 2, 'h304);
        emit('h204, 'h2122); // PTEH selects VA 0x1000.
        literal('h206, 3, 'h308);
        emit('h208, 'h1131); // PTEL maps physical 0x1f000000, uncached.
        emit('h20a, 'h0038);
        literal('h20c, 2, 'h30c);
        emit('h20e, 'h1124); // MMUCR.URC=1 for the second entry.
        emit('h210, 'he200);
        emit('h212, 'h2122);
        literal('h214, 3, 'h310);
        emit('h216, 'h1131); // Executable P0 page with user access.
        emit('h218, 'h0038);
        emit('h21a, 'he201);
        emit('h21c, 'h1124); // Enable translation.
        literal('h21e, 8, 'h314);
        literal('h220, 9, 'h318);
        literal('h222, 10, 'h31c);
        literal('h224, 11, 'h320);
        literal('h226, 12, 'h324);
        emit('h228, 'h4c2b);
        emit('h22a, 'h0009);
        emit('h80, 'h6d82);
        literal('h82, 14, 'hc0);
        emit('h84, 'h28e2); // Write CCR through the alias.
        emit('h86, 'h6eb2); // Read CCR directly.
        emit('h88, 'he510);
        emit('h8a, 'h2952); // Write QACR0 through the alias.
        emit('h8c, 'h6fa2); // Read QACR0 directly.
        literal('h8e, 12, 'hc4);
        emit('h90, 'h4c0e); // Enter user mode with synchronous faults blocked.
        emit('h92, 'h6582);
        emit('h94, 'h6692);
        literal('h96, 0, 'hc8);
        literal('h98, 1, 'hcc);
        emit('h9a, 'h2012); // Access TTB from user mode via its mapping.
        emit('h9c, 'h6202);
        emit('h9e, 'h6780); // Byte access to CCR must report an address error.
        image['hc0 >> 2] = 'h0000_0183;
        image['hc4 >> 2] = 'h1000_0000;
        image['hc8 >> 2] = 'h1008;
        image['hcc >> 2] = 'h1234_0000;
        image['h300 >> 2] = 'hff00_0000;
        image['h304 >> 2] = 'h1000;
        image['h308 >> 2] = 'h1f00_0174;
        image['h30c >> 2] = 'h400;
        image['h310 >> 2] = 'h0000_017c;
        image['h314 >> 2] = 'h101c;
        image['h318 >> 2] = 'h1038;
        image['h31c >> 2] = 'hff00_0038;
        image['h320 >> 2] = 'hff00_001c;
        image['h324 >> 2] = 'h80;
        start_bus();
        expect_stop(9, 'h9e);
        assert (dut.gpr[13] == 'h0103 && dut.gpr[14] == 'h0183 && dut.gpr[15] == 'h10 &&
                dut.gpr[5] == 'h0183 && dut.gpr[6] == 'h10 && dut.gpr[2] == 'h1234_0000 &&
                dut.mmu.tea == 'h101c)
            else $fatal(1, "Area 7 aliases, U0 privilege or virtual TEA failed");

        // Write executable data to the instruction cache. The first
        // call hits the injected line, and the second reads external RAM
        // after an associative invalidation.
        image = '{default:32'hfffd_fffd};
        literal('h00, 1, 'h80);
        literal('h02, 2, 'h84);
        emit('h04, 'h2122); // IC data word 0
        emit('h06, 'h6812); // Read injected data back
        emit('h08, 'he209);
        emit('h0a, 'h1121); // IC data word 1 supplies the RTS delay slot
        literal('h0c, 1, 'h88);
        emit('h0e, 'he201);
        emit('h10, 'h2122); // IC tag valid, PA in the first 1 KiB
        literal('h12, 2, 'h8c);
        emit('h14, 'h1122); // Associative miss must preserve validity
        emit('h16, 'h6a12);
        literal('h18, 6, 'h90);
        emit('h1a, 'h460b); // JSR @R6
        emit('h1c, 'h0009);
        emit('h1e, 'h6b93);
        emit('h20, 'he200);
        emit('h22, 'h1122); // Associative invalidate of the correct IC tag
        emit('h24, 'h6d12);
        emit('h26, 'h460b);
        emit('h28, 'h0009);
        image['h80 >> 2] = 'hf100_0200;
        image['h84 >> 2] = 'h000b_e94d;
        image['h88 >> 2] = 'hf000_0200;
        image['h8c >> 2] = 'h400;
        image['h90 >> 2] = 'h200;
        image['h200 >> 2] = 'h000b_e92b;
        emit('h204, 'h0009);
        start_bus();
        expect_stop(1, 'h2a);
        assert (dut.gpr[8] == 'h000b_e94d && dut.gpr[10] == 1 && dut.gpr[11] == 'h4d &&
                dut.gpr[13] == 0 && dut.gpr[9] == 'h2b && resident_reads == 8)
            else $fatal(1, "IC array write, associative invalidate, or execution was incorrect");

        // A tag write must write back a valid dirty OC victim before
        // retagging it. An associative miss must leave both data and U/V alone.
        image = '{default:32'hfffd_fffd};
        literal('h00, 5, 'h80);
        emit('h02, 'h6852); // Fill the line, so every victim word is initialized
        literal('h04, 1, 'h84);
        literal('h06, 2, 'h88);
        emit('h08, 'h2122); // Replace OC data word 1
        emit('h0a, 'h6912);
        literal('h0c, 4, 'h8c);
        emit('h0e, 'he203);
        emit('h10, 'h2422); // Set V and U in address array
        emit('h12, 'h6a42);
        literal('h14, 2, 'h90);
        emit('h16, 'h1422); // Associative miss
        emit('h18, 'he201);
        emit('h1a, 'h1422); // Clean and preserve the tag using association
        emit('h1c, 'h6b42);
        emit('h1e, 'he203);
        emit('h20, 'h2422); // Mark resident line dirty again
        literal('h22, 2, 'h94);
        emit('h24, 'h2122);
        literal('h26, 2, 'h90);
        emit('h28, 'h2422); // Indexed retag, writing back old line
        emit('h2a, 'h6c42);
        image['h80 >> 2] = 'h204;
        image['h84 >> 2] = 'hf500_0204;
        image['h88 >> 2] = 'h6789_abcd;
        image['h8c >> 2] = 'hf400_0200;
        image['h90 >> 2] = 'h400;
        image['h94 >> 2] = 'h89ab_cdef;
        image['h204 >> 2] = 'h1234_5678;
        start_bus();
        wait (retired && retired_pc == 'h16);
        assert (resident_writes == 0 && ext_mem['h204 >> 2] == 'h1234_5678)
            else $fatal(1, "OC associative mismatch wrote memory");
        wait (retired && retired_pc == 'h1c);
        assert (resident_writes == 8 && ext_mem['h204 >> 2] == 'h6789_abcd)
            else $fatal(1, "OC associative clean did not preserve the dirty data");
        expect_stop(1, 'h2c);
        assert (dut.gpr[8] == 'h1234_5678 && dut.gpr[9] == 'h6789_abcd &&
                dut.gpr[10] == 3 && dut.gpr[11] == 1 && dut.gpr[12] == 'h400 &&
                resident_writes == 16 && ext_mem['h204 >> 2] == 'h89ab_cdef)
            else $fatal(1, "OC array data or dirty victim retagging was incorrect");

        // Exercise both floating register banks, single moves, paired
        // register and memory moves, and FPUL/FPSCR stack round trips.
        image = '{default:32'hfffd_fffd};
        emit('h00, 'h086a); // STS FPSCR,R8
        emit('h02, 'he000);
        emit('h04, 'h406a); // LDS R0,FPSCR
        emit('h06, 'hf09d); // FLDI1 FR0
        emit('h08, 'hf04d); // FNEG FR0
        emit('h0a, 'hf10c); // FMOV FR0,FR1
        emit('h0c, 'hf15d); // FABS FR1
        emit('h0e, 'hf11d); // FLDS FR1,FPUL
        emit('h10, 'h095a); // STS FPUL,R9
        literal('h12, 0, 'h80);
        emit('h14, 'h405a); // LDS R0,FPUL
        emit('h16, 'hf20d); // FSTS FPUL,FR2
        literal('h18, 5, 'h84);
        emit('h1a, 'hf359); // FMOV.S @R5+,FR3
        emit('h1c, 'hf3fd); // FSCHG (paired moves)
        emit('h1e, 'hf42c); // FMOV DR2,DR4
        emit('h20, 'hf54c); // FMOV DR4,XD4
        emit('h22, 'hfbfd); // FRCHG
        literal('h24, 7, 'h88);
        emit('h26, 'hf74b); // FMOV DR4,@-R7
        emit('h28, 'hf679); // FMOV @R7+,DR6
        emit('h2a, 'he008);
        emit('h2c, 'hf767); // FMOV DR6,@(R0,R7)
        emit('h2e, 'hf976); // FMOV @(R0,R7),XD8
        emit('h30, 'hf79a); // FMOV XD8,@R7
        emit('h32, 'h0a6a); // STS FPSCR,R10
        literal('h34, 15, 'h8c);
        emit('h36, 'h4f62); // STS.L FPSCR,@-R15
        emit('h38, 'h4f52); // STS.L FPUL,@-R15
        emit('h3a, 'he000);
        emit('h3c, 'h405a);
        emit('h3e, 'h406a);
        emit('h40, 'h4f56); // LDS.L @R15+,FPUL
        emit('h42, 'h4f66); // LDS.L @R15+,FPSCR
        emit('h44, 'h0b6a);
        emit('h46, 'h0c5a);
        image['h80 >> 2] = 'h1234_5678;
        image['h84 >> 2] = 'h380;
        image['h88 >> 2] = 'h300;
        image['h8c >> 2] = 'h400;
        image['h380 >> 2] = 'h89ab_cdef;
        start_bus();
        expect_stop(1, 'h48);
        assert (dut.gpr[8] == 'h0004_0001 && dut.gpr[9] == 'h3f80_0000 &&
                dut.gpr[10] == 'h0030_0000 && dut.gpr[11] == 'h0030_0000 &&
                dut.gpr[12] == 'h1234_5678 && dut.gpr[5] == 'h384 &&
                dut.gpr[7] == 'h300 && dut.gpr[15] == 'h400 &&
                dut.fpu_regs.fr_even[0] == 'hbf80_0000)
            else $fatal(1, "Floating-point register, FPSCR, FPUL, or address update was incorrect");
        assert (ext_mem['h2f8 >> 2] == 'h89ab_cdef && ext_mem['h2fc >> 2] == 'h1234_5678 &&
                ext_mem['h300 >> 2] == 'h89ab_cdef && ext_mem['h304 >> 2] == 'h1234_5678 &&
                ext_mem['h308 >> 2] == 'h89ab_cdef && ext_mem['h30c >> 2] == 'h1234_5678)
            else $fatal(1, "Paired floating-point memory moves did not preserve bank or endian order");

        // The FD exception handler clears FD in SSR and returns to the faulting
        // FPU instruction or the branch which owns the faulting delay slot.
        image = '{default:32'hfffd_fffd};
        literal('h00, 1, 'h80);
        emit('h02, 'h410e);
        emit('h04, 'hf08d); // FLDI0 FR0 with FD set
        image['h80 >> 2] = 'h4000_8000;
        literal('h100, 2, 'h140);
        emit('h102, 'h6822);
        emit('h104, 'h0932);
        literal('h106, 10, 'h144);
        emit('h108, 'h29a9); // AND R10,R9
        emit('h10a, 'h493e); // LDC R9,SSR
        emit('h10c, 'h002b);
        emit('h10e, 'h0009);
        image['h140 >> 2] = 'hff00_0024;
        image['h144 >> 2] = 'hffff_7fff;
        start_bus();
        wait (retired && retired_pc == 'h04);
        assert (dut.gpr[8] == 'h800 && dut.spc == 'h04 && dut.sr[15] == 0 &&
                dut.fpu_regs.fr_even[0] == 0)
            else $fatal(1, "FD exception failed to resume FLDI0 through RTE");

        emit('h04, 'ha002); // BRA 0x0c
        emit('h06, 'hf09d); // FLDI1 FR0, initially disabled in the slot
        start_bus();
        wait (retired && retired_pc == 'h06);
        assert (dut.gpr[8] == 'h820 && dut.spc == 'h04 && dut.sr[15] == 0 &&
                dut.fpu_regs.fr_even[0] == 'h3f80_0000)
            else $fatal(1, "FD delay-slot exception failed to replay its branch");

        // A misaligned paired load must change neither the FPR pair nor the
        // postincrement register.
        image = '{default:32'hfffd_fffd};
        emit('h00, 'hf09d);
        emit('h02, 'hf18d);
        emit('h04, 'hf3fd);
        literal('h06, 7, 'h80);
        emit('h08, 'hf079); // FMOV @R7+,DR0
        image['h80 >> 2] = 'h304;
        start_bus();
        expect_stop(4, 'h08);
        assert (dut.gpr[7] == 'h304 && dut.fpu_regs.fr_even[0] == 'h3f80_0000 &&
                dut.fpu_regs.fr_odd[0] == 0)
            else $fatal(1, "Misaligned FMOV changed its source pointer or destination");

        // Guest single and double arithmetic share the register file and
        // write back to the original destination in each precision.
        image = '{default:32'hfffd_fffd};
        emit('h00, 'he000);
        emit('h02, 'h406a); // FPSCR=0, round to nearest
        emit('h04, 'hf09d);
        emit('h06, 'hf19d);
        emit('h08, 'hf010); // FADD FR1,FR0
        emit('h0a, 'hf01d); // FLDS FR0,FPUL
        emit('h0c, 'h085a);
        emit('h0e, 'hf011); // FSUB FR1,FR0
        emit('h10, 'hf01d);
        emit('h12, 'h095a);
        literal('h14, 0, 'h80);
        emit('h16, 'h406a); // PR=1, SZ=1
        literal('h18, 1, 'h84);
        literal('h1a, 7, 'h88);
        emit('h1c, 'hf019); // FMOV @R1+,DR0
        emit('h1e, 'hf219); // FMOV @R1+,DR2
        emit('h20, 'hf020); // FADD DR2,DR0
        emit('h22, 'hf70a); // FMOV DR0,@R7
        emit('h24, 'h7708);
        emit('h26, 'hf021); // FSUB DR2,DR0
        emit('h28, 'hf70a);
        emit('h2a, 'h7708);
        emit('h2c, 'hf022); // FMUL DR2,DR0
        emit('h2e, 'hf70a);
        emit('h30, 'h7708);
        emit('h32, 'hf023); // FDIV DR2,DR0
        emit('h34, 'hf70a);
        emit('h36, 'h7708);
        emit('h38, 'hf019); // Load 4.0 into DR0
        emit('h3a, 'hf06d); // FSQRT DR0
        emit('h3c, 'hf70a);
        emit('h3e, 'he000);
        emit('h40, 'h406a); // PR=0, SZ=0
        emit('h42, 'hf09d); // 1.0
        emit('h44, 'hf19d);
        emit('h46, 'hf110); // FR1 = 2.0
        emit('h48, 'hf012); // FMUL FR1,FR0
        emit('h4a, 'hf01d);
        emit('h4c, 'h0a5a);
        emit('h4e, 'hf013); // FDIV FR1,FR0
        emit('h50, 'hf01d);
        emit('h52, 'h0b5a);
        emit('h54, 'hf112); // FMUL FR1,FR1, producing 4.0
        emit('h56, 'hf16d); // FSQRT FR1
        emit('h58, 'hf11d);
        emit('h5a, 'h0c5a);
        image['h80 >> 2] = 'h0018_0000;
        image['h84 >> 2] = 'h200;
        image['h88 >> 2] = 'h300;
        image['h200 >> 2] = 0;
        image['h204 >> 2] = 'h3ff8_0000;
        image['h208 >> 2] = 0;
        image['h20c >> 2] = 'h4002_0000;
        image['h210 >> 2] = 0;
        image['h214 >> 2] = 'h4010_0000;
        start_bus();
        expect_stop(1, 'h5c);
        assert (dut.gpr[8] == 'h4000_0000 && dut.gpr[9] == 'h3f80_0000 && dut.gpr[10] == 'h4000_0000 &&
                dut.gpr[11] == 'h3f80_0000 && dut.gpr[12] == 'h4000_0000 &&
                ext_mem['h300 >> 2] == 0 && ext_mem['h304 >> 2] == 'h400e_0000 &&
                ext_mem['h308 >> 2] == 0 && ext_mem['h30c >> 2] == 'h3ff8_0000 &&
                ext_mem['h310 >> 2] == 0 && ext_mem['h314 >> 2] == 'h400b_0000 &&
                ext_mem['h318 >> 2] == 0 && ext_mem['h31c >> 2] == 'h3ff8_0000 &&
                ext_mem['h320 >> 2] == 0 && ext_mem['h324 >> 2] == 'h4000_0000)
            else $fatal(1, "Floating-point arithmetic was not committed to the guest destination");

        // An enabled invalid exception in a branch delay slot must leave
        // the FR unchanged while setting FPSCR status and saving the branch PC.
        image = '{default:32'hfffd_fffd};
        literal('h00, 1, 'h80);
        emit('h02, 'h410e); // SR.BL=0
        literal('h04, 0, 'h84);
        emit('h06, 'h406a); // FPSCR.EN.V
        emit('h08, 'hf09d);
        literal('h0a, 1, 'h88);
        emit('h0c, 'hf118); // FMOV @R1,FR1 (SH-4 signaling NaN)
        emit('h0e, 'ha002); // BRA 0x16
        emit('h10, 'hf012); // FMUL FR1,FR0 in the delay slot
        literal('h100, 2, 'h140);
        emit('h102, 'h6822); // EXPEVT
        emit('h104, 'h096a); // FPSCR
        emit('h106, 'hf01d); // Observe the unchanged FR0
        emit('h108, 'h0a5a);
        emit('h10a, 'h0b42); // SPC
        emit('h10c, 'he000);
        emit('h10e, 'h406a); // Disable the trap
        emit('h110, 'h002b);
        emit('h112, 'h0009);
        image['h80 >> 2] = 'h4000_0000;
        image['h84 >> 2] = 'h800;
        image['h88 >> 2] = 'h200;
        image['h140 >> 2] = 'hff00_0024;
        image['h200 >> 2] = 'h7fc0_0000;
        start_bus();
        wait (retired && retired_pc == 'h10);
        assert (dut.gpr[8] == 'h120 && dut.gpr[9] == 'h10840 && dut.gpr[10] == 'h3f80_0000 &&
                dut.gpr[11] == 'he && dut.fpu_regs.fr_even[0] == 'h7fbf_ffff)
            else $fatal(1, "Arithmetic exception did not preserve the destination or retry the branch");

        // The SH-4 requests software completion for normal-operand addition
        // with the O/U/I trap enables, even if the sum would be exact.
        image = '{default:32'hfffd_fffd};
        literal('h00, 0, 'h80);
        emit('h02, 'h406a);
        emit('h04, 'hf09d);
        emit('h06, 'hf19d);
        emit('h08, 'hf010);
        image['h80 >> 2] = 'h80; // FPSCR.EN.I
        start_bus();
        expect_stop(13, 'h08);
        assert (dut.expevt == 'h120 && dut.fpscr == 'h80 && dut.fpu_regs.fr_even[0] == 'h3f80_0000)
            else $fatal(1, "Enabled FADD exact-result software trap was not precise");

        // The divider raises FPSCR.Z and leaves the destination intact
        // when a zero divisor trap is enabled.
        image = '{default:32'hfffd_fffd};
        literal('h00, 0, 'h80);
        emit('h02, 'h406a);
        emit('h04, 'hf09d);
        emit('h06, 'hf18d);
        emit('h08, 'hf013);
        image['h80 >> 2] = 'h400; // FPSCR.EN.Z
        start_bus();
        expect_stop(13, 'h08);
        assert (dut.expevt == 'h120 && dut.fpscr == 'h8420 && dut.fpu_regs.fr_even[0] == 'h3f80_0000)
            else $fatal(1, "FDIV did not trap before updating FR0");

        // FSQRT software completion is controlled by EN.I alone.
        image = '{default:32'hfffd_fffd};
        literal('h00, 0, 'h80);
        emit('h02, 'h406a);
        emit('h04, 'hf09d);
        emit('h06, 'hf06d);
        literal('h08, 0, 'h84);
        emit('h0a, 'h406a);
        emit('h0c, 'hf06d);
        image['h80 >> 2] = 'h200; // EN.O must let the exact square root retire.
        image['h84 >> 2] = 'h80; // EN.I traps even for an exact result.
        start_bus();
        expect_stop(13, 'h0c);
        assert (dut.expevt == 'h120 && dut.fpscr == 'h80 && dut.fpu_regs.fr_even[0] == 'h3f80_0000)
            else $fatal(1, "FSQRT exact-result software trap was not precise");

        // Comparisons retire T, while conversions target the selected FR pair or FPUL.
        image = '{default:32'hfffd_fffd};
        emit('h00, 'he000);
        emit('h02, 'h406a); // FPSCR.PR=0
        emit('h04, 'he0fd);
        emit('h06, 'h405a);
        emit('h08, 'hf02d); // FLOAT FPUL,FR0
        emit('h0a, 'hf19d); // FLDI1 FR1
        emit('h0c, 'hf105); // FCMP/GT FR0,FR1
        emit('h0e, 'h0829); // MOVT R8
        emit('h10, 'hf014); // FCMP/EQ FR1,FR0
        emit('h12, 'h0929);
        emit('h14, 'hf03d); // FTRC FR0,FPUL
        emit('h16, 'h0a5a);
        literal('h18, 0, 'h80);
        emit('h1a, 'h406a); // FPSCR.PR=1
        literal('h1c, 0, 'h84);
        emit('h1e, 'h405a);
        emit('h20, 'hf2ad); // FCNVSD FPUL,DR2
        emit('h22, 'he000);
        emit('h24, 'h405a);
        emit('h26, 'hf2bd); // FCNVDS DR2,FPUL
        emit('h28, 'h0b5a);
        emit('h2a, 'hf23d); // FTRC DR2,FPUL
        emit('h2c, 'h0c5a);
        image['h80 >> 2] = 'h0008_0000;
        image['h84 >> 2] = 'h3fc0_0000;
        start_bus();
        wait (retired && retired_pc == 'h2c);
        assert (dut.gpr[8] == 1 && dut.gpr[9] == 0 && dut.gpr[10] == 'hffff_fffd &&
                dut.gpr[11] == 'h3fc0_0000 && dut.gpr[12] == 1)
            else $fatal(1, "Floating-point comparison or conversion did not retire");

        // A signaling NaN with enabled invalid leaves SR.T at its previous value.
        image = '{default:32'hfffd_fffd};
        literal('h00, 0, 'h80);
        emit('h02, 'h406a);
        literal('h04, 0, 'h84);
        emit('h06, 'h405a);
        emit('h08, 'hf00d); // FSTS FPUL,FR0
        emit('h0a, 'hf19d);
        emit('h0c, 'h0018); // SETT
        emit('h0e, 'hf014);
        image['h80 >> 2] = 'h800;
        image['h84 >> 2] = 'h7fc0_0000;
        start_bus();
        expect_stop(13, 'h0e);
        assert (dut.expevt == 'h120 && dut.fpscr == 'h10840 && dut.sr[0])
            else $fatal(1, "FCMP/EQ invalid exception changed SR.T");

        // FMAC consumes FR0, FRm, and the old FRn with one final rounding.
        image = '{default:32'hfffd_fffd};
        emit('h00, 'he000);
        emit('h02, 'h406a);
        literal('h04, 5, 'h80);
        emit('h06, 'hf059);
        emit('h08, 'hf159);
        emit('h0a, 'hf259);
        emit('h0c, 'hf21e); // FMAC FR0,FR1,FR2
        emit('h0e, 'hf21d);
        emit('h10, 'h085a);
        image['h80 >> 2] = 'h200;
        image['h200 >> 2] = 'h3f80_0001;
        image['h204 >> 2] = 'h3f7f_fffe;
        image['h208 >> 2] = 'hbf80_0000;
        start_bus();
        expect_stop(1, 'h12);
        assert (dut.gpr[8] == 'ha880_0000 && dut.fpscr == 0)
            else $fatal(1, "FMAC did not read all three operands or rounded the product early");

        // FIPR writes the last lane. FTRV uses the opposite bank in column order
        // and reads every source lane before publishing the first result.
        image = '{default:32'hfffd_fffd};
        emit('h00, 'he000);
        emit('h02, 'h406a);
        literal('h04, 5, 'h80);
        emit('h06, 'hfbfd);
        for (int lane = 0; lane < 16; lane++)
            emit('h08 + lane * 2, {4'hf, 4'(lane), 8'h59});
        emit('h28, 'hfbfd);
        for (int lane = 4; lane < 12; lane++)
            emit('h2a + (lane - 4) * 2, {4'hf, 4'(lane), 8'h59});
        emit('h3a, 'hf9ed); // FIPR FV4,FV8
        emit('h3c, 'hf5fd); // FTRV XMTRX,FV4
        emit('h3e, 'h086a); // STS FPSCR,R8
        literal('h40, 0, 'h84);
        emit('h42, 'h406a);
        emit('h44, 'hf5fd); // EN.V traps before any matrix result writes.
        image['h80 >> 2] = 'h200;
        image['h84 >> 2] = 'h800;
        image['h200 >> 2] = 'h3f80_0000; // Matrix column 0: (1,2,0,0)
        image['h204 >> 2] = 'h4000_0000;
        image['h208 >> 2] = 0;
        image['h20c >> 2] = 0;
        image['h210 >> 2] = 0;           // Matrix column 1: (0,1,-1,0)
        image['h214 >> 2] = 'h3f80_0000;
        image['h218 >> 2] = 'hbf80_0000;
        image['h21c >> 2] = 0;
        image['h220 >> 2] = 0;           // Matrix column 2: (0,0,1,0)
        image['h224 >> 2] = 0;
        image['h228 >> 2] = 'h3f80_0000;
        image['h22c >> 2] = 0;
        image['h230 >> 2] = 'h4080_0000; // Matrix column 3: (4,0,0,1)
        image['h234 >> 2] = 0;
        image['h238 >> 2] = 0;
        image['h23c >> 2] = 'h3f80_0000;
        image['h240 >> 2] = 'h3f80_0000; // FV4: (1,2,3,4)
        image['h244 >> 2] = 'h4000_0000;
        image['h248 >> 2] = 'h4040_0000;
        image['h24c >> 2] = 'h4080_0000;
        image['h250 >> 2] = 'h40a0_0000; // FV8: (5,6,7,8)
        image['h254 >> 2] = 'h40c0_0000;
        image['h258 >> 2] = 'h40e0_0000;
        image['h25c >> 2] = 'h4100_0000;
        start_bus();
        expect_stop(13, 'h44);
        assert (dut.fpu_regs.fr_odd[5] == 'h428c_0000 &&
                dut.fpu_regs.fr_even[2] == 'h4188_0000 && dut.fpu_regs.fr_odd[2] == 'h4080_0000 &&
                dut.fpu_regs.fr_even[3] == 'h3f80_0000 && dut.fpu_regs.fr_odd[3] == 'h4080_0000 &&
                dut.gpr[8] == 'h1004 && dut.fpscr == 'h1804)
            else $fatal(1, "Vector execution or precise FTRV software trap failed");

        // The three compound instructions are illegal in double-precision mode.
        for (int instruction = 0; instruction < 3; instruction++) begin
            image = '{default:32'hfffd_fffd};
            literal('h00, 0, 'h80);
            emit('h02, 'h406a);
            case (instruction)
                0: emit('h04, 'hf20e);
                1: emit('h04, 'hf9ed);
                2: emit('h04, 'hf5fd);
                default: ;
            endcase
            image['h80 >> 2] = 'h0008_0000;
            start_bus();
            expect_stop(1, 'h04);
        end

        // FSRRA writes a reciprocal root and FSCA writes an even/odd pair.
        image = '{default:32'hfffd_fffd};
        emit('h00, 'he000);
        emit('h02, 'h406a);
        literal('h04, 5, 'h80);
        emit('h06, 'hf459); // 4.0 in FR4
        emit('h08, 'hf47d); // FSRRA FR4
        emit('h0a, 'hf41d);
        emit('h0c, 'h085a);
        literal('h0e, 0, 'h84);
        emit('h10, 'h405a);
        emit('h12, 'hf2fd); // FSCA FPUL,DR2, a quarter revolution
        emit('h14, 'h096a);
        literal('h16, 0, 'h88);
        emit('h18, 'h406a);
        emit('h1a, 'he000);
        emit('h1c, 'h405a);
        emit('h1e, 'hf2fd); // EN.I traps before overwriting either lane.
        image['h80 >> 2] = 'h200;
        image['h84 >> 2] = 'h0123_4000;
        image['h88 >> 2] = 'h80;
        image['h200 >> 2] = 'h4080_0000;
        start_bus();
        expect_stop(13, 'h1e);
        assert (dut.gpr[8] == 'h3f00_0000 && dut.gpr[9] == 'h1004 &&
                dut.fpu_regs.fr_even[1] == 'h3f80_0000 && dut.fpu_regs.fr_odd[1][30:0] == 0 &&
                dut.fpscr == 'h1084)
            else $fatal(1, "Approximate floating-point instruction failed");

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

        // Exercise the on-chip timer and interrupt arbitration through
        // the same wrapper that is used for the Quartus diagnostic.
        @(negedge clk);
        reset = 1;
        bram_reset = 1;
        image = '{default:32'hfffd_fffd};
        literal('h00, 1, 'h80);
        literal('h02, 2, 'h84);
        emit('h04, 'h2121); // IPRA: TMU0 priority 5
        literal('h06, 3, 'h88);
        emit('h08, 'he210);
        emit('h0a, 'h1323); // TCNT0 = 16, allowing SLEEP to retire even with bus arbitration
        literal('h0c, 4, 'h8c);
        emit('h0e, 'he220);
        emit('h10, 'h2421); // TCR0: interrupt enable, peripheral clock /4
        literal('h12, 5, 'h90);
        emit('h14, 'h450e); // Clear BL and IMASK while remaining privileged
        literal('h16, 6, 'h94);
        emit('h18, 'he001);
        emit('h1a, 'h2600); // TSTR: start timer 0
        emit('h1c, 'h001b); // SLEEP
        emit('h1e, 'hee2d);
        image['h80 >> 2] = 'hffd0_0004;
        image['h84 >> 2] = 'h5000;
        image['h88 >> 2] = 'hffd8_0000;
        image['h8c >> 2] = 'hffd8_0010;
        image['h90 >> 2] = 'h4000_0000;
        image['h94 >> 2] = 'hffd8_0004;
        literal('h600, 1, 'h640);
        emit('h602, 'he000);
        emit('h604, 'h2100); // Stop TMU0
        literal('h606, 2, 'h644);
        emit('h608, 'h2201); // Clear UNF
        literal('h60a, 3, 'h648);
        emit('h60c, 'h6d32); // Read INTEVT in the interrupt handler
        emit('h60e, 'h002b);
        emit('h610, 'h0009);
        image['h640 >> 2] = 'hffd8_0004;
        image['h644 >> 2] = 'hffd8_0010;
        image['h648 >> 2] = 'hff00_0028;
        for (int word = 0; word <= 'h648 >> 2; word++) begin
            @(negedge clk);
            load_we = 1;
            load_word_addr = 9'(word);
            load_data = image[word];
        end
        @(negedge clk);
        load_we = 0;
        bram_reset = 0;
        wait (bram_fault != 0 || (bram_retired && bram_retired_pc == 'h1e));
        @(negedge clk);
        assert (bram_fault == 0 && bram.system_cpu.cpu.gpr[13] == 'h400 &&
                bram.system_cpu.cpu.gpr[14] == 'h2d)
            else $fatal(1, "TMU guest fault %d at %h, INTEVT %h, marker %h",
                        bram_fault, bram_fault_pc, bram.system_cpu.cpu.gpr[13], bram.system_cpu.cpu.gpr[14]);
        bram_reset = 1;

        // Program the DMAC through P4, then take its completion interrupt.
        image = '{default:32'hfffd_fffd};
        literal('h00, 1, 'h80);
        literal('h02, 2, 'h84);
        emit('h04, 'h2121); // IPRC: DMAC priority 5
        literal('h06, 1, 'h88);
        literal('h08, 2, 'h8c);
        emit('h0a, 'h2122); // SAR0
        literal('h0c, 2, 'h90);
        emit('h0e, 'h1121); // DAR0
        emit('h10, 'he201);
        emit('h12, 'h1122); // DMATCR0
        literal('h14, 3, 'h94);
        emit('h16, 'h1133); // CHCR0
        literal('h18, 4, 'h98);
        emit('h1a, 'h440e); // Enable interrupts
        literal('h1c, 1, 'h9c);
        emit('h1e, 'he201);
        emit('h20, 'h2122); // DMAOR: start
        emit('h22, 'h001b); // SLEEP
        literal('h24, 1, 'ha0);
        emit('h26, 'h6e12); // Read the destination for the first time
        image['h80 >> 2] = 'hffd0_000c;
        image['h84 >> 2] = 'h0500;
        image['h88 >> 2] = 'hffa0_0000;
        image['h8c >> 2] = 'h0100;
        image['h90 >> 2] = 'h0140;
        image['h94 >> 2] = 'h5445;
        image['h98 >> 2] = 'h4000_0000;
        image['h9c >> 2] = 'hffa0_0040;
        image['ha0 >> 2] = 'h0140;
        image['h100 >> 2] = 'h0929_3aec;
        literal('h600, 1, 'h640);
        emit('h602, 'h6212); // Read CHCR0 before clearing TE
        emit('h604, 'he200);
        emit('h606, 'h2122);
        literal('h608, 3, 'h644);
        emit('h60a, 'h6d32); // Read INTEVT
        emit('h60c, 'h002b);
        emit('h60e, 'h0009);
        image['h640 >> 2] = 'hffa0_000c;
        image['h644 >> 2] = 'hff00_0028;
        for (int word = 0; word <= 'h644 >> 2; word++) begin
            @(negedge clk);
            load_we = 1;
            load_word_addr = 9'(word);
            load_data = image[word];
        end
        @(negedge clk);
        load_we = 0;
        bram_reset = 0;
        wait (bram_fault != 0 || (bram_retired && bram_retired_pc == 'h26));
        @(negedge clk);
        assert (bram_fault == 0 && bram.system_cpu.cpu.gpr[13] == 'h640 &&
                bram.system_cpu.cpu.gpr[14] == 'h0929_3aec)
            else $fatal(1, "DMA guest fault %d at %h, INTEVT %h, destination %h",
                        bram_fault, bram_fault_pc, bram.system_cpu.cpu.gpr[13], bram.system_cpu.cpu.gpr[14]);
        bram_reset = 1;

        // The reset handler sees the watchdog flag and manual reset event.
        image = '{default:32'hfffd_fffd};
        literal('h00, 1, 'h80);
        emit('h02, 'h6010); // MOV.B @R1,R0 (WTCSR)
        emit('h04, 'hc810); // TST #0x10,R0
        emit('h06, 'h8b0b); // BF 0x20 once the watchdog has reset the core
        literal('h08, 2, 'h84);
        literal('h0a, 3, 'h88);
        emit('h0c, 'h2231); // WTCNT: keyed write of 0xff
        literal('h0e, 3, 'h8c);
        emit('h10, 'h2131); // WTCSR: start in manual reset mode
        emit('h12, 'h001b); // SLEEP
        literal('h20, 2, 'h90);
        emit('h22, 'h6d22); // EXPEVT
        literal('h24, 3, 'h94);
        emit('h26, 'h2131); // Stop the watchdog and clear WOVF
        emit('h28, 'hee71);
        image['h80 >> 2] = 'hffc0_000c;
        image['h84 >> 2] = 'hffc0_0008;
        image['h88 >> 2] = 'h5aff;
        image['h8c >> 2] = 'ha5e0;
        image['h90 >> 2] = 'hff00_0024;
        image['h94 >> 2] = 'ha500;
        for (int word = 0; word <= 'h94 >> 2; word++) begin
            @(negedge clk);
            load_we = 1;
            load_word_addr = 9'(word);
            load_data = image[word];
        end
        @(negedge clk);
        load_we = 0;
        bram_reset = 0;
        wait (bram_fault != 0 || (bram_retired && bram_retired_pc == 'h28));
        @(negedge clk);
        assert (bram_fault == 0 && bram.system_cpu.cpu.gpr[13] == 'h020 &&
                bram.system_cpu.cpu.gpr[14] == 'h71)
            else $fatal(1, "Watchdog restart fault %d at %h, EXPEVT %h",
                        bram_fault, bram_fault_pc, bram.system_cpu.cpu.gpr[13]);
        bram_reset = 1;

        // Enable NMI entry with BL set, then verify that acceptance clears NMIB.
        image = '{default:32'hfffd_fffd};
        literal('h00, 1, 'h80);
        literal('h02, 2, 'h84);
        emit('h04, 'h2121); // ICR.NMIB
        emit('h06, 'h0009);
        emit('h08, 'haffe); // BRA 0x08
        emit('h0a, 'h0009);
        image['h80 >> 2] = 'hffd0_0000;
        image['h84 >> 2] = 'h0200;
        literal('h600, 1, 'h640);
        emit('h602, 'h6d12); // INTEVT
        literal('h604, 2, 'h644);
        emit('h606, 'h6e21); // ICR after NMI
        image['h640 >> 2] = 'hff00_0028;
        image['h644 >> 2] = 'hffd0_0000;
        for (int word = 0; word <= 'h644 >> 2; word++) begin
            @(negedge clk);
            load_we = 1;
            load_word_addr = 9'(word);
            load_data = image[word];
        end
        @(negedge clk);
        load_we = 0;
        bram_reset = 0;
        wait (bram_fault != 0 || (bram_retired && bram_retired_pc == 'h06));
        @(negedge clk);
        assert (bram_fault == 0);
        bram_nmi = 1;
        wait (bram_fault != 0 || (bram_retired && bram_retired_pc == 'h606));
        @(negedge clk);
        assert (bram_fault == 0 && bram.system_cpu.cpu.gpr[13] == 'h1c0 &&
                bram.system_cpu.cpu.gpr[14] == 0)
            else $fatal(1, "ICR guest fault %d at %h, INTEVT %h, ICR %h",
                        bram_fault, bram_fault_pc, bram.system_cpu.cpu.gpr[13], bram.system_cpu.cpu.gpr[14]);
        bram_nmi = 0;
        bram_reset = 1;

        // Wake on the RTC periodic interrupt from the board's oscillator tick.
        image = '{default:32'hfffd_fffd};
        literal('h00, 1, 'h80);
        emit('h02, 'he205);
        emit('h04, 'h2121); // IPRA: RTC priority 5
        literal('h06, 1, 'h84);
        emit('h08, 'he219);
        emit('h0a, 'h2120); // RTC periodic interrupt every 128 oscillator ticks
        literal('h0c, 3, 'h88);
        emit('h0e, 'h430e); // Enable interrupts
        emit('h10, 'h001b);
        emit('h12, 'hee53);
        image['h80 >> 2] = 'hffd0_0004;
        image['h84 >> 2] = 'hffc8_003c;
        image['h88 >> 2] = 'h4000_0000;
        literal('h600, 1, 'h640);
        emit('h602, 'he209);
        emit('h604, 'h2120); // Clear PEF and disable periodic requests
        literal('h606, 3, 'h644);
        emit('h608, 'h6d32); // INTEVT
        emit('h60a, 'h002b);
        emit('h60c, 'h0009);
        image['h640 >> 2] = 'hffc8_003c;
        image['h644 >> 2] = 'hff00_0028;
        for (int word = 0; word <= 'h644 >> 2; word++) begin
            @(negedge clk);
            load_we = 1;
            load_word_addr = 9'(word);
            load_data = image[word];
        end
        @(negedge clk);
        load_we = 0;
        bram_reset = 0;
        wait (bram_fault != 0 || (bram_retired && bram_retired_pc == 'h10));
        @(negedge clk);
        assert (bram_fault == 0);
        bram_rtc_tick = 1;
        wait (bram_fault != 0 || (bram_retired && bram_retired_pc == 'h12));
        @(negedge clk);
        assert (bram_fault == 0 && bram.system_cpu.cpu.gpr[13] == 'h4a0 &&
                bram.system_cpu.cpu.gpr[14] == 'h53)
            else $fatal(1, "RTC guest fault %d at %h, INTEVT %h",
                        bram_fault, bram_fault_pc, bram.system_cpu.cpu.gpr[13]);
        bram_rtc_tick = 0;
        bram_reset = 1;

        // Move two bytes through each serial port using separate TX and RX DMA channels.
        for (int port = 0; port < 2; port++) begin
            logic logical_sci;
            logical_sci = port == 1;
            image = '{default:32'hfffd_fffd};
            literal('h00, 1, 'h80);
            emit('h02, 'he200);
            emit('h04, 'h2120); // SCBRR = 0
            literal('h06, 1, 'h84);
            emit('h08, 'he201);
            emit('h0a, logical_sci ? 16'h0009 : 16'h2121); // SCFCR.LOOP or external test loopback
            literal('h0c, 1, 'h88);
            emit('h0e, 'he230);
            emit('h10, logical_sci ? 16'h2120 : 16'h2121); // SCSCR.TE and RE
            literal('h12, 1, 'h8c);
            literal('h14, 2, 'h90);
            emit('h16, 'h2122); // SAR0
            literal('h18, 2, 'h94);
            emit('h1a, 'h1121); // DAR0
            emit('h1c, 'he202);
            emit('h1e, 'h1122); // DMATCR0
            literal('h20, 3, 'h98);
            emit('h22, 'h1133); // CHCR0
            literal('h24, 1, 'h9c);
            literal('h26, 3, 'ha0);
            emit('h28, 'h2132); // SAR1
            literal('h2a, 3, 'ha4);
            emit('h2c, 'h1131); // DAR1
            emit('h2e, 'h1122); // DMATCR1
            literal('h30, 3, 'ha8);
            emit('h32, 'h1133); // CHCR1
            literal('h34, 1, 'hac);
            emit('h36, 'he201);
            emit('h38, 'h2122); // Start the DMAC.
            literal('h3a, 1, 'hb0);
            emit('h3c, 'h6012); // Wait for CHCR1.TE.
            emit('h3e, 'hc802);
            emit('h40, 'h89fc);
            literal('h42, 1, 'ha4);
            emit('h44, 'h6e12);
            image['h80 >> 2] = logical_sci ? 32'hffe0_0004 : 32'hffe8_0004;
            image['h84 >> 2] = 'hffe8_0018;
            image['h88 >> 2] = logical_sci ? 32'hffe0_0008 : 32'hffe8_0008;
            image['h8c >> 2] = 'hffa0_0000;
            image['h90 >> 2] = 'h0180;
            image['h94 >> 2] = logical_sci ? 32'hffe0_000c : 32'hffe8_000c;
            image['h98 >> 2] = logical_sci ? 32'h1811 : 32'h1a11;
            image['h9c >> 2] = 'hffa0_0010;
            image['ha0 >> 2] = logical_sci ? 32'hffe0_0014 : 32'hffe8_0014;
            image['ha4 >> 2] = 'h0184;
            image['ha8 >> 2] = logical_sci ? 32'h4911 : 32'h4b11;
            image['hac >> 2] = 'hffa0_0040;
            image['hb0 >> 2] = 'hffa0_001c;
            image['h180 >> 2] = 'h4893;
            image['h184 >> 2] = 0;
            for (int word = 0; word <= 'h184 >> 2; word++) begin
                @(negedge clk);
                load_we = 1;
                load_word_addr = 9'(word);
                load_data = image[word];
            end
            @(negedge clk);
            load_we = 0;
            bram_reset = 0;
            wait (bram_fault != 0 || (bram_retired && bram_retired_pc == 'h44));
            @(negedge clk);
            assert (bram_fault == 0 && bram.system_cpu.cpu.gpr[14] == 'h4893)
                else $fatal(1, "Serial DMA port %d fault %d at %h, buffer %h", logical_sci,
                            bram_fault, bram_fault_pc, bram.system_cpu.cpu.gpr[14]);
            bram_reset = 1;

        end

        // PORTEN and GPIOIC route a pin transition to the guest handler.
        image = '{default:32'hfffd_fffd};
        literal('h00, 1, 'h80);
        literal('h02, 2, 'h84);
        emit('h04, 'h2121); // BCR2.PORTEN
        literal('h06, 1, 'h88);
        emit('h08, 'he201);
        emit('h0a, 'h2121); // PDTRA: prepare pin 0 high
        literal('h0c, 1, 'h8c);
        emit('h0e, 'h2122); // PCTRA: configure pin 0 as an output
        literal('h10, 1, 'h90);
        emit('h12, 'he204);
        emit('h14, 'h2121); // GPIOIC: enable input pin 2
        literal('h16, 1, 'h94);
        literal('h18, 2, 'h98);
        emit('h1a, 'h2121); // IPRC: GPIO priority 5
        literal('h1c, 4, 'h9c);
        emit('h1e, 'h440e); // Enable interrupts
        emit('h20, 'h001b);
        emit('h22, 'hee62);
        image['h80 >> 2] = 'hff80_0004;
        image['h84 >> 2] = 'h3ffd;
        image['h88 >> 2] = 'hff80_0030;
        image['h8c >> 2] = 'hff80_002c;
        image['h90 >> 2] = 'hff80_0048;
        image['h94 >> 2] = 'hffd0_000c;
        image['h98 >> 2] = 'h5000;
        image['h9c >> 2] = 'h4000_0000;
        literal('h600, 3, 'h640);
        emit('h602, 'h6d32); // INTEVT
        literal('h604, 3, 'h644);
        emit('h606, 'h6c31); // PDTRA
        literal('h608, 3, 'h648);
        emit('h60a, 'he200);
        emit('h60c, 'h2321); // Mask GPIO interrupt
        emit('h60e, 'h002b);
        emit('h610, 'h0009);
        image['h640 >> 2] = 'hff00_0028;
        image['h644 >> 2] = 'hff80_0030;
        image['h648 >> 2] = 'hff80_0048;
        for (int word = 0; word <= 'h648 >> 2; word++) begin
            @(negedge clk);
            load_we = 1;
            load_word_addr = 9'(word);
            load_data = image[word];
        end
        @(negedge clk);
        load_we = 0;
        bram_reset = 0;
        wait (bram_fault != 0 || (bram_retired && bram_retired_pc == 'h20));
        @(negedge clk);
        assert (bram_fault == 0 && bram_gpio_out == 20'h1 && bram_gpio_oe == 20'h1 &&
                bram_gpio_pullup == 20'hf_fffe)
            else $fatal(1, "GPIO pins did not follow BCR2 and PCTRA");
        bram_gpio_in[2] = 0;
        wait (bram_fault != 0 || (bram_retired && bram_retired_pc == 'h22));
        @(negedge clk);
        assert (bram_fault == 0 && bram.system_cpu.cpu.gpr[13] == 'h620 &&
                bram.system_cpu.cpu.gpr[12] == 'hffff_fffb && bram.system_cpu.cpu.gpr[14] == 'h62)
            else $fatal(1, "GPIO guest fault %d at %h, INTEVT %h, PDTRA %h", bram_fault,
                        bram_fault_pc, bram.system_cpu.cpu.gpr[13], bram.system_cpu.cpu.gpr[12]);
        bram_reset = 1;
        bram_gpio_in = '1;

        // Type 1 standby waits for the watchdog's 32-tick stabilization.
        // The SCI register port is inaccessible with MSTP0 set.
        image = '{default:32'hfffd_fffd};
        literal('h00, 1, 'h80);
        literal('h02, 2, 'h84);
        emit('h04, 'h2121); // FRQCR
        literal('h06, 1, 'h88);
        emit('h08, 'he220);
        emit('h0a, 'h2120); // SCSCR1.TE
        literal('h0c, 1, 'h8c);
        emit('h0e, 'he2c1);
        emit('h10, 'h2120); // Enter standby on SLEEP, enable PHZ and MSTP0
        literal('h12, 1, 'h88);
        emit('h14, 'h6b10); // Stopped SCI read
        literal('h16, 1, 'h90);
        literal('h18, 2, 'h94);
        emit('h1a, 'h2121); // WTCNT = 0xff
        emit('h1c, 'h001b);
        literal('h1e, 1, 'h8c);
        emit('h20, 'he200);
        emit('h22, 'h2120); // Restart SCI
        literal('h24, 1, 'h88);
        emit('h26, 'h6c10); // SCSCR1 resets while stopped
        emit('h28, 'hee71);
        image['h80 >> 2] = 'hffc0_0000;
        image['h84 >> 2] = 'h0e0b;
        image['h88 >> 2] = 'hffe0_0008;
        image['h8c >> 2] = 'hffc0_0004;
        image['h90 >> 2] = 'hffc0_0008;
        image['h94 >> 2] = 'h5aff;
        literal('h600, 1, 'h640);
        emit('h602, 'h6d12); // INTEVT
        literal('h604, 1, 'h644);
        emit('h606, 'h6a10); // WTCSR: stabilization must not set WOVF or IOVF
        emit('h608, 'h002b);
        emit('h60a, 'h0009);
        image['h640 >> 2] = 'hff00_0028;
        image['h644 >> 2] = 'hffc0_000c;
        for (int word = 0; word <= 'h644 >> 2; word++) begin
            @(negedge clk);
            load_we = 1;
            load_word_addr = 9'(word);
            load_data = image[word];
        end
        @(negedge clk);
        load_we = 0;
        bram_reset = 0;
        wait (bram_fault != 0 || bram_freq_change);
        assert (bram_fault == 0 && bram_frequency_control == 'he0b)
            else $fatal(1, "FRQCR access did not report the new clock control");
        wait (bram_fault != 0 || bram_standby);
        @(negedge clk);
        assert (bram_fault == 0 && bram_processor_status == 1 && bram_high_z &&
                bram_peripheral_pullup && !bram_deep_sleep);
        bram_nmi = 1;
        repeat (31) @(negedge clk);
        assert (bram_standby) else $fatal(1, "Standby bypassed watchdog stabilization");
        wait (!bram_standby);
        wait (bram_fault != 0 || (bram_retired && bram_retired_pc == 'h28));
        @(negedge clk);
        assert (bram_fault == 0 && bram.system_cpu.cpu.gpr[10] == 0 &&
                bram.system_cpu.cpu.gpr[11] == 0 && bram.system_cpu.cpu.gpr[12] == 0 &&
                bram.system_cpu.cpu.gpr[13] == 'h1c0 && bram.system_cpu.cpu.gpr[14] == 'h71)
            else $fatal(1, "Standby wake or SCI reset failed at %h, INTEVT %h",
                        bram_fault_pc, bram.system_cpu.cpu.gpr[13]);
        bram_reset = 1;
        bram_nmi = 0;

        // Deep sleep exits on NMI without the watchdog stabilization delay.
        image = '{default:32'hfffd_fffd};
        literal('h00, 1, 'h80);
        emit('h02, 'he280);
        emit('h04, 'h2120); // STBCR2.DSLP
        emit('h06, 'h001b);
        emit('h08, 'hee27);
        image['h80 >> 2] = 'hffc0_0010;
        emit('h600, 'h002b);
        emit('h602, 'h0009);
        for (int word = 0; word <= 'h602 >> 2; word++) begin
            @(negedge clk);
            load_we = 1;
            load_word_addr = 9'(word);
            load_data = image[word];
        end
        @(negedge clk);
        load_we = 0;
        bram_reset = 0;
        wait (bram_fault != 0 || bram_deep_sleep);
        @(negedge clk);
        assert (bram_fault == 0 && !bram_standby && bram_processor_status == 2);
        bram_nmi = 1;
        repeat (8) @(negedge clk);
        assert (!bram_deep_sleep) else $fatal(1, "Deep sleep unexpectedly delayed NMI wake");
        wait (bram_fault != 0 || (bram_retired && bram_retired_pc == 'h08));
        @(negedge clk);
        assert (bram_fault == 0 && bram.system_cpu.cpu.gpr[14] == 'h27);
        bram_reset = 1;
        bram_nmi = 0;

        assert (cpg_finished) else $fatal(1, "CPG test did not finish");
        assert (gpio_finished) else $fatal(1, "GPIO test did not finish");
        assert (fp_arith_finished) else $fatal(1, "Floating-point arithmetic checks did not finish");
        assert (mmu_done) else $fatal(1, "MMU unit check did not finish");
        assert (cache_cancel_done) else $fatal(1, "Cache cancellation check did not finish");

        assert (tmu_finished) else $fatal(1, "TMU/INTC regression timed out");
        assert (dma_finished) else $fatal(1, "DMAC regression timed out");
        assert (bsc_finished) else $fatal(1, "BSC regression timed out");
        wait (rtc_finished && scif_finished && sci_finished);
        $display("PASS SH-4 integer, stalls, delay slots, caches, SH-4 MMU control, page sizes, permissions, area 7, exception return, extended integer execution, MAC, control transfers, interrupts, byte operations, copy-back, cache maintenance, cache index and RAM modes, store queues, TLB arrays, cache arrays, FPU transfers, arithmetic, comparisons, conversions, vectors, FSCA, FSRRA, BSC, TMU, DMAC, WDT, RTC, SCI, SCIF, GPIO, CPG, standby, and bus and interrupt arbitration");
        $finish;
    end

    initial begin
        #1100000;
        $display("Diagnostic BRAM reset %b, last retirement %h, fault %d at %h",
                 bram_reset, bram_retired_pc, bram_fault, bram_fault_pc);
        $fatal(1, "Timed out at PC %h (last retirement %h, fault %d at %h)",
               dut.pc, retired_pc, fault, fault_pc);
    end
endmodule
