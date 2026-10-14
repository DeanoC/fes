// SPDX-License-Identifier: GPL-2.0-or-later
// Digital ADSR pipeline derived from reSID EnvelopeGenerator.
// Copyright (C) 2010 Dag Lem <resid@nimrod.no>
module c64_sid_envelope (
    input wire clk, reset, phi, we,
    input wire [1:0] addr,
    input wire [7:0] din,
    output wire [7:0] level, env3
);
    localparam [1:0] ATTACK=0, DECAY=1, RELEASE=2;
    typedef struct packed {
        logic [14:0] rate;
        logic [14:0] period;
        logic [4:0] exponential, exp_period;
        logic [7:0] value, sampled;
        logic [2:0] envelope_pipe;
        logic [1:0] exp_pipe, state_pipe;
        logic hold_zero, reset_rate;
        logic [3:0] attack, decay, sustain, release_rate;
        logic gate;
        logic [1:0] state, next_state;
    } state_t;
    state_t q, n;
    initial begin q='0; q.value=8'haa; end
    assign level=q.value;
    assign env3=q.sampled;
    function automatic [14:0] rate_period(input [3:0] rate);
        case(rate)
        0:rate_period=8; 1:rate_period=31; 2:rate_period=62; 3:rate_period=94;
        4:rate_period=148; 5:rate_period=219; 6:rate_period=266; 7:rate_period=312;
        8:rate_period=391; 9:rate_period=976; 10:rate_period=1953; 11:rate_period=3125;
        12:rate_period=3906; 13:rate_period=11719; 14:rate_period=19531; 15:rate_period=31250;
        endcase
    endfunction
    always @* begin
        n=q;
        if (phi) begin
            n.sampled=n.value;
            if (n.state_pipe!=0) begin
                n.state_pipe=n.state_pipe-1'b1;
                if(n.next_state==ATTACK && n.state_pipe==0) begin
                    n.state=ATTACK; n.period=rate_period(n.attack); n.hold_zero=0;
                end else if(n.next_state==RELEASE &&
                    ((n.state==ATTACK && n.state_pipe==0) || (n.state==DECAY && n.state_pipe==1))) begin
                    n.state=RELEASE; n.period=rate_period(n.release_rate);
                end
            end
            if(n.envelope_pipe!=0) begin
                n.envelope_pipe=n.envelope_pipe-1'b1;
                if(n.envelope_pipe==0 && !n.hold_zero) begin
                    if(n.state==ATTACK) begin
                        n.value=n.value+1'b1;
                        if(n.value==255) begin n.state=DECAY; n.period=rate_period(n.decay); end
                    end else if(n.state==DECAY || n.state==RELEASE) n.value=n.value-1'b1;
                    case(n.value)
                    255:n.exp_period=1; 93:n.exp_period=2; 54:n.exp_period=4;
                    26:n.exp_period=8; 14:n.exp_period=16; 6:n.exp_period=30;
                    0:begin n.exp_period=1; n.hold_zero=1; end
                    default:;
                    endcase
                end
            end
            if(n.exp_pipe!=0 && n.exp_pipe==1) begin
                n.exp_pipe=0; n.exponential=0;
                if((n.state==DECAY && n.value!={n.sustain,n.sustain}) || n.state==RELEASE)
                    n.envelope_pipe=1;
            end else begin
                if(n.exp_pipe!=0) n.exp_pipe=n.exp_pipe-1'b1;
                if(n.reset_rate) begin
                    n.rate=0; n.reset_rate=0;
                    if(n.state==ATTACK) begin n.exponential=0; n.envelope_pipe=2; end
                    else if(!n.hold_zero) begin
                        n.exponential=n.exponential+1'b1;
                        if(n.exponential==n.exp_period) n.exp_pipe=n.exp_period!=1 ? 2 : 1;
                    end
                end
            end
            if(n.rate!=n.period) n.rate=n.rate==15'h7fff ? 15'd1 : n.rate+1'b1;
            else n.reset_rate=1;
        end
        // CPU writes follow this cycle's digital envelope clock.
        if(we) case(addr)
        0:if(n.gate!=din[0]) begin
            n.next_state=din[0] ? ATTACK : RELEASE;
            if(din[0]) begin
                n.state=DECAY; n.period=rate_period(n.decay); n.state_pipe=2;
                if(n.reset_rate || n.exp_pipe==2)
                    n.envelope_pipe=(n.exp_period==1 || n.exp_pipe==2) ? 2 : 4;
                else if(n.exp_pipe==1) n.state_pipe=3;
            end else n.state_pipe=n.envelope_pipe!=0 ? 3 : 2;
            n.gate=din[0];
        end
        1:begin
            n.attack=din[7:4]; n.decay=din[3:0];
            if(n.state==ATTACK) n.period=rate_period(n.attack);
            else if(n.state==DECAY) n.period=rate_period(n.decay);
        end
        2:begin
            n.sustain=din[7:4]; n.release_rate=din[3:0];
            if(n.state==RELEASE) n.period=rate_period(n.release_rate);
        end
        default:;
        endcase
        if(reset) begin
            n='0; n.value=q.value; n.sampled=q.sampled;
            n.exp_period=1; n.state=RELEASE; n.next_state=RELEASE; n.period=8;
        end
    end
    always @(posedge clk) q<=n;
endmodule
