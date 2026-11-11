// SPDX-License-Identifier: MIT
// Spike S1d bench (#1581): drives one return-stack variant with a fixed
// pseudo-random sequence and prints the inputs and top on every clock.
// compare_traces.py checks the variants' traces against the current form's.
module s1d_tb;
    logic clk = 0;
    logic reset, redirect, set_wr, push, pop;
    logic [3:0]  set_ptr;
    logic [31:0] set_lin, push_lin, top;

    fes_i386_ras dut (.*);

    logic [63:0] s = 64'h1581_51D0_DEAD_BEEF;
    function automatic logic [31:0] rnd();
        s ^= s << 13; s ^= s >> 7; s ^= s << 17;
        return s[47:16];
    endfunction

    int unsigned cycles;
    int unsigned phase_len;
    int unsigned mode;
    initial begin
        if (!$value$plusargs("cycles=%d", cycles)) cycles = 200000;
        reset = 1; redirect = 0; set_wr = 0; push = 0; pop = 0;
        set_ptr = 0; set_lin = 0; push_lin = 0;
        // Fill all sixteen entries first, so no compared top reads an
        // unwritten entry (FFs and an MLAB both power up as zero, but the
        // comparison should not rest on that).
        repeat (2) @(negedge clk);
        for (int i = 0; i < 16; i++) begin
            reset = 0; redirect = 1; set_wr = 1; set_ptr = 4'(i); set_lin = rnd();
            @(negedge clk);
        end
        redirect = 0; set_wr = 0;
        mode = 0; phase_len = 0;
        for (int unsigned n = 0; n < cycles; n++) begin
            logic [31:0] r;
            if (phase_len == 0) begin
                mode = rnd() % 5;
                phase_len = 1 + rnd() % 200;
            end
            phase_len--;
            r = rnd();
            // mode 0: everything random, including overlapping requests.
            // mode 1: push-heavy (wraps upward). mode 2: pop-heavy (wraps
            // downward). mode 3: frequent redirects. mode 4: reset held
            // with a write requested on every clock (none may happen).
            case (mode)
                0: begin reset = r[9:0] == 0; redirect = r[10]; set_wr = r[11]; push = r[12]; pop = r[13]; end
                1: begin reset = 0; redirect = r[13:10] == 0; set_wr = r[14]; push = r[16:15] != 0; pop = r[17]; end
                2: begin reset = 0; redirect = r[13:10] == 0; set_wr = r[14]; push = r[16:15] == 0; pop = r[17] | r[18]; end
                3: begin reset = 0; redirect = r[11:10] == 0; set_wr = r[14]; push = r[15]; pop = r[16]; end
                default: begin reset = 1; redirect = r[10]; set_wr = 1; push = 1; pop = r[11]; end
            endcase
            set_ptr = r[23:20];
            set_lin = rnd();
            push_lin = rnd();
            @(negedge clk);
        end
        $finish;
    end

    always #5 clk = ~clk;
    // One line per rising edge: the inputs the edge samples and the top
    // that was valid going into it.
    always @(posedge clk)
        $display("%b%b%b%b%b %h %h %h %h", reset, redirect, set_wr, push, pop, set_ptr, set_lin, push_lin, top);
endmodule
