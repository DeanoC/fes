"""Replay the bounded KSZ9031 fast-link-pulse trial for each physical grant."""
import re


TRAIN = "FES_CBL_TX_TRAIN "
PHY = "FES_CBL_TX_PHY "
FLP = "FES_CBL_TX_FLP "
END = "FES_CBL_MAC_TX END "
FLAGS = "portal pointers snapshot attempted applied restart restored".split()
WORDS = "selector pointer dev0_pointer bmcr old_hi old_lo set_hi set_lo final_hi final_lo final_selector final_bmcr".split()
PHY_WORDS = "id1 id2 bmcr bmsr_latch bmsr adv partner gig_adv gig_partner pcs resolved".split()


def need(value, reason):
    if not value:
        raise ValueError(reason)


def fields(line, schema):
    tokens = line.split(" ")
    need(len(tokens) == len(schema), "incorrect KSZ9031 field count")
    values = {}
    for token, (name, pattern, radix) in zip(tokens, schema):
        key, equal, value = token.partition("=")
        need(equal and key == name and re.fullmatch(pattern, value), "invalid KSZ9031 field " + name)
        values[name] = int(value, radix)
    return values


def sample(line, prefix, identity):
    leader = prefix + "id=" + identity + " "
    need(line.startswith(leader), "wrong training grant")
    return fields(line[len(leader):], [
        ("sample", r"0|[1-9][0-9]?", 10), ("us", r"0|[1-9][0-9]{0,18}", 10),
        *((k, r"[0-9a-f]{4}", 16) for k in PHY_WORDS),
    ])


def validate(line, terminal, training, final, identity):
    prefix = FLP + "id=" + identity + " "
    need(line.startswith(prefix), "missing PHY completion record")
    p = fields(line[len(prefix):], [(k, "[01]", 10) for k in FLAGS] +
               [(k, "[0-9a-f]{4}", 16) for k in WORDS])
    old = (p["old_hi"], p["old_lo"])
    programmed = (p["set_hi"], p["set_lo"])
    need(p["restored"] == p["portal"] and
         p["restart"] <= p["applied"] <= p["snapshot"] <= p["pointers"] <= p["portal"] and
         p["attempted"] <= p["snapshot"], "incomplete or contradictory MDIO ownership")
    if p["portal"]:
        need(training and p["selector"] == p["final_selector"] and not p["selector"] & 0x3fe0
             and p["bmcr"] == p["final_bmcr"] == training[0]["bmcr"]
             and p["bmcr"] & 0xde00 == 0x1000 and training[0]["id1"] == 0x22
             and training[0]["id2"] == 0x1622 and training[0]["adv"] & 31 == 1
             and training[0]["gig_adv"] & 0xe200 == 0x200, "wrong PHY or unrestored MDIO portal")
        if p["pointers"] and not p["selector"] & 31:
            need(p["pointer"] == p["dev0_pointer"], "changed MMD0 pointer")
    if p["snapshot"]:
        need(old == (p["final_hi"], p["final_lo"]), "PHY timing registers not restored")
    if p["attempted"]:
        need(old == (3, 0x4000), "unexpected FLP pair overwritten")
    if p["applied"]:
        need(programmed == (6, 0x1a80) and old in ((3, 0x4000), programmed)
             and bool(p["attempted"]) == (old != programmed), "unverified 16 ms FLP setting")
    need([x["sample"] for x in training] == list(range(2, 2 + len(training)))
         and len(training) <= 7 and (len(training) <= 1 or p["restart"]), "invalid training sample sequence")
    for i in range(1, len(training)):
        delta = training[i]["us"] - training[i-1]["us"]
        need(delta > 0 and (i == 1 or delta >= 5000000), "unstable PHY training clock")
    if final:
        need(len(training) >= 2 and [x["sample"] for x in final] == list(range(len(final)))
             and training[1]["us"] <= final[0]["us"] and training[-1]["us"] <= final[-1]["us"],
             "final PHY pair lacks ordered post-restart observation")
        # One hardware sample may be emitted as both a progress row and final row.
        for s in final:
            for progress in training:
                if progress["us"] == s["us"]:
                    need(all(progress[k] == s[k] for k in PHY_WORDS), "conflicting duplicate PHY time")
    if " dispatch=1 " in terminal:
        need(p["restart"] and len(final) == 2 and
             final[-1]["us"] - training[1]["us"] < 25000000 and
             all(all(s[k] == training[0][k] for k in ("id1", "id2", "bmcr", "adv", "gig_adv"))
                 for s in final), "dispatch without verified PHY training and restoration")
    if training:
        need(" mac=1 " in terminal, "MDIO observation before MAC release")
    p["training"] = training
    return p


def extract(rows, identity):
    """Strip verified FLP/training rows and retain the ordinary PHY/terminal rows."""
    remaining, witnesses, consumed = [], [], set()
    for i, row in enumerate(rows):
        if not row.startswith(END):
            continue
        need(i > 0 and rows[i-1].startswith(FLP), "missing terminal FLP record")
        cursor = i-2
        while cursor >= 0 and rows[cursor].startswith(PHY):
            cursor -= 1
        last_train = cursor
        while cursor >= 0 and rows[cursor].startswith(TRAIN):
            cursor -= 1
        training = [sample(x, TRAIN, identity) for x in rows[cursor+1:last_train+1]]
        final = [sample(x, PHY, identity) for x in rows[last_train+1:i-1]]
        witnesses.append(validate(rows[i-1], row, training, final, identity))
        consumed.update(range(cursor+1, last_train+1))
        consumed.add(i-1)
    for i, row in enumerate(rows):
        if i in consumed:
            continue
        need(not row.startswith(("FES_CBL_TX_TRAIN", "FES_CBL_TX_FLP")), "misplaced PHY evidence")
        remaining.append(row)
    need(witnesses, "missing PHY completion")
    return remaining, witnesses
