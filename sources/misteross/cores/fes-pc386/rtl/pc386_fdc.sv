// SPDX-License-Identifier: MIT
`include "fes_application.vh"

// Floppy disk controller (docs/design/pc386-removable-media.md, "Floppy"):
// the 82077AA subset that DOS, the BIOS, Windows 9x and Linux use, in PC-AT
// mode, with two drives whose disk images sit whole in the DDR window. It is
// written from the 82077AA datasheet and Bochs's iodev/floppy.cc, the replay
// reference (sim/fdc_replay_tb.cpp); sim/fdc_divergences.toml lists every
// place the two differ, with its deciding case.
//
//   3F2h  DOR: drive select (bit 0), /RESET, DMA and INT gate, motors A and B
//   3F4h  read MSR; write DSR (bit 7 software reset, bits 1-0 data rate)
//   3F5h  FIFO: command bytes, result bytes, non-DMA data
//   3F7h  read DIR: bit 7 the selected drive's disk change while its motor
//         bit is set, bits 6-0 float (7Fh); write CCR (data rate)
//
// The AT core decodes those four ports; 3F0h, 3F1h and 3F3h read FFh as on
// an AT, and 3F6h is the ATA device control register. Bit 1 of a drive
// select is ignored, as on a two-drive AT adapter (and in Bochs): drives 2
// and 3 alias 0 and 1. INT (irq6) and DRQ (DMA channel 2) are gated by DOR
// bit 3, as in the 82077AA's AT mode.
//
// Commands are decoded on the whole byte, so a command takes only the
// modifier bits (MT, MFM, SK) the 82077AA gives it: Specify, Sense Drive
// Status, Recalibrate, Sense Interrupt Status, Seek, Relative Seek, Version
// (90h), Configure, Lock, Dumpreg, Perpendicular Mode, Read Data, Read
// Deleted Data, Write Data, Write Deleted Data, Read ID and Format Track.
// Anything else is invalid: one result byte, 80h. Every image is MFM, so a
// command with MFM clear finds no address mark (ST0 IC=01, ST1 MA). Images
// carry no deleted marks: Read Deleted Data reads the data, and without SK
// reports ST2 CM; Write Deleted Data writes data.
//
// Drives. Each has a cylinder, head and sector, a disk-change latch (set when
// a disk leaves or arrives, cleared by a step with a disk present; machine
// reset clears it) and a spin-up timer (400 ms for 5.25", 500 ms for 3.5")
// that runs while its DOR motor bit is set. A command that touches the disk
// waits until its drive is ready: fitted, motor on, spun up and a disk
// present. Without a disk it waits for one, as a drive turns no index pulse.
// A step takes Specify's SRT at the data rate, and a seek that moves settles
// for SETTLE_US. The image's cylinder is the drive's: a 40-track disk in an
// 80-track drive is not double-stepped (the locked BIOS never double-steps).
//
// Sector path. A read fetches the whole sector from the image in one 64-beat
// burst on port 2 into a 512-byte buffer, then offers its bytes one per byte
// time of the medium: 32 us at 250 kbit/s (up to 9 sectors a track), 16 us
// at 500 kbit/s, 8 us at 1 Mbit/s (36). A byte waits for its DACK, or with
// Specify's ND set for its 3F5h access, so there is no overrun. A write
// collects the sector in the buffer, then stores it in 64 single beats, the
// bytes after a TC as 00h, so a torn sector never reaches the image. A store
// runs to its end whatever the controller does meanwhile, a software reset
// included; busy covers it and every read beat still owed, and the shell
// resets the machine only while busy is low. frozen holds back the next
// sector store.
//
// Times count microseconds from a rational enable of clk (US_N/US_D, the AT
// core's 1 MHz), so the governor's presets leave floppy speed alone.
module pc386_fdc #(
    parameter integer US_N = 1, US_D = 100,
    parameter integer SETTLE_US = 15000,
    parameter integer RESET_US = 10
) (
    input wire clk, reset,
    // The AT core's device cycle at its access clock: 3F2h, 3F4h, 3F5h or 3F7h.
    input wire access, write,
    input wire [2:0] address,
    input wire [7:0] wdata,
    output reg [7:0] rdata,
    output wire irq6,
    // DMA channel 2: a DACK takes or gives one byte.
    output wire drq,
    input wire dack_valid, dack_tc,
    input wire [7:0] dack_wdata,
    output reg dack_done,
    output reg [7:0] dack_rdata,
    // Drives 0 (A:) and 1 (B:): the CMOS 10h type (0 none, 1 360K, 2 1.2M,
    // 3 720K, 4 1.44M, 5 2.88M) and the disk's geometry.
    input wire [1:0][2:0] drive_type,
    input wire [1:0] present, write_protect, two_sided,
    input wire [1:0][6:0] cylinders,
    input wire [1:0][5:0] spt,
    // The image port: 64-beat reads and single-beat writes on port 2.
    output wire [28:0] aux_address,
    output wire [7:0] aux_burstcount,
    output wire aux_read, aux_write,
    output wire [63:0] aux_writedata,
    output wire [7:0] aux_byteenable,
    input wire aux_waitrequest, aux_readdatavalid,
    input wire [63:0] aux_readdata,
    input wire frozen,
    output wire busy
);
    localparam [31:0] IMAGE0 = `FES_APPLICATION_HPS_DDR_WINDOW_BASE + 32'h01800000;
    localparam [28:0] IMAGE0_WORD = IMAGE0[31:3];
    localparam [28:0] DRIVE_WORDS = 29'h80000;   // 4 MiB per drive

    localparam [4:0] C_READ = 5'h06, C_READ_DELETED = 5'h0c, C_WRITE = 5'h05, C_WRITE_DELETED = 5'h09,
                     C_READ_ID = 5'h0a, C_FORMAT = 5'h0d;
    // Command length in bytes, 0 for an invalid byte.
    function automatic [3:0] command_size(input [7:0] c);
        case (c)
            8'h03, 8'h0f, 8'h8f, 8'hcf: command_size = 4'd3;
            8'h04, 8'h07, 8'h12, 8'h0a, 8'h4a: command_size = 4'd2;
            8'h08, 8'h0e, 8'h10, 8'h14, 8'h94: command_size = 4'd1;
            8'h13: command_size = 4'd4;
            8'h0d, 8'h4d: command_size = 4'd6;
            default:
                if (c[4:0] == C_READ || c[4:0] == C_READ_DELETED) command_size = 4'd9;
                else if ((c[4:0] == C_WRITE || c[4:0] == C_WRITE_DELETED) && !c[5]) command_size = 4'd9;
                else command_size = 4'd0;
        endcase
    endfunction

    // ---- Registers ----------------------------------------------------------
    localparam [2:0] P_COMMAND = 3'd0, P_EXEC = 3'd1, P_RESULT = 3'd2, P_RESET = 3'd3, P_RESETTING = 3'd4;
    localparam [2:0] R_ST0 = 3'd0, R_ST3 = 3'd1, R_SENSE = 3'd2, R_RW = 3'd3, R_FORMAT = 3'd4, R_DUMP = 3'd5,
                     R_VERSION = 3'd6, R_LOCK = 3'd7;
    localparam [3:0] X_DISPATCH = 4'd0, X_STEP = 4'd1, X_SETTLE = 4'd2, X_READY = 4'd3, X_READ_ID = 4'd4,
                     X_FETCH = 4'd5, X_FETCH_WAIT = 4'd6, X_BYTE_WAIT = 4'd7, X_BYTE = 4'd8, X_STORE = 4'd9,
                     X_STORE_GO = 4'd10, X_STORE_WAIT = 4'd11;
    reg [7:0] dor, msr;
    reg [1:0] rate;                                // DSR/CCR: 500, 300, 250, 1000 kbit/s
    reg nd, lock;
    reg [3:0] srt, hut;
    reg [6:0] hlt;
    reg [7:0] config_byte, pretrk, perp;
    reg [1:0][7:0] cyl, sector, eot;
    reg [1:0] head, changed, present_q, spun;
    reg [1:0][18:0] spin_us;
    reg [7:0] st0, st1, st2, st3, last_result;
    reg pending_irq, pio_irq;
    reg [2:0] reset_sensei;
    reg [2:0] phase, result_kind;
    reg [3:0] ex;
    reg [8:0][7:0] command;
    reg [3:0] command_index, command_size_r, result_index, result_size;
    reg [23:0] wait_us;
    reg recalibrating, moved, writing, tc_seen, drq_r;
    reg [7:0] steps;
    reg [7:0] format_cyl, format_left;
    reg [1:0] id_byte;
    reg format_cyl_set, format_bad;
    reg [8:0] index;                               // byte within the sector

    wire d = dor[0];                               // the selected drive
    wire [7:0] op = command[0];
    wire [4:0] base = op[4:0];
    wire mt = op[7], mfm = op[6], sk = op[5];
    wire formatting = base == C_FORMAT;
    wire pio = nd;
    wire [7:0] tracks = {1'b0, cylinders[d]};
    wire [7:0] sectors_per_track = {2'b00, spt[d]};
    wire ready = drive_type[d] != 3'd0 && present[d] && dor[{2'b10, d}] && spun[d];
    // Microseconds per byte of the medium in the selected drive.
    wire [5:0] byte_us = spt[d] >= 6'd30 ? 6'd8 : spt[d] >= 6'd15 ? 6'd16 : 6'd32;
    wire [23:0] byte_wait = {18'd0, byte_us};

    // ---- Time ---------------------------------------------------------------
    wire us;
    pc386_rate #(.N(US_N), .D(US_D)) us_rate (.clk(clk), .reset(reset), .tick(us));
    wire waited = wait_us == 24'd0;
    // One step at the data rate: (16 - SRT) x 1, 1.67, 2 or 0.5 ms.
    wire [10:0] step_unit = rate == 2'd0 ? 11'd1000 : rate == 2'd1 ? 11'd1667 : rate == 2'd2 ? 11'd2000 : 11'd500;
    wire [15:0] step_us = 16'(step_unit * {7'd0, 5'd16 - {1'b0, srt}});

    // Spin-up per drive, while its motor bit is set.
    integer s;
    always @(posedge clk)
        for (s = 0; s < 2; s = s + 1)
            if (reset || !dor[4 + s]) begin
                spun[s] <= 0; spin_us[s] <= 0;
            end else if (us && !spun[s]) begin
                spin_us[s] <= spin_us[s] + 19'd1;
                if (spin_us[s] == (drive_type[s] <= 3'd2 ? 19'd399999 : 19'd499999)) spun[s] <= 1;
            end

    // ---- Seeks: the target and the steps to it --------------------------
    wire command_drive = command[1][0];
    wire [7:0] from_cyl = cyl[command_drive];
    wire [8:0] seek_target = op == 8'h07 ? 9'd0 : op == 8'h0f ? {1'b0, command[2]} :
                             op[6] ? {1'b0, from_cyl} + {1'b0, command[2]} : {1'b0, from_cyl} - {1'b0, command[2]};
    wire [7:0] seek_distance = op[7] ? command[2] :
                               seek_target[7:0] > from_cyl ? seek_target[7:0] - from_cyl : from_cyl - seek_target[7:0];

    // ---- Sector buffer: 64 words of 8 bytes -----------------------------
    reg buffer_write;
    reg [5:0] buffer_waddress, buffer_raddress;
    reg [63:0] buffer_wdata;
    reg [7:0] buffer_wbyteenable;
    wire [63:0] buffer_word;
    pc386_ram #(.WIDTH(64), .DEPTH(64), .BYTES(8)) buffer (
        .clk(clk), .write(buffer_write), .waddress(buffer_waddress), .wdata(buffer_wdata),
        .wbyteenable(buffer_wbyteenable), .read(1'b1), .raddress(buffer_raddress), .rdata(buffer_word)
    );
    wire [7:0] buffer_byte = buffer_word[8 * index[2:0] +: 8];

    // ---- Image port ---------------------------------------------------------
    // A fetch is one 64-beat read; every beat still owed is counted so that
    // a burst cut short by a software reset drains before the next. The
    // store engine writes one sector, from the buffer or a fill byte, and
    // runs to its end.
    reg [12:0] target_lba;
    reg target_drive;
    wire [28:0] sector_word = IMAGE0_WORD + (target_drive ? DRIVE_WORDS : 29'd0) + {10'd0, target_lba, 6'd0};
    reg fetching, storing, store_loaded, store_fill;
    reg [6:0] fetch_beats;
    reg [7:0] owed;
    reg [5:0] store_word;
    reg [9:0] store_bytes;                         // bytes kept; later ones are written 00h
    reg [7:0] fill_byte;
    reg [63:0] store_data;
    integer lane;
    always @* begin
        for (lane = 0; lane < 8; lane = lane + 1)
            store_data[8 * lane +: 8] = store_fill ? fill_byte :
                {1'b0, store_word, 3'(lane)} < store_bytes ? buffer_word[8 * lane +: 8] : 8'h00;
    end
    assign aux_address = sector_word + (storing ? {23'd0, store_word} : 29'd0);
    assign aux_burstcount = storing ? 8'd1 : 8'd64;
    assign aux_read = fetching;
    assign aux_write = storing && store_loaded;
    assign aux_writedata = store_data;
    assign aux_byteenable = 8'hff;
    assign busy = fetching || storing || owed != 8'd0;
    // The store engine reads its word, and the next once the port takes it;
    // otherwise the guest's byte.
    always @* buffer_raddress = !storing ? index[8:3] : store_word + {5'd0, store_loaded && !aux_waitrequest};

    // ---- The next sector (Bochs's increment_sector) and end of track ----
    wire [7:0] next_sector = sector[d] + 8'd1;
    wire wrap = next_sector > eot[d] || next_sector > sectors_per_track;
    wire cylinder_step = wrap && (!mt || head[d]);
    wire [7:0] stepped_cyl = cyl[d] + 8'd1;
    wire end_of_track = sector[d] == eot[d] && (!mt || head[d] == two_sided[d]);
    // LBA of the selected drive's current sector, and of a format ID's.
    wire [7:0] lba_cyl = formatting ? format_cyl : cyl[d];
    wire [8:0] lba_track = ({lba_cyl, 1'b0} >> !two_sided[d]) + {8'd0, head[d] & two_sided[d]};
    wire [12:0] lba = 13'({4'd0, lba_track} * {5'd0, sectors_per_track}) + 13'(sector[d]) - 13'd1;

    // A byte offered to the guest, by DMA or through 3F5h.
    assign drq = drq_r && dor[3];
    assign irq6 = (pending_irq || pio_irq) && dor[3];
    wire dack = dack_valid && !dack_done;
    wire pio_take = access && address == 3'd5 && phase == P_EXEC && ex == X_BYTE && pio && write == writing;
    wire byte_taken = (dack && drq_r) || pio_take;
    wire [7:0] taken_byte = pio_take ? wdata : dack_wdata;
    // Non-DMA has no TC (the AT's TC comes from the 8237): such a transfer
    // ends at EOT with ST1 EN, as on the 82077AA.
    wire taken_tc = !pio_take && dack_tc;
    wire reset_write = access && write && (address == 3'd2 && dor[2] != wdata[2] || address == 3'd4 && wdata[7]);

    // ---- Result bytes ---------------------------------------------------------
    reg [7:0] result_byte;
    always @* begin
        case (result_kind)
            R_ST3: result_byte = st3;
            R_SENSE: result_byte = result_index == 4'd0 ? st0 : cyl[d];
            R_RW, R_FORMAT:
                case (result_index)
                    4'd0: result_byte = st0;
                    4'd1: result_byte = st1;
                    4'd2: result_byte = st2;
                    4'd3: result_byte = result_kind == R_FORMAT ? 8'h00 : cyl[d];
                    4'd4: result_byte = result_kind == R_FORMAT ? 8'h00 : {7'd0, head[d]};
                    4'd5: result_byte = result_kind == R_FORMAT ? 8'h00 : sector[d];
                    default: result_byte = result_kind == R_FORMAT ? 8'h00 : 8'h02;
                endcase
            R_DUMP:
                case (result_index)
                    4'd0: result_byte = cyl[0];
                    4'd1: result_byte = cyl[1];
                    4'd2, 4'd3: result_byte = 8'h00;
                    4'd4: result_byte = {srt, hut};
                    4'd5: result_byte = {hlt, nd};
                    4'd6: result_byte = eot[d] != 8'd0 ? eot[d] : present[d] ? sectors_per_track : 8'd0;
                    4'd7: result_byte = {lock, perp[6:0]};
                    4'd8: result_byte = config_byte;
                    default: result_byte = pretrk;
                endcase
            R_VERSION: result_byte = 8'h90;
            R_LOCK: result_byte = {3'd0, lock, 4'd0};
            default: result_byte = st0;
        endcase
    end

    always @* begin
        case (address)
            3'd2: rdata = dor;
            3'd4: rdata = msr;
            3'd5: rdata = phase == P_RESULT ? result_byte : pio_take ? buffer_byte : last_result;
            3'd7: rdata = {changed[d] && dor[{2'b10, d}], 7'h7f};
            default: rdata = 8'hff;
        endcase
    end

    // ---- Phases -------------------------------------------------------------
    // The result phase; R/W and Format raise the interrupt (Bochs's
    // enter_result_phase). Every MSR update follows Bochs's, except NDM,
    // which is set only in a non-DMA execution phase, as on the 82077AA.
    task automatic enter_result(input [2:0] kind);
        begin
            phase <= P_RESULT;
            msr <= {4'b1101, msr[3:0]};
            result_index <= 0;
            result_kind <= kind;
            drq_r <= 0;
            pio_irq <= 0;
            case (kind)
                R_SENSE: result_size <= 4'd2;
                R_RW, R_FORMAT: begin result_size <= 4'd7; pending_irq <= 1; reset_sensei <= 0; end
                R_DUMP: result_size <= 4'd10;
                default: result_size <= 4'd1;
            endcase
        end
    endtask

    task automatic enter_idle;
        begin
            phase <= P_COMMAND;
            msr <= {4'b1000, msr[3:0]};
            command_index <= 0;
            result_size <= 0;
            drq_r <= 0;
            pio_irq <= 0;
        end
    endtask

    // The end of a software reset: each of the four drive positions answers
    // a Sense Interrupt Status (C0h-C3h).
    task automatic soft_reset;
        begin
            phase <= P_COMMAND; msr <= 8'h80;
            command_index <= 0; result_size <= 0;
            st0 <= 8'hc0; st1 <= 0; st2 <= 0; st3 <= 0;
            pending_irq <= 1; reset_sensei <= 3'd4; pio_irq <= 0; drq_r <= 0;
            nd <= 0; perp <= 0;
            if (!lock) begin config_byte <= 0; pretrk <= 0; end
            cyl <= 0; head <= 0; sector <= {8'd1, 8'd1}; eot <= 0;
        end
    endtask

    task automatic fail(input [2:0] kind, input h, input [7:0] status1);
        begin
            st0 <= {5'b01000, h, 1'b0, d}; st1 <= status1; st2 <= 0;
            enter_result(kind);
        end
    endtask

    // Move to the next sector.
    task automatic step_sector;
        begin
            sector[d] <= wrap ? 8'd1 : next_sector;
            if (wrap && mt) head[d] <= !head[d];
            if (cylinder_step) begin
                cyl[d] <= stepped_cyl >= tracks ? tracks : stepped_cyl;
                if (present[d]) changed[d] <= 0;
            end
        end
    endtask

    // The end of a read or write: past EOT without TC is abnormal (ST1 EN);
    // otherwise normal, at the next sector.
    task automatic finish_transfer(input error_eot);
        begin
            if (error_eot) begin
                st0 <= {5'b01000, head[d], 1'b0, d}; st1 <= 8'h80; st2 <= 0;
            end else begin
                step_sector();
                st0 <= {5'd0, wrap && mt ? !head[d] : head[d], 1'b0, d}; st1 <= 0;
                st2 <= base == C_READ_DELETED && !sk ? 8'h40 : 8'h00;
            end
            enter_result(R_RW);
        end
    endtask

    integer k;
    always @(posedge clk) begin
        dack_done <= 0;
        buffer_write <= 0;
        if (us && !waited) wait_us <= wait_us - 24'd1;
        if (aux_readdatavalid && owed != 8'd0) owed <= owed - 8'd1;
        if (reset) begin
            dor <= 8'h0c; msr <= 8'h80; rate <= 2'd2; nd <= 0; lock <= 0;
            srt <= 0; hut <= 0; hlt <= 0; config_byte <= 0; pretrk <= 0; perp <= 0;
            cyl <= 0; head <= 0; sector <= {8'd1, 8'd1}; eot <= 0;
            changed <= 0; present_q <= present;
            st0 <= 0; st1 <= 0; st2 <= 0; st3 <= 0; last_result <= 0;
            pending_irq <= 0; pio_irq <= 0; reset_sensei <= 0;
            phase <= P_COMMAND; result_kind <= R_ST0; ex <= X_DISPATCH;
            command <= 0; command_index <= 0; command_size_r <= 0; result_index <= 0; result_size <= 0;
            wait_us <= 0; recalibrating <= 0; moved <= 0; writing <= 0; tc_seen <= 0; drq_r <= 0;
            steps <= 0; format_cyl <= 0; format_left <= 0; id_byte <= 0; format_cyl_set <= 0; format_bad <= 0;
            index <= 0; dack_rdata <= 8'hff;
            target_lba <= 0; target_drive <= 0;
            fetching <= 0; storing <= 0; store_loaded <= 0; store_fill <= 0;
            fetch_beats <= 0; owed <= 0; store_word <= 0; store_bytes <= 0; fill_byte <= 0;
            buffer_waddress <= 0; buffer_wdata <= 0; buffer_wbyteenable <= 0;
        end else begin
            // A disk leaving or arriving sets its drive's change latch.
            present_q <= present;
            for (k = 0; k < 2; k = k + 1) if (present[k] != present_q[k]) changed[k] <= 1;

            // ---- The store engine and the fetch burst
            if (storing) begin
                if (!store_loaded) store_loaded <= 1;
                else if (!aux_waitrequest) begin
                    store_word <= store_word + 6'd1;
                    if (store_word == 6'd63) storing <= 0;
                end
            end
            if (fetching && !aux_waitrequest) begin
                fetching <= 0;
                owed <= owed + 8'd64 - {7'd0, aux_readdatavalid && owed != 8'd0};
            end
            if (phase == P_EXEC && ex == X_FETCH_WAIT && aux_readdatavalid && owed != 8'd0) begin
                buffer_write <= 1;
                buffer_waddress <= fetch_beats[5:0];
                buffer_wdata <= aux_readdata;
                buffer_wbyteenable <= 8'hff;
                fetch_beats <= fetch_beats + 7'd1;
            end

            // A DACK with no byte offered completes with nothing (FFh).
            if (dack && !drq_r) begin dack_done <= 1; dack_rdata <= 8'hff; end

            // ---- Port writes
            if (access && write) case (address)
                3'd2: begin
                    if (dor[2] && !wdata[2]) begin
                        // Into reset: Bochs's MSR shows busy until the release.
                        phase <= P_RESET; msr <= 8'h10; drq_r <= 0; pio_irq <= 0;
                    end else if (!dor[2] && wdata[2]) begin
                        phase <= P_RESETTING; wait_us <= 24'(RESET_US);
                    end
                    dor <= wdata;
                end
                3'd4: begin
                    rate <= wdata[1:0];
                    if (wdata[7]) begin
                        msr <= 8'h00; dor <= 8'h0c; drq_r <= 0; pio_irq <= 0;
                        phase <= P_RESETTING; wait_us <= 24'(RESET_US);
                    end
                end
                3'd7: rate <= wdata[1:0];
                3'd5: if (phase == P_COMMAND) begin
                    command[command_index] <= wdata;
                    if (command_index == 4'd0) begin
                        msr <= {4'b1001, msr[3:0]};
                        if (command_size(wdata) == 4'd0) begin
                            st0 <= 8'h80;
                            enter_result(R_ST0);
                        end else begin
                            if (wdata != 8'h08) begin st0 <= 0; st1 <= 0; st2 <= 0; end
                            command_size_r <= command_size(wdata);
                            if (command_size(wdata) == 4'd1) begin phase <= P_EXEC; ex <= X_DISPATCH; end
                            else command_index <= 4'd1;
                        end
                    end else if (command_index + 4'd1 == command_size_r) begin
                        phase <= P_EXEC; ex <= X_DISPATCH; command_index <= 0;
                    end else command_index <= command_index + 4'd1;
                end
                default: ;
            endcase

            // ---- Result reads
            if (access && !write && address == 3'd5 && !pio_take) begin
                if (phase == P_RESULT) begin
                    last_result <= result_byte;
                    pending_irq <= 0;
                    result_index <= result_index + 4'd1;
                    if (result_index + 4'd1 >= result_size) begin
                        enter_idle();
                        msr <= 8'h80;
                    end else msr <= {msr[7:4], 4'd0};
                end else if (phase == P_COMMAND && command_index == 4'd0) begin
                    // No result to read: Bochs answers the last result byte
                    // and enters a result phase of one 80h.
                    st0 <= 8'h80;
                    enter_result(R_ST0);
                    msr <= 8'hd0;
                end
            end

            if (phase == P_RESETTING && waited && !reset_write) soft_reset();

            // ---- Execution
            if (phase == P_EXEC && !reset_write) case (ex)
                X_DISPATCH: begin
                    writing <= 0; tc_seen <= 0; index <= 0;
                    case (op)
                        8'h03: begin
                            srt <= command[1][7:4]; hut <= command[1][3:0];
                            hlt <= command[2][7:1]; nd <= command[2][0];
                            enter_idle();
                        end
                        8'h04: begin
                            head[command_drive] <= command[1][2];
                            st3 <= {1'b0, present[command_drive] && write_protect[command_drive], 1'b1,
                                    drive_type[command_drive] != 3'd0 && from_cyl == 8'd0, 1'b1,
                                    command[1][2], 1'b0, command_drive};
                            enter_result(R_ST3);
                        end
                        8'h07, 8'h0f, 8'h8f, 8'hcf: begin
                            // Recalibrate and the seeks: the cylinder moves now
                            // and the steps take their time.
                            dor[0] <= command_drive;
                            recalibrating <= op == 8'h07;
                            if (op != 8'h07) begin
                                head[command_drive] <= command[1][2];
                                // Past the last cylinder is still success;
                                // below 0 is abnormal with EC (Bochs).
                                st0 <= op == 8'h8f && seek_target[8] ? {7'b0111000, command_drive} :
                                                                       {7'b0010000, command_drive};
                            end
                            moved <= seek_distance != 8'd0;
                            steps <= seek_distance == 8'd0 ? 8'd1 : seek_distance;
                            if (seek_distance != 8'd0 && present[command_drive]) changed[command_drive] <= 0;
                            cyl[command_drive] <= seek_target[7:0];
                            msr <= {6'd0, command_drive, !command_drive};
                            wait_us <= {8'd0, step_us};
                            ex <= X_STEP;
                        end
                        8'h08: begin
                            pending_irq <= 0;
                            if (reset_sensei != 3'd0) begin
                                st0 <= {st0[7:3], 1'b0, 2'(3'd4 - reset_sensei)};
                                reset_sensei <= reset_sensei - 3'd1;
                                enter_result(R_SENSE);
                            end else if (!pending_irq) begin
                                st0 <= 8'h80;
                                enter_result(R_ST0);
                            end else enter_result(R_SENSE);
                        end
                        8'h10: enter_result(R_VERSION);
                        8'h13: begin config_byte <= command[2]; pretrk <= command[3]; enter_idle(); end
                        8'h14, 8'h94: begin lock <= op[7]; enter_result(R_LOCK); end
                        8'h0e: enter_result(R_DUMP);
                        8'h12: begin perp <= command[1]; enter_idle(); end
                        default: begin
                            // Read ID, Format Track, and the reads and writes.
                            dor[0] <= command_drive;
                            msr <= 8'h10;
                            writing <= base == C_WRITE || base == C_WRITE_DELETED || formatting;
                            if (base == C_READ_ID || formatting) begin
                                head[command_drive] <= command[1][2];
                                ex <= X_READY;
                            end else if (command[3][0] != command[1][2]) begin
                                // The head in the ID differs from the drive's (Bochs, SF #439945).
                                st0 <= {5'b01000, head[command_drive], 1'b0, command_drive};
                                st1 <= 8'h04; st2 <= 0;
                                enter_result(R_RW);
                            end else ex <= X_READY;
                        end
                    endcase
                end
                X_STEP: if (waited) begin
                    if (steps <= 8'd1) begin
                        wait_us <= moved ? 24'(SETTLE_US) : 24'd0;
                        ex <= X_SETTLE;
                    end else begin
                        steps <= steps - 8'd1;
                        wait_us <= {8'd0, step_us};
                    end
                end
                X_SETTLE: if (waited) begin
                    if (recalibrating)
                        st0 <= {(drive_type[d] == 3'd0 || !dor[{2'b10, d}]) ? 4'b0111 : 4'b0010, 3'd0, d};
                    pending_irq <= 1; reset_sensei <= 0;
                    enter_idle();
                end
                X_READY: if (ready) begin
                    if (base == C_READ_ID) begin
                        if (!mfm || head[d] && !two_sided[d] || cyl[d] >= tracks) begin
                            st0 <= {5'b01000, head[d], 1'b0, d}; st1 <= 8'h01;
                        end else begin
                            st0 <= {5'd0, head[d], 1'b0, d};
                            sector[d] <= sector[d] >= sectors_per_track ? 8'd1 : sector[d] + 8'd1;
                        end
                        wait_us <= {9'd0, byte_us, 9'd0};   // a sector's time
                        ex <= X_READ_ID;
                    end else if (formatting) begin
                        if (!mfm) fail(R_FORMAT, head[d], 8'h01);
                        else if (command[2] != 8'd2) fail(R_FORMAT, head[d], 8'h04);
                        else if (write_protect[d]) fail(R_FORMAT, head[d], 8'h02);
                        else begin
                            format_left <= command[3] > sectors_per_track ? sectors_per_track : command[3];
                            fill_byte <= command[5];
                            format_cyl_set <= 0; format_bad <= 0; id_byte <= 0;
                            sector[d] <= 8'd1;
                            wait_us <= byte_wait;
                            ex <= X_BYTE_WAIT;
                        end
                    end else if (!mfm) fail(R_RW, command[3][0], 8'h01);
                    else if (command[5] != 8'd2 || command[2] >= tracks || command[4] == 8'd0 ||
                             command[4] > sectors_per_track || command[3][0] && !two_sided[d]) begin
                        cyl[d] <= command[2]; head[d] <= command[3][0]; sector[d] <= command[4];
                        fail(R_RW, command[3][0], 8'h04);
                    end else begin
                        if (command[2] != cyl[d] && present[d]) changed[d] <= 0;
                        cyl[d] <= command[2]; head[d] <= command[3][0]; sector[d] <= command[4];
                        eot[d] <= command[6] == 8'd0 ? sectors_per_track : command[6];
                        if (writing && write_protect[d]) fail(R_RW, command[3][0], 8'h02);
                        else if (writing) begin
                            wait_us <= byte_wait;
                            ex <= X_BYTE_WAIT;
                        end else ex <= X_FETCH;
                    end
                end
                X_READ_ID: if (waited) enter_result(R_RW);
                // A read's next sector from the image.
                X_FETCH: if (ready && owed == 8'd0 && !storing) begin
                    if (cyl[d] >= tracks || sector[d] > sectors_per_track || head[d] && !two_sided[d])
                        fail(R_RW, head[d], 8'h04);
                    else begin
                        target_lba <= lba; target_drive <= d;
                        fetching <= 1; fetch_beats <= 0;
                        ex <= X_FETCH_WAIT;
                    end
                end
                X_FETCH_WAIT: if (fetch_beats == 7'd64) begin
                    index <= 0;
                    wait_us <= byte_wait;
                    ex <= X_BYTE_WAIT;
                end
                // A byte, offered once its byte time has passed.
                X_BYTE_WAIT: if (waited && !storing) begin
                    ex <= X_BYTE;
                    if (pio) begin
                        msr <= writing ? 8'hb0 : 8'hf0;
                        pio_irq <= 1;
                    end else drq_r <= 1;
                end
                X_BYTE: if (byte_taken) begin
                    drq_r <= 0;
                    pio_irq <= 0;
                    if (pio) msr <= 8'h30;
                    if (dack) begin dack_done <= 1; dack_rdata <= buffer_byte; end
                    tc_seen <= taken_tc;
                    if (formatting) begin
                        // One byte of a sector ID: C, H, R, N. A C past the
                        // disk ends the command at once (Bochs); a mismatch
                        // anywhere else ends it at the ID's last byte.
                        id_byte <= id_byte + 2'd1;
                        if (id_byte == 2'd0 && taken_byte >= tracks) fail(R_FORMAT, head[d], 8'h04);
                        else if (id_byte == 2'd3 && (format_bad || taken_byte != 8'd2)) fail(R_FORMAT, head[d], 8'h04);
                        else if (id_byte == 2'd3) ex <= X_STORE;
                        else if (taken_tc) begin
                            st0 <= {5'd0, head[d], 1'b0, d};
                            enter_result(R_FORMAT);
                        end else begin
                            wait_us <= byte_wait;
                            ex <= X_BYTE_WAIT;
                        end
                        case (id_byte)
                            2'd0: if (!format_cyl_set) begin
                                format_cyl <= taken_byte; format_cyl_set <= 1;
                                if (taken_byte < tracks) cyl[d] <= taken_byte;
                            end else if (taken_byte != format_cyl) format_bad <= 1;
                            2'd1: if (taken_byte != {7'd0, head[d]}) format_bad <= 1;
                            2'd2: begin
                                if (taken_byte == 8'd0 || taken_byte > sectors_per_track) format_bad <= 1;
                                sector[d] <= taken_byte;
                            end
                            default: ;
                        endcase
                    end else begin
                        if (writing) begin
                            buffer_write <= 1;
                            buffer_waddress <= index[8:3];
                            buffer_wdata <= {8{taken_byte}};
                            buffer_wbyteenable <= 8'd1 << index[2:0];
                        end
                        index <= index + 9'd1;
                        if (index == 9'd511 || taken_tc) begin
                            if (writing) ex <= X_STORE;
                            else if (taken_tc || end_of_track) finish_transfer(end_of_track && !taken_tc);
                            else begin
                                step_sector();
                                ex <= X_FETCH;
                            end
                        end else begin
                            wait_us <= byte_wait;
                            ex <= X_BYTE_WAIT;
                        end
                    end
                end
                // Write and format: store the sector once the fence allows.
                X_STORE: if (!storing && !frozen && ready) begin
                    target_lba <= lba; target_drive <= d;
                    ex <= X_STORE_GO;
                end
                X_STORE_GO: begin
                    storing <= 1; store_loaded <= 0; store_word <= 0;
                    store_fill <= formatting;
                    store_bytes <= index == 9'd0 ? 10'd512 : {1'b0, index};
                    ex <= X_STORE_WAIT;
                end
                X_STORE_WAIT: if (!storing) begin
                    if (formatting) begin
                        id_byte <= 0;
                        if (format_left <= 8'd1 || tc_seen) begin
                            st0 <= {5'd0, head[d], 1'b0, d}; st1 <= 0; st2 <= 0;
                            enter_result(R_FORMAT);
                        end else begin
                            format_left <= format_left - 8'd1;
                            wait_us <= byte_wait;
                            ex <= X_BYTE_WAIT;
                        end
                    end else if (tc_seen || end_of_track) finish_transfer(end_of_track && !tc_seen);
                    else begin
                        step_sector();
                        index <= 0;
                        wait_us <= byte_wait;
                        ex <= X_BYTE_WAIT;
                    end
                end
                default: ;
            endcase
        end
    end
    wire unused = ^{perp[7], command[8], command[7]};
endmodule
