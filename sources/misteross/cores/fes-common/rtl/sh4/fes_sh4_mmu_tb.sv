module fes_sh4_mmu_tb(input logic clk, output logic done);
    logic reset = 1, request = 0, reg_write = 0, ldtlb = 0;
    logic instruction, write, supervisor, ready, response, cacheable, enabled;
    logic [3:0] fault, reg_select;
    logic [31:0] virtual_addr, physical_addr, reg_wdata, reg_rdata;

    logic unused_write_through, unused_sqmd;
    logic array_access = 0;
    logic [31:0] array_rdata;
    fes_sh4_mmu dut (.write_through(unused_write_through), .sqmd(unused_sqmd), .quiet(1'b0), .address_exception(1'b0), .fault_address(32'b0), .*);

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

    task automatic array_transfer(input logic [31:0] addr, data,
                                  input logic do_write, input logic [3:0] error);
        wait (ready);
        @(negedge clk);
        virtual_addr = addr;
        reg_wdata = data;
        instruction = 0;
        write = do_write;
        supervisor = 1;
        array_access = 1;
        request = 1;
        @(negedge clk);
        request = 0;
        array_access = 0;
        wait (response);
        @(negedge clk);
        assert (fault == error)
            else $fatal(1, "TLB array access %h reported %d, expected %d", addr, fault, error);
        if (!do_write)
            assert (array_rdata == data)
                else $fatal(1, "TLB array %h read %h, expected %h", addr, array_rdata, data);
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
        // Indexed reads include invalid entries and ignore the A bit.
        array_transfer('hf600_2700, 'h1234_5107, 1, 0);
        array_transfer('hf700_2700, 'h0112_3179, 1, 0);
        array_transfer('hf780_2700, 'h0000_000d, 1, 0);
        array_transfer('hf600_2780, 'h1234_5107, 0, 0);
        array_transfer('hf700_2700, 'h0112_3179, 0, 0);
        array_transfer('hf780_2700, 'h0000_000d, 0, 0);
        translate('h1234_5198, 0, 0, 1, 0, 0, 8);
        array_transfer('hf600_0080, 'h1234_5f99, 1, 0); // ASID comes from PTEH, not this value.
        translate('h1234_5198, 'h0112_3198, 0, 1, 0, 1, 0);

        config_write(4, 1); // Reset the ITLB replacement order, preserving entries.
        translate('h1234_5004, 'h0112_3004, 1, 0, 0, 1, 0);
        array_transfer('hf200_0300, 'h1234_5107, 0, 0);
        array_transfer('hf380_0300, 'hd, 0, 0);
        array_transfer('hf700_2700, 'h0116_317d, 1, 0);
        translate('h1234_5004, 'h0112_3004, 1, 0, 0, 1, 0); // ITLB still has the old mapping.
        array_transfer('hf600_0080, 'h1234_5300, 1, 0);
        translate('h1234_5004, 'h0116_3004, 1, 0, 0, 1, 0);
        array_transfer('hf300_0300, 'h0116_3158, 0, 0);

        config_write(0, 8);
        array_transfer('hf600_0080, 'h1234_5007, 1, 0); // Wrong PTEH.ASID is a no-op.
        array_transfer('hf600_2700, 'h1234_5307, 0, 0);
        config_write(0, 7);
        array_transfer('hf600_0080, 'h1234_5000, 1, 0);
        array_transfer('hf600_2700, 'h1234_5007, 0, 0);
        translate('h1234_5004, 0, 1, 0, 0, 0, 5);

        // Direct ITLB-only translation, including the data2 range.
        array_transfer('hf200_0100, 'h2222_2107, 1, 0);
        array_transfer('hf300_0100, 'h0456_7148, 1, 0);
        array_transfer('hf380_0100, 'hb, 1, 0);
        array_transfer('hf380_0100, 'hb, 0, 0);
        translate('h2222_2080, 'h0456_7080, 1, 0, 0, 1, 0);
        array_transfer('hf600_0080, 'h2222_2000, 1, 0); // ITLB hit and UTLB miss.
        translate('h2222_2080, 0, 1, 0, 0, 0, 5);

        install(8, 'h4000_0000, 'h0000_1000, 7, 9'h16d);
        install(61, 'h4000_0000, 'h0000_2000, 7, 9'h16d);
        array_transfer('hf600_0080, 'h4000_0000, 1, 6);
        array_transfer('hf600_0800, 'h4000_0307, 0, 0);
        array_transfer('hf600_3d00, 'h4000_0307, 0, 0);

        done = 1;
    end
endmodule
