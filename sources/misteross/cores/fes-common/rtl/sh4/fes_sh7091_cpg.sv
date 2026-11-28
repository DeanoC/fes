// Clock and standby control registers. The FPGA fabric clock remains fixed;
// the board can apply the programmed ratios to its clock-enable generators.
module fes_sh7091_cpg #(
    parameter logic [11:0] RESET_FRQCR = 12'he0a
) (
    input  logic        clk, reset, power_reset, watchdog_reset,
    input  logic        request,
    input  logic [31:2] address,
    input  logic [3:0]  rmask, wstrb,
    input  logic [11:0] wdata,
    output logic        selected, rvalid,
    output logic [31:0] rdata,
    input  logic        sleeping,
    output logic [11:0] frequency_control,
    output logic        frequency_change,
    output logic [4:0]  module_stop,
    output logic [1:0]  processor_status,
    output logic        standby, deep_sleep, peripheral_high_z, peripheral_pullup
);
    logic [7:0] standby_control;
    logic deep_enable;
    logic [2:0] index;
    logic frequency_access;
    logic [3:0] lanes;

    assign index = address[4:2];
    assign selected = address[31:5] == 27'h7fe0000 && index inside {3'd0, 3'd1, 3'd4};
    assign frequency_access = index == 0;
    assign lanes = frequency_access ? 4'h3 : 4'h1;
    assign module_stop = standby_control[4:0];
    assign standby = sleeping && standby_control[7];
    assign deep_sleep = sleeping && !standby_control[7] && deep_enable;
    assign processor_status = reset ? 2'b11 : standby ? 2'b01 : sleeping ? 2'b10 : 2'b00;
    assign peripheral_high_z = standby && standby_control[6];
    assign peripheral_pullup = !standby_control[5];

    always_ff @(posedge clk) begin
        frequency_change <= 0;
        if (power_reset)
            frequency_control <= RESET_FRQCR;
        else if (!reset && request && selected && frequency_access && wstrb == lanes) begin
            frequency_control <= wdata[11:0];
            frequency_change <= wdata[10:0] != frequency_control[10:0];
        end

        if (power_reset || watchdog_reset) begin
            standby_control <= 0;
            deep_enable <= 0;
        end else if (!reset && request && selected && wstrb == lanes) begin
            if (index == 1)
                standby_control <= wdata[7:0];
            if (index == 4)
                deep_enable <= wdata[7];
        end

        rvalid <= !reset && request && selected && wstrb == 0;
        if (!reset && request && selected && wstrb == 0) begin
            rdata <= 0;
            if (rmask == lanes) case (index)
                0: rdata[11:0] <= frequency_control;
                1: rdata[7:0] <= standby_control;
                4: rdata[7] <= deep_enable;
                default: ;
            endcase
        end
    end
endmodule
