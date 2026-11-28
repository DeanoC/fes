// Three SH7091 down-counters and the channel 2 input-capture unit.
module fes_sh7091_tmu (
    input  logic        clk, reset,
    input  logic        peripheral_tick, rtc_tick, tclk,
    input  logic        request,
    input  logic [31:2] address,
    input  logic [3:0]  rmask, wstrb,
    input  logic [31:0] wdata,
    output logic        selected, rvalid,
    output logic [31:0] rdata,
    output logic [3:0]  timer_irq,
    output logic        tclk_output_enable,
    output logic        capture_dma,
    output logic [31:0] capture_value,
    input  logic        capture_ack
);
    logic [2:0] running, underflow;
    logic [31:0] constant_value [0:2], count [0:2];
    logic [7:0] control [0:2];
    logic captured, tclk_sync, tclk_sample, tclk_previous;
    logic [9:0] prescaler;
    logic [3:0] reg_number, read_mask;
    logic read_reg, write_reg, capture_event;
    logic [2:0] pin_edge, count_tick;

    assign reg_number = address[5:2];
    assign selected = address[31:6] == 26'h3ff6000 && reg_number <= 11;
    assign read_mask = reg_number <= 1 ? 4'h1 :
                       reg_number inside {4'd4, 4'd7, 4'd10} ? 4'h3 : 4'hf;
    assign read_reg = request && selected && wstrb == 0 && rmask == read_mask;
    assign write_reg = request && selected && wstrb == read_mask;
    assign capture_event = control[2][7] && pin_edge[2];
    assign timer_irq[3] = captured && (&control[2][7:6]);

    for (genvar i = 0; i < 3; i++) begin : channels
        localparam logic [3:0] TCOR = 4'(2 + i * 3);
        localparam logic [3:0] TCNT = TCOR + 4'd1;
        localparam logic [3:0] TCR = TCOR + 4'd2;
        logic prescaled_tick;

        assign timer_irq[i] = underflow[i] && control[i][5];
        assign pin_edge[i] = !tclk_output_enable && tclk_sample != tclk_previous &&
                             (control[i][4] || tclk_sample != control[i][3]);
        assign count_tick[i] = running[i] && prescaled_tick;
        always_comb begin
            case (control[i][2:0])
                0: prescaled_tick = peripheral_tick && (&prescaler[1:0]);
                1: prescaled_tick = peripheral_tick && (&prescaler[3:0]);
                2: prescaled_tick = peripheral_tick && (&prescaler[5:0]);
                3: prescaled_tick = peripheral_tick && (&prescaler[7:0]);
                4: prescaled_tick = peripheral_tick && (&prescaler);
                6: prescaled_tick = rtc_tick;
                7: prescaled_tick = pin_edge[i];
                default: prescaled_tick = 0;
            endcase
        end
        always_ff @(posedge clk) begin
            if (reset) begin
                constant_value[i] <= '1;
                count[i] <= '1;
                control[i] <= 0;
                underflow[i] <= 0;
            end else begin
                if (write_reg && reg_number == TCOR)
                    constant_value[i] <= wdata;
                if (write_reg && reg_number == TCNT)
                    count[i] <= wdata;
                else if (count_tick[i])
                    count[i] <= count[i] == 0 ? constant_value[i] : count[i] - 32'd1;
                if (write_reg && reg_number == TCR) begin
                    control[i] <= wdata[7:0] & (i == 2 ? 8'hff : 8'h3f);
                    underflow[i] <= underflow[i] && wdata[8];
                end
                if (count_tick[i] && count[i] == 0)
                    underflow[i] <= 1;
            end
        end
    end

    always_ff @(posedge clk) begin
        if (reset) begin
            tclk_output_enable <= 0;
            running <= 0;
            captured <= 0;
            capture_dma <= 0;
            prescaler <= 0;
            tclk_sync <= 0;
            tclk_sample <= 0;
            tclk_previous <= 0;
            rvalid <= 0;
        end else begin
            tclk_sync <= tclk;
            tclk_sample <= tclk_sync;
            tclk_previous <= tclk_sample;
            if (peripheral_tick)
                prescaler <= prescaler + 10'd1;
            if (write_reg && reg_number == 0)
                tclk_output_enable <= wdata[0];
            if (write_reg && reg_number == 1)
                running <= wdata[2:0];
            if (write_reg && reg_number == 10)
                captured <= captured && wdata[9];
            if (capture_ack)
                capture_dma <= 0;
            if (capture_event) begin
                captured <= 1;
                capture_dma <= 1;
                if (!captured)
                    capture_value <= count[2];
            end
            rvalid <= request && selected && wstrb == 0;
            if (request && selected && wstrb == 0) begin
                rdata <= 0;
                if (read_reg) case (reg_number)
                    0: rdata <= {31'b0, tclk_output_enable};
                    1: rdata <= {29'b0, running};
                    2: rdata <= constant_value[0];
                    3: rdata <= count[0];
                    4: rdata <= {23'b0, underflow[0], control[0]};
                    5: rdata <= constant_value[1];
                    6: rdata <= count[1];
                    7: rdata <= {23'b0, underflow[1], control[1]};
                    8: rdata <= constant_value[2];
                    9: rdata <= count[2];
                    10: rdata <= {22'b0, captured, underflow[2], control[2]};
                    11: rdata <= capture_value;
                    default: ;
                endcase
            end
        end
    end
endmodule
