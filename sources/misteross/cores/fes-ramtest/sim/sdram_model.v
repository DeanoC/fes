// SPDX-License-Identifier: GPL-2.0-or-later
// Behavioral 16-bit SDRAM. CAS latency 2, burst length 1.
module sdram_model (
    input wire clk,
    input wire cke,
    input wire ncs,
    input wire nras,
    input wire ncas,
    input wire nwe,
    input wire [1:0] ba,
    input wire [12:0] a,
    input wire dqml,
    input wire dqmh,
    inout wire [15:0] dq
);
    // Wide enough for the sim span, which walks past 64K halfwords.
    reg [15:0] mem [0:131071];
    reg [1:0] open_ba = 2'd0;
    reg [12:0] open_row = 13'd0;
    reg [15:0] beat = 16'h0000;
    reg [2:0] read_delay = 3'd0;
    reg reading = 1'b0;
    reg [15:0] read_data = 16'h0000;
    reg [1:0] cycles_after_activate = 2'd3;
    integer index;

    initial begin
        for (index = 0; index < 131072; index = index + 1)
            mem[index] = 16'h0000;
    end

    wire [3:0] command = {ncs, nras, ncas, nwe};
    // A10 is auto-precharge. The MiSTer tester's halfword is
    // {chip select, column[9:2], row, bank, column[1:0]}.
    wire [25:0] word_addr = {ncs, a[9:2], open_row, open_ba, a[1:0]};

    assign dq = reading ? read_data : 16'hzzzz;

    always @(posedge clk) begin
        if (cycles_after_activate != 2'd3)
            cycles_after_activate <= cycles_after_activate + 2'd1;
        if (reading)
            reading <= 1'b0;
        if (cke && read_delay != 3'd0) begin
            if (read_delay == 3'd1) begin
                reading <= 1'b1;
                read_delay <= 3'd0;
            end else begin
                read_delay <= read_delay - 3'd1;
            end
        end
        if (cke && !ncs) begin
            case (command)
                4'b0011: begin
                    open_ba <= ba;
                    open_row <= a;
                    cycles_after_activate <= 2'd0;
                end
                4'b0101: begin
                    if (cycles_after_activate < 2'd2)
                        $fatal(1, "SDRAM READ violates ACTIVATE-to-CAS timing");
                    read_delay <= 3'd2;
                    read_data <= mem[word_addr[16:0]];
                end
                4'b0100: begin
                    if (cycles_after_activate < 2'd2)
                        $fatal(1, "SDRAM WRITE violates ACTIVATE-to-CAS timing");
                    beat = mem[word_addr[16:0]];
                    if (!dqml)
                        beat[7:0] = dq[7:0];
                    if (!dqmh)
                        beat[15:8] = dq[15:8];
                    mem[word_addr[16:0]] <= beat;
                end
                default: begin
                end
            endcase
        end
    end
endmodule
