module cyclonev_hps_interface_mpu_general_purpose (
    input wire [31:0] gp_in /* verilator public_flat_rd */,
    output wire [31:0] gp_out
); /* verilator public_module */

    reg [31:0] gpo /* verilator public_flat_rw */;
    wire [31:0] gpi /* verilator public_flat_rd */;

    initial gpo = 32'hDEADBEEF;

    assign gp_out = gpo;
    assign gpi = gp_in;

endmodule
