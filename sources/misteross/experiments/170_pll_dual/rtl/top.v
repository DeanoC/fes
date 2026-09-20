module top(input wire FPGA_CLK1_50);
    wire [1:0] clocks;
    wire locked;
    wire [31:0] gpo;
    wire [31:0] gpi;
    wire busy, done, lost_lock, lock_status;
    wire [15:0] result;
    wire busy1, done1, lost1, lock1;
    wire [15:0] result1;
    (* async_reg = "true" *) reg reset_meta = 0;
    reg reset_sync = 0;
    always @(posedge FPGA_CLK1_50) begin
        reset_meta <= gpo[2];
        reset_sync <= reset_meta;
    end
    altera_pll #(
        .reference_clock_frequency("50.0 MHz"),
        .number_of_clocks(2),
        .output_clock_frequency0("25.0 MHz"),
        .output_clock_frequency1("40.0 MHz"),
        .phase_shift0("0 ps"),
        .phase_shift1("0 ps"),
        .duty_cycle0(50),
        .duty_cycle1(50),
        .operation_mode("direct"),
        .fractional_vco_multiplier("false")
    ) pll (
        .refclk(FPGA_CLK1_50),
        .rst(reset_sync),
        .outclk(clocks),
        .locked(locked)
    );
    cyclonev_hps_interface_mpu_general_purpose hps_gp (.gp_in(gpi), .gp_out(gpo));
    pll_meter meter (
        .refclk(FPGA_CLK1_50),
        .testclk(clocks[0]),
        .locked(locked),
        .request(gpo[1]),
        .busy(busy),
        .done(done),
        .lost_lock(lost_lock),
        .lock_status(lock_status),
        .result(result)
    );
    pll_meter meter1 (
        .refclk(FPGA_CLK1_50),
        .testclk(clocks[1]),
        .locked(locked),
        .request(gpo[1]),
        .busy(busy1),
        .done(done1),
        .lost_lock(lost1),
        .lock_status(lock1),
        .result(result1)
    );
    wire [15:0] selected = gpo[3] ? result1 : result;
    wire selected_busy = gpo[3] ? busy1 : busy;
    wire selected_done = gpo[3] ? done1 : done;
    wire selected_lock = gpo[3] ? lock1 : lock_status;
    wire selected_lost = gpo[3] ? lost1 : lost_lock;
    assign gpi = {16'hD714, selected_busy, selected_done, selected_lock, selected_lost,
                  reset_sync, 3'b0, gpo[0] ? selected[15:8] : selected[7:0]};
endmodule

module pll_meter #(
    parameter WINDOW_BITS = 20
) (
    input wire refclk,
    input wire testclk,
    input wire locked,
    input wire request,
    output reg busy = 0,
    output reg done = 0,
    output reg lost_lock = 0,
    output reg lock_status = 0,
    output reg [15:0] result = 0
);
    reg [7:0] prescale = 0;
    always @(posedge testclk)
        prescale <= prescale + 1'b1;

    (* async_reg = "true" *) reg divided_meta = 0;
    (* async_reg = "true" *) reg divided_sync = 0;
    (* async_reg = "true" *) reg request_meta = 0;
    (* async_reg = "true" *) reg request_sync = 0;
    (* async_reg = "true" *) reg lock_meta = 0;
    reg divided_last = 0;
    reg active_request = 0;
    reg [WINDOW_BITS-1:0] remaining = 0;
    reg [15:0] edges = 0;
    wire rise = divided_sync && !divided_last;

    always @(posedge refclk) begin
        divided_meta <= prescale[7];
        divided_sync <= divided_meta;
        divided_last <= divided_sync;
        request_meta <= request;
        request_sync <= request_meta;
        lock_meta <= locked;
        lock_status <= lock_meta;
        if (!busy && request_sync != done) begin
            active_request <= request_sync;
            busy <= 1;
            remaining <= {WINDOW_BITS{1'b1}};
            edges <= 0;
            lost_lock <= !lock_status;
        end else if (busy) begin
            edges <= edges + {15'b0, rise};
            lost_lock <= lost_lock || !lock_status;
            remaining <= remaining - 1'b1;
            if (remaining == 0) begin
                result <= edges + {15'b0, rise};
                done <= active_request;
                busy <= 0;
            end
        end
    end
endmodule
