// SPDX-License-Identifier: MIT
// ATA hard disk, master on the primary channel, image in the DDR window
// (docs/pc386-machine-design.md, "Disk").
//
// Commands: IDENTIFY DEVICE, READ/WRITE SECTORS, READ/WRITE MULTIPLE with SET
// MULTIPLE MODE, READ VERIFY, INITIALIZE DEVICE PARAMETERS, RECALIBRATE, SEEK,
// EXECUTE DEVICE DIAGNOSTIC, SET FEATURES, the idle and standby group and
// FLUSH CACHE; anything else aborts. CHS goes through the current translation,
// or LBA28. Data is PIO through the 16-bit data port, out of a two-sector
// buffer that streams sectors to and from the image on the disk port, 64 bits
// per beat.
//
// Reads keep the next sector of the command prefetched in the other half, so
// a block of READ MULTIPLE streams without BSY between its sectors. Writes
// complete into the buffer: once the guest has written a whole sector it is
// queued for the image, and the drive reports that sector done (BSY clear)
// while the store runs, like a drive with its write cache on. A torn sector
// never reaches the image, and the next command waits until every queued
// store has landed. BSY otherwise covers only the transfer to or from the
// image; no seek or rotation time is modelled. Each sector store sets the
// dirty bit of its 64 KiB chunk.
//
// The buffer and the dirty map are M10K with registered reads. The data port
// reads the buffer word at the guest's pointer, re-read every clock: the
// pointer moves only at a device cycle's access, 36 clocks or more apart. A
// guest's 16-bit writes collect in a register and reach the buffer with the
// fourth of each 64-bit word, so the buffer takes only whole words; a data-port
// read during a write command, a protocol error, returns no defined data. The
// store engine presents each word's address a clock before it issues the word.
// A sector store marks its chunk by read-modify-write two clocks later, and a
// reset clears the map over 128 clocks; dirty_word is the word at dirty_index
// two clocks before, held over a read that met a mark (M10K leaves that read
// undefined).
//
// Device 1 is absent: with DEV set its status reads 00h and it runs no
// command but EXECUTE DEVICE DIAGNOSTIC; the other registers are shared.
module pc386_ata #(
    parameter integer RESET_CLOCKS = 1000
) (
    input wire clk, reset,
    input wire access, write, word,
    input wire command_block,            // 1F0h-1F7h; otherwise 3F6h
    input wire [2:0] register,
    input wire [15:0] wdata,
    output reg [15:0] rdata,
    output wire irq14,
    // Image geometry (fes.media.ata-disk DiskCommit).
    input wire present,
    input wire [31:0] sectors,
    input wire [15:0] cylinders,
    input wire [4:0] heads,
    input wire [5:0] spt,
    // Disk image port: 64-bit words at the image offset; one request at a time.
    output reg disk_valid,
    output reg disk_write,
    output reg [23:0] disk_address,
    output reg [63:0] disk_wdata,
    input wire disk_ready,
    input wire disk_rvalid,
    input wire [63:0] disk_rdata,
    // Dirty map, 16 chunks per word, two clocks after the index.
    input wire [6:0] dirty_index,
    output reg [15:0] dirty_word
);
    localparam [7:0] ABRT = 8'h04, IDNF = 8'h10;

    // Task file
    reg [7:0] features, count, number, cylinder_low, cylinder_high, drive_head, error;
    reg nien, srst;
    wire device1 = drive_head[4];
    wire lba_mode = drive_head[6];

    // Translation for CHS addressing
    reg [15:0] logical_cylinders;
    reg [4:0] logical_heads;
    reg [5:0] logical_spt;
    reg [4:0] multiple;                  // sectors per block, 0 when disabled

    // Command state
    localparam IDLE = 4'd0, DRAIN = 4'd1, ADDRESS = 4'd2, ADDRESS2 = 4'd3, CHECK = 4'd4,
               READ = 4'd5, WRITE = 4'd6, IDENTIFY = 4'd7, DIVIDE = 4'd8, RESETTING = 4'd9,
               VERIFY = 4'd10;
    reg [3:0] phase;
    reg [7:0] opcode;
    reg busy, drq, err, pending_interrupt;
    reg [8:0] left;                      // sectors left for the guest
    reg [8:0] verify_left;               // sectors to advance over after READ VERIFY
    reg [4:0] block_left;
    reg announce;                        // interrupt when the next block's data is ready
    reg [7:0] pointer;                   // word within the current sector
    reg half;                            // the guest's buffer half
    reg [27:0] start, guest_lba;
    reg [20:0] track;
    reg [15:0] reset_timer;

    // Buffer: two sectors of 64 quarter-sector words.
    reg [1:0] loaded, queued;            // per half: fetched for reading / waiting to be stored
    reg [8:0] fetch_left;
    reg [27:0] fetch_lba, store_lba;
    reg fetch_half, store_half;
    localparam ENGINE_IDLE = 2'd0, ENGINE_ISSUE = 2'd1, ENGINE_WAIT = 2'd2, ENGINE_ABORT = 2'd3;
    reg [1:0] fetch_state, store_state;
    reg [5:0] fetch_word, store_word;
    reg [63:0] gathered;                 // the guest's words of a buffer word
    reg deferred;                        // a whole word from the guest waits for a fetch beat
    reg [6:0] deferred_address;
    wire fetch_fill = fetch_state == ENGINE_WAIT && disk_rvalid;
    wire store_start = store_state == ENGINE_IDLE && queued[store_half] && fetch_state == ENGINE_IDLE && !disk_valid;
    wire buffer_write;
    wire [6:0] buffer_waddress;
    wire [63:0] buffer_wdata;
    reg [6:0] buffer_raddress;
    wire [63:0] buffer_word;
    pc386_ram #(.WIDTH(64), .DEPTH(128)) buffer (
        .clk(clk), .write(buffer_write), .waddress(buffer_waddress), .wdata(buffer_wdata), .wbyteenable(1'b1),
        .read(1'b1), .raddress(buffer_raddress), .rdata(buffer_word)
    );
    // The store engine has the read port from the clock before its first
    // word; the data port has it otherwise.
    always @* begin
        buffer_raddress = {half, pointer[7:2]};
        if (store_start) buffer_raddress = {store_half, 6'd0};
        else if (store_state == ENGINE_ISSUE) buffer_raddress = {store_half, store_word};
        else if (store_state == ENGINE_WAIT) buffer_raddress = {store_half, store_word + 1'b1};
    end

    // Dirty map: a sector store marks its chunk in two clocks, ahead of dirty_index.
    reg mark_read, mark_write, clearing;
    reg host_read;                       // last clock's map read was dirty_index's, with no write
    reg [6:0] mark_index, dirty_clear;
    reg [3:0] mark_bit;
    wire [15:0] dirty_read;
    wire dirty_write = clearing || mark_write;
    pc386_ram #(.WIDTH(16), .DEPTH(128)) dirty (
        .clk(clk), .write(dirty_write), .waddress(clearing ? dirty_clear : mark_index),
        .wdata(clearing ? 16'h0000 : dirty_read | (16'h0001 << mark_bit)), .wbyteenable(1'b1),
        .read(1'b1), .raddress(mark_read ? mark_index : dirty_index), .rdata(dirty_read)
    );

    // BSY, DRDY, DF, DSC (always set: no seek is modelled), DRQ, CORR, IDX, ERR
    wire [7:0] status = device1 ? 8'h00 : {busy, !busy && present, 1'b0, 1'b1, drq, 2'b00, err};
    assign irq14 = pending_interrupt && !nien && !device1;
    wire [4:0] block = (opcode == 8'hc4 || opcode == 8'hc5) ? multiple : 5'd1;
    wire [8:0] total = count == 0 ? 9'd256 : {1'b0, count};
    wire [15:0] current_cylinders = logical_cylinders;
    // IDENTIFY words 57-58. Only the power-on translation, on the first cycle
    // after reset, and INITIALIZE DEVICE PARAMETERS, under BSY, change the
    // geometry, so the product comes from a register and its multiplies stay
    // off the I/O read path.
    reg [28:0] current_capacity;
    always @(posedge clk) current_capacity <= 29'(logical_cylinders * logical_heads) * logical_spt;

    function automatic [15:0] identify_word(input [7:0] w);
        // Strings are two characters per word, first character high.
        reg [8*40-1:0] model;
        reg [8*20-1:0] serial;
        reg [8*8-1:0] firmware;
        model = "FES pc386 ATA disk                      ";
        serial = "FES00000000000000001";
        firmware = "M1      ";
        case (w)
            8'd0: identify_word = 16'h0040;            // fixed disk
            8'd1: identify_word = cylinders;
            8'd3: identify_word = {11'b0, heads};
            // Obsolete bytes per track and per sector; the Bochs BIOS takes
            // its transfer block size from word 5.
            8'd4: identify_word = {1'b0, spt, 9'b0};
            8'd5: identify_word = 16'd512;
            8'd6: identify_word = {10'b0, spt};
            8'd47: identify_word = 16'h8010;           // READ/WRITE MULTIPLE up to 16 sectors
            8'd48: identify_word = 16'h0000;           // no 32-bit PIO
            8'd49: identify_word = 16'h0200;           // LBA; no DMA
            8'd51: identify_word = 16'h0200;           // PIO mode 2 timing
            8'd53: identify_word = 16'h0001;           // words 54-58 valid
            8'd54: identify_word = current_cylinders;
            8'd55: identify_word = {11'b0, logical_heads};
            8'd56: identify_word = {10'b0, logical_spt};
            8'd57: identify_word = current_capacity[15:0];
            8'd58: identify_word = {3'b0, current_capacity[28:16]};
            8'd59: identify_word = multiple != 0 ? {11'b00000001000, multiple} : 16'h0000;
            8'd60: identify_word = sectors[15:0];
            8'd61: identify_word = sectors[31:16];
            8'd80: identify_word = 16'h001e;           // ATA-1 to ATA-4
            8'd82, 8'd83, 8'd84, 8'd85, 8'd86, 8'd87: identify_word = 16'h4000;
            default: begin
                if (w >= 10 && w <= 19) identify_word = serial[8*(20-2*(w-10))-1 -: 16];
                else if (w >= 23 && w <= 26) identify_word = firmware[8*(8-2*(w-23))-1 -: 16];
                else if (w >= 27 && w <= 46) identify_word = model[8*(40-2*(w-27))-1 -: 16];
                else identify_word = 16'h0000;
            end
        endcase
    endfunction

    // Task file address of the sector after the current one.
    task automatic advance_address;
        if (lba_mode) {drive_head[3:0], cylinder_high, cylinder_low, number} <=
                      {drive_head[3:0], cylinder_high, cylinder_low, number} + 1'b1;
        else if (number[5:0] < logical_spt) number <= number + 1'b1;
        else begin
            number <= 1;
            if (drive_head[3:0] + 1'b1 < logical_heads) drive_head[3:0] <= drive_head[3:0] + 1'b1;
            else begin
                drive_head[3:0] <= 0;
                {cylinder_high, cylinder_low} <= {cylinder_high, cylinder_low} + 1'b1;
            end
        end
    endtask

    always @* begin
        rdata = 16'hffff;  // no drive: a floating bus
        if (present) begin
            if (!command_block) rdata = {8'hff, status};
            else case (register)
                3'd0: rdata = phase == IDENTIFY ? identify_word(pointer) : buffer_word[16 * pointer[1:0] +: 16];
                3'd1: rdata = {8'hff, error};
                3'd2: rdata = {8'hff, count};
                3'd3: rdata = {8'hff, number};
                3'd4: rdata = {8'hff, cylinder_low};
                3'd5: rdata = {8'hff, cylinder_high};
                3'd6: rdata = {8'hff, drive_head};
                default: rdata = {8'hff, status};
            endcase
        end
    end

    wire data_access = access && present && command_block && register == 0 && drq && !device1;
    wire guest_read = data_access && !write;
    wire guest_write = data_access && write;
    wire last_word = pointer == 8'd255;
    wire abort_fetch = access && present && write &&
                       ((!command_block && wdata[2]) ||
                        (command_block && register == 3'd7 && !busy && !srst && !device1));
    // A guest's whole word goes in with its fourth write. Only a guest writing
    // the data port during a read command meets a fetch beat; its word then
    // follows a clock later.
    wire guest_word = guest_write && pointer[1:0] == 2'd3;
    assign buffer_write = fetch_fill || guest_word || deferred;
    assign buffer_waddress = fetch_fill ? {fetch_half, fetch_word} : deferred ? deferred_address : {half, pointer[7:2]};
    assign buffer_wdata = fetch_fill ? disk_rdata : deferred ? gathered : {wdata, gathered[47:0]};

    always @(posedge clk) begin
        host_read <= !mark_read && !dirty_write;
        if (clearing) dirty_word <= 16'h0000;
        else if (host_read) dirty_word <= dirty_read;
        if (reset) begin
            features <= 0; count <= 1; number <= 1; cylinder_low <= 0; cylinder_high <= 0; drive_head <= 0;
            error <= 8'h01; nien <= 0; srst <= 0;
            logical_cylinders <= 0; logical_heads <= 0; logical_spt <= 0; multiple <= 0;
            phase <= IDLE; opcode <= 0; busy <= 0; drq <= 0; err <= 0; pending_interrupt <= 0;
            left <= 0; verify_left <= 0; block_left <= 0; announce <= 0; pointer <= 0; half <= 0;
            start <= 0; guest_lba <= 0; track <= 0; reset_timer <= 0;
            loaded <= 0; queued <= 0; fetch_left <= 0; fetch_lba <= 0; store_lba <= 0;
            fetch_half <= 0; store_half <= 0; fetch_state <= ENGINE_IDLE; store_state <= ENGINE_IDLE;
            fetch_word <= 0; store_word <= 0;
            disk_valid <= 0; disk_write <= 0; disk_address <= 0; disk_wdata <= 0;
            gathered <= 0; deferred <= 0; deferred_address <= 0;
            mark_read <= 0; mark_write <= 0; mark_index <= 0; mark_bit <= 0;
            clearing <= 1; dirty_clear <= 0;
        end else begin
            if (clearing) begin
                dirty_clear <= dirty_clear + 1'b1;
                if (dirty_clear == 7'd127) clearing <= 0;
            end
            mark_write <= mark_read;
            mark_read <= 0;
            if (!fetch_fill) deferred <= 0;
            if (guest_word && fetch_fill) begin
                deferred <= 1;
                deferred_address <= {half, pointer[7:2]};
            end

            // The power-on translation is the image's default geometry.
            if (logical_heads == 0) begin
                logical_cylinders <= cylinders; logical_heads <= heads; logical_spt <= spt;
            end

            // Fetch engine: sector fetch_lba into half fetch_half.
            case (fetch_state)
                ENGINE_IDLE: if (fetch_left != 0 && !loaded[fetch_half] && store_state == ENGINE_IDLE && !disk_valid) begin
                    fetch_state <= ENGINE_ISSUE;
                    fetch_word <= 0;
                end
                ENGINE_ISSUE: if (!disk_valid) begin
                    disk_valid <= 1; disk_write <= 0;
                    disk_address <= {fetch_lba[17:0], fetch_word};
                    fetch_state <= ENGINE_WAIT;
                end
                ENGINE_WAIT: begin
                    if (disk_valid && disk_ready) disk_valid <= 0;
                    if (disk_rvalid) begin
                        fetch_word <= fetch_word + 1'b1;
                        if (fetch_word == 6'd63) begin
                            loaded[fetch_half] <= 1;
                            fetch_half <= !fetch_half;
                            fetch_lba <= fetch_lba + 1'b1;
                            fetch_left <= fetch_left - 1'b1;
                            fetch_state <= ENGINE_IDLE;
                        end else fetch_state <= ENGINE_ISSUE;
                    end
                end
                ENGINE_ABORT: begin
                    // Keep an unaccepted request stable until the image port
                    // accepts it, then drain its response without using it.
                    if (disk_valid && disk_ready) disk_valid <= 0;
                    if (disk_rvalid) begin
                        fetch_state <= ENGINE_IDLE;
                        fetch_word <= 0;
                    end
                end
                default: begin end
            endcase

            // Store engine: half store_half to sector store_lba.
            case (store_state)
                ENGINE_IDLE: if (store_start) begin
                    store_state <= ENGINE_ISSUE;
                    store_word <= 0;
                end
                ENGINE_ISSUE: if (!disk_valid) begin
                    disk_valid <= 1; disk_write <= 1;
                    disk_address <= {store_lba[17:0], store_word};
                    disk_wdata <= buffer_word;
                    store_state <= ENGINE_WAIT;
                end
                default: if (disk_valid && disk_ready) begin
                    disk_valid <= 0;
                    store_word <= store_word + 1'b1;
                    if (store_word == 6'd63) begin
                        queued[store_half] <= 0;
                        mark_read <= 1; mark_index <= store_lba[17:11]; mark_bit <= store_lba[10:7];
                        store_half <= !store_half;
                        store_lba <= store_lba + 1'b1;
                        store_state <= ENGINE_IDLE;
                    end else store_state <= ENGINE_ISSUE;
                end
            endcase

            // Guest data transfers
            if (guest_read) begin
                pointer <= pointer + 1'b1;
                if (last_word) begin
                    if (phase == IDENTIFY) begin
                        drq <= 0; phase <= IDLE;
                    end else begin
                        loaded[half] <= 0;
                        half <= !half;
                        left <= left - 1'b1;
                        block_left <= block_left - 1'b1;
                        count <= count - 1'b1;
                        advance_address();
                        if (left == 1) begin drq <= 0; phase <= IDLE; end
                        else if (block_left == 1 || !loaded[!half]) begin
                            // Next block, or a sector not fetched yet: BSY until it is.
                            drq <= 0; busy <= 1;
                            announce <= block_left == 1;
                            if (block_left == 1) block_left <= {4'b0, block} > left - 1'b1 ? 5'(left - 1'b1) : block;
                        end
                    end
                end
            end
            if (guest_write) begin
                case (pointer[1:0])
                    2'd0: gathered[15:0] <= wdata;
                    2'd1: gathered[31:16] <= wdata;
                    2'd2: gathered[47:32] <= wdata;
                    default: gathered[63:48] <= wdata;
                endcase
                pointer <= pointer + 1'b1;
                if (last_word) begin
                    queued[half] <= 1;
                    half <= !half;
                    left <= left - 1'b1;
                    block_left <= block_left - 1'b1;
                    count <= count - 1'b1;
                    advance_address();
                    if (left == 1) begin
                        drq <= 0; phase <= IDLE; pending_interrupt <= 1;
                    end else if (block_left == 1 || queued[!half]) begin
                        drq <= 0; busy <= 1;
                        announce <= block_left == 1;
                        if (block_left == 1) block_left <= {4'b0, block} > left - 1'b1 ? 5'(left - 1'b1) : block;
                    end
                end
            end

            case (phase)
                DRAIN: if (queued == 0 && store_state == ENGINE_IDLE) phase <= ADDRESS;
                ADDRESS: begin
                    track <= 21'({cylinder_high, cylinder_low} * logical_heads) + 21'(drive_head[3:0]);
                    phase <= ADDRESS2;
                end
                ADDRESS2: begin
                    start <= lba_mode ? {drive_head[3:0], cylinder_high, cylinder_low, number} :
                                        28'(track * logical_spt) + 28'(number) - 28'd1;
                    phase <= CHECK;
                end
                CHECK: begin
                    if ((!lba_mode && (number == 0 || number[5:0] > logical_spt || number[7:6] != 0 ||
                                       {1'b0, drive_head[3:0]} >= logical_heads)) ||
                        {4'b0, start} + 32'(total) > sectors) begin
                        error <= IDNF; err <= 1; busy <= 0; pending_interrupt <= 1; phase <= IDLE;
                    end else if (opcode == 8'h40 || opcode == 8'h41) begin
                        // READ VERIFY: the range is valid; no data moves.
                        // Advance one sector per clock to leave the task file
                        // at the final verified address in either addressing mode.
                        verify_left <= total - 1'b1;
                        phase <= VERIFY;
                    end else begin
                        left <= total;
                        block_left <= {4'b0, block} > total ? 5'(total) : block;
                        pointer <= 0; half <= 0;
                        loaded <= 0; fetch_half <= 0; store_half <= 0;
                        if (opcode == 8'h30 || opcode == 8'h31 || opcode == 8'hc5) begin
                            store_lba <= start;
                            busy <= 0; drq <= 1; phase <= WRITE;
                        end else begin
                            fetch_lba <= start; fetch_left <= total;
                            announce <= 1; phase <= READ;
                        end
                    end
                end
                READ: if (busy && loaded[half]) begin
                    busy <= 0; drq <= 1;
                    if (announce) pending_interrupt <= 1;
                end
                VERIFY: if (verify_left != 0) begin
                    advance_address();
                    verify_left <= verify_left - 1'b1;
                end else begin
                    count <= 0; busy <= 0; pending_interrupt <= 1; phase <= IDLE;
                end
                WRITE: if (busy && !queued[half]) begin
                    busy <= 0; drq <= 1;
                    if (announce) pending_interrupt <= 1;
                end
                DIVIDE: begin
                    // INITIALIZE DEVICE PARAMETERS: cylinders = sectors / (heads * spt),
                    // by subtraction one cylinder at a time.
                    if (track != 0 && {4'b0, start} + 32'(track) <= sectors && logical_cylinders != 16'hffff) begin
                        start <= start + 28'(track);
                        logical_cylinders <= logical_cylinders + 1'b1;
                    end else begin
                        busy <= 0; pending_interrupt <= 1; phase <= IDLE;
                    end
                end
                RESETTING: if (!srst) begin
                    if (reset_timer != 0) reset_timer <= reset_timer - 1'b1;
                    else begin
                        // Diagnostic signature of an ATA device; device 1 absent.
                        count <= 1; number <= 1; cylinder_low <= 0; cylinder_high <= 0; drive_head <= 0;
                        error <= 8'h01; busy <= 0; err <= 0; phase <= IDLE;
                    end
                end
                default: begin end
            endcase

            if (access && present && write) begin
                if (!command_block) begin
                    nien <= wdata[1];
                    srst <= wdata[2];
                    if (wdata[2]) begin
                        phase <= RESETTING; busy <= 1; drq <= 0; err <= 0; pending_interrupt <= 0;
                        fetch_left <= 0; loaded <= 0;
                        reset_timer <= 16'(RESET_CLOCKS);
                    end
                end else case (register)
                    3'd1: features <= wdata[7:0];
                    3'd2: count <= wdata[7:0];
                    3'd3: number <= wdata[7:0];
                    3'd4: cylinder_low <= wdata[7:0];
                    3'd5: cylinder_high <= wdata[7:0];
                    3'd6: drive_head <= wdata[7:0];
                    3'd7: if (!busy && !srst) begin
                        opcode <= wdata[7:0];
                        pending_interrupt <= 0;
                        err <= 0; error <= 0; drq <= 0;
                        if (wdata[7:0] == 8'h90) begin
                            // EXECUTE DEVICE DIAGNOSTIC: device 0 passed, no device 1.
                            count <= 1; number <= 1; cylinder_low <= 0; cylinder_high <= 0; drive_head <= 0;
                            error <= 8'h01; pending_interrupt <= 1;
                        end else if (device1) begin
                            // No device 1 to run it.
                        end else casez (wdata[7:0])
                            8'hec: begin phase <= IDENTIFY; pointer <= 0; drq <= 1; pending_interrupt <= 1; end
                            8'h20, 8'h21, 8'h30, 8'h31, 8'h40, 8'h41: begin busy <= 1; phase <= DRAIN; end
                            8'hc4, 8'hc5: if (multiple != 0) begin busy <= 1; phase <= DRAIN; end
                                          else begin error <= ABRT; err <= 1; pending_interrupt <= 1; end
                            8'hc6: begin
                                if (count == 0 || count > 16 || (count & (count - 1'b1)) != 0) begin
                                    error <= ABRT; err <= 1;
                                end else multiple <= count[4:0];
                                pending_interrupt <= 1;
                            end
                            8'h91: begin
                                logical_heads <= drive_head[3:0] + 1'b1;
                                logical_spt <= count[5:0];
                                logical_cylinders <= 0;
                                track <= 21'({1'b0, drive_head[3:0]} + 5'd1) * 21'(count[5:0]);
                                start <= 0;
                                busy <= 1; phase <= DIVIDE;
                            end
                            8'b0001????, 8'b0111????, 8'hef, 8'he7,
                            8'he0, 8'he1, 8'he2, 8'he3, 8'he6, 8'h94, 8'h95, 8'h96, 8'h97, 8'h98, 8'h99:
                                pending_interrupt <= 1;
                            8'he5: begin count <= 8'hff; pending_interrupt <= 1; end  // CHECK POWER MODE: active
                            default: begin error <= ABRT; err <= 1; pending_interrupt <= 1; end
                        endcase
                    end
                    default: begin end
                endcase
            end
            if (access && present && !write && (register == 3'd7 && command_block) && !device1)
                pending_interrupt <= 0;

            if (abort_fetch) begin
                loaded <= 0;
                fetch_left <= 0;
                case (fetch_state)
                    ENGINE_ISSUE: begin
                        // No request has been presented yet.
                        fetch_state <= ENGINE_IDLE;
                        fetch_word <= 0;
                        disk_valid <= 0;
                    end
                    ENGINE_WAIT: begin
                        if (disk_rvalid) begin
                            // The response already arrived this cycle.
                            fetch_state <= ENGINE_IDLE;
                            fetch_word <= 0;
                        end else fetch_state <= ENGINE_ABORT;
                    end
                    ENGINE_ABORT: if (disk_rvalid) begin
                        fetch_state <= ENGINE_IDLE;
                        fetch_word <= 0;
                    end
                    default: begin
                        fetch_state <= ENGINE_IDLE;
                        fetch_word <= 0;
                    end
                endcase
            end
        end
    end
    wire unused = ^{features, word, wdata[15:8], sectors[31:28], left[8], guest_lba};
endmodule
