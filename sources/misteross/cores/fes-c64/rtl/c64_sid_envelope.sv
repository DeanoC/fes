// SPDX-License-Identifier: GPL-2.0-or-later
// Digital ADSR pipeline derived from reSID EnvelopeGenerator.
// Copyright (C) 2010 Dag Lem <resid@nimrod.no>
module c64_sid_envelope #(
    // The caller guarantees an idle SYS edge between Phi edges.
    parameter bit PREPARE_STATE = 1'b0
) (
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
    // Apply writes to the supplied state: clocked state on Phi, current state
    // on idle edges. Preparation uses the latter explicitly, avoiding the full
    // Phi transition in its own combinational input path.
    function automatic state_t write_registers(input state_t current,
        input write_enable, input [1:0] write_address, input [7:0] write_data);
        state_t written;
        begin
            written=current;
            if(write_enable) case(write_address)
            0:if(written.gate!=write_data[0]) begin
                written.next_state=write_data[0] ? ATTACK : RELEASE;
                if(write_data[0]) begin
                    written.state=DECAY; written.period=rate_period(written.decay); written.state_pipe=2;
                    if(written.reset_rate || written.exp_pipe==2)
                        written.envelope_pipe=(written.exp_period==1 || written.exp_pipe==2) ? 2 : 4;
                    else if(written.exp_pipe==1) written.state_pipe=3;
                end else written.state_pipe=written.envelope_pipe!=0 ? 3 : 2;
                written.gate=write_data[0];
            end
            1:begin
                written.attack=write_data[7:4]; written.decay=write_data[3:0];
                if(written.state==ATTACK) written.period=rate_period(written.attack);
                else if(written.state==DECAY) written.period=rate_period(written.decay);
            end
            2:begin
                written.sustain=write_data[7:4]; written.release_rate=write_data[3:0];
                if(written.state==RELEASE) written.period=rate_period(written.release_rate);
            end
            default:;
            endcase
            write_registers=written;
        end
    endfunction

    typedef struct packed {
        logic [1:0] state_pipe, state;
        logic [14:0] period;
        logic hold_zero;
    } prepared_t;
    function automatic prepared_t first_transition(input state_t current);
        prepared_t advanced;
        begin
            advanced={current.state_pipe,current.state,current.period,current.hold_zero};
            if(advanced.state_pipe!=0) begin
                advanced.state_pipe=advanced.state_pipe-1'b1;
                if(current.next_state==ATTACK && advanced.state_pipe==0) begin
                    advanced.state=ATTACK;
                    advanced.period=rate_period(current.attack);
                    advanced.hold_zero=0;
                end else if(current.next_state==RELEASE &&
                    ((advanced.state==ATTACK && advanced.state_pipe==0) ||
                     (advanced.state==DECAY && advanced.state_pipe==1))) begin
                    advanced.state=RELEASE;
                    advanced.period=rate_period(current.release_rate);
                end
            end
            first_transition=advanced;
        end
    endfunction

    wire [19:0] transition_value;
    generate if(PREPARE_STATE) begin : prepared_state
        // E(power-up q) is all zero, including its initial ATTACK state.
        prepared_t value='0;
        always @(posedge clk)
            if(reset) value<={2'd0,RELEASE,15'd8,1'b0};
            else if(!phi) value<=first_transition(write_registers(q,we,addr,din));
        assign transition_value=value;
    end else begin : direct_state
        assign transition_value=first_transition(q);
    end endgenerate

    always @* begin
        n=q;
        if (phi) begin
            n.sampled=n.value;
            {n.state_pipe,n.state,n.period,n.hold_zero}=transition_value;
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
        n=write_registers(n,we,addr,din);
        if(reset) begin
            n='0; n.value=q.value; n.sampled=q.sampled;
            n.exp_period=1; n.state=RELEASE; n.next_state=RELEASE; n.period=8;
        end
    end
    always @(posedge clk) q<=n;
endmodule
