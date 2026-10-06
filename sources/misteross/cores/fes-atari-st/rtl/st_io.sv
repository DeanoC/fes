// SPDX-License-Identifier: GPL-3.0-or-later
// 520ST motherboard peripherals in the system domain. The native interrupt
// clocks are independent of the HDMI scanout clock. Timing is functional,
// rather than a cycle-exact GLUE model. Unclaimed addresses go to the socket.
module st_io #(
    parameter integer SYSTEM_CLOCK_HZ = 52_224_000,
    parameter integer ENABLE_FLOPPY_WRITE = 0
) (
    input wire clk, reset, cold_reset,
    input wire req,
    input wire [23:1] addr,
    input wire write,
    input wire [15:0] wdata,
    input wire [1:0] byte_enable,
    output wire selected, ack,
    output wire [15:0] rdata,
    output wire [2:0] irq,
    output wire irq_vectored,
    output wire [7:0] irq_vector,
    input wire irq_ack,
    input wire [2:0] irq_level,
    input wire [23:0] screen_base,
    input wire [1:0] resolution,
    input wire [7:0] sync_mode,
    input wire monochrome,
    output reg [23:0] video_counter,
    input wire [143:0] keyboard,
    input wire [15:0] controller_buttons,
    input wire mouse_valid,
    input wire signed [15:0] mouse_dx, mouse_dy,
    input wire [1:0] mouse_buttons,
    output wire mouse_ready,
    output wire signed [15:0] audio_pcm,
    output wire audio_valid,
    input wire media_ready, media_frozen,
    output wire media_write_req,
    output wire [19:1] media_write_addr,
    output wire [15:0] media_write_data,
    input wire media_write_ready,
    output wire media_write_busy, media_changed,
    output wire media_req,
    output wire [19:0] media_addr,
    input wire [7:0] media_data,
    input wire media_valid,
    output wire dma_req, dma_write,
    output wire [23:0] dma_addr,
    output wire [15:0] dma_wdata,
    output wire [1:0] dma_byte_enable,
    input wire dma_ready,
    input wire [15:0] dma_rdata,
    output wire vblank, hblank
);
    wire [23:0] address = {addr, 1'b0};
    wire unused_inputs = ^{screen_base[7:0], sync_mode[7:2], sync_mode[0]};
    // HID rows arrive as separate mailbox writes. Interpret a coherent key
    // and modifier snapshot after the complete matrix is quiet for 1 ms.
    localparam integer KEYBOARD_SETTLE = (SYSTEM_CLOCK_HZ + 999) / 1000;
    localparam integer KEYBOARD_BITS = $clog2(KEYBOARD_SETTLE + 1);
    reg [143:0] observed_keyboard, stable_keyboard;
    reg [KEYBOARD_BITS-1:0] keyboard_age;
    always @(posedge clk) begin
        if (reset) begin
            observed_keyboard <= 0; stable_keyboard <= 0; keyboard_age <= 0;
        end else if (keyboard != observed_keyboard) begin
            observed_keyboard <= keyboard; keyboard_age <= 0;
        end else if (keyboard_age < KEYBOARD_BITS'(KEYBOARD_SETTLE)) begin
            keyboard_age <= keyboard_age + 1'b1;
            if (keyboard_age == KEYBOARD_BITS'(KEYBOARD_SETTLE - 1))
                stable_keyboard <= observed_keyboard;
        end
    end
    wire mfp_select = address >= 24'hfffa00 && address <= 24'hfffa2e;
    wire psg_select = address[23:8] == 16'hff88;
    wire kbd_select = address == 24'hfffc00 || address == 24'hfffc02;
    wire midi_select = address == 24'hfffc04 || address == 24'hfffc06;
    wire fdc_select = address == 24'hff8604 || address == 24'hff8606 ||
                      address == 24'hff8608 || address == 24'hff860a ||
                      address == 24'hff860c;
    assign selected = mfp_select || psg_select || kbd_select || midi_select || fdc_select;
    wire mfp_ack, psg_ack, kbd_ack, midi_ack, fdc_ack;
    wire [7:0] mfp_data, psg_data, kbd_data, midi_data;
    wire [15:0] fdc_data;
    assign ack = req && (mfp_select ? (byte_enable[0] ? mfp_ack : 1'b1) :
                         psg_select ? (byte_enable[1] ? psg_ack : 1'b1) :
                         kbd_select ? (byte_enable[1] ? kbd_ack : 1'b1) :
                         midi_select ? (byte_enable[1] ? midi_ack : 1'b1) :
                         fdc_select ? fdc_ack : 1'b0);
    assign rdata = mfp_select ? {8'hff, mfp_data} :
                   psg_select ? {psg_data, 8'hff} :
                   kbd_select ? {kbd_data, 8'hff} :
                   midi_select ? {midi_data, 8'hff} : fdc_data;

    reg [31:0] timer_phase, vertical_phase, horizontal_phase;
    wire [32:0] timer_sum = {1'b0, timer_phase} + 33'd2457600;
    wire [32:0] vertical_sum = {1'b0, vertical_phase} +
        (resolution == 2'd2 ? 33'd71 : sync_mode[1] ? 33'd50 : 33'd60);
    wire [32:0] horizontal_sum = {1'b0, horizontal_phase} +
        (resolution == 2'd2 ? 33'd35500 : sync_mode[1] ? 33'd15650 : 33'd15750);
    wire timer_ce = timer_sum >= 33'(SYSTEM_CLOCK_HZ);
    assign vblank = vertical_sum >= 33'(SYSTEM_CLOCK_HZ) && !reset;
    assign hblank = horizontal_sum >= 33'(SYSTEM_CLOCK_HZ) && !reset;
    reg vbl_pending, hbl_pending;
    reg [8:0] native_line;
    // Timer B is driven by native display enable, rather than every HBL.
    // Functional active widths are 40 us color / 20 us mono; blank lines
    // never produce events. The original GLUE's border phase is reduced.
    wire native_display = native_line < (resolution == 2'd2 ? 9'd400 : 9'd200) &&
        horizontal_phase < (resolution == 2'd2 ?
            32'((64'(SYSTEM_CLOCK_HZ) * 71) / 100) : 32'((64'(SYSTEM_CLOCK_HZ) * 5) / 8));
    always @(posedge clk) begin
        if (reset) begin
            timer_phase <= 0; vertical_phase <= 0; horizontal_phase <= 0;
            vbl_pending <= 0; hbl_pending <= 0; native_line <= 0;
            video_counter <= 0;
        end else begin
            timer_phase <= timer_ce ? 32'(timer_sum - 33'(SYSTEM_CLOCK_HZ)) : timer_sum[31:0];
            vertical_phase <= vblank ? 32'(vertical_sum - 33'(SYSTEM_CLOCK_HZ)) : vertical_sum[31:0];
            horizontal_phase <= hblank ? 32'(horizontal_sum - 33'(SYSTEM_CLOCK_HZ)) : horizontal_sum[31:0];
            if (vblank) vbl_pending <= 1;
            if (hblank) hbl_pending <= 1;
            if (irq_ack && irq_level == 4) vbl_pending <= 0;
            if (irq_ack && irq_level == 2) hbl_pending <= 0;
            if (vblank) begin
                video_counter <= {screen_base[23:8], 8'd0};
                native_line <= 0;
            end else if (hblank) begin
                if (native_line < (resolution == 2'd2 ? 9'd400 : 9'd200))
                    video_counter <= video_counter + (resolution == 2'd2 ? 24'd80 : 24'd160);
                if (native_line != 9'h1ff) native_line <= native_line + 1'b1;
            end
        end
    end
    wire mfp_irq, kbd_irq, midi_irq, fdc_irq;
    wire [7:0] port_a;
    assign irq = mfp_irq ? 3'd6 : vbl_pending ? 3'd4 : hbl_pending ? 3'd2 : 3'd0;
    assign irq_vectored = mfp_irq;
    st_mfp mfp (
        .clk(clk), .reset(reset), .timer_ce(timer_ce),
        .req(req && mfp_select && byte_enable[0]), .addr({addr[5:1], 1'b1}),
        .write(write), .wdata(wdata[7:0]), .rdata(mfp_data), .ack(mfp_ack),
        .gpip({!monochrome, 1'b1, !fdc_irq, !(kbd_irq || midi_irq), 4'b1111}),
        .timer_a(1'b0), .timer_b(native_display), .irq(mfp_irq),
        .irq_vector(irq_vector), .iack(irq_ack && irq_level == 3'd6)
    );
    wire kbd_tx_valid, kbd_tx_ready, kbd_rx_valid, kbd_rx_ready;
    wire [7:0] kbd_tx_data, kbd_rx_data;
    st_acia #(.SYSTEM_CLOCK_HZ(SYSTEM_CLOCK_HZ)) keyboard_acia (
        .clk(clk), .reset(reset), .bus_req(req && kbd_select && byte_enable[1]),
        .bus_reg(addr[1]), .bus_write(write), .bus_wdata(wdata[15:8]),
        .bus_rdata(kbd_data), .bus_ack(kbd_ack), .irq(kbd_irq),
        .tx_valid(kbd_tx_valid), .tx_data(kbd_tx_data), .tx_ready(kbd_tx_ready),
        .rx_valid(kbd_rx_valid), .rx_data(kbd_rx_data), .rx_ready(kbd_rx_ready),
        .rx_frame_error(1'b0), .rx_parity_error(1'b0)
    );
    wire unused_midi_valid, unused_midi_ready;
    wire [7:0] unused_midi_data;
    st_acia #(.SYSTEM_CLOCK_HZ(SYSTEM_CLOCK_HZ)) midi_acia (
        .clk(clk), .reset(reset), .bus_req(req && midi_select && byte_enable[1]),
        .bus_reg(addr[1]), .bus_write(write), .bus_wdata(wdata[15:8]),
        .bus_rdata(midi_data), .bus_ack(midi_ack), .irq(midi_irq),
        .tx_valid(unused_midi_valid), .tx_data(unused_midi_data), .tx_ready(1'b1),
        .rx_valid(1'b0), .rx_data(8'd0), .rx_ready(unused_midi_ready),
        .rx_frame_error(1'b0), .rx_parity_error(1'b0)
    );
    st_ikbd #(.SYSTEM_CLOCK_HZ(SYSTEM_CLOCK_HZ)) ikbd (
        .clk(clk), .reset(reset), .command_valid(kbd_tx_valid), .command_data(kbd_tx_data),
        .command_ready(kbd_tx_ready), .response_valid(kbd_rx_valid), .response_data(kbd_rx_data),
        .response_ready(kbd_rx_ready), .keyboard(stable_keyboard), .controller_buttons(controller_buttons),
        .mouse_valid(mouse_valid), .mouse_dx(mouse_dx), .mouse_dy(mouse_dy),
        .mouse_buttons(mouse_buttons), .mouse_ready(mouse_ready)
    );
    st_ym2149 #(.SYSTEM_CLOCK_HZ(SYSTEM_CLOCK_HZ)) psg (
        .clk(clk), .reset(reset), .bus_req(req && psg_select && byte_enable[1]),
        .bus_reg(addr[1]), .bus_write(write), .bus_wdata(wdata[15:8]),
        .bus_rdata(psg_data), .bus_ack(psg_ack), .pcm_signed(audio_pcm),
        .sample_valid(audio_valid), .port_a(port_a)
    );
    st_floppy #(.ENABLE_WRITE(ENABLE_FLOPPY_WRITE)) floppy (
        .clk(clk), .reset(reset), .mmio_req(req && fdc_select), .mmio_addr({addr[3:1], 1'b0}),
        .mmio_write(write), .mmio_wdata(wdata), .mmio_byte_enable(byte_enable),
        .mmio_rdata(fdc_data), .mmio_ack(fdc_ack), .drive_select(port_a[2:1]), .side(port_a[0]),
        .cold_reset(cold_reset), .media_frozen(media_frozen),
        .media_write_req(media_write_req), .media_write_addr(media_write_addr),
        .media_write_data(media_write_data), .media_write_ready(media_write_ready),
        .media_write_busy(media_write_busy), .media_changed(media_changed),
        .media_ready(media_ready), .media_req(media_req), .media_addr(media_addr),
        .media_data(media_data), .media_valid(media_valid), .dma_req(dma_req),
        .dma_addr(dma_addr), .dma_wdata(dma_wdata), .dma_byte_enable(dma_byte_enable),
        .dma_ready(dma_ready), .dma_write(dma_write), .dma_rdata(dma_rdata), .irq(fdc_irq)
    );
endmodule
