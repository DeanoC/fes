// MiSTer 128 MB SDRAM addon probe. The addon is the optional GPIO module
// (32/64/128 MB share this 16-bit header). It is not HPS DDR.
// Host traffic uses fes.application 1.0 framing. Opcode 18 is experiment-local.
module top (
    input wire FPGA_CLK1_50,
    output wire SDRAM_CLK,
    output wire SDRAM_CKE,
    output wire SDRAM_nCS,
    output wire SDRAM_nRAS,
    output wire SDRAM_nCAS,
    output wire SDRAM_nWE,
    output wire SDRAM_DQML,
    output wire SDRAM_DQMH,
    output wire [1:0] SDRAM_BA,
    output wire [12:0] SDRAM_A,
    inout wire [15:0] SDRAM_DQ
);
    localparam [127:0] BUILD_ID = 128'h4631_3039_5f53_4452_414d_0000_0000_0000;

    wire [31:0] gp_in;
    wire [31:0] gp_out;
    wire mem_start;
    wire mem_write;
    wire [15:0] mem_addr;
    wire [15:0] mem_wdata;
    wire mem_done;
    wire [15:0] mem_rdata;
    wire [15:0] dq_out;
    wire [15:0] dq_in;
    wire dq_oe;

    fes_mem_window #(.BUILD_ID(BUILD_ID)) window (
        .clk(FPGA_CLK1_50),
        .gpo(gp_out),
        .gpi(gp_in),
        .mem_start(mem_start),
        .mem_write(mem_write),
        .mem_addr(mem_addr),
        .mem_wdata(mem_wdata),
        .mem_done(mem_done),
        .mem_rdata(mem_rdata)
    );

    sdram_addon_port sdram (
        .clk(FPGA_CLK1_50),
        .start(mem_start),
        .write(mem_write),
        .addr(mem_addr),
        .wdata(mem_wdata),
        .done(mem_done),
        .rdata(mem_rdata),
        .sdram_clk(SDRAM_CLK),
        .sdram_cke(SDRAM_CKE),
        .sdram_ncs(SDRAM_nCS),
        .sdram_nras(SDRAM_nRAS),
        .sdram_ncas(SDRAM_nCAS),
        .sdram_nwe(SDRAM_nWE),
        .sdram_dqml(SDRAM_DQML),
        .sdram_dqmh(SDRAM_DQMH),
        .sdram_ba(SDRAM_BA),
        .sdram_a(SDRAM_A),
        .dq_out(dq_out),
        .dq_oe(dq_oe),
        .dq_in(dq_in)
    );

    genvar dq_bit;
    generate
        for (dq_bit = 0; dq_bit < 16; dq_bit = dq_bit + 1) begin : dq_buf
            altiobuf_bidir #(
                .number_of_channels(1),
                .enable_bus_hold("OFF")
            ) pad (
                .dataio(SDRAM_DQ[dq_bit]),
                .oe(dq_oe),
                .datain(dq_out[dq_bit]),
                .dataout(dq_in[dq_bit])
            );
        end
    endgenerate

    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(gp_in),
        .gp_out(gp_out)
    );

endmodule

// Application framing. Identity follows fes.application 1.0. Memory opcode 18
// is local to this probe: index 0 sets the halfword address, index 1 writes
// the argument, index 2 reads it back.
module fes_mem_window #(
    parameter [127:0] BUILD_ID = 128'd0
) (
    input wire clk,
    input wire [31:0] gpo,
    output wire [31:0] gpi,
    output wire mem_start,
    output wire mem_write,
    output wire [15:0] mem_addr,
    output wire [15:0] mem_wdata,
    input wire mem_done,
    input wire [15:0] mem_rdata
);
    localparam [31:0] SIGNATURE = 32'hf5000000;
    localparam [31:0] REQUEST_MASK = 32'h80000000;
    localparam [31:0] OPCODE_MASK = 32'h7f000000;
    localparam [31:0] INDEX_MASK = 32'h00ff0000;
    localparam [31:0] ARGUMENT_MASK = 32'h0000ffff;
    localparam [31:0] ACK_MASK = 32'h00800000;
    localparam [31:0] ERROR_MASK = 32'h00400000;
    localparam [15:0] MAGIC0 = 16'h4546;
    localparam [15:0] MAGIC1 = 16'h3153;
    localparam [6:0] OPCODE_IDENTITY = 7'd1;
    localparam [6:0] OPCODE_MEM = 7'd18;
    localparam [15:0] ERR_OPCODE = 16'd1;
    localparam [15:0] ERR_INDEX = 16'd2;
    localparam [15:0] ERR_ARGUMENT = 16'd3;

    reg request_meta = 1'b0;
    reg request_sync = 1'b0;
    reg acknowledged_toggle = 1'b0;
    reg response_error = 1'b0;
    reg [15:0] response_data = 16'h0000;
    reg waiting = 1'b0;
    reg write_op = 1'b0;
    reg [15:0] address = 16'h0000;
    reg [15:0] write_data = 16'h0000;

    wire request_toggle = (gpo & REQUEST_MASK) != 32'h00000000;
    wire [31:0] opcode_field = gpo & OPCODE_MASK;
    wire [31:0] index_field = gpo & INDEX_MASK;
    wire [31:0] argument_field = gpo & ARGUMENT_MASK;
    wire [6:0] opcode = opcode_field[30:24];
    wire [7:0] index = index_field[23:16];
    wire [15:0] argument = argument_field[15:0];

    assign gpi = SIGNATURE |
                 (acknowledged_toggle ? ACK_MASK : 32'h00000000) |
                 (response_error ? ERROR_MASK : 32'h00000000) |
                 {16'h0000, response_data};
    assign mem_start = waiting;
    assign mem_write = write_op;
    assign mem_addr = address;
    assign mem_wdata = write_data;

    function automatic [15:0] identity_word;
        input [7:0] word_index;
        begin
            case (word_index)
                8'd0: identity_word = MAGIC0;
                8'd1: identity_word = MAGIC1;
                8'd2: identity_word = 16'd1;
                8'd3: identity_word = 16'd0;
                8'd4: identity_word = 16'd3;
                8'd5: identity_word = 16'd1;
                8'd6: identity_word = 16'd0;
                8'd7: identity_word = 16'd0;
                8'd8: identity_word = {BUILD_ID[119:112], BUILD_ID[127:120]};
                8'd9: identity_word = {BUILD_ID[103:96], BUILD_ID[111:104]};
                8'd10: identity_word = {BUILD_ID[87:80], BUILD_ID[95:88]};
                8'd11: identity_word = {BUILD_ID[71:64], BUILD_ID[79:72]};
                8'd12: identity_word = {BUILD_ID[55:48], BUILD_ID[63:56]};
                8'd13: identity_word = {BUILD_ID[39:32], BUILD_ID[47:40]};
                8'd14: identity_word = {BUILD_ID[23:16], BUILD_ID[31:24]};
                8'd15: identity_word = {BUILD_ID[7:0], BUILD_ID[15:8]};
                default: identity_word = 16'h0000;
            endcase
        end
    endfunction

    always @(posedge clk) begin
        request_meta <= request_toggle;
        request_sync <= request_meta;
        if (!waiting && (request_sync != acknowledged_toggle)) begin
            response_error <= 1'b0;
            response_data <= 16'h0000;
            if (opcode == OPCODE_IDENTITY) begin
                acknowledged_toggle <= request_sync;
                if (index > 8'd15) begin
                    response_error <= 1'b1;
                    response_data <= ERR_INDEX;
                end else if (argument != 16'h0000) begin
                    response_error <= 1'b1;
                    response_data <= ERR_ARGUMENT;
                end else begin
                    response_data <= identity_word(index);
                end
            end else if (opcode == OPCODE_MEM && index == 8'd0) begin
                acknowledged_toggle <= request_sync;
                address <= argument;
            end else if (opcode == OPCODE_MEM && index == 8'd1) begin
                write_data <= argument;
                write_op <= 1'b1;
                waiting <= 1'b1;
            end else if (opcode == OPCODE_MEM && index == 8'd2 && argument == 16'h0000) begin
                write_op <= 1'b0;
                waiting <= 1'b1;
            end else if (opcode == OPCODE_MEM && index == 8'd2) begin
                acknowledged_toggle <= request_sync;
                response_error <= 1'b1;
                response_data <= ERR_ARGUMENT;
            end else if (opcode == OPCODE_MEM) begin
                acknowledged_toggle <= request_sync;
                response_error <= 1'b1;
                response_data <= ERR_INDEX;
            end else begin
                acknowledged_toggle <= request_sync;
                response_error <= 1'b1;
                response_data <= ERR_OPCODE;
            end
        end else if (waiting && mem_done) begin
            waiting <= 1'b0;
            acknowledged_toggle <= request_sync;
            response_error <= 1'b0;
            response_data <= write_op ? 16'h0000 : mem_rdata;
        end
    end

endmodule

// 50 MHz 16-bit SDR SDRAM, burst length 1, CAS latency 2.
// Halfword address 0..65535 stays inside the first 128 KB of every addon size.
module sdram_addon_port (
    input wire clk,
    input wire start,
    input wire write,
    input wire [15:0] addr,
    input wire [15:0] wdata,
    output reg done,
    output reg [15:0] rdata,
    output wire sdram_clk,
    output reg sdram_cke,
    output reg sdram_ncs,
    output reg sdram_nras,
    output reg sdram_ncas,
    output reg sdram_nwe,
    output reg [1:0] sdram_ba,
    output reg [12:0] sdram_a,
    output reg sdram_dqml,
    output reg sdram_dqmh,
    output reg [15:0] dq_out,
    output reg dq_oe,
    input wire [15:0] dq_in
);
    localparam [3:0] ST_BOOT = 4'd0;
    localparam [3:0] ST_PRE = 4'd1;
    localparam [3:0] ST_REF1 = 4'd2;
    localparam [3:0] ST_REF2 = 4'd3;
    localparam [3:0] ST_MRS = 4'd4;
    localparam [3:0] ST_IDLE = 4'd5;
    localparam [3:0] ST_ACT = 4'd6;
    localparam [3:0] ST_RW = 4'd7;
    localparam [3:0] ST_HOLD = 4'd8;
    localparam [3:0] ST_CAP = 4'd9;
    localparam [3:0] ST_PRE2 = 4'd10;
    localparam [3:0] ST_FINISH = 4'd11;

    reg [3:0] state = ST_BOOT;
    reg [12:0] wait_count = 13'd0;
    reg seen = 1'b0;
    reg writing = 1'b0;
    reg [15:0] held_addr = 16'h0000;
    reg [15:0] held_data = 16'h0000;

    assign sdram_clk = clk;

    always @(posedge clk) begin
        done <= 1'b0;
        dq_oe <= 1'b0;
        sdram_dqml <= 1'b0;
        sdram_dqmh <= 1'b0;
        sdram_ncs <= 1'b0;
        sdram_nras <= 1'b1;
        sdram_ncas <= 1'b1;
        sdram_nwe <= 1'b1;
        case (state)
            ST_BOOT: begin
                sdram_cke <= 1'b0;
                sdram_ncs <= 1'b1;
                if (wait_count == 13'd5000) begin
                    sdram_cke <= 1'b1;
                    state <= ST_PRE;
                    wait_count <= 13'd0;
                end else begin
                    wait_count <= wait_count + 13'd1;
                end
            end
            ST_PRE, ST_PRE2: begin
                sdram_cke <= 1'b1;
                sdram_nras <= 1'b0;
                sdram_nwe <= 1'b0;
                sdram_a[10] <= 1'b1;
                state <= (state == ST_PRE) ? ST_REF1 : ST_FINISH;
            end
            ST_REF1, ST_REF2: begin
                sdram_cke <= 1'b1;
                sdram_nras <= 1'b0;
                sdram_ncas <= 1'b0;
                state <= (state == ST_REF1) ? ST_REF2 : ST_MRS;
            end
            ST_MRS: begin
                sdram_cke <= 1'b1;
                sdram_nras <= 1'b0;
                sdram_ncas <= 1'b0;
                sdram_nwe <= 1'b0;
                sdram_ba <= 2'b00;
                sdram_a <= 13'h0020;
                state <= ST_IDLE;
            end
            ST_IDLE: begin
                sdram_cke <= 1'b1;
                sdram_ncs <= 1'b1;
                if (!start)
                    seen <= 1'b0;
                else if (!seen) begin
                    seen <= 1'b1;
                    writing <= write;
                    held_addr <= addr;
                    held_data <= wdata;
                    sdram_ba <= addr[10:9];
                    sdram_a <= {8'd0, addr[15:11]};
                    sdram_ncs <= 1'b0;
                    sdram_nras <= 1'b0;
                    state <= ST_ACT;
                end
            end
            ST_ACT: begin
                sdram_cke <= 1'b1;
                sdram_ba <= held_addr[10:9];
                sdram_a <= {4'b0000, held_addr[8:0]};
                sdram_ncas <= 1'b0;
                sdram_nwe <= writing ? 1'b0 : 1'b1;
                dq_out <= held_data;
                dq_oe <= writing;
                state <= ST_RW;
            end
            ST_RW: begin
                sdram_cke <= 1'b1;
                dq_out <= held_data;
                dq_oe <= writing;
                state <= writing ? ST_HOLD : ST_CAP;
                wait_count <= 13'd0;
            end
            ST_HOLD: begin
                sdram_cke <= 1'b1;
                state <= ST_PRE2;
            end
            ST_CAP: begin
                sdram_cke <= 1'b1;
                if (wait_count == 13'd2) begin
                    rdata <= dq_in;
                    state <= ST_PRE2;
                end else begin
                    wait_count <= wait_count + 13'd1;
                end
            end
            ST_FINISH: begin
                sdram_cke <= 1'b1;
                sdram_ncs <= 1'b1;
                done <= 1'b1;
                state <= ST_IDLE;
            end
            default: begin
                sdram_cke <= 1'b1;
                state <= ST_IDLE;
            end
        endcase
    end
endmodule
