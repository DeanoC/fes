// SPDX-License-Identifier: MIT
// Default-off reference-model integration checks against the real governor and cost ROM.
`timescale 1ns/1ps
module governor_integration_tb;
  import fes_i386_pkg::*;
  logic clk=0; always #1 clk=~clk;
  logic reset=1, boundary=0,eoi=0,element=0,taken=0,redir=0,abandon=0,replay=0;
  gov_t gov='0;
  logic [3:0] ilen=0; logic [1:0] accesses=0; logic ordinary_ram=1; int saved_queue; logic [1:0] mmio=0;
  logic [2:0] preset=1; logic turbo=0;
  logic hold0,hold1; logic [11:0] debt0,debt1; logic [12:0] charge0,charge1;
  logic tally=0; int sum0=0,sum1=0;
  always @(posedge clk) if(tally) begin sum0 += int'(charge0); sum1 += int'(charge1); end
  fes_i386_governor #(.GOV_N(1),.GOV_D(1000)) baseline (
    .clk,.reset,.preset,.frequency(10'd0),.turbo,
    .boundary,.eoi,.element,.taken,.gov,.ilen,.redirect(redir),.abandon,.replay,
    .accesses,.dword(1'b0),.ordinary_ram,.io(1'b0),.io_port(16'd0),.io_size(2'd0),.mmio,
    .mmio_halves(3'd1),.mmio_st2(1'b0),.mmio_st2_halves(3'd0),.hold(hold0),.debt_clocks(debt0),.charge(charge0)
  );
  fes_i386_governor #(.GOV_N(1),.GOV_D(1000),.REFERENCE_DIAGNOSTIC(1)) candidate (
    .clk,.reset,.preset,.frequency(10'd0),.turbo,
    .boundary,.eoi,.element,.taken,.gov,.ilen,.redirect(redir),.abandon,.replay,
    .accesses,.dword(1'b0),.ordinary_ram,.io(1'b0),.io_port(16'd0),.io_size(2'd0),.mmio,
    .mmio_halves(3'd1),.mmio_st2(1'b0),.mmio_st2_halves(3'd0),.hold(hold1),.debt_clocks(debt1),.charge(charge1)
  );
  task automatic instruction(input byte op, input int regfield, nbytes, naccess, input bit branch_taken);
    @(negedge clk); gov='0; gov.op=op; gov.mreg=3'(regfield); ilen=4'(nbytes); accesses=2'(naccess);
    boundary=1; eoi=1; taken=branch_taken; redir=branch_taken;
  endtask
  task automatic restart;
    @(negedge clk); tally=0; reset=1; boundary=0; eoi=0; accesses=0; mmio=0; abandon=0; replay=0; redir=0; taken=0; ordinary_ram=1; gov='0;
    repeat(5) @(negedge clk); reset=0; repeat(8) @(negedge clk); sum0=0; sum1=0; tally=1;
  endtask
  task automatic finish_trial(input int expected0,expected1, input string label);
    @(negedge clk); boundary=0; eoi=0; taken=0; redir=0; accesses=0; mmio=0; gov='0; abandon=0; replay=0;
    repeat(8) @(negedge clk); tally=0;
    if(sum0!=expected0 || sum1!=expected1) $fatal(1,"%s base=%0d expected=%0d candidate=%0d expected=%0d",label,sum0,expected0,sum1,expected1);
  endtask
  task automatic mixed_check(input byte op, input int nbytes, naccess, expected, expected_length, input bit ram, is_rep, excluded, enter_zero, stack32, baseline_cost);
    restart;
    instruction(op,0,nbytes,naccess,0);
    gov.rep=is_rep; gov.ref_test_excluded=excluded; gov.ref_enter_valid=1; gov.ref_enter_level=enter_zero ? 5'd0 : 5'd1; gov.ref_stack32=stack32; ordinary_ram=ram;
    @(negedge clk); boundary=0; eoi=0; accesses=0; ordinary_ram=1;
    @(negedge clk);
    if (int'(candidate.execution_clocks) != expected || (baseline_cost && candidate.execution_clocks != baseline.execution_clocks))
      $fatal(1,"mixed candidate op=%h rep=%0d excluded=%0d enter0=%0d stack32=%0d ram=%0d actual=%0d expected=%0d baseline=%0d",op,is_rep,excluded,enter_zero,stack32,ram,candidate.execution_clocks,expected,baseline.execution_clocks);
    if (int'(candidate.gen_reference_queue.length) != expected_length)
      $fatal(1,"mixed queue length op=%h actual=%0d expected=%0d",op,candidate.gen_reference_queue.length,expected_length);
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
    // TEST with a memory operand has separate execution and overlap clocks.
    restart;
    instruction(8'hf6,0,3,1,0);
    @(negedge clk); boundary=0; eoi=0; accesses=0;
    @(negedge clk);
    if (candidate.execution_clocks != 5 || candidate.refill_execution != 5)
      $fatal(1,"immediate byte TEST timing");
    finish_trial(7,9,"immediate byte TEST");
    restart;
    instruction(8'hf7,0,4,1,0);
    @(negedge clk); boundary=0; eoi=0; accesses=0;
    @(negedge clk);
    if (candidate.execution_clocks != 5 || candidate.refill_execution != 5)
      $fatal(1,"immediate word TEST timing");
    finish_trial(7,9,"immediate word TEST");
    restart;
    instruction(8'h84,0,2,1,0);
    @(negedge clk); boundary=0; eoi=0; accesses=0;
    @(negedge clk);
    if (candidate.execution_clocks != 5 || candidate.refill_execution != 7)
      $fatal(1,"register/memory byte TEST timing");
    finish_trial(7,7,"register/memory byte TEST");
    restart;
    instruction(8'h85,0,2,1,0);
    @(negedge clk); boundary=0; eoi=0; accesses=0;
    @(negedge clk);
    if (candidate.execution_clocks != 5 || candidate.refill_execution != 7)
      $fatal(1,"register/memory word TEST timing");
    finish_trial(7,7,"register/memory word TEST");
    restart;
    instruction(8'hf6,1,3,1,0);
    @(negedge clk); boundary=0; eoi=0; accesses=0;
    @(negedge clk);
    if (candidate.execution_clocks != 6 || candidate.refill_execution != 6)
      $fatal(1,"unmeasured TEST extension changed");
    finish_trial(7,10,"unmeasured TEST extension");
    restart;
    instruction(8'hf7,2,2,2,0);
    @(negedge clk); boundary=0; eoi=0; accesses=0;
    @(negedge clk);
    if (candidate.execution_clocks != 7 || candidate.refill_execution != 7)
      $fatal(1,"unrelated F7 unary form changed");
    finish_trial(9,9,"unrelated unary memory form");
    restart;
    instruction(8'h85,0,2,0,0); gov.rm_reg=1;
    @(negedge clk); boundary=0; eoi=0; accesses=0;
    @(negedge clk);
    if (candidate.execution_clocks != 2 || candidate.refill_execution != 2)
      $fatal(1,"register-only TEST changed");
    finish_trial(2,4,"register-only TEST");
    restart;
    instruction(8'hf7,0,6,1,0); gov.o32=1;
    @(negedge clk); boundary=0; eoi=0; accesses=0;
    @(negedge clk);
    if (candidate.execution_clocks != 6 || candidate.refill_execution != 6)
      $fatal(1,"32-bit TEST fallback changed");
    finish_trial(7,12,"32-bit TEST fallback");
    restart;
    instruction(8'h84,0,4,1,0); gov.ref_test_excluded=1;
    @(negedge clk); boundary=0; eoi=0; accesses=0;
    @(negedge clk);
    if (candidate.execution_clocks != 6 || candidate.refill_execution != 6)
      $fatal(1,"32-bit address TEST fallback changed");
    finish_trial(7,10,"32-bit address TEST fallback");
    restart;
    instruction(8'hd3,4,2,2,0); gov.ref_shift_nonzero=0;
    @(negedge clk); boundary=0; eoi=0; accesses=0;
    @(negedge clk);
    if (!candidate.reference_zero_shift || candidate.execution_clocks != 0)
      $fatal(1,"zero-count RAM did not select zero handling");
    finish_trial(14,0,"zero-count RAM");
    restart;
    instruction(8'hd3,4,2,2,0); gov.ref_shift_nonzero=1;
    @(negedge clk); boundary=0; eoi=0; accesses=0;
    @(negedge clk);
    if (candidate.reference_zero_shift || candidate.execution_clocks != 7)
      $fatal(1,"nonzero memory shift did not select normal handling");
    finish_trial(14,9,"nonzero-count RAM");
    restart;
    instruction(8'hd3,4,2,2,0); gov.ref_shift_nonzero=0; ordinary_ram=0;
    @(negedge clk); boundary=0; eoi=0; accesses=0; ordinary_ram=1;
    @(negedge clk);
    if (candidate.reference_zero_shift || candidate.execution_clocks != 12)
      $fatal(1,"unknown operand did not retain prior diagnostic path");
    finish_trial(14,14,"unknown zero-count operand");
    restart;
    instruction(8'hd3,4,2,2,0); gov.ref_shift_nonzero=0; ordinary_ram=0; mmio=1;
    @(negedge clk); boundary=0; eoi=0; accesses=0; ordinary_ram=1; mmio=0;
    @(negedge clk);
    if (candidate.reference_zero_shift || candidate.execution_clocks != 12)
      $fatal(1,"MMIO did not retain prior diagnostic path");
    repeat(8) @(negedge clk); tally=0;
    restart;
    instruction(8'h90,0,1,0,0);
    @(negedge clk); boundary=0; eoi=0; accesses=0;
    repeat(4) @(negedge clk);
    saved_queue = int'(candidate.gen_reference_queue.queue_bytes);
    instruction(8'hd3,4,2,2,0); gov.ref_shift_nonzero=0;
    @(negedge clk); boundary=0; eoi=0; accesses=0;
    repeat(4) @(negedge clk);
    if (int'(candidate.gen_reference_queue.queue_bytes) != saved_queue)
      $fatal(1,"zero-count RAM altered queue");
    tally=0;
    restart;
    for (int i=0; i<8; i++) begin
      @(negedge clk); gov='0; gov.op=8'h60; ilen=1; accesses=1;
      boundary=(i==7); eoi=(i==7);
    end
    @(negedge clk); boundary=0; eoi=0; accesses=0;
    @(negedge clk);
    if (candidate.execution_clocks != 18 || candidate.s2_data_bus != 16)
      $fatal(1,"PUSHA execution or bus accumulation");
    finish_trial(25,20,"PUSHA eight committed stores");
    restart;
    for (int i=0; i<7; i++) begin
      @(negedge clk); gov='0; gov.op=8'h61; ilen=1; accesses=1;
      boundary=(i==6); eoi=(i==6);
    end
    @(negedge clk); boundary=0; eoi=0; accesses=0;
    @(negedge clk);
    if (candidate.execution_clocks != 24 || candidate.s2_data_bus != 14)
      $fatal(1,"POPA execution or bus accumulation");
    finish_trial(26,26,"POPA seven committed loads");
    restart;
    instruction(8'h8d,0,3,0,0); gov.ea3=1;
    @(negedge clk); boundary=0; eoi=0; accesses=0;
    @(negedge clk);
    if (candidate.execution_clocks != 2 || !candidate.s2_ea)
      $fatal(1,"complex LEA input classification");
    finish_trial(4,6,"complex LEA without diagnostic EA surcharge");
    restart;
    instruction(8'hf6,4,2,1,0);
    @(negedge clk); boundary=0; eoi=0; accesses=0;
    @(negedge clk);
    if (candidate.execution_clocks != 13 || candidate.refill_execution != 13)
      $fatal(1,"byte MUL memory reference budget");
    finish_trial(17,15,"byte MUL memory");
    restart;
    instruction(8'hf6,6,2,1,0);
    @(negedge clk); boundary=0; eoi=0; accesses=0;
    @(negedge clk);
    if (candidate.execution_clocks != 14 || candidate.refill_execution != 14)
      $fatal(1,"byte DIV memory reference budget");
    finish_trial(18,16,"byte DIV memory");
    restart;
    instruction(8'hc1,4,3,2,0); gov.ref_shift_nonzero=0;
    @(negedge clk); boundary=0; eoi=0; accesses=0;
    repeat(4) @(negedge clk);
    if (candidate.reference_pending != 1 || candidate.gen_reference_queue.queue_bytes != 0)
      $fatal(1,"immediate zero did not defer one fetch byte");
    instruction(8'h90,0,2,0,0);
    @(negedge clk); boundary=0; eoi=0; accesses=0;
    repeat(4) @(negedge clk);
    if (candidate.reference_pending != 0)
      $fatal(1,"normal instruction failed to consume deferred byte");
    tally=0;
    restart;
    instruction(8'hd3,4,4,2,0); gov.ref_shift_nonzero=0; gov.ref_prefix_count=2; gov.prefix286=8;
    @(negedge clk); boundary=0; eoi=0; accesses=0;
    repeat(4) @(negedge clk);
    if (candidate.reference_pending != 2 || candidate.gen_reference_queue.queue_bytes != 0 || sum1 != 8)
      $fatal(1,"two segment prefixes were not charged and deferred");
    tally=0;
    restart;
    instruction(8'hd3,4,4,2,0); gov.ref_shift_nonzero=0; gov.ea3=1;
    @(negedge clk); boundary=0; eoi=0; accesses=0;
    @(negedge clk);
    if (!candidate.reference_zero_shift || candidate.execution_clocks != 0)
      $fatal(1,"complex-EA zero not selected");
    finish_trial(15,0,"complex-EA zero reference handling");
    restart;
    instruction(8'hd3,4,4,2,0); gov.ref_shift_nonzero=0; gov.ref_zero_unsafe=1;
    @(negedge clk); boundary=0; eoi=0; accesses=0;
    @(negedge clk);
    if (candidate.reference_zero_shift || candidate.execution_clocks != 12)
      $fatal(1,"unsupported prefix did not retain prior path");
    finish_trial(14,16,"unsupported-prefix fallback");
    restart;
    for (int i=0; i<4096; i++) begin
      @(negedge clk); gov='0; gov.op=8'hc1; gov.mreg=4; gov.ref_shift_nonzero=0;
      ilen=3; accesses=2; boundary=1; eoi=1;
    end
    @(negedge clk); boundary=0; eoi=0; accesses=0;
    repeat(5) @(negedge clk);
    if (candidate.reference_pending != 0)
      $fatal(1,"pending boundary did not drain");
    finish_trial(57344,4110,"pending bound fallback does not wrap");
    restart;
    for (int i=0; i<8; i++) begin
      @(negedge clk); gov='0; gov.op=8'h60; ilen=1; accesses=1; ordinary_ram=(i!=2);
      boundary=(i==7); eoi=(i==7);
    end
    @(negedge clk); boundary=0; eoi=0; accesses=0; ordinary_ram=1;
    @(negedge clk);
    if (candidate.s2_ordinary_ram || candidate.execution_clocks != 17)
      $fatal(1,"mixed-access PUSHA failed to retain prior path");
    finish_trial(25,19,"mixed-access PUSHA fallback");
    restart;
    instruction(8'hd3,4,2,2,0); gov.ref_shift_nonzero=1; ordinary_ram=0;
    @(negedge clk); boundary=0; eoi=0; accesses=0; ordinary_ram=1;
    @(negedge clk);
    if (candidate.s2_ordinary_ram || candidate.execution_clocks != 12)
      $fatal(1,"nonzero shift outside RAM changed");
    finish_trial(14,14,"nonzero shift outside RAM");
    restart;
    instruction(8'hf6,4,2,1,0); ordinary_ram=0;
    @(negedge clk); boundary=0; eoi=0; accesses=0; ordinary_ram=1;
    @(negedge clk);
    if (candidate.s2_ordinary_ram || candidate.execution_clocks != 16)
      $fatal(1,"MUL outside RAM changed");
    finish_trial(17,18,"MUL outside RAM");
    restart;
    instruction(8'hf6,0,3,1,0); ordinary_ram=0;
    @(negedge clk); boundary=0; eoi=0; accesses=0; ordinary_ram=1;
    @(negedge clk);
    if (candidate.s2_ordinary_ram || candidate.execution_clocks != 5 || candidate.refill_execution != 5)
      $fatal(1,"pre-existing TEST path outside RAM changed");
    finish_trial(7,9,"pre-existing TEST path outside RAM");
    restart;
    instruction(8'hf6,4,2,1,0); ordinary_ram=0;
    instruction(8'hf6,4,2,1,0); ordinary_ram=1;
    @(negedge clk); boundary=0; eoi=0; accesses=0; ordinary_ram=1;
    @(negedge clk);
    if (!candidate.s2_ordinary_ram || candidate.execution_clocks != 13)
      $fatal(1,"adjacent RAM instruction inherited previous non-RAM access");
    repeat(8) @(negedge clk); tally=0;
    // New candidate form guards, including SS.B and exact encoded nesting.
    mixed_check(8'ha8,2,0,2,2,1,0,0,0,0,0);
    mixed_check(8'ha8,2,0,3,2,1,0,1,0,0,1);
    mixed_check(8'haa,1,1,4,1,1,0,0,0,0,0);
    mixed_check(8'haa,1,1,3,1,0,0,0,0,0,1);
    mixed_check(8'haa,1,1,0,1,1,1,0,0,0,0);
    mixed_check(8'hc8,4,1,10,3,1,0,0,1,0,0);
    mixed_check(8'hc8,4,1,11,4,1,0,0,1,1,1);
    mixed_check(8'hc8,4,1,15,3,1,0,0,0,0,0);
    mixed_check(8'hc8,4,1,11,4,1,0,1,1,0,1);
    mixed_check(8'hc8,4,1,11,4,0,0,0,1,0,1);
    mixed_check(8'hc9,1,1,4,1,1,0,0,0,0,0);
    mixed_check(8'hc9,1,1,5,1,1,0,0,0,1,1);
    restart;
    instruction(8'hc8,0,4,1,0); gov.ref_enter_valid=1; gov.ref_enter_level=31;
    @(negedge clk); boundary=0; eoi=0; accesses=0;
    @(negedge clk);
    if (candidate.execution_clocks != 135 || candidate.gen_reference_queue.length != 3)
      $fatal(1,"ENTER level 31 budget or fetch");
    restart;
    instruction(8'hc8,0,4,1,0); gov.ref_enter_valid=0; gov.ref_enter_level=0;
    @(negedge clk); boundary=0; eoi=0; accesses=0;
    @(negedge clk);
    if (candidate.execution_clocks != baseline.execution_clocks || candidate.gen_reference_queue.length != 4)
      $fatal(1,"ENTER high encoded nesting fallback");
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
