// SPDX-License-Identifier: MIT
// Default-off reference-model integration checks against the real governor and cost ROM.
`timescale 1ns/1ps
module governor_integration_tb;
  import fes_i386_pkg::*;
  logic clk=0; always #1 clk=~clk;
  logic reset=1, boundary=0,eoi=0,element=0,taken=0,redir=0,abandon=0,replay=0;
  gov_t gov='0;
  logic [3:0] ilen=0; logic [1:0] accesses=0; logic [1:0] mmio=0;
  logic [2:0] preset=1; logic turbo=0;
  logic hold0,hold1; logic [11:0] debt0,debt1; logic [12:0] charge0,charge1;
  logic tally=0; int sum0=0,sum1=0;
  always @(posedge clk) if(tally) begin sum0 += int'(charge0); sum1 += int'(charge1); end
  fes_i386_governor #(.GOV_N(1),.GOV_D(1000)) baseline (
    .clk,.reset,.preset,.frequency(10'd0),.turbo,
    .boundary,.eoi,.element,.taken,.gov,.ilen,.redirect(redir),.abandon,.replay,
    .accesses,.dword(1'b0),.io(1'b0),.io_port(16'd0),.io_size(2'd0),.mmio,
    .mmio_halves(3'd1),.mmio_st2(1'b0),.mmio_st2_halves(3'd0),.hold(hold0),.debt_clocks(debt0),.charge(charge0)
  );
  fes_i386_governor #(.GOV_N(1),.GOV_D(1000),.REFERENCE_DIAGNOSTIC(1)) candidate (
    .clk,.reset,.preset,.frequency(10'd0),.turbo,
    .boundary,.eoi,.element,.taken,.gov,.ilen,.redirect(redir),.abandon,.replay,
    .accesses,.dword(1'b0),.io(1'b0),.io_port(16'd0),.io_size(2'd0),.mmio,
    .mmio_halves(3'd1),.mmio_st2(1'b0),.mmio_st2_halves(3'd0),.hold(hold1),.debt_clocks(debt1),.charge(charge1)
  );
  task automatic instruction(input byte op, input int regfield, nbytes, naccess, input bit branch_taken);
    @(negedge clk); gov='0; gov.op=op; gov.mreg=3'(regfield); ilen=4'(nbytes); accesses=2'(naccess);
    boundary=1; eoi=1; taken=branch_taken; redir=branch_taken;
  endtask
  task automatic restart;
    @(negedge clk); tally=0; reset=1; boundary=0; eoi=0; accesses=0; mmio=0; abandon=0; replay=0; redir=0; taken=0; gov='0;
    repeat(5) @(negedge clk); reset=0; repeat(8) @(negedge clk); sum0=0; sum1=0; tally=1;
  endtask
  task automatic finish_trial(input int expected0,expected1, input string label);
    @(negedge clk); boundary=0; eoi=0; taken=0; redir=0; accesses=0; mmio=0; gov='0; abandon=0; replay=0;
    repeat(8) @(negedge clk); tally=0;
    if(sum0!=expected0 || sum1!=expected1) $fatal(1,"%s base=%0d expected=%0d candidate=%0d expected=%0d",label,sum0,expected0,sum1,expected1);
  endtask
  initial begin
    repeat(5) @(negedge clk); reset=0; repeat(8) @(negedge clk); tally=1;
    instruction(8'hff,0,4,2,0);
    instruction(8'h83,7,5,1,0);
    instruction(8'h75,0,2,0,1);
    finish_trial(24,29,"consecutive");
    restart;
    @(negedge clk); gov='0; gov.op=8'hff; ilen=4; accesses=1; boundary=0; eoi=0;
    @(negedge clk); accesses=1; boundary=1; eoi=1;
    instruction(8'h83,7,5,1,0);
    instruction(8'h75,0,2,0,1);
    finish_trial(24,29,"multi-uop accumulation");
    restart;
    instruction(8'hff,0,4,2,0);
    instruction(8'h83,7,6,1,0); gov.prefix286=4;
    instruction(8'h75,0,2,0,1);
    finish_trial(24,33,"segment prefix");
    restart;
    instruction(8'hff,0,4,2,0);
    instruction(8'h75,0,2,0,1);
    instruction(8'hff,0,4,2,0);
    finish_trial(26,29,"redirect");
    restart;
    @(negedge clk); gov='0; gov.op=8'hff; ilen=4; accesses=1; boundary=0; eoi=0;
    @(negedge clk); accesses=0; abandon=1;
    @(negedge clk); abandon=0;
    instruction(8'hff,0,4,2,0);
    finish_trial(10,11,"abandon partial");
    restart;
    instruction(8'hff,0,4,2,0);
    @(negedge clk); boundary=0; eoi=0; accesses=0; replay=1;
    @(negedge clk); replay=0;
    instruction(8'h83,7,5,1,0);
    finish_trial(16,22,"replay preserves queued bytes");
    restart;
    instruction(8'h8b,0,4,1,0); mmio=1;
    finish_trial(12,15,"MMIO subtraction");
    restart;
    instruction(8'h01,0,4,2,0);
    instruction(8'h83,7,5,1,0);
    instruction(8'he2,0,2,0,1);
    finish_trial(25,35,"word ALU overlap");
    restart;
    instruction(8'h00,0,4,2,0);
    instruction(8'h83,7,5,1,0);
    instruction(8'he2,0,2,0,1);
    finish_trial(25,33,"byte ALU overlap");
    restart;
    @(negedge clk); gov='0; gov.op=8'ha4; gov.rep=1; gov.prefix286=2; ilen=2; accesses=2; boundary=1; eoi=0; element=1;
    @(negedge clk); boundary=1; eoi=0; element=1;
    @(negedge clk); boundary=1; eoi=1; element=1;
    @(negedge clk); element=0; boundary=0; eoi=0; accesses=0;
    finish_trial(23,16,"REP MOVS boundary mechanics");
    restart;
    @(negedge clk); gov='0; gov.op=8'ha7; gov.rep=1; gov.prefix286=2; ilen=2; accesses=2; boundary=1; eoi=0; element=1;
    @(negedge clk); boundary=1; eoi=0; element=1;
    @(negedge clk); boundary=1; eoi=1; element=1;
    @(negedge clk); element=0; boundary=0; eoi=0; accesses=0;
    finish_trial(38,35,"REP CMPS re-decode");
    restart;
    @(negedge clk); gov='0; gov.op=8'haf; gov.rep=1; gov.prefix286=2; ilen=2; accesses=1; boundary=1; eoi=0; element=1;
    @(negedge clk); boundary=1; eoi=0; element=1;
    @(negedge clk); boundary=1; eoi=1; element=1;
    @(negedge clk); element=0; boundary=0; eoi=0; accesses=0;
    finish_trial(32,28,"REP SCAS chunk");
    restart; instruction(8'h3b,0,2,1,0); finish_trial(7,9,"CMP memory form");
    restart; instruction(8'h9c,0,1,1,0); finish_trial(4,6,"PUSHF form");
    restart; instruction(8'heb,0,2,0,1); finish_trial(8,9,"short JMP form");
    restart; instruction(8'he8,0,3,1,1); finish_trial(9,11,"relative CALL form");
    restart; instruction(8'hc3,0,1,1,1); finish_trial(13,12,"near RET form");
    restart;
    for (int i=0; i<27; i++) begin
      @(negedge clk); gov='0; gov.op=8'ha4; gov.rep=1; gov.prefix286=2; ilen=2;
      accesses=2; boundary=1; eoi=(i==26); element=1;
    end
    @(negedge clk); element=0; boundary=0; eoi=0; accesses=0;
    finish_trial(167,116,"REP MOVS crosses execution chunk");
    restart;
    for (int i=0; i<64; i++) begin
      @(negedge clk); gov='0; gov.op=8'ha4; gov.rep=1; gov.prefix286=2; ilen=2;
      accesses=2; boundary=1; eoi=(i==63); element=1;
    end
    @(negedge clk); element=0; boundary=0; eoi=0; accesses=0;
    finish_trial(389,268,"REP MOVS crosses two execution chunks");
    restart;
    for (int i=0; i<22; i++) begin
      @(negedge clk); gov='0; gov.op=8'haa; gov.rep=1; gov.prefix286=2; ilen=2;
      accesses=1; boundary=1; eoi=(i==21); element=1;
    end
    @(negedge clk); element=0; boundary=0; eoi=0; accesses=0;
    finish_trial(92,116,"REP STOS crosses chunk and retains refill fraction");
    restart;
    for (int i=0; i<22; i++) begin
      @(negedge clk); gov='0; gov.op=8'hac; gov.rep=1; gov.prefix286=2; ilen=2;
      accesses=1; boundary=1; eoi=(i==21); element=1;
    end
    @(negedge clk); element=0; boundary=0; eoi=0; accesses=0;
    finish_trial(115,116,"REP LODS crosses chunk and retains refill fraction");
    restart;
    for (int i=0; i<14; i++) begin
      @(negedge clk); gov='0; gov.op=8'hae; gov.rep=1; gov.prefix286=2; ilen=2;
      accesses=1; boundary=1; eoi=(i==13); element=1;
    end
    @(negedge clk); element=0; boundary=0; eoi=0; accesses=0;
    finish_trial(131,118,"REP SCAS crosses chunk");
    restart;
    for (int i=0; i<13; i++) begin
      @(negedge clk); gov='0; gov.op=8'ha4; gov.rep=1; gov.prefix286=2; ilen=2;
      accesses=2; boundary=1; eoi=0; element=1;
    end
    @(negedge clk); boundary=0; element=0; accesses=0; abandon=1;
    @(negedge clk); abandon=0;
    repeat(4) @(negedge clk);
    for (int i=0; i<14; i++) begin
      @(negedge clk); gov='0; gov.op=8'ha4; gov.rep=1; gov.prefix286=2; ilen=2;
      accesses=2; boundary=1; eoi=(i==13); element=1;
    end
    @(negedge clk); element=0; boundary=0; eoi=0; accesses=0;
    finish_trial(167,116,"REP MOVS interrupted and refetched");
    restart;
    for (int i=0; i<26; i++) begin
      @(negedge clk); gov='0; gov.op=8'ha4; gov.rep=1; gov.prefix286=2; ilen=2;
      accesses=2; boundary=1; eoi=0; element=1;
    end
    @(negedge clk); boundary=0; element=0; accesses=0; replay=1;
    @(negedge clk); replay=0;
    repeat(4) @(negedge clk);
    @(negedge clk); gov='0; gov.op=8'ha4; gov.rep=1; gov.prefix286=2; ilen=2;
    accesses=2; boundary=1; eoi=1; element=1;
    @(negedge clk); element=0; boundary=0; eoi=0; accesses=0;
    finish_trial(167,116,"REP MOVS replay preserves chunk budget");
    preset=3; restart;
    instruction(8'hff,0,4,2,0); instruction(8'h83,7,5,1,0); instruction(8'h75,0,2,0,1);
    finish_trial(19,19,"non-286");
    preset=0; restart;
    instruction(8'hff,0,4,2,0); instruction(8'h83,7,5,1,0); instruction(8'h75,0,2,0,1);
    finish_trial(0,0,"max");
    preset=1; turbo=1; restart;
    instruction(8'hff,0,4,2,0); instruction(8'h83,7,5,1,0); instruction(8'h75,0,2,0,1);
    finish_trial(0,0,"turbo");
    $display("PASS reference governor integration"); $finish;
  end
endmodule
