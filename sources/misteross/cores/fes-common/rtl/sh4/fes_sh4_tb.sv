module fes_sh4_tb;
    logic clk = 0;
    always #5 clk <= ~clk;

    logic reset = 1, bram_reset = 1, load_we = 0;
    logic [8:0] load_word_addr;
    logic [31:0] load_data;
    logic valid, ready, rvalid, retired, bram_retired;
    logic [31:0] addr, wdata, rdata, retired_pc, fault_pc, bram_retired_pc, bram_fault_pc;
    logic [2:0] fault, bram_fault;
    logic [3:0] wstrb;
    logic [31:0] image [0:511];
    logic [31:0] ext_mem [0:511];
    int cycle, latency;
    logic held;
    logic [67:0] held_request;
    int retire_count;

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
        end else begin
            cycle <= cycle + 1;
            if (retired) begin
                assert (retired_pc[0] == 0) else $fatal(1, "Retired an odd PC");
                if (retired_pc == 32'h14)
                    assert (dut.gpr[6] == 32'hffff_ff80) else $fatal(1, "Aliased load adjusted its base");
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
                assert (latency == 0 && addr[1:0] == 0 && addr < 2048)
                    else $fatal(1, "Unexpected memory request at %h", addr);
                if (wstrb == 0) begin
                    rdata <= ext_mem[addr[10:2]];
                    latency <= 1 + cycle % 3;
                end else begin
                    for (int lane = 0; lane < 4; lane++)
                        if (wstrb[lane])
                            ext_mem[addr[10:2]][8*lane +: 8] <= wdata[8*lane +: 8];
                end
            end
        end
    end

    task automatic emit(input int address, input logic [15:0] opcode);
        image[address >> 2][16*address[1] +: 16] = opcode;
    endtask

    task automatic start_bus;
        @(negedge clk);
        reset = 1;
        @(negedge clk);
        ext_mem = image;
        reset = 0;
    endtask

    task automatic expect_stop(input logic [2:0] cause, input logic [31:0] pc);
        wait (fault != 0);
        @(negedge clk);
        assert (fault == cause && fault_pc == pc)
            else $fatal(1, "Expected fault %d at %h, got %d at %h", cause, pc, fault, fault_pc);
        repeat (4) begin
            @(negedge clk);
            assert (!valid && !retired && fault == cause && fault_pc == pc)
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

        $display("PASS SH-4 integer execution, delay slots, faults, delayed bus and synchronous BRAM");
        $finish;
    end

    initial begin
        #20000;
        $fatal(1, "Timed out");
    end
endmodule
