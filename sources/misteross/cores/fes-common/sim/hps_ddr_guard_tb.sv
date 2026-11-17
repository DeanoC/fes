// SPDX-License-Identifier: GPL-2.0-or-later
// Exercise the shared guard's public command and hold/drain contract.
module hps_ddr_guard_tb;
    parameter DATA_W = 64;
    parameter ADDR_W = 29;
    localparam BE_W = DATA_W / 8;

    reg clk = 0;
    always #5 clk = ~clk;
    reg hold = 0, read = 0, write = 0, m_waitrequest = 0, m_readdatavalid = 0;
    reg [ADDR_W-1:0] address = 0;
    reg [7:0] burstcount = 1;
    reg [DATA_W-1:0] writedata = 0, m_readdata = 0;
    reg [BE_W-1:0] byteenable = 0;
    wire drained, waitrequest, readdatavalid, m_read, m_write;
    wire [DATA_W-1:0] readdata, m_writedata;
    wire [ADDR_W-1:0] m_address;
    wire [7:0] m_burstcount;
    wire [BE_W-1:0] m_byteenable;

    fes_hps_ddr_guard #(.DATA_W(DATA_W), .ADDR_W(ADDR_W)) dut (.*);

    // Inputs change at the falling edge; checks see settled registered outputs.
    task tick;
        @(posedge clk); #1; @(negedge clk);
    endtask
    task check(input bit condition, input string message);
        if (!condition) $fatal(1, "%s", message);
    endtask
    task command(input [ADDR_W-1:0] a, input [7:0] n,
                 input [DATA_W-1:0] d, input [BE_W-1:0] be);
        address = a; burstcount = n; writedata = d; byteenable = be;
    endtask
    task expect_write(input [ADDR_W-1:0] a, input [7:0] n,
                      input [DATA_W-1:0] d, input [BE_W-1:0] be);
        check(m_write && !m_read, "expected write");
        check(m_address == a && m_burstcount == n && m_byteenable == be,
              "write command changed or out of order");
        check(m_writedata == d, "write data changed or out of order");
    endtask

    localparam [DATA_W-1:0] DATA_A = {DATA_W/8{8'hA5}};
    localparam [DATA_W-1:0] DATA_B = {DATA_W/8{8'h3C}};
    localparam [DATA_W-1:0] DATA_C = {DATA_W/16{16'h12e7}} ^
                                       {{DATA_W/2{1'b1}}, {DATA_W/2{1'b0}}};
    reg [DATA_W-1:0] stalled_data;
    localparam [BE_W-1:0] MASK_A = {BE_W/2{2'b10}};
    localparam [BE_W-1:0] MASK_B = {BE_W{1'b1}};

    initial begin
        // Full skid: hold A stable, queue B once, and block C until B moves.
        command('h12, 1, DATA_A, MASK_A); write = 1;
        tick(); expect_write('h12, 1, DATA_A, MASK_A);
        m_waitrequest = 1; command('h21, 1, DATA_B, MASK_B);
        tick(); expect_write('h12, 1, DATA_A, MASK_A);
        check(waitrequest, "skid did not block the core");
        command('h32, 1, DATA_C, MASK_A);
        repeat (3) begin
            tick(); expect_write('h12, 1, DATA_A, MASK_A);
            check(waitrequest, "core accepted while skid was full");
        end
        m_waitrequest = 0;
        check(waitrequest, "full skid must block core on retire edge");
        tick(); expect_write('h21, 1, DATA_B, MASK_B);
        check(!waitrequest, "core remained blocked after skid retirement");
        // B retires and C is accepted on the same edge.
        tick(); expect_write('h32, 1, DATA_C, MASK_A);
        command('h43, 1, DATA_A, MASK_B);
        tick(); expect_write('h43, 1, DATA_A, MASK_B);
        write = 0; tick(); check(!m_write, "write did not retire");

        // Start a four-beat write, leave beat 2 in skid, then abandon it.
        command('h55, 4, DATA_A, MASK_A); write = 1;
        tick(); expect_write('h55, 4, DATA_A, MASK_A);
        m_waitrequest = 1; command('h55, 4, DATA_B, MASK_B);
        tick(); expect_write('h55, 4, DATA_A, MASK_A);
        hold = 1; command('h77, 1, DATA_C, MASK_B);
        tick(); check(waitrequest && !drained, "hold did not contain burst");
        expect_write('h55, 4, DATA_A, MASK_A);
        m_waitrequest = 0;
        tick(); expect_write('h55, 4, DATA_B, MASK_B);
        tick();
        check(m_write && m_address == 'h55 && m_burstcount == 4 && m_byteenable == 0,
              "first filler beat missing");
        // Release hold while filler remains: finishing must keep the core blocked.
        stalled_data = m_writedata;
        hold = 0; m_waitrequest = 1;
        check(waitrequest, "core unblocked before burst finishing");
        tick(); check(waitrequest, "core unblocked while filler was queued");
        check(m_write && m_address == 'h55 && m_burstcount == 4 &&
                      m_byteenable == 0 && m_writedata == stalled_data,
                      "filler changed under stall");
        m_waitrequest = 0;
        tick(); check(m_write && m_address == 'h55 && m_burstcount == 4 && m_byteenable == 0,
                      "second filler beat missing");
        // Stop offering the blocked core command before the now-free next edge.
        write = 0;
        tick(); check(!m_write && !waitrequest, "burst did not finish after hold release");
        tick(); check(drained, "completed burst did not drain");

        // Multi-beat read response during and after hold must be hidden.
        read = 1; command('h66, 3, 0, 0);
        tick(); check(m_read && m_address == 'h66 && m_burstcount == 3, "read missing");
        read = 0; hold = 1; tick();
        check(!drained && waitrequest && !readdatavalid, "read hold did not block");
        m_readdatavalid = 1; m_readdata = DATA_A; tick();
        check(!readdatavalid && !drained, "held response escaped or drained early");
        hold = 0; m_readdata = DATA_B; tick();
        check(!readdatavalid && waitrequest && !drained, "old read escaped after hold");
        m_readdata = DATA_C; tick();
        check(!readdatavalid && waitrequest && !drained, "old response escaped during drain");
        m_readdatavalid = 0;
        repeat (4) begin
            tick(); check(!readdatavalid, "old response leaked from pipeline");
        end
        check(!waitrequest && drained, "read response pipeline failed to drain");

        // A fresh read after drain must be delivered normally.
        read = 1; command('h70, 1, 0, 0); tick();
        check(m_read && m_address == 'h70, "fresh read was not presented");
        read = 0; tick(); m_readdatavalid = 1; m_readdata = DATA_C; tick();
        m_readdatavalid = 0; tick();
        check(readdatavalid && readdata == DATA_C, "fresh read response missing");
        tick(); check(!readdatavalid, "fresh read response repeated");
        $display("PASS guard width %0d", DATA_W);
        $finish;
    end
    initial begin #10000; $fatal(1, "guard test timed out"); end
endmodule
