// BCR2.PORTEN connects the shared bus pins to the SH7091's two GPIO ports.
module fes_sh7091_gpio (
    input  logic        clk, reset, power_reset, bus_tick, port_enable,
    input  logic        request,
    input  logic [31:2] address,
    input  logic [3:0]  rmask, wstrb,
    input  logic [31:0] wdata,
    output logic        selected, rvalid,
    output logic [31:0] rdata,
    input  logic [19:0] pins_in,
    output logic [19:0] pins_out, pins_oe, pins_pullup,
    output logic        irq
);
    localparam logic [4:0] PCTRA = 5'd11, PDTRA = 5'd12,
                           PCTRB = 5'd16, PDTRB = 5'd17, GPIOIC = 5'd18;
    logic [39:0] port_control;
    logic [19:0] data_latch = 0, pin_meta, pin_sync, pin_sample, port_value;
    logic [19:0] port_direction, port_no_pullup;
    logic [15:0] irq_enable;
    logic [4:0] index;
    logic [3:0] lanes;

    assign index = address[6:2];
    assign selected = address[31:7] == 25'h1ff0000 && index inside {PCTRA, PDTRA, PCTRB, PDTRB, GPIOIC};
    assign lanes = index inside {PCTRA, PCTRB} ? 4'hf : 4'h3;
    for (genvar pin = 0; pin < 20; pin++) begin : pin_config
        assign port_direction[pin] = port_control[2*pin];
        assign port_no_pullup[pin] = port_control[2*pin + 1];
    end

    assign pins_out = data_latch;
    assign pins_oe = port_enable ? port_direction : 20'b0;
    assign pins_pullup = port_enable ? ~(port_no_pullup | port_direction) : 20'b0;
    assign port_value = (pin_sample & ~port_direction) | (data_latch & port_direction);
    assign irq = port_enable && (|(irq_enable & ~port_direction[15:0] & ~pin_sample[15:0]));

    always_ff @(posedge clk) begin
        pin_meta <= pins_in;
        pin_sync <= pin_meta;
        if (bus_tick)
            pin_sample <= pin_sync;

        if (power_reset) begin
            port_control <= 0;
            irq_enable <= 0;
        end else if (!reset && request && selected && wstrb == lanes) case (index)
            PCTRA: port_control[31:0] <= wdata;
            PDTRA: data_latch[15:0] <= wdata[15:0];
            PCTRB: port_control[39:32] <= wdata[7:0];
            PDTRB: data_latch[19:16] <= wdata[3:0];
            GPIOIC: irq_enable <= wdata[15:0];
            default: ;
        endcase

        rvalid <= !reset && request && selected && wstrb == 0;
        if (!reset && request && selected && wstrb == 0) begin
            rdata <= 0;
            if (rmask == lanes) case (index)
                PCTRA: rdata <= port_control[31:0];
                PDTRA: rdata[15:0] <= port_value[15:0];
                PCTRB: rdata[7:0] <= port_control[39:32];
                PDTRB: rdata[3:0] <= port_value[19:16];
                GPIOIC: rdata[15:0] <= irq_enable;
                default: ;
            endcase
        end
    end
endmodule
