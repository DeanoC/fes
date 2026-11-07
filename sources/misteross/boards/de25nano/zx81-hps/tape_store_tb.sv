`timescale 1ns/1ps
module tape_store_tb;
    reg clk = 0;
    reg [1:0] unit_state = 0;
    reg [31:0] unit_size = 0;
    reg [13:0] write_addr = 0, tape_addr = 0;
    reg [15:0] write_data = 0;
    reg [1:0] write_enable = 0;
    wire [7:0] primitive_data, behavioral_data, reference_data;
    wire primitive_ready, behavioral_ready, reference_ready;
    wire [14:0] primitive_size, behavioral_size, reference_size;
    zx81_tape_store primitive_store (.*,
        .tape_data(primitive_data), .tape_ready(primitive_ready), .tape_size(primitive_size));
    zx81_tape_store_behavior behavioral_store (.*,
        .tape_data(behavioral_data), .tape_ready(behavioral_ready), .tape_size(behavioral_size));
    zx81_tape_store_reference reference_store (.*,
        .tape_data(reference_data), .tape_ready(reference_ready), .tape_size(reference_size));

    reg [7:0] expected [0:16383];
    reg initialized [0:16383];
    reg [31:0] random_state = 32'hd25a8101;
    integer cycles = 0, compared_reads = 0, low_collisions = 0, high_collisions = 0;
    integer enable_counts [0:3];
    integer odd_writes = 0, suppressed_high = 0;
    integer address, enables, iteration;
    reg previous_valid = 0;
    reg [7:0] previous_data;
    reg [31:0] random_a, random_b;

    function [31:0] random_word;
        begin
            random_state = random_state ^ (random_state << 13);
            random_state = random_state ^ (random_state >> 17);
            random_state = random_state ^ (random_state << 5);
            random_word = random_state;
        end
    endfunction

    task cycle(input [13:0] wr, input [15:0] data, input [1:0] mask,
               input [13:0] rd, input [1:0] state, input [31:0] size);
        reg [13:0] selected;
        reg valid;
        reg [7:0] old_byte;
        begin
            clk = 0;
            write_addr = wr; write_data = data; write_enable = mask;
            tape_addr = rd; unit_state = state; unit_size = size;
            #5;
            if (previous_valid && (primitive_data !== previous_data ||
                    behavioral_data !== previous_data || reference_data !== previous_data))
                $fatal(1, "read changed without rising clock at cycle %0d", cycles);
            selected = mask[0] ? wr : rd;
            valid = initialized[selected]; old_byte = expected[selected];
            clk = 1;
            #5;
            if (primitive_ready !== (state == 3) || behavioral_ready !== (state == 3) ||
                    reference_ready !== (state == 3) || primitive_size !== size[14:0] ||
                    behavioral_size !== size[14:0] || reference_size !== size[14:0])
                $fatal(1, "metadata mismatch at cycle %0d", cycles);
            if (valid) begin
                if (primitive_data !== old_byte || behavioral_data !== old_byte || reference_data !== old_byte)
                    $fatal(1, "old-data mismatch cycle=%0d write=%h enables=%b read=%h expected=%h primitive=%h behavioral=%h reference=%h",
                           cycles, wr, mask, rd, old_byte, primitive_data, behavioral_data, reference_data);
                compared_reads = compared_reads + 1;
                if (mask[0]) low_collisions = low_collisions + 1;
                if (mask[1] && wr != 14'h3fff && selected == wr + 14'd1)
                    high_collisions = high_collisions + 1;
            end
            previous_valid = valid; previous_data = old_byte;
            if (mask[0]) begin expected[wr] = data[7:0]; initialized[wr] = 1; end
            if (mask[1] && wr != 14'h3fff) begin
                expected[wr + 14'd1] = data[15:8]; initialized[wr + 14'd1] = 1;
            end
            if (mask != 0 && wr[0]) odd_writes = odd_writes + 1;
            if (mask[1] && wr == 14'h3fff) suppressed_high = suppressed_high + 1;
            enable_counts[mask] = enable_counts[mask] + 1;
            cycles = cycles + 1;
        end
    endtask

    initial begin
        for (address = 0; address < 16384; address = address + 1) initialized[address] = 0;
        for (enables = 0; enables < 4; enables = enables + 1) enable_counts[enables] = 0;
        // No equality is claimed for unwritten bytes: the portable array is
        // undefined, while the primitive requests initialized power-up memory.
        for (address = 0; address < 16384; address = address + 2)
            cycle(address[13:0], address ^ 16'h5aa5, 3, 0, 0, 0);
        for (address = 0; address < 16384; address = address + 1)
            cycle(0, 0, 0, address[13:0], address[1:0], 32'hffff4000);
        // Every address and each nonzero enable, including read/write collision
        // on the high-byte-only port and odd-address two-byte writes.
        for (enables = 1; enables < 4; enables = enables + 1)
            for (address = 0; address < 16384; address = address + 1)
                cycle(address[13:0], address ^ (16'h69b4 + enables), enables[1:0],
                      address[13:0] + 14'd1, enables[1:0], 32'h80000001 + address);
        for (iteration = 0; iteration < 65536; iteration = iteration + 1) begin
            random_a = random_word(); random_b = random_word();
            cycle(random_a[13:0], random_b[15:0], random_a[15:14], random_b[29:16],
                  random_b[31:30], random_a ^ random_b);
        end
        for (enables = 0; enables < 4; enables = enables + 1) begin
            cycle(14'h3fff, 16'hc3a5, enables[1:0], 0, 3, 16384);
            cycle(0, 0, 0, 0, 3, 16384);
            cycle(0, 0, 0, 14'h3fff, 3, 16384);
        end
        for (address = 0; address < 16384; address = address + 1)
            cycle(0, 0, 0, address[13:0], 3, 16384);
        $display("PASS tape-store differential: cycles=%0d compared_reads=%0d enables00/01/10/11=%0d/%0d/%0d/%0d odd_writes=%0d old_low=%0d old_high=%0d suppressed_high=%0d full_readbacks=2",
                 cycles, compared_reads, enable_counts[0], enable_counts[1], enable_counts[2],
                 enable_counts[3], odd_writes, low_collisions, high_collisions, suppressed_high);
        $finish;
    end
endmodule
