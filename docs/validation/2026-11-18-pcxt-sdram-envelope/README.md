# PC/XT common SDRAM timing envelope, 18 November 2026

**Physical qualification is BLOCKED.** The common pin envelope below extends
the Etron-only reference across multiple chip makers. The retained diagnostic
still leaves only 0.340 ns for additional read setup costs and zero nominal
CL2 cycle-time headroom at 100 MHz. Board electrical/clock bounds are absent.
The conditional production build, seal and exact-artifact HIL therefore do
not start. No constraint is relaxed and the new path remains default-off.

Base: `5e592f00389fd2bb61194788c917c36f5c880dec`, after merged
[#1954](https://github.com/DeanoC/fes/pull/1954). Owner: misteross for FPGA
timing, with FES owning this integration evidence. No RTL, producer, lock,
shared ABI, runtime, image or package-selection change is made.

## Common requirements, independent of chip brand

Deano requested conservative support across DE10/MiSTer SDRAM boards rather
than specialization for Kit A's Etron population. Kit A remains one physical
test target, not the definition of support. A different brand meeting the same
electrical, timing, protocol and capacity requirements needs no vendor-specific
RTL. A connector or a successful boot alone does not establish those properties.

[devices.json](devices.json) records the manufacturer specifications and exact
speed grades compared. Etron and Alliance primary PDFs are retained with hashes;
ISSI and Winbond manufacturer documents were read through the linked distributor
mirrors because direct downloads were blocked. Their values are datasheet
requirements, not observed kit populations or proof that every shipped module
uses one of these parts. Published MiSTer hardware names Alliance -7, while
Retro Remake's schematic names -6. Both are included.

| Family / reviewed grades | CL2 minimum period | CL3 minimum period | CL2 maximum access | Minimum output hold | Required input hold | Maximum CL2 DQ release |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Etron EM63B165TS -6/-7 | 10 ns | 7 ns | 6 ns | 2.5 ns | 0.8 ns | 6 ns |
| Alliance AS4C32M16SB -6/-7 | 10 ns | 7 ns | 6 ns | 2.5 ns | 0.8 ns | 5.4 ns |
| ISSI IS42S16320F -5/-6/-7 | 10 ns | 7 ns | 6 ns | 2.5 ns | 0.8 ns | 6 ns |
| Winbond W9825G6KH grades listed in JSON | 10 ns | 7.5 ns | 6 ns | 3 ns | 1 ns | 6 ns |
| **Common conservative envelope** | **10 ns** | **7.5 ns** | **6 ns** | **2.5 ns** | **1 ns** | **6 ns** |

Rows take the most demanding value across the listed grades, so a row need not
describe one individual grade. Input setup is at least 1.5 ns, clock high/low
are at least 2.5/2.5 ns, and earliest DQ low impedance is 0 ns. The common
operating intersection is 3.0–3.6 V and 0–70 °C. Industrial device grades do not
extend commercial parts' range. Read hold uses the **smallest guaranteed**
output hold; receiver setup/hold and release limits use the largest requirement.

Reference loads differ: Etron/Alliance/Winbond use 30 pF AC loads, ISSI uses
50 pF. These are distinct test circuits, not permission to substitute a single
arbitrary capacitance for any board. Device-specific slew compensation,
thresholds, signal integrity and physical loads remain mandatory. Other chip
families can meet this envelope without a brand whitelist; this comparison is
not an exhaustive inventory or a universal acceptance claim. Command spacing,
initialization, refresh and geometry also need qualification; this pin envelope
alone cannot qualify a smaller memory's address map or refresh organization.

## Retained fit assessment

[assess.py](assess.py) checks the diagnostic payload and all 568 raw channel
rows against the hashes in #1954's retained result, then recalculates the
cross-vendor limits. It uses the same proved selected-word relationship:
algebraic +10 ns setup / -10 ns hold, preserving the raw periodic failures.
It adds no TimeQuest exception and runs no new STA or fit.

| Conditional minimum across four FPGA corners | Setup | Hold |
| --- | ---: | ---: |
| Selected first read word | +0.340 ns | +0.912 ns |
| Direct command/address | +3.578 ns | +0.669 ns |
| Conditional second-CS inverter | +0.131 ns | +1.676 ns |

The common 1 ns receiver hold requirement consumes 0.2 ns more than the old
0.8 ns reference. The second-CS row still assumes the unverified TI inverter
reference; it is not generic module-topology acceptance. The release envelope
increases to 6 ns, but no electrical OE/turnaround margin has been established.
The existing trace is a logical selected-word contract, not a physical chip
or board model. See [result.json](result.json) for worst ports/corners and hashes.

At the retained FPGA clock/data paths and electrical assumptions, a sufficient
read setup requirement is `Fc_max + Fr_max + additional adverse timing <=
0.340 ns`, where Fc is FPGA clock-pad to memory clock flight and Fr is memory
DQ to FPGA data-pad flight, including motherboard, connector and module. Clock
and returning-data flight add; they do not cancel as output skew can. The
additional term includes any adverse change in access, loading/slew or clock
uncertainty relative to the reference. It is not a measured flight allowance.
Electrical model changes can alter fitted clock/data paths and require fresh
STA rather than this arithmetic alone.

For outputs, the reference delay formulas remain
`output_max = tIS + Fw_max - Fc_min` and
`output_min = Fw_min - Fc_max - tIH`.
The table's setup margin bounds adverse data-minus-clock flight and its hold
margin bounds adverse clock-minus-data flight, with other changes included.
Second-CS propagation is already charged at 1–4.2 ns in its conditional row;
do not charge it twice or assume every module has that inverter.

The CL2 minimum period is already 10 ns before physical clock bounds. A
10 ns nominal period cannot budget any positive frequency error or cycle
shortening; applicable slew compensation can make the requirement stricter.
This is an unclosed worst-case gate, not a measured claim that Kit A fails.
CL3's common 7.5 ns device cycle limit offers 2.5 ns nominal cycle headroom
at 100 MHz, but **is not implemented or qualified here**. Changing CAS latency
requires a coordinated controller/selected-word/deadline proof and new fit;
changing the mode bits alone or reusing the CL2 proof would be invalid.

## Physical evidence and stop point

The manufacturer repository was checked at
[`787c33cd60fe0c1f3df9a369744b675022676c2d`](https://github.com/Retro-Remake/Hardware_MiSTER-Pi/tree/787c33cd60fe0c1f3df9a369744b675022676c2d).
Its complete recursive inventory has an SDRAM schematic and a motherboard
mechanical PDF, but no SDRAM/motherboard routed layout, stackup or extracted
flight report. The schematic establishes shared bus topology, tied CKE,
address-pin masks and CS inversion; it cannot supply route length or flight.
Its Alliance population does not supersede Kit A's operator-confirmed Etron
identity. No flight value is inferred from a product photo or an unrelated
XS-DS layout. Scope of this search is the published manufacturer tree, not an
exhaustive search for private production design files.

The retained native STA report has zero-length board transmission-line models
and open far-end loads on SDRAM pins. Fitter output choices include 16 mA,
slew setting 1. Those settings and internal pulse checks do not establish
memory-pin rise/fall time, loading, pulse width or electrical acceptance.
On 18 November Deano confirmed there are no existing timing measurements or
remotely accessible scope. No kit access or claim was needed for this audit.

Protected evidence: `/home/deano/fes-hil/k835-etron-board-timing/`, including
the manufacturer tree, original PDFs/text, selected source metadata and raw
native board-model excerpt. [board-sources.json](board-sources.json) binds the
published inputs. Original fit/proof databases remain untouched under
`/home/deano/fes-hil/k835-capture-path/`.

The next integration step is a controller/capture design with a positive
100 MHz cycle budget across this common envelope, together with justified
board flight/load/clock and electrical-turnaround bounds. CAS-3 is a candidate
design investigation, not adopted production behavior. Once those bounds and
production STA pass, run the registered producer with fresh design/record
identity, all declared seeds and the independent repeat, require the normal
seal, and run exact-package/payload HIL with kit claim and verified baseline
restore. The user authorized those later stages **if timing passes**; that
condition has not been met. #835 remains open. No production build, seal or
HIL PASS is claimed or waived.

## Reproduce

```sh
python3 docs/validation/2026-11-18-pcxt-sdram-envelope/assess.py \
  --fit /home/deano/fes-hil/k835-capture-path/coordinated-fit-retained/seed-1 \
  --output /tmp/pcxt-sdram-envelope.json
```

Expected exit is **2**, with physical timing `BLOCKED`. Malformed/modified raw
channel or payload evidence rejects instead of generating an assessment.
This command is host-only arithmetic and cannot authorize production or HIL.
