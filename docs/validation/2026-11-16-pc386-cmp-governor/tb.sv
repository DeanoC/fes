`timescale 1ns/1ps
module tb;
  import fes_i386_pkg::*;
  logic clk=0; always #5 clk=~clk;
  logic reset=1, turbo=0, boundary=0,eoi=0,element=0,taken=0;
  logic [2:0] preset=0; logic [9:0] frequency=0;
  gov_t gov='0; logic [3:0] ilen=0; logic redirect=0,abandon=0,preserve_de=0,replay=0,ordinary_ram=1;
  logic [1:0] accesses=0,mmio=0,io_size=0; logic dword=0,io=0,mmio_st2=0;
  logic [15:0] io_port=0; logic [2:0] mmio_halves=0,mmio_st2_halves=0;
  wire hold_new,hold_old; wire [11:0] debt_new,debt_old; wire [12:0] charge_new,charge_old;
  fes_i386_governor_original #(.GOV_N(1),.GOV_D(1000)) old (
    .clk,.reset,.preset,.frequency,.turbo,.boundary,.eoi,.element,.taken,.gov,
    .ilen,.redirect,.abandon,.preserve_de,.replay,.accesses,.dword,.ordinary_ram,.io,.io_port,.io_size,.mmio,.mmio_halves,.mmio_st2,.mmio_st2_halves,
    .hold(hold_old),.debt_clocks(debt_old),.charge(charge_old));
  fes_i386_governor #(.GOV_N(1),.GOV_D(1000),.REFERENCE_DIAGNOSTIC(0)) current (
    .clk,.reset,.preset,.frequency,.turbo,.boundary,.eoi,.element,.taken,.gov,
    .ilen,.redirect,.abandon,.preserve_de,.replay,.accesses,.dword,.ordinary_ram,.io,.io_port,.io_size,.mmio,
    .mmio_halves,.mmio_st2,.mmio_st2_halves,.hold(hold_new),.debt_clocks(debt_new),.charge(charge_new));
  int n;
  initial begin
    for (n=0; n<262144; n++) begin
      @(negedge clk);
      reset = ((n % 8192) < 4);
      if (reset) begin preset=3'((n / 8192)%8); turbo=1'((n / 65536)%2); frequency=(n<131072) ? 10'd0 : 10'(1+($urandom % 1023)); end
      gov.two=1'($urandom); gov.op=8'($urandom); gov.mreg=3'($urandom); gov.rm_reg=1'($urandom);
      gov.o32=1'($urandom); gov.rep=1'($urandom); gov.ea3=1'($urandom); gov.prefix286=6'($urandom);
      boundary=1'($urandom); eoi=1'($urandom); element=1'($urandom); taken=1'($urandom);
      ilen=4'($urandom); redirect=1'($urandom); abandon=1'($urandom); preserve_de=1'($urandom); replay=1'($urandom); ordinary_ram=1'($urandom);
      accesses=2'($urandom); dword=1'($urandom); io=1'($urandom); io_port=16'($urandom);
      io_size=2'($urandom); mmio=2'($urandom); mmio_halves=3'($urandom);
      mmio_st2=1'($urandom); mmio_st2_halves=3'($urandom);
      @(posedge clk); #1;
      if ({hold_new,debt_new,charge_new} !== {hold_old,debt_old,charge_old})
        $fatal(1,"cycle=%0d preset=%0d new=%h old=%h",n,preset,{hold_new,debt_new,charge_new},{hold_old,debt_old,charge_old});
      if ((n % 16384)==0) $display("heartbeat cycle=%0d",n);
    end
    $display("PASS default-off original-vs-current: %0d sampled cycles, all presets and turbo",n); $finish;
  end
endmodule
