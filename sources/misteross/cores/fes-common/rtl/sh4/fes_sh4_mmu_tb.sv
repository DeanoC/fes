module fes_sh4_mmu_tb(input logic clk, output logic done);
    logic reset = 1, request = 0, reg_write = 0, ldtlb = 0;
    logic instruction, write, supervisor, ready, response, cacheable, enabled;
    logic [3:0] fault, reg_select;
    logic [31:0] virtual_addr, physical_addr, reg_wdata, reg_rdata;

    fes_sh4_mmu dut (.*);

    task automatic config_write(input logic [3:0] sel, input logic [31:0] data);
        @(negedge clk);
        reg_select = sel;
        reg_wdata = data;
        reg_write = 1;
        @(negedge clk);
        reg_write = 0;
    endtask

    task automatic install(input logic [5:0] idx, input logic [31:0] vpn, ppn,
                           input logic [7:0] asid, input logic [8:0] flags);
        config_write(4, {16'b0, idx, 9'b0, 1'b1});
        config_write(0, vpn | {24'b0, asid});
        config_write(1, ppn | {23'b0, flags});
        @(negedge clk);
        ldtlb = 1;
        @(negedge clk);
        ldtlb = 0;
    endtask

    task automatic translate(input logic [31:0] va, pa,
                             input logic insn, store, priv, cached,
                             input logic [3:0] error);
        wait (ready);
        @(negedge clk);
        assert (enabled) else $fatal(1, "AT cleared unexpectedly");
        virtual_addr = va;
        instruction = insn;
        write = store;
        supervisor = priv;
        request = 1;
        @(negedge clk);
        request = 0;
        wait (response);
        @(negedge clk);
        assert (fault == error)
            else $fatal(1, "MMU translation %h returned fault %d, expected %d", va, fault, error);
        if (error == 0)
            assert (physical_addr == pa && cacheable == cached)
                else $fatal(1, "MMU translated %h to %h (C=%b), expected %h (C=%b)",
                            va, physical_addr, cacheable, pa, cached);
    endtask

    task automatic expect_register(input logic [3:0] sel, input logic [31:0] mask, data);
        reg_select = sel;
        @(negedge clk);
        assert ((reg_rdata & mask) == data)
            else $fatal(1, "MMU register %d = %h (mask %h), expected %h", sel, reg_rdata, mask, data);
    endtask

    initial begin
        done = 0;
        @(negedge clk);
        assert (!enabled) else $fatal(1, "AT must reset clear");
        reset = 0;

        config_write(2, 'h0c00_0400);
        expect_register(2, '1, 'h0c00_0400);

        // Use separate reduction banks and all four supported page sizes.
        install(63, 'h1234_5000, 'h0112_3000, 7, 9'h16c);
        expect_register(4, 'h0000_fc01, 'h0000_fc01); // LDTLB leaves URC at 63.
        install(55, 'h2245_6000, 'h0456_7000, 7, 9'h174);
        install(32, 'h3456_0000, 'h0365_0000, 7, 9'h1ec);
        install(17, 'h4560_0000, 'h0510_0000, 7, 9'h1fe); // Shared 1 MiB page
        translate('h1234_52d4, 'h0112_32d4, 0, 1, 0, 1, 0);
        translate('h2245_63ac, 'h0456_73ac, 0, 0, 0, 0, 0);
        translate('h3456_b6c8, 'h0365_b6c8, 0, 0, 0, 1, 0);
        translate('h456f_2478, 'h051f_2478, 0, 0, 0, 1, 0);

        config_write(4, 'h000c_0801); // URB 3, URC 2
        translate('h1234_5004, 'h0112_3004, 1, 0, 0, 1, 0);
        expect_register(4, 'h0000_fc00, 0); // ITLB miss consulted UTLB and wrapped.
        translate('h1234_5010, 'h0112_3010, 1, 0, 0, 1, 0);
        expect_register(4, 'h0000_fc00, 0); // ITLB hit leaves URC alone.

        config_write(0, 8);
        translate('h4562_0120, 'h0512_0120, 0, 0, 0, 1, 0); // Shared ignores ASID.
        translate('h1234_521e, 0, 0, 0, 0, 0, 5);
        expect_register(3, '1, 'h1234_521e);
        expect_register(0, '1, 'h1234_5008);
        config_write(4, 'h0000_0101); // Supervisor single-virtual mode
        translate('h1234_5004, 'h0112_3004, 0, 0, 1, 1, 0);
        translate('h1234_5004, 0, 0, 0, 0, 0, 5); // User still checks ASID.

        install(5, 'h1000_0000, 'h0000_4000, 8, 9'h148); // User read-only.
        translate('h1000_0018, 'h0000_4018, 0, 0, 0, 1, 0);
        translate('h1000_0018, 0, 0, 1, 0, 0, 7);
        install(5, 'h1000_0000, 'h0000_4000, 8, 9'h168); // Writable, clean.
        translate('h1000_0018, 0, 0, 1, 0, 0, 8);
        install(22, 'h1000_0000, 'h0000_8000, 8, 9'h16c);
        translate('h1000_0018, 0, 0, 0, 0, 0, 6); // Duplicate across banks.
        config_write(4, 5); // Enable AT and invalidate both TLBs.
        expect_register(4, 5, 1);
        config_write(0, 7);
        translate('h1234_5004, 0, 1, 0, 0, 0, 5);
        done = 1;
    end
endmodule
