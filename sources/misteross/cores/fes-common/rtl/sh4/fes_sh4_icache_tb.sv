// Exercise cancellation at the cache interface, independently of the CPU's
// current branch-resolution latency.
module fes_sh4_icache_tb(input logic clk, output logic done);
    logic reset = 1, flush = 0, lookup = 0, consume = 0;
    logic read_ready = 0, response_valid = 0;
    logic instruction_valid, read_valid;
    logic [31:0] lookup_pc, read_addr, response_data;
    logic [15:0] instruction;

    fes_sh4_icache dut (
        .clk, .reset, .flush, .invalidate(1'b0), .invalidate_index(8'b0),
        .lookup, .lookup_pc, .cacheable(1'b1), .instruction_valid, .instruction, .consume,
        .read_valid, .read_addr, .read_ready, .response_valid, .response_data
    );

    task automatic accept_beat(input logic [31:0] address);
        wait (read_valid);
        @(negedge clk);
        assert (read_addr == address) else $fatal(1, "Refill address %h, expected %h", read_addr, address);
        read_ready = 1;
        @(negedge clk);
        read_ready = 0;
    endtask

    task automatic return_beat(input logic [31:0] data);
        response_data = data;
        response_valid = 1;
        @(negedge clk);
        response_valid = 0;
    endtask

    task automatic fill_zero_line(input logic [31:0] first_word);
        for (int beat = 0; beat < 8; beat++) begin
            accept_beat(32'(beat * 4));
            return_beat(first_word + 32'(beat));
            if (beat < 7)
                assert (!instruction_valid) else $fatal(1, "Incomplete line was issued");
        end
    endtask

    initial begin
        done = 0;
        @(negedge clk);
        reset = 0;
        lookup_pc = 0;
        lookup = 1;
        fill_zero_line(32'h1234_0100);
        assert (instruction_valid && instruction == 16'h0100)
            else $fatal(1, "Initial fill returned the wrong opcode");

        // Overwrite word zero at a conflicting index, then redirect while
        // the second read is outstanding. Its response must be drained.
        lookup_pc = 32'h2004;
        consume = 1;
        @(negedge clk);
        consume = 0;
        accept_beat(32'h2000);
        return_beat(32'h4321_0200);
        accept_beat(32'h2004);
        lookup_pc = 0;
        flush = 1;
        @(negedge clk);
        flush = 0;
        repeat (3) begin
            @(negedge clk);
            assert (!instruction_valid && !read_valid) else $fatal(1, "Abandoned read was not drained");
        end
        return_beat(32'hdead_beef);
        assert (!instruction_valid) else $fatal(1, "Wrong-path response was issued");

        // The old line is no longer a hit after being partly overwritten.
        fill_zero_line(32'hcafe_0300);
        assert (instruction_valid && instruction == 16'h0300)
            else $fatal(1, "Partially overwritten line returned stale bytes");
        lookup_pc = 2;
        consume = 1;
        @(negedge clk);
        consume = 0;
        repeat (3) begin
            @(negedge clk);
            assert (!read_valid) else $fatal(1, "Cached high halfword triggered a refill");
        end
        assert (instruction_valid && instruction == 16'hcafe)
            else $fatal(1, "Cache hit returned the wrong halfword");
        done = 1;
    end
endmodule
