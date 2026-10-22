// SPDX-License-Identifier: MIT
// Synchronous primitive simulation for testing the production ROM branch.
// Deterministic contents indexed by lane name avoid patching its blank INIT.
`define CONTENTS \
 reg [7:0] mem[0:1023]; \
 string path; integer lane, found, pos, idx; \
 initial begin \
  path=$sformatf("%m"); found=0;lane=0; \
  for(pos=0;pos+4<path.len();pos=pos+1) \
   if(path.substr(pos,pos+3)=="lane")begin \
    found=$sscanf(path.substr(pos+4,path.len()-1),"%d",lane); \
   end \
  if(found!=1)$fatal(1,"no lane suffix in %s",path); \
  for(integer i=0;i<1024;i=i+1)begin \
   idx=lane*1024+i;mem[i]=8'((idx*37+(idx>>8)*11+(idx>>10)*17)); \
  end \
 end
module MISTRAL_M10K #(parameter CFG_ABITS=10,CFG_DBITS=10,CFG_ASYNC_READ=0,parameter[10239:0]INIT=0)(
 input wire CLK1,input wire[9:0]A1ADDR,A1DATA,input wire A1EN,B1EN,
 input wire[9:0]B1ADDR,output reg[9:0]B1DATA,input wire ACLR0,ACLR1);
 `CONTENTS
 always @(posedge CLK1)if(B1EN)B1DATA<={2'b00,mem[B1ADDR]};
endmodule
module MISTRAL_M10K_TDP #(parameter CFG_ABITS=10,CFG_DBITS=10,CFG_ASYNC_READ=0,parameter[10239:0]INIT=0)(
 input wire CLK1,CLK2,input wire[9:0]A1ADDR,B1ADDR,A1DATA,B1DATA,
 output reg[9:0]A1Q,B1Q,input wire A1EN,B1EN,A1WE,B1WE,ACLR0,ACLR1);
 `CONTENTS
 always @(posedge CLK1)if(A1EN)A1Q<={2'b00,mem[A1ADDR]};
 always @(posedge CLK2)if(B1EN)B1Q<={2'b00,mem[B1ADDR]};
endmodule
`undef CONTENTS
