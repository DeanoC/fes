module product_unit (
    input wire clk40,
    input wire [7:0] left,
    input wire [7:0] right,
    output reg [15:0] wide_product
);
    (* async_reg = "true" *) reg [7:0] left_meta = 8'h00;
    (* async_reg = "true" *) reg [7:0] left_sync = 8'h00;
    (* async_reg = "true" *) reg [7:0] right_meta = 8'h00;
    (* async_reg = "true" *) reg [7:0] right_sync = 8'h00;
    (* multstyle = "dsp" *) wire [15:0] product;

    assign product = left_sync * right_sync;

    initial wide_product = 16'h0000;

    always @(posedge clk40) begin
        left_meta <= left;
        left_sync <= left_meta;
        right_meta <= right;
        right_sync <= right_meta;
        wide_product <= product;
    end
endmodule

module host_port (
    input wire FPGA_CLK1_50,
    input wire locked,
    input wire [15:0] product,
    input wire high_select,
    output reg lock_status,
    output reg [7:0] selected
);
    (* async_reg = "true" *) reg lock_meta = 1'b0;
    (* async_reg = "true" *) reg [15:0] product_meta = 16'h0000;
    reg [15:0] product_sync = 16'h0000;

    initial begin
        lock_status = 1'b0;
        selected = 8'h00;
    end

    always @(posedge FPGA_CLK1_50) begin
        lock_meta <= locked;
        lock_status <= lock_meta;
        product_meta <= product;
        product_sync <= product_meta;
        selected <= high_select ? product_sync[15:8] : product_sync[7:0];
    end
endmodule

module top (
    input wire FPGA_CLK1_50
);
    localparam [15:0] SIGNATURE = 16'hDC40;

    wire clk40;
    wire locked;
    wire [31:0] fpga_to_hps;
    wire [31:0] hps_to_fpga;
    wire [15:0] wide_product;
    wire lock_status;
    wire [7:0] selected;

    altera_pll #(
        .reference_clock_frequency("50.0 MHz"),
        .number_of_clocks(1),
        .output_clock_frequency0("40.0 MHz"),
        .phase_shift0("0 ps"),
        .duty_cycle0(50),
        .operation_mode("direct"),
        .fractional_vco_multiplier("false")
    ) pll (
        .refclk(FPGA_CLK1_50),
        .rst(1'b0),
        .outclk(clk40),
        .locked(locked)
    );

    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(fpga_to_hps),
        .gp_out(hps_to_fpga)
    );

    product_unit product (
        .clk40(clk40),
        .left(hps_to_fpga[7:0]),
        .right(hps_to_fpga[15:8]),
        .wide_product(wide_product)
    );

    host_port host_port (
        .FPGA_CLK1_50(FPGA_CLK1_50),
        .locked(locked),
        .product(wide_product),
        .high_select(hps_to_fpga[16]),
        .lock_status(lock_status),
        .selected(selected)
    );

    assign fpga_to_hps = {SIGNATURE, 2'b00, lock_status, 5'b00000, selected};
endmodule
