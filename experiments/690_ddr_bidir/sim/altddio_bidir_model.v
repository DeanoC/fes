// Simulation only: fabric DDR bidirectional data as registered high/low
// muxed by the shared clock. It does not model analog GPIO DDR registers
// or pin delay.
module altddio_bidir #(
    parameter width = 1,
    parameter intended_device_family = "Cyclone V",
    parameter power_up_high = "OFF",
    parameter oe_reg = "UNREGISTERED",
    parameter extend_oe_disable = "OFF",
    parameter implement_input_in_lcell = "UNUSED",
    parameter invert_output = "OFF"
) (
    input wire datain_h,
    input wire datain_l,
    input wire inclock,
    input wire inclocken,
    input wire outclock,
    input wire outclocken,
    input wire aset,
    input wire aclr,
    input wire sset,
    input wire sclr,
    input wire oe,
    output reg dataout_h,
    output reg dataout_l,
    output wire combout,
    output wire oe_out,
    output wire dqsundelayedout,
    inout wire padio
);
    reg q_h;
    reg q_l;
    initial begin
        q_h = 1'b0;
        q_l = 1'b0;
        dataout_h = 1'b0;
        dataout_l = 1'b0;
    end
    always @(posedge outclock)
        if (outclocken)
            q_h <= datain_h;
    always @(negedge outclock)
        if (outclocken)
            q_l <= datain_l;
    assign padio = oe ? (outclock ? q_h : q_l) : 1'bz;
    assign combout = padio;
    always @(posedge inclock)
        if (inclocken)
            dataout_h <= padio;
    always @(negedge inclock)
        if (inclocken)
            dataout_l <= padio;
    assign oe_out = 1'b0;
    assign dqsundelayedout = 1'b0;
endmodule
