// Simulation only: complementary edge capture. It does not model analog
// GPIO DDR registers or pin delay.
module altddio_in #(
    parameter width = 1,
    parameter intended_device_family = "Cyclone V",
    parameter power_up_high = "OFF",
    parameter invert_input_clocks = "OFF"
) (
    input wire datain,
    input wire inclock,
    input wire inclocken,
    input wire aset,
    input wire aclr,
    input wire sset,
    input wire sclr,
    output reg dataout_h,
    output reg dataout_l
);
    initial begin
        dataout_h = 1'b0;
        dataout_l = 1'b0;
    end
    always @(posedge inclock)
        if (inclocken)
            dataout_h <= datain;
    always @(negedge inclock)
        if (inclocken)
            dataout_l <= datain;
endmodule
