// SPDX-License-Identifier: GPL-2.0-or-later
// Behavioral cyclonev_hps_interface_fpga2sdram for the fes.memory.hps-ddr
// layout: port 0 is 128-bit on command 0 and data 0/1, ports 1 and 2 are
// 64-bit on commands 1/2 and data 2/3, all Avalon-MM as Quartus wires them.
// Commands are refused at random and while 14 reads are pending, as the
// controller does, and read data returns late with gaps.
// The model flags a command outside the FPGA window or the simulated spans,
// a malformed command field, and a read or a new burst while a write burst
// is still owed beats. The bench clocks every port from one clock.
/* verilator lint_off UNUSEDSIGNAL */
/* verilator lint_off MULTIDRIVEN */
module cyclonev_hps_interface_fpga2sdram (
    input wire [5:0] cfg_axi_mm_select,
    input wire [17:0] cfg_cport_rfifo_map,
    input wire [11:0] cfg_cport_type,
    input wire [17:0] cfg_cport_wfifo_map,
    input wire [11:0] cfg_port_width,
    input wire [15:0] cfg_rfifo_cport_map,
    input wire [15:0] cfg_wfifo_cport_map,
    input wire cmd_port_clk_0, input wire cmd_port_clk_1, input wire cmd_port_clk_2,
    input wire cmd_port_clk_3, input wire cmd_port_clk_4, input wire cmd_port_clk_5,
    input wire cmd_valid_0, input wire cmd_valid_1, input wire cmd_valid_2,
    input wire cmd_valid_3, input wire cmd_valid_4, input wire cmd_valid_5,
    input wire [59:0] cmd_data_0, input wire [59:0] cmd_data_1, input wire [59:0] cmd_data_2,
    input wire [59:0] cmd_data_3, input wire [59:0] cmd_data_4, input wire [59:0] cmd_data_5,
    output wire cmd_ready_0, output wire cmd_ready_1, output wire cmd_ready_2,
    output wire cmd_ready_3, output wire cmd_ready_4, output wire cmd_ready_5,
    input wire wrack_ready_0, input wire wrack_ready_1, input wire wrack_ready_2,
    input wire wrack_ready_3, input wire wrack_ready_4, input wire wrack_ready_5,
    output wire [9:0] wrack_data_0, output wire [9:0] wrack_data_1, output wire [9:0] wrack_data_2,
    output wire [9:0] wrack_data_3, output wire [9:0] wrack_data_4, output wire [9:0] wrack_data_5,
    output wire wrack_valid_0, output wire wrack_valid_1, output wire wrack_valid_2,
    output wire wrack_valid_3, output wire wrack_valid_4, output wire wrack_valid_5,
    input wire wr_clk_0, input wire wr_clk_1, input wire wr_clk_2, input wire wr_clk_3,
    input wire wr_valid_0, input wire wr_valid_1, input wire wr_valid_2, input wire wr_valid_3,
    input wire [89:0] wr_data_0, input wire [89:0] wr_data_1,
    input wire [89:0] wr_data_2, input wire [89:0] wr_data_3,
    output wire wr_ready_0, output wire wr_ready_1, output wire wr_ready_2, output wire wr_ready_3,
    input wire rd_clk_0, input wire rd_clk_1, input wire rd_clk_2, input wire rd_clk_3,
    input wire rd_ready_0, input wire rd_ready_1, input wire rd_ready_2, input wire rd_ready_3,
    output reg [79:0] rd_data_0, output reg [79:0] rd_data_1,
    output reg [79:0] rd_data_2, output reg [79:0] rd_data_3,
    output reg rd_valid_0, output reg rd_valid_1, output reg rd_valid_2, output reg rd_valid_3
); /* verilator public_module */
    // 64-bit lanes indexed by {a[27:26], a[11:3]}: the port bases
    // 0x30000000, 0x38000000 and 0x3c000000 select a region, and each
    // simulated span is at most 4 KiB.
    reg [63:0] mem [0:4095] /* verilator public_flat_rd */;
    reg violation /* verilator public_flat_rd */;
    reg [31:0] accepted [0:2] /* verilator public_flat_rd */;
    // Write beats still owed per port.
    reg [7:0] owed [0:2] /* verilator public_flat_rd */;
    reg [31:0] write_next [0:2];
    reg [59:0] burst_command [0:2];
    // Pending read bursts per port: byte address and beats, in order.
    reg [31:0] queue_addr [0:2][0:15];
    reg [7:0] queue_beats [0:2][0:15];
    reg [3:0] queue_head [0:2];
    reg [3:0] queue_tail [0:2];
    reg [4:0] queue_count [0:2];
    reg [7:0] queue_delay [0:2];
    reg [15:0] lfsr [0:2];
    integer p, index;

    initial begin
        violation = 1'b0;
        for (index = 0; index < 4096; index = index + 1)
            mem[index] = 64'h0123456789abcdef;
        for (p = 0; p < 3; p = p + 1) begin
            accepted[p] = 32'd0;
            owed[p] = 8'd0;
            queue_head[p] = 4'd0;
            queue_tail[p] = 4'd0;
            queue_count[p] = 5'd0;
            queue_delay[p] = 8'd0;
            lfsr[p] = 16'hace1 + p[15:0] * 16'h1111;
        end
        rd_valid_0 = 1'b0;
        rd_valid_1 = 1'b0;
        rd_valid_2 = 1'b0;
        rd_valid_3 = 1'b0;
    end

    function [11:0] slot;
        input [31:0] a;
        slot = {1'b0, a[27:26], a[11:3]};
    endfunction

    function in_span;
        input [31:0] a;
        in_span = a[31:28] == 4'b0011 && a[25:12] == 14'd0;
    endfunction

    // Refuse roughly one command cycle in four, and while 14 reads wait.
    assign cmd_ready_0 = lfsr[0][1:0] != 2'b00 && queue_count[0] < 5'd14;
    assign cmd_ready_1 = lfsr[1][1:0] != 2'b00 && queue_count[1] < 5'd14;
    assign cmd_ready_2 = lfsr[2][1:0] != 2'b00 && queue_count[2] < 5'd14;
    assign {cmd_ready_3, cmd_ready_4, cmd_ready_5} = 3'b000;
    assign {wr_ready_0, wr_ready_1, wr_ready_2, wr_ready_3} = 4'b0000;
    assign {wrack_valid_0, wrack_valid_1, wrack_valid_2} = 3'b000;
    assign {wrack_valid_3, wrack_valid_4, wrack_valid_5} = 3'b000;
    assign {wrack_data_0, wrack_data_1, wrack_data_2} = 30'd0;
    assign {wrack_data_3, wrack_data_4, wrack_data_5} = 30'd0;

    task automatic take;
        input integer port;
        input [59:0] command;
        input [127:0] data;
        input [15:0] enables;
        integer lanes, lane, b;
        reg [31:0] base;
        reg [7:0] beats;
        reg [63:0] word;
        begin
            lanes = port == 0 ? 2 : 1;
            base = port == 0 ? {command[29:2], 4'd0} : {command[30:2], 3'd0};
            beats = command[41:34];
            accepted[port] = accepted[port] + 32'd1;
            if (command[59:42] != 18'd0 || (port == 0 ? command[33:30] != 4'd0 : command[33:31] != 3'd0)
                    || command[1] == command[0]) begin
                $display("hps ddr model: port %0d malformed command %h", port, command);
                violation = 1'b1;
            end
            if (owed[port] != 8'd0) begin
                // Continuation beat of the open write burst: the same command.
                if (command != burst_command[port]) begin
                    $display("hps ddr model: port %0d new command %h inside a write burst", port, command);
                    violation = 1'b1;
                end
                base = write_next[port];
                owed[port] = owed[port] - 8'd1;
            end else begin
                if (beats == 8'd0 || beats > 8'd128) begin
                    $display("hps ddr model: port %0d burst %0d", port, beats);
                    violation = 1'b1;
                end
                if (!in_span(base) || !in_span(base + beats * lanes * 8 - 1)) begin
                    $display("hps ddr model: port %0d address %h outside the simulated window", port, base);
                    violation = 1'b1;
                end
                burst_command[port] = command;
                if (command[1])
                    owed[port] = beats - 8'd1;
                else begin
                    queue_addr[port][queue_tail[port]] = base;
                    queue_beats[port][queue_tail[port]] = beats;
                    queue_tail[port] = queue_tail[port] + 4'd1;
                    queue_count[port] = queue_count[port] + 5'd1;
                end
            end
            if (command[1]) begin
                for (lane = 0; lane < lanes; lane = lane + 1) begin
                    word = mem[slot(base + lane * 8)];
                    for (b = 0; b < 8; b = b + 1)
                        if (enables[lane * 8 + b])
                            word[b * 8 +: 8] = data[lane * 64 + b * 8 +: 8];
                    mem[slot(base + lane * 8)] = word;
                end
                write_next[port] = base + lanes * 8;
            end
        end
    endtask

    task automatic give;
        input integer port;
        output valid;
        output [127:0] data;
        integer lanes;
        reg [31:0] a;
        begin
            lanes = port == 0 ? 2 : 1;
            valid = 1'b0;
            data = 128'd0;
            if (queue_head[port] != queue_tail[port]) begin
                if (queue_delay[port] < 8'd6)
                    queue_delay[port] = queue_delay[port] + 8'd1;
                else if (lfsr[port][4:3] != 2'b00) begin
                    a = queue_addr[port][queue_head[port]];
                    data[63:0] = mem[slot(a)];
                    if (lanes == 2)
                        data[127:64] = mem[slot(a + 8)];
                    valid = 1'b1;
                    queue_addr[port][queue_head[port]] = a + lanes * 8;
                    queue_beats[port][queue_head[port]] = queue_beats[port][queue_head[port]] - 8'd1;
                    if (queue_beats[port][queue_head[port]] == 8'd0) begin
                        queue_head[port] = queue_head[port] + 4'd1;
                        queue_count[port] = queue_count[port] - 5'd1;
                        queue_delay[port] = 8'd0;
                    end
                end
            end
        end
    endtask

    reg valid0, valid1, valid2;
    reg [127:0] data0, data1, data2;
    always @(posedge cmd_port_clk_0) begin
        if (cmd_valid_0 && cmd_ready_0)
            take(0, cmd_data_0, {wr_data_1[63:0], wr_data_0[63:0]}, {wr_data_1[87:80], wr_data_0[87:80]});
        if (cmd_valid_1 && cmd_ready_1)
            take(1, cmd_data_1, {64'd0, wr_data_2[63:0]}, {8'd0, wr_data_2[87:80]});
        if (cmd_valid_2 && cmd_ready_2)
            take(2, cmd_data_2, {64'd0, wr_data_3[63:0]}, {8'd0, wr_data_3[87:80]});
        give(0, valid0, data0);
        give(1, valid1, data1);
        give(2, valid2, data2);
        rd_valid_0 <= valid0;
        rd_valid_1 <= valid0;
        rd_data_0 <= {16'd0, data0[63:0]};
        rd_data_1 <= {16'd0, data0[127:64]};
        rd_valid_2 <= valid1;
        rd_data_2 <= {16'd0, data1[63:0]};
        rd_valid_3 <= valid2;
        rd_data_3 <= {16'd0, data2[63:0]};
        for (p = 0; p < 3; p = p + 1)
            lfsr[p] <= {lfsr[p][14:0], lfsr[p][15] ^ lfsr[p][13] ^ lfsr[p][12] ^ lfsr[p][10]};
    end
    /* verilator lint_on MULTIDRIVEN */
    /* verilator lint_on UNUSEDSIGNAL */
endmodule
