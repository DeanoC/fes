// SPDX-License-Identifier: GPL-3.0-or-later
// Original WD1772/ST DMA register model for an exact 720 KiB .st image.
// Primary contracts: WD177X-00 datasheet and EmuTOS VERSION_1_4 bios/{fdc,dma}.h
// https://info-coach.fr/atari/documents/general/fd/WD177x-00.pdf
// https://github.com/emutos/emutos/tree/978e37569bff95841e42675d11fcc6799aad8483/bios
//
// One connected drive A; drive B is absent. side is raw YM2149 port-A bit 0:
// 1 selects side 0, 0 selects side 1. Media and RAM must share clk with this
// interface (a board adapter owns any CDC). A request stays stable until its
// valid/ready handshake; a full idle cycle separates successive requests.
// MMIO reads/writes complete once per asserted request. CPU register changes
// cannot change a pending media or DMA transfer. DMA only writes words wholly
// inside the original 520ST RAM window, excluding the read-only vector alias.
//
// Sector images omit flux, CRC and deleted-sector metadata. Step/spin-up timing
// is reduced; read-address/track, formatting, PIO streams and ACSI are absent.
module st_floppy #(
    parameter bit ENABLE_WRITE = 0,
    parameter integer COMMAND_DELAY_CYCLES = 64,
    parameter integer SYSTEM_CLOCK_HZ = 52224000,
    parameter integer INDEX_PERIOD_CYCLES = SYSTEM_CLOCK_HZ / 5
) (
    input  wire        clk,
    input  wire        reset,
    input  wire        cold_reset,
    input  wire        media_frozen,
    input  wire        mmio_req,
    input  wire [3:0]  mmio_addr,
    input  wire        mmio_write,
    input  wire [15:0] mmio_wdata,
    input  wire [1:0]  mmio_byte_enable,
    output reg  [15:0] mmio_rdata,
    output reg         mmio_ack,
    input  wire [1:0]  drive_select,
    input  wire        side,
    input  wire        media_ready,
    output wire        media_req,
    output wire [19:0] media_addr,
    input  wire [7:0]  media_data,
    input  wire        media_valid,
    output wire        media_write_req,
    output wire [19:1] media_write_addr,
    output wire [15:0] media_write_data,
    input  wire        media_write_ready,
    output wire        media_write_busy,
    output wire        media_changed,
    output wire        dma_req,
    output wire [23:0] dma_addr,
    output wire        dma_write,
    output wire [15:0] dma_wdata,
    output wire [1:0]  dma_byte_enable,
    input  wire        dma_ready,
    input  wire [15:0] dma_rdata,
    output reg         irq
);
    localparam integer DELAY_BITS = $clog2(COMMAND_DELAY_CYCLES + 1);
    localparam integer INDEX_BITS = INDEX_PERIOD_CYCLES > 1 ?
                                    $clog2(INDEX_PERIOD_CYCLES) : 1;
    typedef enum logic [2:0] {
        IDLE, TYPE_I_WAIT, MEDIA_WAIT, MEDIA_GAP, DMA_WAIT, DMA_GAP, NO_DMA, WRITE_WAIT
    } state_t;
    state_t state;
    reg [8:0] dma_mode;
    reg [23:0] dma_base, dma_cursor;
    reg [7:0] sector_count, track_reg, sector_reg, data_reg;
    reg [7:0] head_track;
    reg step_direction;
    reg type_i, motor_on, record_error, lost_data, dma_error, drq;
    reg multi_sector, have_high_byte, writing;
    reg [19:0] media_cursor;
    reg [15:0] word_buffer;
    reg [7:0] sector_word;
    reg [DELAY_BITS-1:0] command_delay;
    reg [7:0] new_track, new_head;
    reg new_direction, seek_error;
    reg immediate_irq, force_on_index;
    reg [INDEX_BITS-1:0] index_count;
    reg [3:0] idle_revolutions;

    wire drive_a = drive_select == 2'b10;
    wire busy = state != IDLE || media_write_busy;
    wire index_pulse = motor_on && media_ready && drive_a &&
                       index_count == INDEX_BITS'(INDEX_PERIOD_CYCLES - 1);
    wire dma_address_valid = !dma_cursor[0] && dma_cursor >= 24'd8 &&
                             dma_cursor < 24'h07ffff;
    // Ordinary IRQ clears on status/new-command. A D8 immediate IRQ remains
    // latched until D0, as specified for WD1772 Type IV commands.
    wire [7:0] fdc_status = {
        motor_on, drive_a && (!ENABLE_WRITE || !media_ready), type_i, record_error, 1'b0,
        type_i ? (drive_a && head_track == 0) : lost_data,
        type_i ? index_pulse : drq, busy
    };
    wire known_mmio = mmio_addr[3:1] == 3'd2 || mmio_addr[3:1] == 3'd3 ||
                      mmio_addr[3:1] == 3'd4 || mmio_addr[3:1] == 3'd5 ||
                      mmio_addr[3:1] == 3'd6;
    wire word_access = mmio_byte_enable == 2'b11;
    wire accept_mmio = mmio_req && !mmio_ack && known_mmio;
    wire write_command = accept_mmio && mmio_write && word_access &&
                         mmio_addr[3:1] == 3'd2 && !dma_mode[4] &&
                         !dma_mode[3] && dma_mode[2:1] == 2'd0;
    wire force_command = write_command && mmio_wdata[7:4] == 4'hd;
    reg [15:0] read_value;
    always @* begin
        read_value = 16'hffff;
        case (mmio_addr[3:1])
            3'd2: if (word_access) begin
                if (dma_mode[4]) read_value = {8'hff, sector_count};
                else if (!dma_mode[3]) begin
                    case (dma_mode[2:1])
                        2'd0: read_value = {8'hff, fdc_status};
                        2'd1: read_value = {8'hff, track_reg};
                        2'd2: read_value = {8'hff, sector_reg};
                        2'd3: read_value = {8'hff, data_reg};
                    endcase
                end
            end
            3'd3: read_value = {13'd0, drq, sector_count != 0, !dma_error};
            3'd4: read_value = {8'hff, dma_base[23:16]};
            3'd5: read_value = {8'hff, dma_base[15:8]};
            3'd6: read_value = {8'hff, dma_base[7:0]};
            default: ;
        endcase
    end

    assign media_req = state == MEDIA_WAIT && !reset;
    assign media_addr = media_cursor;
    // Removal also withdraws a word that has not reached RAM yet. A shared
    // memory controller may drain a command it already issued, without
    // returning that old completion to this canceled transfer.
    wire writer_dma_req;
    wire [23:0] writer_dma_addr;
    wire writer_ready, writer_error;
    wire writer_req = ENABLE_WRITE && state == WRITE_WAIT && media_ready && drive_a && !reset;
    st_floppy_writer writer (
        .clk(clk), .cold_reset(cold_reset), .frozen(media_frozen),
        .job_req(writer_req), .job_ram_addr(dma_cursor), .job_media_addr(media_cursor[19:1]),
        .job_ready(writer_ready), .job_error(writer_error), .busy(media_write_busy), .changed(media_changed),
        .dma_req(writer_dma_req), .dma_addr(writer_dma_addr), .dma_ready(dma_ready), .dma_data(dma_rdata),
        .media_req(media_write_req), .media_addr(media_write_addr), .media_data(media_write_data),
        .media_ready(media_write_ready)
    );
    assign dma_req = writer_dma_req || (state == DMA_WAIT && dma_address_valid &&
                     media_ready && drive_a && !reset);
    assign dma_addr = writer_dma_req ? writer_dma_addr : dma_cursor;
    assign dma_write = !writer_dma_req;
    assign dma_wdata = word_buffer;
    assign dma_byte_enable = 2'b11;

    integer computed_head;
    integer computed_track;
    reg computed_direction;
    wire [19:0] sector_offset = 20'(((int'(track_reg) * 2 + (side ? 0 : 1)) * 9 +
                                   int'(sector_reg) - 1) * 512);
    // These reserved/word-pair bits have no original-ST behavior.
    wire unused_inputs = mmio_addr[0] ^ (^mmio_wdata[15:9]) ^
                         dma_mode[5] ^ dma_mode[0];
    always @* begin
        computed_head = int'(head_track);
        computed_track = int'(track_reg);
        computed_direction = step_direction;
        case (mmio_wdata[7:4])
            4'h0: begin
                computed_head = 0;
                computed_track = 0;
                computed_direction = 1'b0;
            end
            4'h1: begin
                computed_head = int'(head_track) + int'(data_reg) - int'(track_reg);
                computed_track = int'(data_reg);
                computed_direction = data_reg >= track_reg;
            end
            4'h2, 4'h3: begin
                computed_head = computed_head + (step_direction ? 1 : -1);
                if (mmio_wdata[4])
                    computed_track = computed_track + (step_direction ? 1 : -1);
            end
            4'h4, 4'h5: begin
                computed_head = computed_head + 1;
                if (mmio_wdata[4]) computed_track = computed_track + 1;
                computed_direction = 1'b1;
            end
            4'h6, 4'h7: begin
                computed_head = computed_head - 1;
                if (mmio_wdata[4]) computed_track = computed_track - 1;
                computed_direction = 1'b0;
            end
            default: ;
        endcase
        if (computed_head < 0) computed_head = 0;
        if (computed_head > 255) computed_head = 255;
        if (computed_track < 0) computed_track = 0;
        if (computed_track > 255) computed_track = 255;
    end
    always @(posedge clk) begin
        if (reset) begin
            state <= IDLE;
            dma_mode <= 9'd0;
            dma_base <= 24'd0;
            dma_cursor <= 24'd0;
            sector_count <= 8'd0;
            track_reg <= 8'd0;
            sector_reg <= 8'd1;
            data_reg <= 8'd0;
            head_track <= 8'd0;
            step_direction <= 1'b0;
            type_i <= 1'b1;
            motor_on <= 1'b0;
            record_error <= 1'b0;
            lost_data <= 1'b0;
            dma_error <= 1'b0;
            drq <= 1'b0;
            irq <= 1'b0;
            multi_sector <= 1'b0;
            have_high_byte <= 1'b0;
            writing <= 1'b0;
            media_cursor <= 20'd0;
            word_buffer <= 16'd0;
            sector_word <= 8'd0;
            command_delay <= '0;
            new_track <= 8'd0;
            new_head <= 8'd0;
            new_direction <= 1'b0;
            seek_error <= 1'b0;
            immediate_irq <= 1'b0;
            force_on_index <= 1'b0;
            index_count <= '0;
            idle_revolutions <= 4'd0;
            mmio_rdata <= 16'hffff;
            mmio_ack <= 1'b0;
        end else begin
            // A sector image has no index metadata; a 300 RPM virtual spindle
            // provides the Type I IP bit, D4 IRQ and idle motor shutdown.
            if (!motor_on || !media_ready || !drive_a) index_count <= '0;
            else if (index_pulse) index_count <= '0;
            else index_count <= index_count + 1'b1;
            if (index_pulse && force_on_index) irq <= 1'b1;
            if (index_pulse && !busy) begin
                if (idle_revolutions == 4'd8) motor_on <= 1'b0;
                else idle_revolutions <= idle_revolutions + 1'b1;
            end
            // Backend progress is independent of CPU register selection.
            case (state)
                IDLE: ;
                TYPE_I_WAIT: begin
                    if (command_delay != 0) command_delay <= command_delay - 1'b1;
                    else begin
                        track_reg <= new_track;
                        head_track <= new_head;
                        step_direction <= new_direction;
                        record_error <= seek_error;
                        state <= IDLE;
                        irq <= 1'b1;
                    end
                end
                MEDIA_WAIT: begin
                    if (!media_ready || !drive_a) begin
                        state <= IDLE;
                        record_error <= 1'b1;
                        drq <= 1'b0;
                        irq <= 1'b1;
                    end else if (media_valid) begin
                        data_reg <= media_data;
                        media_cursor <= media_cursor + 1'b1;
                        drq <= 1'b1;
                        if (!have_high_byte) begin
                            word_buffer[15:8] <= media_data;
                            have_high_byte <= 1'b1;
                            state <= MEDIA_GAP;
                        end else begin
                            word_buffer[7:0] <= media_data;
                            have_high_byte <= 1'b0;
                            state <= DMA_WAIT;
                        end
                    end
                end
                MEDIA_GAP: begin
                    drq <= 1'b0;
                    state <= MEDIA_WAIT;
                end
                DMA_WAIT: begin
                    if (!media_ready || !drive_a) begin
                        state <= IDLE;
                        record_error <= 1'b1;
                        drq <= 1'b0;
                        irq <= 1'b1;
                    end else if (!dma_address_valid) begin
                        state <= IDLE;
                        dma_error <= 1'b1;
                        lost_data <= 1'b1;
                        drq <= 1'b0;
                        irq <= 1'b1;
                    end else if (dma_ready) begin
                        dma_cursor <= dma_cursor + 24'd2;
                        dma_base <= dma_cursor + 24'd2;
                        drq <= 1'b0;
                        state <= DMA_GAP;
                        if (sector_word == 8'hff) begin
                            sector_count <= sector_count - 1'b1;
                            sector_word <= 8'd0;
                            if (!multi_sector) begin
                                state <= IDLE;
                                irq <= 1'b1;
                            end else begin
                                sector_reg <= sector_reg + 1'b1;
                                if (sector_reg == 8'd9) begin
                                    record_error <= 1'b1;
                                    state <= IDLE;
                                    irq <= 1'b1;
                                end else if (sector_count == 8'd1) begin
                                    // The FDC keeps reading after DMA is full.
                                    // Software must terminate with D0/D8. No
                                    // additional RAM write may escape the count.
                                    state <= NO_DMA;
                                    drq <= 1'b1;
                                end
                            end
                        end else sector_word <= sector_word + 1'b1;
                    end
                end
                DMA_GAP: state <= writing ? WRITE_WAIT : MEDIA_WAIT;
                WRITE_WAIT: begin
                    if (!media_ready || !drive_a) begin
                        state <= IDLE; record_error <= 1; drq <= 0; irq <= 1;
                    end else if (writer_ready) begin
                        drq <= 0;
                        if (writer_error) begin
                            state <= IDLE; dma_error <= 1; lost_data <= 1; irq <= 1;
                        end else begin
                            dma_cursor <= dma_cursor + 24'd512;
                            dma_base <= dma_cursor + 24'd512;
                            sector_count <= sector_count - 1'b1;
                            if (!multi_sector) begin state <= IDLE; irq <= 1; end
                            else begin
                                sector_reg <= sector_reg + 1'b1;
                                media_cursor <= media_cursor + 20'd512;
                                if (sector_reg == 9) begin
                                    record_error <= 1; state <= IDLE; irq <= 1;
                                end else if (sector_count == 1) begin
                                    state <= NO_DMA; drq <= 1;
                                end else state <= DMA_GAP;
                            end
                        end
                    end
                end
                NO_DMA: begin
                    if (!media_ready || !drive_a) begin
                        state <= IDLE;
                        record_error <= 1'b1;
                        drq <= 1'b0;
                        irq <= 1'b1;
                    end
                end
                default: state <= IDLE;
            endcase

            if (!mmio_req) mmio_ack <= 1'b0;
            else if (accept_mmio) begin
                mmio_ack <= 1'b1;
                mmio_rdata <= read_value;
                if (!mmio_write && word_access && mmio_addr[3:1] == 3'd2 &&
                    !dma_mode[4] && !dma_mode[3] && dma_mode[2:1] == 2'd0 &&
                    !immediate_irq)
                    irq <= 1'b0;
                if (mmio_write) begin
                    case (mmio_addr[3:1])
                        3'd3: if (word_access) begin
                            dma_mode <= mmio_wdata[8:0];
                            if (!busy && dma_mode[8] != mmio_wdata[8]) begin
                                dma_error <= 1'b0;
                                sector_count <= 8'd0;
                                drq <= 1'b0;
                            end
                        end
                        3'd4: if (mmio_byte_enable[0] && !busy)
                            dma_base[23:16] <= mmio_wdata[7:0];
                        3'd5: if (mmio_byte_enable[0] && !busy)
                            dma_base[15:8] <= mmio_wdata[7:0];
                        3'd6: if (mmio_byte_enable[0] && !busy)
                            dma_base[7:0] <= {mmio_wdata[7:1], 1'b0};
                        3'd2: if (word_access) begin
                            if (dma_mode[4]) begin
                                if (!busy) sector_count <= mmio_wdata[7:0];
                            end else if (!dma_mode[3] && !busy) begin
                                case (dma_mode[2:1])
                                    2'd1: track_reg <= mmio_wdata[7:0];
                                    2'd2: sector_reg <= mmio_wdata[7:0];
                                    2'd3: data_reg <= mmio_wdata[7:0];
                                    default: ;
                                endcase
                            end
                        end
                        default: ;
                    endcase
                end
            end

            // A force command cancels future requests. A backend handshake on
            // this same edge is already accepted and still advances the cursor.
            if (force_command) begin
                state <= IDLE;
                if (!busy) begin
                    type_i <= 1'b1;
                    record_error <= 1'b0;
                    lost_data <= 1'b0;
                    drq <= 1'b0;
                end
                force_on_index <= mmio_wdata[2];
                if (mmio_wdata[3:0] == 4'd0) begin
                    immediate_irq <= 1'b0;
                    irq <= 1'b0;
                end else if (mmio_wdata[3]) begin
                    immediate_irq <= 1'b1;
                    irq <= 1'b1;
                end
                have_high_byte <= 1'b0;
            end else if (write_command && !busy) begin
                if (!immediate_irq) irq <= 1'b0;
                force_on_index <= 1'b0;
                drq <= 1'b0;
                record_error <= 1'b0;
                lost_data <= 1'b0;
                motor_on <= 1'b1;
                idle_revolutions <= 4'd0;
                have_high_byte <= 1'b0;
                type_i <= !mmio_wdata[7];
                if (!mmio_wdata[7]) begin
                    new_direction <= computed_direction;
                    new_head <= 8'(computed_head);
                    new_track <= 8'(computed_track);
                    seek_error <= !drive_a ||
                        (mmio_wdata[2] && (!media_ready || computed_head >= 80 ||
                                          computed_head != computed_track));
                    command_delay <= DELAY_BITS'(COMMAND_DELAY_CYCLES);
                    state <= TYPE_I_WAIT;
                end else if (mmio_wdata[7:5] == 3'b100) begin
                    multi_sector <= mmio_wdata[4];
                    writing <= 0;
                    dma_cursor <= dma_base;
                    sector_word <= 8'd0;
                    if (!drive_a || !media_ready || track_reg >= 8'd80 ||
                        track_reg != head_track || sector_reg < 8'd1 || sector_reg > 8'd9) begin
                        record_error <= 1'b1;
                        irq <= 1'b1;
                    end else if (!dma_mode[7] || dma_mode[8] || dma_mode[6] || sector_count == 0) begin
                        lost_data <= 1'b1;
                        dma_error <= 1'b1;
                        irq <= 1'b1;
                    end else begin
                        media_cursor <= sector_offset;
                        state <= MEDIA_WAIT;
                    end
                end else if (mmio_wdata[7:5] == 3'b101 && ENABLE_WRITE) begin
                    multi_sector <= mmio_wdata[4]; writing <= 1;
                    dma_cursor <= dma_base; media_cursor <= sector_offset;
                    if (!drive_a || !media_ready || track_reg >= 80 ||
                        track_reg != head_track || sector_reg < 1 || sector_reg > 9 || mmio_wdata[0]) begin
                        record_error <= 1; irq <= 1;
                    end else if (!dma_mode[7] || !dma_mode[8] || dma_mode[6] || sector_count == 0) begin
                        lost_data <= 1; dma_error <= 1; irq <= 1;
                    end else begin state <= WRITE_WAIT; drq <= 1; end
                end else if (mmio_wdata[7:5] == 3'b101 || mmio_wdata[7:4] == 4'hf) begin
                    // Legacy disks are write protected; raw .st has no track
                    // stream or deleted-sector metadata even in writable mode.
                    if (ENABLE_WRITE) record_error <= 1;
                    irq <= 1'b1;
                end else begin
                    record_error <= 1'b1;
                    irq <= 1'b1;
                end
            end
        end
    end
    initial begin
        if (COMMAND_DELAY_CYCLES < 1)
            $fatal(1, "st_floppy COMMAND_DELAY_CYCLES must be positive");
        if (INDEX_PERIOD_CYCLES < 1)
            $fatal(1, "st_floppy INDEX_PERIOD_CYCLES must be positive");
        if (SYSTEM_CLOCK_HZ < 1)
            $fatal(1, "st_floppy SYSTEM_CLOCK_HZ must be positive");
    end
endmodule
