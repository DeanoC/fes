// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_application.vh"
`include "fes_display_plane.vh"
// fes.video.session-display plane (fes.fabric.video.display-plane 1.0): blend premultiplied RGBA launcher frames from
// HPS DDR over a shell's finished 720p video word (after any video part).
//
// machine and video use the part response layout: RGB888 in 23..0, then DE,
// HS, VS and CE. The plane derives raster coordinates from the DE/VS edges of
// that word and delays the entire word by three pixel clocks, so RGB and
// sync stay aligned. Alpha 0, a closed or faulted plane, or an unlocked raster
// returns the machine word unchanged. The plane has its own control, reset and
// drain; opening or closing never changes sync or machine execution.
//
// The plane reads through fes.memory.hps-ddr port 0 in pixel_clk. The shell
// owns fes_hps_ddr, so its other ports stay free for the core: it passes
// ddr_hold as that wrapper's hold and returns port 0's reset and drained.
module fes_display_plane #(
    parameter [31:0] WINDOW_BASE = `FES_APPLICATION_HPS_DDR_WINDOW_BASE,
    // Where the machine picture lands in the 1280x720 raster, for surrounds.
    parameter [15:0] PICTURE_LEFT = 16'd0,
    parameter [15:0] PICTURE_TOP = 16'd0,
    parameter [15:0] PICTURE_WIDTH = 16'd1280,
    parameter [15:0] PICTURE_HEIGHT = 16'd720
) (
    input wire clk_sys, pixel_clk, reset_hold, exec_reset,
    input wire request,
    input wire [6:0] opcode,
    input wire [7:0] index,
    input wire [15:0] argument,
    output wire response_valid, response_error,
    output wire [15:0] response_data,
    output wire quiesced_sys,
    input wire [27:0] machine,
    output reg [27:0] video = 28'd0,
    output reg ui_active = 1'b0,
    output reg locked = 1'b0,
    output wire [31:0] displayed_sequence, underflows,
    output wire faulted,
    output wire ddr_hold,
    input wire ddr_reset, ddr_drained,
    output wire [27:0] ddr_address,
    output wire [7:0] ddr_burstcount,
    output wire ddr_read,
    input wire ddr_waitrequest,
    input wire [127:0] ddr_readdata,
    input wire ddr_readdatavalid
);
    localparam [10:0] H_LAST = 11'd1649;
    localparam [9:0] V_LAST = 10'd749;
    localparam [9:0] VS_FIRST = 10'd725;

    wire pixel_request, pixel_response_valid, pixel_response_error;
    wire [6:0] pixel_opcode;
    wire [7:0] pixel_index;
    wire [15:0] pixel_argument, pixel_response_data;
    fes_display_cdc crossing (
        .clk_sys(clk_sys), .pixel_clk(pixel_clk), .request(request),
        .opcode(opcode), .index(index), .argument(argument),
        .response_valid(response_valid), .response_error(response_error), .response_data(response_data),
        .pixel_request(pixel_request), .pixel_opcode(pixel_opcode),
        .pixel_index(pixel_index), .pixel_argument(pixel_argument),
        .pixel_response_valid(pixel_response_valid), .pixel_response_error(pixel_response_error),
        .pixel_response_data(pixel_response_data)
    );

    // Stage 1 holds the incoming word and its raster coordinate. The fixed
    // CTA-770.3 720p60 raster is predicted from the previous coordinate; a DE
    // rise starts a row and a VS rise is row 725. A whole frame matching that
    // prediction locks the coordinate; any mismatch unlocks it.
    reg [27:0] stage1 = 28'd0;
    reg [10:0] raster_h = H_LAST;
    reg [9:0] raster_v = V_LAST;
    reg consistent = 1'b0;
    wire [10:0] h_step = raster_h == H_LAST ? 11'd0 : raster_h + 11'd1;
    wire [9:0] v_step = raster_h != H_LAST ? raster_v :
                        raster_v == V_LAST ? 10'd0 : raster_v + 10'd1;
    wire expected_de = h_step < 11'd1280 && v_step < 10'd720;
    wire expected_hs = h_step >= 11'd1390 && h_step < 11'd1430;
    wire expected_vs = v_step >= VS_FIRST && v_step < 10'd730;
    wire mismatch = machine[27:24] != {1'b1, expected_vs, expected_hs, expected_de};
    wire de_rise = machine[24] && !stage1[24];
    wire vs_rise = machine[26] && !stage1[26];
    always @(posedge pixel_clk) begin
        stage1 <= machine;
        raster_h <= de_rise || vs_rise ? 11'd0 : h_step;
        raster_v <= vs_rise ? VS_FIRST : v_step;
        if (vs_rise) begin
            locked <= consistent && !mismatch;
            consistent <= 1'b1;
        end else if (mismatch) begin
            locked <= 1'b0;
            consistent <= 1'b0;
        end
    end

    (* async_reg = "true" *) reg [1:0] execution_sync = 2'b11;
    (* async_reg = "true" *) reg [1:0] quiesced_sync = 2'b11;
    wire enable, quiesce, submit_valid, submit_slot, submit_ready, drained;
    wire [31:0] submit_sequence;
    wire port_reset = ddr_reset;
    wire port_drained = ddr_drained;
    reg started = 1'b0, hard_fault = 1'b0, scanout_fault = 1'b0;
    // On a hard hold the reader cannot see responses and must never restart.
    // Only the guard can prove its accepted/queued traffic has actually drained.
    wire quiesced = port_drained && (drained || (hard_fault && port_reset)) &&
                    !ui_active && (!enable || quiesce);
    always @(posedge pixel_clk) execution_sync <= {execution_sync[0], exec_reset};
    always @(posedge clk_sys) quiesced_sync <= {quiesced_sync[0], quiesced};
    assign quiesced_sys = quiesced_sync[1];
    fes_menu_control #(
        .FAULT_QUIESCE_ACK(1'b1),
        .PIXEL_FORMAT(16'(`FES_DISPLAY_PLANE_PIXEL_FORMAT_PREMULTIPLIED_RGBA)),
        .PICTURE_INFO(1'b1), .PICTURE_LEFT(PICTURE_LEFT), .PICTURE_TOP(PICTURE_TOP),
        .PICTURE_WIDTH(PICTURE_WIDTH), .PICTURE_HEIGHT(PICTURE_HEIGHT)
    ) control (
        .clk(pixel_clk), .request(pixel_request), .opcode(pixel_opcode),
        .index(pixel_index), .argument(pixel_argument), .exec_reset(execution_sync[1]),
        .drained(quiesced), .faulted(faulted), .submit_ready(submit_ready),
        .displayed_sequence(displayed_sequence), .underflows(underflows),
        .response_valid(pixel_response_valid), .response_error(pixel_response_error),
        .response_data(pixel_response_data), .enable(enable), .quiesce(quiesce),
        .submit_valid(submit_valid), .submit_slot(submit_slot), .submit_sequence(submit_sequence)
    );

    wire [23:0] launcher_rgb;
    wire [7:0] launcher_alpha;
    reg was_enabled = 1'b0;
    reg [31:0] opening_sequence = 32'd0, opening_underflows = 32'd0;
    wire frame_end = raster_h == H_LAST && raster_v == V_LAST;
    assign faulted = hard_fault || scanout_fault;
    always @(posedge pixel_clk) begin
        if (enable && !port_reset && !reset_hold) started <= 1'b1;
        // An unexpected PLL hold hides old guard responses. Keep that port
        // contained, but retain the machine and allow the runtime to close UI.
        if (started && reset_hold && !drained) hard_fault <= 1'b1;
        // A visible plane that misses a pixel or loses the raster stops
        // compositing; the runtime sees the fault and closes the plane.
        if (ui_active && (underflows != opening_underflows || !locked)) scanout_fault <= 1'b1;
        was_enabled <= enable;
        if (enable && !was_enabled) begin
            opening_sequence <= displayed_sequence;
        end
        if (frame_end) begin
            if (!enable || quiesce || faulted || reset_hold || !locked) ui_active <= 1'b0;
            else if (!ui_active && displayed_sequence != opening_sequence) begin
                // Hidden startup retries may have counted misses. Monitor only
                // underruns after the completed launcher frame becomes visible.
                opening_underflows <= underflows;
                ui_active <= 1'b1;
            end
        end
    end
    fes_menu_video #(.WINDOW_BASE(WINDOW_BASE),
                    .EXTERNAL_RASTER(1'b1), .COMPLETE_FRAME_ACK(1'b1)) scanout (
        .window_base(32'd0), .window_base_valid(1'b0), .response(2'b00),
        .current_sequence(), .current_slot(), .memory_errors(), .outstanding_beats(),
        .clk(pixel_clk), .rst(port_reset), .enable(enable && !faulted), .quiesce(quiesce),
        .raster_h(raster_h), .raster_v(raster_v),
        .submit_valid(submit_valid), .submit_slot(submit_slot), .submit_sequence(submit_sequence),
        .submit_ready(submit_ready), .displayed_sequence(displayed_sequence),
        .underflows(underflows), .quiesced(drained), .rgb(launcher_rgb), .alpha(launcher_alpha),
        .de(), .hs(), .vs(),
        .address(ddr_address), .burstcount(ddr_burstcount), .read(ddr_read),
        .waitrequest(ddr_waitrequest), .readdata(ddr_readdata), .readdatavalid(ddr_readdatavalid)
    );
    // No execution hold: physical machine reset and display drain are separate.
    assign ddr_hold = reset_hold || hard_fault;

    // Stage 2 selects the plane pixel and the machine weight. Premultiplied
    // "over" is out = P + M * (255 - a) / 255; 256 - a - a[7] maps alpha 0 to
    // 256 (machine exact) and 255 to 0 (plane exact).
    reg [27:0] stage2 = 28'd0;
    reg [23:0] plane_rgb = 24'd0;
    reg [8:0] machine_weight = 9'd256;
    wire show = ui_active && !faulted && !reset_hold && locked;
    always @(posedge pixel_clk) begin
        stage2 <= stage1;
        plane_rgb <= show ? launcher_rgb : 24'd0;
        machine_weight <= show ? 9'd256 - {1'b0, launcher_alpha} - {8'd0, launcher_alpha[7]} : 9'd256;
    end
    // Stage 3: three unsigned 9x9 DSP products, then saturate the sum. A
    // weight of at most 256 keeps each product below 2^16.
    function automatic [7:0] over(input [7:0] launcher, input [16:0] product);
        reg [8:0] sum;
        begin
            sum = {1'b0, launcher} + {1'b0, product[15:8]};
            over = sum[8] ? 8'hff : sum[7:0];
        end
    endfunction
    (* multstyle = "dsp" *) wire [16:0] red_product = stage2[23:16] * machine_weight;
    (* multstyle = "dsp" *) wire [16:0] green_product = stage2[15:8] * machine_weight;
    (* multstyle = "dsp" *) wire [16:0] blue_product = stage2[7:0] * machine_weight;
    always @(posedge pixel_clk) begin
        video <= {stage2[27:24],
                  over(plane_rgb[23:16], red_product),
                  over(plane_rgb[15:8], green_product),
                  over(plane_rgb[7:0], blue_product)};
    end
endmodule
