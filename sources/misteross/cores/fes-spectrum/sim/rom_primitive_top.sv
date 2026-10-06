// SPDX-License-Identifier: GPL-2.0-or-later
// Compile with -UVERILATOR and the selected Yosys mem_sim.v to exercise
// production primitive wiring, rather than spectrum_rom's simulation branch.
module rom_primitive_top (
    input wire clk,
    input wire load,
    input wire [7:0] salt,
    input wire [13:0] address,
    output wire [7:0] actual,
    output reg [7:0] expected
);
    spectrum_rom dut (.clk(clk), .address(address), .data(actual));
    reg [7:0] reference_memory [0:16383];
    reg [7:0] reference_stage;
    integer word_index;
    function automatic [7:0] pattern(input integer word_address, input [7:0] seed);
        pattern = 8'((word_address * 73) ^ (word_address >> 3) ^
                     (word_address >> 8) ^ (word_address >> 11)) ^ seed;
    endfunction
    // Populate the actual Yosys model after its INIT has run, as a linked
    // image would populate the physical lanes before the machine starts.
    always @(negedge clk) begin
        if (load) begin
            for (word_index = 0; word_index < 16384; word_index = word_index + 1)
                reference_memory[word_index] = pattern(word_index, salt);
            for (word_index = 0; word_index < 1024; word_index = word_index + 1) begin
                dut.lane0.legacy.mem[word_index] = {2'b0, pattern(0 + word_index, salt)};
                dut.lane1.legacy.mem[word_index] = {2'b0, pattern(1024 + word_index, salt)};
                dut.lane2.legacy.mem[word_index] = {2'b0, pattern(2048 + word_index, salt)};
                dut.lane3.legacy.mem[word_index] = {2'b0, pattern(3072 + word_index, salt)};
                dut.lane4.legacy.mem[word_index] = {2'b0, pattern(4096 + word_index, salt)};
                dut.lane5.legacy.mem[word_index] = {2'b0, pattern(5120 + word_index, salt)};
                dut.lane6.legacy.mem[word_index] = {2'b0, pattern(6144 + word_index, salt)};
                dut.lane7.legacy.mem[word_index] = {2'b0, pattern(7168 + word_index, salt)};
                dut.lane8.legacy.mem[word_index] = {2'b0, pattern(8192 + word_index, salt)};
                dut.lane9.legacy.mem[word_index] = {2'b0, pattern(9216 + word_index, salt)};
                dut.lane10.legacy.mem[word_index] = {2'b0, pattern(10240 + word_index, salt)};
                dut.lane11.legacy.mem[word_index] = {2'b0, pattern(11264 + word_index, salt)};
                dut.lane12.legacy.mem[word_index] = {2'b0, pattern(12288 + word_index, salt)};
                dut.lane13.legacy.mem[word_index] = {2'b0, pattern(13312 + word_index, salt)};
                dut.lane14.legacy.mem[word_index] = {2'b0, pattern(14336 + word_index, salt)};
                dut.lane15.legacy.mem[word_index] = {2'b0, pattern(15360 + word_index, salt)};
            end
        end
    end
    always @(posedge clk) begin
        reference_stage <= reference_memory[address];
        expected <= reference_stage;
    end
endmodule
