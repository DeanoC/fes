#!/usr/bin/env python3
"""Reproduce the PC/XT budget preflight from immutable nextpnr evidence.

No routes, board measurements, SDC generation or acceptance decisions occur.
"""
import argparse
import gzip
import hashlib
import json
from pathlib import Path
import subprocess

REV = "dee2b36890255a3c9c9b85ceff74e83095d7c9d0"
EVIDENCE = "docs/validation/2026-10-20-unregistered-pads-265/"


def audit(repo):
    hashes = {}

    def read(path):
        raw = subprocess.check_output(["git", "-C", str(repo), "show", f"{REV}:{path}"])
        hashes[path] = hashlib.sha256(raw).hexdigest()
        return json.loads(gzip.decompress(raw) if path.endswith(".gz") else raw)

    snapshot = read(EVIDENCE + "host-final.json.gz")["unregistered"]
    hps = read(EVIDENCE + "hps-reference.json")
    assert hps["status"] == "unsupported-peripheral-launch"
    assert hps["clock_relation"] is None
    for corner in hps["source_trace"].values():
        assert corner["i2c~FF_4397"][5] == ""
        for name in ("i2c~FF_4791", "i2c~FF_4792"):
            assert corner[name][5] == "i2c~FF_4397"
    assert snapshot["unsupported"] == [["scl", "OE"], ["sda", "OE"]]
    clock = snapshot["registered_contracts"][json.dumps(["SDRAM_CLK_MISTRAL_OB_PAD", "PAD", False])]
    assert clock["type"] == "MISTRAL_DDROUT"
    assert int(clock["params"]["DDR_HIGH"], 2) == 0
    assert clock["attrs"]["LOC"] == "PIN_AD20"
    channels = clock["channels"]
    assert channels["rise"]["edge"] == 1 and channels["fall"]["edge"] == 0
    waveform = snapshot["clocks"]["clocks[0]"]
    assert waveform[:6] == [10000, 10000, 5000, 5000, 5000, 5000]

    def pulses(rise, fall):
        # Inverted forwarder: chip high spans the fabric low interval.
        # Ideal upstream waveform and zero board spread/uncertainty are an
        # optimistic diagnostic. Pad intervals remain independent extrema.
        high = waveform[4] + fall[0] - rise[1]
        low = waveform[2] + rise[0] - fall[1]
        return {"rise_interval_ps": rise, "fall_interval_ps": fall,
                "high_min_ps": high, "low_min_ps": low,
                "minimum_margin_to_reference_2ns_ps": min(high, low) - 2000}

    cases = {"current_model": pulses(channels["rise"]["clock_to_q_rise"],
                                      channels["fall"]["clock_to_q_fall"])}
    observed = {}
    for load in (0, 30):
        data = read(f"mistral/tests/gpio-timing/ramtest-pad-reference{load}.json")
        assert data["ddr_output_pin"] == "AD20" and data["output_load_pf"] == load
        bounds = data["bounds"]
        by_edge = {}
        for edge, phase in (("rise", "low"), ("fall", "high")):
            early = int(bounds[f"write_ddr_{phase}_early"]["minimum"]["value_ps"])
            late = int(bounds[f"write_ddr_{phase}_late"]["maximum"]["value_ps"])
            by_edge[edge] = [early // 10 * 10, (late + 9) // 10 * 10]
        observed[load] = by_edge
        cases[f"observations_at_{load}pf_only"] = pulses(**by_edge)
    pooled = {edge: [min(observed[p][edge][0] for p in observed),
                     max(observed[p][edge][1] for p in observed)] for edge in ("rise", "fall")}
    cases["pooled_0_and_30pf_observations"] = pulses(**pooled)
    assert (cases["current_model"]["high_min_ps"], cases["current_model"]["low_min_ps"]) == (-380, -400)
    assert (cases["pooled_0_and_30pf_observations"]["high_min_ps"], cases["pooled_0_and_30pf_observations"]["low_min_ps"]) == (1660, 1640)

    masks = {}
    for port in ("SDRAM_DQML", "SDRAM_DQMH"):
        cells = snapshot["output_contracts"][port]["cells"]
        assert len(cells) == 1
        name, cell = next(iter(cells.items()))
        assert cell["type"] == "MISTRAL_OB" and cell["pins"]["I"] == 0
        assert "NEXTPNR_GPIO_TIMING_PROFILE" not in cell["attrs"]
        assert json.dumps([name, "I"]) not in snapshot["unregistered_contracts"]
        masks[port] = {"pin": cell["attrs"]["LOC"], "type": cell["type"],
                       "boundary": "routing only; external board connectivity unverified"}
    return {"nextpnr_commit": REV, "status": "blocked-before-physical-budget",
            "physical_signoff": False, "new_routes": 0,
            "reference_part_only": "EM63B165 -6; not identification of Kit A SDRAM",
            "reference_high_low_requirement_ps": 2000,
            "ideal_upstream_and_board_assumption": True,
            "pulse_cases": cases, "mask_outputs": masks,
            "hps_launch_relation": None,
            "unverified_kit_inputs": ["memory part/speed grade and module revision",
                "clock/data/read flight intervals and CS inverter delay",
                "pad loads and uncertainty", "high-Z bus turnaround",
                "HPS peripheral clock/configuration and hard-IP boundary timing",
                "I2C pullups, capacitance and receiver timing"],
            "source_sha256": hashes}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--nextpnr", type=Path, required=True,
                        help="Git checkout containing the pinned evidence commit")
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    result = audit(args.nextpnr)
    if args.output:
        args.output.write_text(json.dumps(result, indent=2) + "\n")
    else:
        assert result == json.loads(Path(__file__).with_name("result.json").read_text())
        print("PASS: retained budget preflight reproduced; physical budget remains blocked")
