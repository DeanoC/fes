// Programmed priority registers and arbitration for the on-chip timers.
// The board supplies an already encoded external interrupt request.
module fes_sh7091_intc (
    input  logic        clk, reset,
    input  logic        request,
    input  logic [31:2] address,
    input  logic [3:0]  rmask, wstrb,
    input  logic [15:0] wdata,
    output logic        selected, rvalid,
    output logic [31:0] rdata,
    input  logic [3:0]  ext_level,
    input  logic [11:0] ext_event,
    input  logic [3:0]  timer_irq,
    input  logic [1:0]  refresh_irq,
    input  logic        cpu_ack,
    output logic [3:0]  cpu_level,
    output logic [11:0] cpu_event,
    output logic        ext_ack
);
    logic [15:0] ipra, iprb, iprc, selected_value;
    logic [3:0] levels [0:5];
    logic [11:0] events [0:5];
    logic [5:0] active;
    logic [3:0] selected_level;
    logic [11:0] selected_event;
    logic next_external, cpu_external, ack_external;

    assign selected = address[31:4] == 28'hffd0000 && address[3:2] != 0;
    assign active = {refresh_irq, timer_irq};
    assign levels[0] = ipra[15:12];
    assign levels[1] = ipra[11:8];
    assign levels[2] = ipra[7:4];
    assign levels[3] = ipra[7:4];
    assign levels[4] = iprb[11:8];
    assign levels[5] = iprb[11:8];
    assign events[0] = 12'h400;
    assign events[1] = 12'h420;
    assign events[2] = 12'h440;
    assign events[3] = 12'h460;
    assign events[4] = 12'h580;
    assign events[5] = 12'h5a0;
    assign ext_ack = cpu_ack && ack_external;

    always_comb begin
        case (address[3:2])
            1: selected_value = ipra;
            2: selected_value = iprb;
            3: selected_value = iprc;
            default: selected_value = 0;
        endcase

        selected_level = ext_level;
        selected_event = ext_event;
        next_external = ext_level != 0;
        for (int i = 0; i < 6; i++) begin
            if (active[i] && levels[i] > selected_level) begin
                selected_level = levels[i];
                selected_event = events[i];
                next_external = 0;
            end
        end
    end

    always_ff @(posedge clk) begin
        if (reset) begin
            ipra <= 0;
            iprb <= 0;
            iprc <= 0;
            cpu_level <= 0;
            cpu_event <= 0;
            cpu_external <= 0;
            ack_external <= 0;
            rvalid <= 0;
        end else begin
            cpu_level <= selected_level;
            cpu_event <= selected_event;
            cpu_external <= next_external;
            ack_external <= cpu_external;
            rvalid <= request && selected && wstrb == 0;
            if (request && selected && wstrb == 0)
                rdata <= rmask == 4'h3 ? {16'b0, selected_value} : 32'b0;
            if (request && selected && wstrb == 4'h3) case (address[3:2])
                1: ipra <= wdata;
                2: iprb <= wdata & 16'hfff0;
                3: iprc <= wdata;
                default: ;
            endcase
        end
    end
endmodule
