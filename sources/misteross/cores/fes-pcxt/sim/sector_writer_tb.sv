// SPDX-License-Identifier: GPL-2.0-or-later
// Adversarial byte-port stalls and registered sector RAM for the XT writer.
module sector_writer_tb;
    reg clk = 0, cold_reset = 1, sector_valid = 0;
    reg [9:0] sector_lba = 0;
    wire [8:0] sector_index;
    reg [7:0] sector_data = 0;
    wire sector_ready, sector_error, busy, media_changed;
    wire storage_valid;
    wire [18:0] storage_address;
    wire [7:0] storage_data;
    wire storage_ready, storage_done;
    reg [7:0] disk [0:511];
    reg pending = 0;
    reg [2:0] delay_count = 0;
    reg [8:0] held_address = 0;
    reg [7:0] held_data = 0;
    integer cycle = 0, offers = 0, completions = 0, changes = 0;

    always #5 clk <= ~clk;
    // Model the FDC staging RAM's registered read output.
    always @(posedge clk) sector_data <= sector_index[7:0] ^ (sector_index[8] ? 8'h5a : 8'ha5);
    assign storage_ready = !pending && (cycle % 5 != 0);
    assign storage_done = pending && delay_count == 0;

    pcxt_sector_writer dut (
        .clk(clk), .cold_reset(cold_reset), .sector_valid(sector_valid),
        .sector_lba(sector_lba), .sector_index(sector_index), .sector_data(sector_data),
        .sector_ready(sector_ready), .sector_error(sector_error), .busy(busy),
        .media_changed(media_changed), .storage_valid(storage_valid),
        .storage_address(storage_address), .storage_data(storage_data),
        .storage_ready(storage_ready), .storage_done(storage_done)
    );

    always @(posedge clk) begin
        cycle <= cycle + 1;
        if (storage_valid && storage_ready) begin
            if (pending || storage_address != {10'd17, 9'(offers)} ||
                storage_data != (8'(offers) ^ ((offers & 256) != 0 ? 8'h5a : 8'ha5)))
                $fatal(1, "incorrect or duplicate byte offer %0d", offers);
            held_address <= storage_address[8:0];
            held_data <= storage_data;
            pending <= 1;
            delay_count <= 3'd4;
            offers <= offers + 1;
        end else if (pending && delay_count != 0) begin
            delay_count <= delay_count - 1'b1;
        end else if (storage_done) begin
            disk[held_address] <= held_data;
            completions <= completions + 1;
            pending <= 0;
        end
        if (media_changed) begin
            if (completions != 512) $fatal(1, "dirty before full commit");
            changes <= changes + 1;
        end
    end

    initial begin
        repeat (3) @(negedge clk);
        cold_reset = 0;
        sector_lba = 17;
        sector_valid = 1;
        wait (sector_ready);
        if (sector_error || completions != 512 || !busy)
            $fatal(1, "sector acknowledged before commit");
        @(negedge clk); sector_valid = 0;
        wait (!busy);
        @(negedge clk);
        if (offers != 512 || completions != 512 || changes != 1)
            $fatal(1, "unexpected sector counts");
        for (integer n = 0; n < 512; n = n + 1)
            if (disk[n] != (8'(n) ^ ((n & 256) != 0 ? 8'h5a : 8'ha5))) $fatal(1, "disk byte %0d", n);
        sector_lba = 720; sector_valid = 1;
        wait (sector_ready);
        if (!sector_error || offers != 512) $fatal(1, "invalid LBA accepted");
        @(negedge clk); sector_valid = 0;
        wait (!busy);
        if (changes != 1 || offers != 512) $fatal(1, "invalid sector modified disk");
        $display("PASS PC/XT staged sector writer: 512 completed bytes, stalls, dirty, bounds");
        $finish;
    end
    initial begin
        repeat (20000) @(posedge clk);
        $fatal(1, "sector writer timeout");
    end
endmodule
