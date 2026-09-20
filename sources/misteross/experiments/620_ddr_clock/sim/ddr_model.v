// Simulation only: complementary constant forwarding as dataout = outclock.
// It does not model analog DDR registers or pin delay.
module altddio_out #(
    parameter width = 1,
    parameter intended_device_family = "Cyclone V",
    parameter power_up_high = "OFF",
    parameter oe_reg = "UNREGISTERED",
    parameter extend_oe_disable = "OFF",
    parameter invert_output = "OFF"
) (
    input wire datain_h,
    input wire datain_l,
    input wire outclock,
    input wire outclocken,
    input wire aset,
    input wire aclr,
    input wire sset,
    input wire sclr,
    input wire oe,
    output wire dataout,
    output wire oe_out
);
    assign dataout = outclock;
    assign oe_out = 1'b0;
endmodule
