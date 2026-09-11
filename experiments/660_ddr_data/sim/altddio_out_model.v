// Simulation only: fabric DDR data as registered high/low muxed by outclock.
// It does not model analog GPIO DDR registers or pin delay.
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
    reg q_h;
    reg q_l;
    initial begin
        q_h = 1'b0;
        q_l = 1'b0;
    end
    always @(posedge outclock)
        if (outclocken)
            q_h <= datain_h;
    always @(negedge outclock)
        if (outclocken)
            q_l <= datain_l;
    assign dataout = outclock ? q_h : q_l;
    assign oe_out = 1'b0;
endmodule
