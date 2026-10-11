// SPDX-License-Identifier: GPL-3.0-or-later
// Original 520ST motherboard bring-up: a real 68000 and bounded memory bus.
// Storage is external to this module; it must acknowledge each request once
// and retain read data until the request drops. This is not a board memory
// controller or a cycle-exact GLUE/MMU implementation.
module st_machine #(
    parameter integer SYSTEM_CLOCK_HZ = 52_224_000,
    parameter integer CPU_CLOCK_HZ = 8_000_000,
    parameter integer BUS_TIMEOUT_HALVES = 128,
    // Enable only with a backend that retains an accepted CPU write.
    parameter EARLY_RAM_WRITE_COMPLETION = 0
) (
    input  wire clk_sys,
    input  wire reset,
    output wire rom_req,
    output wire [17:1] rom_addr,
    input  wire [15:0] rom_rdata,
    input  wire rom_ready,
    output wire ram_req,
    output wire [18:1] ram_addr,
    output wire [15:0] ram_wdata,
    output wire [1:0] ram_byte_enable,
    output wire ram_write,
    input  wire [15:0] ram_rdata,
    input  wire ram_ready,
    output wire exp_req,
    output wire [23:1] exp_addr,
    output wire [15:0] exp_wdata,
    output wire [1:0] exp_byte_enable,
    output wire exp_write,
    output wire [2:0] exp_fc,
    output wire exp_reset,
    output wire exp_phi1,
    output wire exp_phi2,
    input  wire exp_ack,
    input  wire exp_berr,
    input  wire [15:0] exp_rdata,
    // Encoded interrupt priority. MFP IRQ6 supplies its vector; other sources
    // use the 68000's VPA autovectors. IACK is held for the entire bus cycle.
    input  wire [2:0] exp_irq,
    input  wire irq_vectored,
    input  wire [7:0] irq_vector,
    output wire irq_ack,
    output reg [2:0] irq_level,
    input  wire [23:0] video_counter,
    output reg [23:0] screen_base,
    output reg [1:0] resolution,
    output reg [7:0] sync_mode,
    output wire [143:0] palette,
    output wire [23:0] debug_addr,
    output wire debug_bus_error,
    output wire debug_overlay,
    output wire debug_halted
);
    wire [23:1] cpu_addr;
    wire [15:0] cpu_wdata;
    reg [15:0] cpu_rdata;
    wire cpu_as_n, cpu_uds_n, cpu_lds_n, cpu_rw;
    wire [2:0] cpu_fc;
    wire phi1, phi2;
    reg dtack_n, berr_n;
    wire vpa_n;
    wire peripheral_reset_n, halted_n;
    wire ram_completion;
    st_cpu #(.SYSTEM_CLOCK_HZ(SYSTEM_CLOCK_HZ), .CPU_CLOCK_HZ(CPU_CLOCK_HZ)) cpu (
        .clk(clk_sys), .reset(reset), .addr(cpu_addr), .wdata(cpu_wdata),
        .rdata(ram_completion ? ram_rdata : cpu_rdata),
        .as_n(cpu_as_n), .uds_n(cpu_uds_n), .lds_n(cpu_lds_n),
        .rw(cpu_rw), .fc(cpu_fc),
        .dtack_n(ram_completion ? 1'b0 : dtack_n), .berr_n(berr_n),
        .vpa_n(vpa_n), .ipl_n(~exp_irq),
        .phi1_enable(phi1), .phi2_enable(phi2),
        .peripheral_reset_n(peripheral_reset_n), .halted_n(halted_n)
    );
    assign debug_addr = {cpu_addr, 1'b0};
    assign debug_bus_error = !berr_n;
    assign debug_overlay = cpu_fc[2] && {cpu_addr, 1'b0} < 24'd8;
    assign exp_reset = reset || !peripheral_reset_n;
    assign debug_halted = !halted_n;
    assign exp_phi1 = phi1;
    assign exp_phi2 = phi2;

    typedef enum logic [1:0] { IDLE, WAITING, COMPLETE, IACK } state_t;
    typedef enum logic [2:0] { ROM, RAM, IO, EXPANSION, FAULT, EMPTY_BANK } target_t;
    state_t state;
    target_t target;
    reg [23:0] address;
    reg [15:0] write_data;
    reg [1:0] lanes;
    reg writing;
    reg [2:0] function_code;
    localparam integer TIMER_BITS = $clog2(BUS_TIMEOUT_HALVES + 1);
    reg [TIMER_BITS-1:0] timeout_halves;
    reg [7:0] memory_config;
    reg vectored_cycle;
    // The RAM backend's ready word is valid before this fabric edge.
    // Present RAM data/DTACK directly to the CPU on that edge, then let
    // WAITING retain both for the rest of the bus cycle. Otherwise an
    // acknowledgement on a CPU phase edge waits a whole extra CPU cycle.
    // Selected posted writes use the same boundary; other targets retain
    // their registered completion.
    assign ram_completion = !reset && !cpu_as_n && state == WAITING &&
        target == RAM && (!writing || EARLY_RAM_WRITE_COMPLETION) && ram_ready &&
        timeout_halves != TIMER_BITS'(BUS_TIMEOUT_HALVES);
    reg [8:0] colors [0:15];
    genvar color;
    generate for (color = 0; color < 16; color = color + 1) begin : palette_pack
        assign palette[color*9 +: 9] = colors[color];
    end endgenerate

    // Upper byte is D15..8 at an even byte address. Lower byte is D7..0 at
    // an odd byte address. A 68000 never supplies A0; UDS/LDS select it.
    assign rom_req = state == WAITING && target == ROM && !reset;
    assign rom_addr = address < 24'd8 ? address[17:1] :
                     17'((address - 24'hfc0000) >> 1);
    wire [23:0] live_address = {cpu_addr, 1'b0};
    wire early_ram_read;
    wire early_ram_write;
    wire early_ram_cycle = early_ram_read || early_ram_write;
    wire [23:0] ram_address = early_ram_cycle ? live_address : address;
    // Reads have stable address/lanes on the same edge that captures the
    // motherboard transaction. Let the RAM arbiter launch on that edge,
    // then retain the captured request until completion. Selected writes
    // launch only with valid address, data and byte strobes on that edge.
    assign ram_req = !reset && (early_ram_cycle || (state == WAITING && target == RAM));
    // One physical 512 KiB bank, using the original ST's multiplexed row/
    // column wiring. Selecting 2 MiB drops MAD9 in both phases; selecting
    // 128 KiB repeats A9 in row and column. ROMs use these aliases to size RAM.
    assign ram_addr = memory_config[3:2] == 2'd2 ? {ram_address[19:11], ram_address[9:1]} :
                      memory_config[3:2] == 2'd0 ? {ram_address[17:9], ram_address[9:1]} : ram_address[18:1];
    assign ram_wdata = early_ram_write ? cpu_wdata :
                       early_ram_read || !writing ? 16'd0 : write_data;
    assign ram_byte_enable = early_ram_cycle ? {!cpu_uds_n, !cpu_lds_n} : lanes;
    assign ram_write = early_ram_cycle ? early_ram_write : writing;
    assign exp_req = state == WAITING && target == EXPANSION && !reset;
    assign exp_addr = address[23:1];
    assign exp_wdata = write_data;
    assign exp_byte_enable = lanes;
    assign exp_write = writing;
    assign exp_fc = function_code;
    assign vpa_n = state != IACK || vectored_cycle || reset;
    assign irq_ack = state == IACK && !reset;

    function automatic [23:0] bank_size(input [1:0] configuration);
        case (configuration)
            2'd0: bank_size = 24'h020000;
            2'd1: bank_size = 24'h080000;
            2'd2: bank_size = 24'h200000;
            default: bank_size = 24'd0;
        endcase
    endfunction
    wire [23:0] bank0_size = bank_size(memory_config[3:2]);
    wire supervisor = cpu_fc[2];
    wire live_palette = live_address >= 24'hff8240 && live_address <= 24'hff825e;
    wire live_io = live_palette || live_address == 24'hff8000 ||
                   live_address == 24'hff8200 || live_address == 24'hff8202 ||
                   live_address == 24'hff8204 || live_address == 24'hff8206 ||
                   live_address == 24'hff8208 ||
                   live_address == 24'hff820a || live_address == 24'hff8260;
    // Match the RAM branch of the normal decoder, including supervisor
    // protection and the reset-vector ROM overlay. IACK, empty banks and
    // all ROM/MMIO/expansion accesses retain their registered dispatch.
    assign early_ram_write = EARLY_RAM_WRITE_COMPLETION && state == IDLE &&
        !reset && !cpu_as_n && !cpu_rw && (!cpu_uds_n || !cpu_lds_n) &&
        cpu_fc != 3'b111 && live_address >= 24'd8 && live_address < bank0_size &&
        (supervisor || live_address >= 24'h000800);
    assign early_ram_read = state == IDLE && !reset && !cpu_as_n && cpu_rw &&
        (!cpu_uds_n || !cpu_lds_n) && cpu_fc != 3'b111 &&
        live_address < bank0_size && (supervisor || live_address >= 24'h000800) &&
        !(supervisor && live_address < 24'd8);
    wire palette_access = address >= 24'hff8240 && address <= 24'hff825e;
    wire [3:0] palette_index = address[4:1];
    reg [15:0] io_rdata;
    always @* begin
        io_rdata = 16'hffff;
        if (palette_access)
            io_rdata = {5'd0, colors[palette_index][8:6], 1'b0,
                        colors[palette_index][5:3], 1'b0, colors[palette_index][2:0]};
        else case (address)
            24'hff8000: io_rdata = {8'hff, memory_config};
            24'hff8200: io_rdata = {8'hff, screen_base[23:16]};
            24'hff8202: io_rdata = {8'hff, screen_base[15:8]};
            24'hff8204: io_rdata = {8'hff, video_counter[23:16]};
            24'hff8206: io_rdata = {8'hff, video_counter[15:8]};
            24'hff8208: io_rdata = {8'hff, video_counter[7:0]};
            24'hff820a: io_rdata = {sync_mode, 8'hff};
            24'hff8260: io_rdata = {6'd0, resolution, 8'hff};
            default: ;
        endcase
    end

    integer i;
    always @(posedge clk_sys) begin
        if (reset) begin
            state <= IDLE;
            target <= FAULT;
            address <= 24'd0;
            write_data <= 16'd0;
            lanes <= 2'd0;
            writing <= 1'b0;
            function_code <= 3'd0;
            timeout_halves <= 0;
            dtack_n <= 1'b1;
            berr_n <= 1'b1;
            cpu_rdata <= 16'hffff;
            screen_base <= 24'd0;
            resolution <= 2'd0;
            memory_config <= 8'd0;
            sync_mode <= 8'd0;
            irq_level <= 3'd0;
            vectored_cycle <= 1'b0;
            for (i = 0; i < 16; i = i + 1) colors[i] <= 9'd0;
        end else begin
            case (state)
                IDLE: begin
                    dtack_n <= 1'b1;
                    berr_n <= 1'b1;
                    if (!cpu_as_n && cpu_fc == 3'b111) begin
                        state <= IACK;
                        irq_level <= cpu_addr[3:1];
                        vectored_cycle <= irq_vectored && cpu_addr[3:1] == 3'd6;
                    end else if (!cpu_as_n && (!cpu_uds_n || !cpu_lds_n)) begin
                        address <= live_address;
                        write_data <= cpu_wdata;
                        lanes <= {!cpu_uds_n, !cpu_lds_n};
                        writing <= !cpu_rw;
                        function_code <= cpu_fc;
                        timeout_halves <= 0;
                        state <= WAITING;
                        if ((!supervisor && (live_address < 24'h000800 ||
                                              live_address[23:16] == 8'hff)) ||
                            (!cpu_rw && live_address < 24'd8))
                            target <= FAULT;
                        else if (supervisor && cpu_rw && live_address < 24'd8)
                            target <= ROM;
                        else if (live_address < bank0_size)
                            target <= RAM;
                        // The MMU acknowledges the whole original-ST 4 MiB
                        // RAM window, including unpopulated/unselected DRAM.
                        // Keep the existing all-ones empty-bank read model;
                        // writes have no physical DRAM destination.
                        else if (live_address < 24'h400000)
                            target <= EMPTY_BANK;
                        else if (live_address >= 24'hfc0000 && live_address < 24'hff0000)
                            target <= cpu_rw ? ROM : FAULT;
                        else if (live_io)
                            target <= IO;
                        else if (live_address >= 24'hfa0000 && live_address < 24'hfc0000)
                            target <= cpu_rw ? EXPANSION : FAULT;
                        else if (live_address[23:16] == 8'hff)
                            target <= EXPANSION;
                        else target <= FAULT;
                    end
                end
                WAITING: begin
                    // A memory backend receives a stable request until ready.
                    // Completion is remembered until the strobes release: no repeated
                    // peripheral side effects during slow CPU acknowledgement.
                    if (cpu_as_n) begin
                        state <= IDLE;
                    end else if (target == FAULT ||
                                 (target == EXPANSION && exp_berr) ||
                                 timeout_halves == TIMER_BITS'(BUS_TIMEOUT_HALVES)) begin
                        berr_n <= 1'b0;
                        state <= COMPLETE;
                    end else if (target == IO || target == EMPTY_BANK ||
                                 (target == RAM && ram_ready) ||
                                 (target == ROM && rom_ready) ||
                                 (target == EXPANSION && exp_ack)) begin
                        case (target)
                            RAM: cpu_rdata <= ram_rdata;
                            ROM: cpu_rdata <= rom_rdata;
                            EXPANSION: cpu_rdata <= exp_rdata;
                            EMPTY_BANK: cpu_rdata <= 16'hffff;
                            IO: begin
                                cpu_rdata <= io_rdata;
                                if (writing) begin
                                    if (palette_access) begin
                                        if (lanes[1]) colors[palette_index][8:6] <= write_data[10:8];
                                        if (lanes[0]) colors[palette_index][5:0] <=
                                            {write_data[6:4], write_data[2:0]};
                                    end else case (address)
                                        24'hff8000: if (lanes[0]) memory_config <= write_data[7:0];
                                        24'hff8200: if (lanes[0]) screen_base[23:16] <= write_data[7:0];
                                        24'hff8202: if (lanes[0]) screen_base[15:8] <= write_data[7:0];
                                        24'hff820a: if (lanes[1]) sync_mode <= write_data[15:8];
                                        24'hff8260: if (lanes[1]) resolution <= write_data[9:8];
                                        default: ;
                                    endcase
                                end
                            end
                            default: ;
                        endcase
                        dtack_n <= 1'b0;
                        state <= COMPLETE;
                    end else if (phi1 || phi2)
                        timeout_halves <= timeout_halves + 1'b1;
                end
                // TAS holds AS low across its read/write halves, releasing
                // both data strobes in between. Rearm at that boundary so
                // the second half issues a distinct, exactly-once write.
                COMPLETE: if (cpu_as_n || (cpu_uds_n && cpu_lds_n)) begin
                    dtack_n <= 1'b1;
                    berr_n <= 1'b1;
                    state <= IDLE;
                end
                IACK: begin
                    if (vectored_cycle) begin
                        cpu_rdata <= {8'hff, irq_vector};
                        dtack_n <= 1'b0;
                    end
                    if (cpu_as_n) begin
                        state <= IDLE;
                        dtack_n <= 1'b1;
                    end
                end
                default: state <= IDLE;
            endcase
        end
    end
endmodule
