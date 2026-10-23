// SPDX-License-Identifier: GPL-2.0-or-later
// 100 MHz byte storage using the existing addon SDRAM controller. Physical
// halfword offsets are byte_address >> 1 in its packed bank/row/column map.
// CPU/DMA share the motherboard port. Firmware writes require reset asserted.
// Cold reset restarts SDRAM initialization; warm reset drains issued work,
// suppresses its CPU response and preserves RAM/firmware and physical refresh.
module pcxt_memory (
    input wire clk, clk_pin, cold_reset, reset,
    output wire initialized,
    input wire memory_valid,
    output wire memory_ready,
    input wire [19:0] address,
    input wire [7:0] wdata,
    input wire write_cycle,
    output reg memory_done,
    output reg [7:0] memory_rdata,
    input wire firmware_valid,
    output wire firmware_ready,
    input wire [15:0] firmware_address,
    input wire [7:0] firmware_data,
    output reg firmware_done,
    output wire sdram_clk, sdram_cke, sdram_ncs, sdram_nras, sdram_ncas, sdram_nwe,
    output wire [1:0] sdram_ba,
    output wire [12:0] sdram_a,
    output wire sdram_dqml, sdram_dqmh,
    output wire [15:0] dq_out,
    output wire dq_oe,
    input wire [15:0] dq_rise, dq_fall
);
    reg busy, owner_firmware, discard, odd, physical;
    reg memory_seen, firmware_seen;
    reg [25:0] held_address;
    reg [15:0] held_data;
    reg [1:0] held_lanes;
    reg held_write;
    // Match the controller's conservative 100 MHz capture count: one fabric
    // stage after the board's input capture registers (ramtest rate-3 path).
    reg [15:0] dq_rise_q, dq_fall_q;
    always @(posedge clk) begin
        dq_rise_q <= dq_rise;
        dq_fall_q <= dq_fall;
    end
    wire controller_done;
    wire [15:0] controller_data;
    assign memory_ready = initialized && !cold_reset && !reset && !busy && !memory_seen;
    assign firmware_ready = initialized && !cold_reset && reset && !busy && !firmware_seen;
    /* verilator lint_off PINCONNECTEMPTY */
    // Separate the controller state/enable muxes from the physical pad FFs.
    // The existing controller mode delays command, address, data and OE
    // together and compensates its read-capture count for the extra cycle.
    sdram_addon_port #(.BYTE_MASK_ENABLED(1), .SINGLE_RANK_REFRESH(1),
                       .IO_OUTPUT_REGISTERS(1)) controller (
        .clk(clk), .clk_pin(clk_pin), .reset(cold_reset), .rate(2'd3),
        .refresh_window(1'b0), .refresh_pending(), .initialized(initialized),
        .start(busy && physical), .write(held_write), .addr(held_address),
        .wdata(held_data), .write_byte_enable(held_lanes),
        .done(controller_done), .rdata(controller_data),
        .sdram_clk(sdram_clk), .sdram_cke(sdram_cke), .sdram_ncs(sdram_ncs),
        .sdram_nras(sdram_nras), .sdram_ncas(sdram_ncas), .sdram_nwe(sdram_nwe),
        .sdram_ba(sdram_ba), .sdram_a(sdram_a),
        .sdram_dqml(sdram_dqml), .sdram_dqmh(sdram_dqmh),
        .dq_out(dq_out), .dq_oe(dq_oe), .dq_rise(dq_rise_q), .dq_fall(dq_fall_q)
    );
    /* verilator lint_on PINCONNECTEMPTY */
    always @(posedge clk) begin
        memory_done <= 0;
        firmware_done <= 0;
        if (cold_reset) begin
            busy <= 0; owner_firmware <= 0; discard <= 0; odd <= 0; physical <= 0;
            memory_seen <= 0; firmware_seen <= 0;
            held_address <= 0; held_data <= 0; held_lanes <= 0; held_write <= 0;
            memory_rdata <= 8'hff;
        end else begin
            if (!memory_valid || reset) memory_seen <= 0;
            if (!firmware_valid) firmware_seen <= 0;
            if (reset && busy && !owner_firmware) discard <= 1;
            if (busy) begin
                if (!physical || controller_done) begin
                    busy <= 0;
                    if (owner_firmware) firmware_done <= 1;
                    else if (!reset && !discard) begin
                        memory_done <= 1;
                        memory_rdata <= !physical ? 8'hff :
                                       odd ? controller_data[15:8] : controller_data[7:0];
                    end
                end
            end else if (firmware_valid && firmware_ready) begin
                busy <= 1; owner_firmware <= 1; discard <= 0; physical <= 1;
                firmware_seen <= 1; odd <= firmware_address[0];
                held_address <= {7'd0, 4'hf, firmware_address[15:1]};
                held_data <= {firmware_data, firmware_data};
                held_lanes <= firmware_address[0] ? 2'b10 : 2'b01;
                held_write <= 1;
            end else if (memory_valid && memory_ready) begin
                busy <= 1; owner_firmware <= 0; discard <= 0;
                memory_seen <= 1; odd <= address[0];
                // Defensively reject ISA holes and ROM writes even if a
                // caller bypasses the motherboard's own address protection.
                physical <= address[19:16] < 4'ha || (address[19:16] == 4'hf && !write_cycle);
                held_address <= {7'd0, address[19:1]};
                held_data <= {wdata, wdata};
                held_lanes <= address[0] ? 2'b10 : 2'b01;
                held_write <= write_cycle;
            end
        end
    end
endmodule
