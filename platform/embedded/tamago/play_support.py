"""Exercise real tenfoot Play/Back over QEMU USB against its software backend."""
import json
import re


def states(text):
    return [json.loads(line) for line in re.findall(r"FES_PLAY_STATE ([^\r\n]+)\r?\n", text)]


def events(text):
    return [json.loads(line) for line in re.findall(r"FES_MODEL_EVENT ([^\r\n]+)\r?\n", text)]


def exercise(guest, entries, timeout):
    guest.wait(lambda t: "FES_TENFOOT_READY" in t and bool(states(t)), timeout, "model catalog")
    if "FES_MODEL_BACKEND_READY" not in guest.text or "FES_USB_READY" not in guest.text:
        raise RuntimeError("model backend or USB startup missing")
    shots = [guest.screenshot("catalog")]
    results = []
    for index, entry in enumerate(entries):
        while states(guest.text)[-1]["focus"] != index:
            current = states(guest.text)[-1]
            focus, columns = current["focus"], current["columns"]
            key = "down" if index // columns > focus // columns else "up" if index // columns < focus // columns else "left" if index < focus else "right"
            guest.press(key)
            guest.wait(lambda t: states(t)[-1]["focus"] != focus, timeout, "next model title")
        before = states(guest.text)[-1]["backend"]
        guest.press("ret")
        success = entry["check"] in ("return", "retry")
        def completed(text):
            current = states(text)[-1]
            if current["launch_game"] != entry["id"] or current["backend"]["Busy"]:
                return False
            if success:
                return current["session"] == "active" and current["parked"] and current["backend"]["Status"]["State"] == "running_model"
            return current["launch"] in ("error", "host") and current["session"] == "idle" and not current["parked"] and current["backend"]["Status"]["State"] == "idle"
        guest.wait(completed, timeout, "model launch result: " + entry["id"])
        current = states(guest.text)[-1]
        after = current["backend"]
        if success:
            if after["Status"]["Package"]["ActivationReceiptSHA256"] != entry["receipt_sha256"]:
                raise RuntimeError("tenfoot session published a different receipt")
            if entry.get("composition_id") and after["PartsCompositionID"] != entry["composition_id"]:
                raise RuntimeError("tenfoot published a different part composition")
            shots.append(guest.screenshot(entry["id"] + "-playing"))
            input_count = len([e for e in events(guest.text) if e["kind"] == "input" and e.get("game") == "right"])
            guest.press("right")
            guest.wait(lambda t: len([e for e in events(t) if e["kind"] == "input" and e.get("game") == "right"]) > input_count, timeout, "model gameplay input")
            forwarded = [e for e in events(guest.text) if e["kind"] == "input" and e.get("game") == "right"][-1]
            if forwarded.get("error") or forwarded["status"]["CoreGeneration"] != after["Status"]["CoreGeneration"]:
                raise RuntimeError("gameplay input was not authorized for the displayed generation")
            stale_input = guest.text.count("FES_MODEL_STALE_INPUT PASS")
            stale_frame = guest.text.count("FES_MODEL_STALE_FRAME PASS")
            guest.press("esc")
            if entry["check"] == "retry":
                guest.wait(lambda t: states(t)[-1]["retry"] and not states(t)[-1]["backend"]["Busy"], timeout, "save failure retained session")
                failed = states(guest.text)[-1]["backend"]
                if failed["Status"]["Package"] != after["Status"]["Package"] or failed["Status"]["CoreGeneration"] != after["Status"]["CoreGeneration"] or failed["Status"]["InputEnabled"]:
                    raise RuntimeError("failed save changed the active package or restored input")
                shots.append(guest.screenshot("save-retry"))
                guest.press("esc")
            guest.wait(lambda t: states(t)[-1]["session"] == "idle" and not states(t)[-1]["parked"] and not states(t)[-1]["retry"] and not states(t)[-1]["backend"]["Busy"], timeout, "return to launcher")
            guest.wait(lambda t: t.count("FES_MODEL_STALE_INPUT PASS") > stale_input and t.count("FES_MODEL_STALE_FRAME PASS") > stale_frame, timeout, "stale generation rejection")
            if states(guest.text)[-1]["focus"] != index:
                raise RuntimeError("return lost the selected title")
        else:
            if entry["check"] == "preserve":
                if before["Status"] != after["Status"] or before["Programs"] != after["Programs"]:
                    raise RuntimeError("pre-admission failure changed the current owner")
            elif after["Status"]["Package"]["PackageID"]:
                raise RuntimeError("failed activation published a package")
            if any(e.get("game") == entry["id"] and e.get("operation") == "release_model" and e["status"]["State"] == "running_model" for e in events(guest.text)):
                raise RuntimeError("failed activation released a model core before recovery")
        shots.append(guest.screenshot(entry["id"] + "-returned"))
        results.append({"id": entry["id"], "check": entry["check"], "outcome": "returned" if success else "rejected", "state": states(guest.text)[-1]})
    # Go through tenfoot's own exit and let its idle release finish before the
    # backend closes. QMP quit afterwards only reaps this runner's process.
    guest.press("q")
    guest.wait(lambda t: "FES_MODEL_CLOSED PASS" in t and "FES_TENFOOT_EXIT PASS" in t, timeout, "tenfoot and model shutdown")
    return results, shots
