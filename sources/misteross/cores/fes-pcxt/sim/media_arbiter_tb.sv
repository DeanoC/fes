// SPDX-License-Identifier: GPL-2.0-or-later
module media_arbiter_tb;
    reg clk=0; always #5 clk<=~clk;
    reg cold_reset=1, machine_reset=0;
    reg host_valid=0, guest_valid=0, storage_ready=0, storage_done=0;
    reg [18:0] host_address=19'h12345, guest_address=19'h23456;
    reg [7:0] host_data=8'h12, guest_data=8'h34;
    wire host_ready, host_done, guest_ready, guest_done, storage_valid;
    wire [18:0] storage_address; wire [7:0] storage_data;
    reg fdc_valid=0, snapshot_valid=0, read_ready=0;
    reg [18:0] fdc_address=19'h111, snapshot_address=19'h222;
    reg [7:0] read_data=8'h00;
    wire fdc_ready, snapshot_ready, read_valid, read_reset_safe;
    wire [18:0] read_address; wire [7:0] fdc_data, snapshot_data;
    pcxt_media_arbiter dut (.*);
    task tick; @(posedge clk); #1; endtask
    initial begin
        tick(); cold_reset=0;
        // A later guest request cannot steal the in-flight host completion.
        host_valid=1; storage_ready=1; #1;
        if (!storage_valid || !host_ready || guest_ready || storage_address != host_address) $fatal(1,"host admission");
        tick(); host_valid=0; guest_valid=1; storage_ready=0; tick();
        storage_done=1; #1;
        if (!host_done || guest_done) $fatal(1,"host completion ownership");
        machine_reset=1; tick(); machine_reset=0; storage_done=0; tick();
        storage_ready=1; #1;
        if (!guest_ready || storage_address != guest_address || storage_data != guest_data) $fatal(1,"guest admission");
        tick(); guest_valid=0; storage_ready=0; host_valid=1; tick();
        storage_done=1; #1;
        if (!guest_done || host_done) $fatal(1,"guest completion ownership");
        tick(); storage_done=0; host_valid=0; tick();
        // Concurrent readers alternate, and each receives its own byte.
        fdc_valid=1; snapshot_valid=1; tick();
        if (!read_valid || read_address != fdc_address) $fatal(1,"first read priority");
        read_data=8'ha5; read_ready=1; #1;
        if (!fdc_ready || snapshot_ready || fdc_data != 8'ha5) $fatal(1,"FDC reply");
        tick(); read_ready=0; tick(); tick();
        if (!read_valid || read_address != snapshot_address) $fatal(1,"snapshot fairness");
        read_data=8'h5a; read_ready=1; #1;
        if (!snapshot_ready || fdc_ready || snapshot_data != 8'h5a) $fatal(1,"snapshot reply");
        tick(); read_ready=0; snapshot_valid=0; fdc_valid=0; tick();
        // A machine reset cancels an outstanding read, not a disk write.
        fdc_valid=1; tick();
        if (!read_valid) $fatal(1,"read start");
        machine_reset=1; tick();
        if (read_valid || fdc_ready || snapshot_ready) $fatal(1,"read reset");
        fdc_valid=0; snapshot_valid=1; tick();
        if (!read_valid || !read_reset_safe || read_address != snapshot_address) $fatal(1,"snapshot during reset");
        read_ready=1; #1;
        if (!snapshot_ready) $fatal(1,"snapshot reply during reset");
        $display("PASS PC/XT media arbiter: write ownership, read fairness, reset");
        $finish;
    end
endmodule
