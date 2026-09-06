module storage_port (
    input wire FPGA_CLK1_50,
    input wire we,
    input wire bank,
    input wire [7:0] addr,
    input wire [7:0] wdata,
    output wire [7:0] rdata
);
    (* ramstyle = "mlab" *) reg [7:0] lab_store [0:31];
    (* ramstyle = "M10K" *) reg [7:0] block_store [0:255];
    reg [7:0] lab_q = 8'h00;
    reg [7:0] block_q = 8'h00;

    always @(posedge FPGA_CLK1_50) begin
        if (we && !bank) begin
            lab_store[addr[4:0]] <= wdata;
        end
        lab_q <= lab_store[addr[4:0]];
    end

    always @(posedge FPGA_CLK1_50) begin
        if (we && bank) begin
            block_store[addr] <= wdata;
        end
        block_q <= block_store[addr];
    end

    assign rdata = bank ? block_q : lab_q;
endmodule

module product_unit (
    input wire FPGA_CLK1_50,
    input wire [7:0] left,
    input wire [7:0] right,
    input wire high_select,
    output reg [7:0] selected
);
    (* multstyle = "dsp" *) wire [15:0] wide_product;

    assign wide_product = left * right;

    initial selected = 8'h00;

    always @(posedge FPGA_CLK1_50) begin
        selected <= high_select ? wide_product[15:8] : wide_product[7:0];
    end
endmodule

module top (
    input wire FPGA_CLK1_50
);
    localparam [15:0] SIGNATURE = 16'hD810;

    wire [31:0] fpga_to_hps;
    wire [31:0] hps_to_fpga;
    wire [7:0] rdata;
    wire [7:0] selected;
    wire [1:0] view = hps_to_fpga[18:17];
    wire product_view = (view == 2'b00);
    wire mem_we = !product_view && hps_to_fpga[16];
    wire mem_bank = view[1];
    reg [1:0] beat = 2'b00;

    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(fpga_to_hps),
        .gp_out(hps_to_fpga)
    );

    storage_port storage (
        .FPGA_CLK1_50(FPGA_CLK1_50),
        .we(mem_we),
        .bank(mem_bank),
        .addr(hps_to_fpga[7:0]),
        .wdata(hps_to_fpga[15:8]),
        .rdata(rdata)
    );

    product_unit product (
        .FPGA_CLK1_50(FPGA_CLK1_50),
        .left(hps_to_fpga[7:0]),
        .right(hps_to_fpga[15:8]),
        .high_select(product_view && hps_to_fpga[16]),
        .selected(selected)
    );

    always @(posedge FPGA_CLK1_50) begin
        beat <= beat + 2'd1;
    end

    assign fpga_to_hps = {SIGNATURE, 6'b000000, beat, product_view ? selected : rdata};
endmodule
