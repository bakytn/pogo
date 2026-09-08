#!/usr/bin/env python3
"""Independent Python validation of the breaker damage math.

Design (mirrors the repo's pvpoke_ref.py parity culture):
  - The FOUR IV states' stats (now / rank 1 / 15-0-0 / 0-15-0) are taken from
    the canonical Go engine (the "STATES" line of `pogo breaker ... dump`),
    which is already byte-validated against pvpoke by pvpoke_ref.py. This avoids
    re-implementing the CP walk twice across languages (a float-equality trap).
  - Everything the breaker *computes* from those stats — the type chart
    (getEffectiveness), STAB, and the damage floor
    floor(power*stab*(atk/def)*eff*0.5*BONUS)+1 — is validated INDEPENDENTLY
    here against pvpoke's verbatim constants/type tables.
  - Opponent def comes from the meta file (rank-1 def from pvpoke's rankings),
    shadow-adjusted, exactly as the Go side does.

Usage:
  python3 bp_ref.py <gamemaster.json> <meta<cp>.json> <pokemon> <cp> <atk> <def> <hp> [shadow 0/1] <goSTATES>
where <goSTATES> is "nowA rank1A maxatkA maxdefA" (from the Go dump).
Prints the same OPP/<move> lines as the Go `dump` mode, minus the STATES line.
"""
import json, math, sys, re

JS = "/Users/bakytn/apps/pvpoke/src/js/pokemon/Pokemon.js"

def load_cpms(path=JS):
    src = open(path).read()
    m = re.search(r"var cpms = \[(.*?)\];", src, re.S)
    return [float(x) for x in re.findall(r"-?\d+\.\d+", m.group(1))]

CPMS = load_cpms()

# DamageCalculator constants (verbatim).
BONUS = 1.2999999523162841796875
SUPER = 1.60000002384185791015625
RESISTED = 0.625
DOUBLE = 0.390625
STAB = 1.2000000476837158203125
SHADOW_ATK = 1.2
SHADOW_DEF = 0.83333331

# Type chart (getEffectiveness). weaknesses > resistances > immunities.
WEAK = {
    "normal": ["fighting"], "fighting": ["flying","psychic","fairy"],
    "flying": ["rock","electric","ice"], "poison": ["ground","psychic"],
    "ground": ["water","grass","ice"], "rock": ["fighting","ground","steel","water","grass"],
    "bug": ["flying","rock","fire"], "ghost": ["ghost","dark"],
    "steel": ["fighting","ground","fire"], "fire": ["ground","rock","water"],
    "water": ["grass","electric"], "grass": ["flying","poison","bug","fire","ice"],
    "electric": ["ground"], "psychic": ["bug","ghost","dark"],
    "ice": ["fighting","fire","steel","rock"], "dragon": ["dragon","ice","fairy"],
    "dark": ["fighting","fairy","bug"], "fairy": ["poison","steel"],
}
RESIST = {
    "normal": [], "fighting": ["rock","bug","dark"], "flying": ["fighting","bug","grass"],
    "poison": ["fighting","poison","bug","fairy","grass"], "ground": ["poison","rock"],
    "rock": ["normal","flying","poison","fire"], "bug": ["fighting","ground","grass"],
    "ghost": ["poison","bug"],
    "steel": ["normal","flying","rock","bug","steel","grass","psychic","ice","dragon","fairy"],
    "fire": ["bug","steel","fire","grass","ice","fairy"], "water": ["steel","fire","water","ice"],
    "grass": ["ground","water","grass","electric"], "electric": ["flying","steel","electric"],
    "psychic": ["fighting","psychic"], "ice": ["ice"],
    "dragon": ["fire","water","grass","electric"], "dark": ["ghost","dark"],
    "fairy": ["fighting","bug","dark"],
}
IMMUNE = {
    "normal": ["ghost"], "fighting": [], "flying": ["ground"], "poison": [],
    "ground": ["electric"], "rock": [], "bug": [], "ghost": ["normal","fighting"],
    "steel": ["poison"], "fire": [], "water": [], "grass": [], "electric": [],
    "psychic": [], "ice": [], "dragon": [], "dark": ["psychic"], "fairy": ["dragon"],
}

def effectiveness(move_type, target_types):
    eff = 1.0
    for t in target_types:
        t = t.lower(); move_type = move_type.lower()
        if move_type in WEAK.get(t, []):
            eff *= SUPER
        elif move_type in RESIST.get(t, []):
            eff *= RESISTED
        elif move_type in IMMUNE.get(t, []):
            eff *= DOUBLE
    return eff

def stab(move_type, types):
    if types and (move_type == types[0] or (len(types) > 1 and move_type == types[1])):
        return STAB
    return 1.0

def cp_for_level(level):
    idx = int((level - 1) * 2)
    idx = max(0, min(idx, len(CPMS) - 1))
    return CPMS[idx]

def calc_cp(base, cpm, atk_iv, def_iv, hp_iv):
    return int((base["atk"]+atk_iv) * math.sqrt(base["def"]+def_iv) * math.sqrt(base["hp"]+hp_iv) * cpm * cpm / 10)

def iv_floor(p):
    tags = set(p.get("tags", []))
    is_shadow = "shadow" in tags
    floor = 0
    if (("legendary" in tags) or ("ultrabeast" in tags)) and ("wildlegendary" not in tags):
        floor = 1
    if (("legendary" in tags) or ("ultrabeast" in tags)) and is_shadow:
        floor = 6
    if "untradeable" in tags:
        floor = 10
    if "RETURN" in (p.get("chargedMoves", []) + p.get("fastMoves", [])):
        floor = 2
    return floor

def level_for_ivs(p, ivs, target_cp):
    base = p["baseStats"]
    level_cap = 50.0
    blf = 1.0
    if p.get("levelFloor"):
        blf = p["levelFloor"]
    level = 0.5 if target_cp <= 500 else blf
    calc = 0
    while level < level_cap and calc < target_cp:
        level += 0.5
        calc = calc_cp(base, cp_for_level(level), ivs[0], ivs[1], ivs[2])
    if calc > target_cp:
        level -= 0.5
    return level

def stats(p, ivs, target_cp, shadow):
    base = p["baseStats"]
    level = level_for_ivs(p, ivs, target_cp)
    m = cp_for_level(level)
    atk = m * (base["atk"] + ivs[0])
    dfn = m * (base["def"] + ivs[1])
    hp = int(m * (base["hp"] + ivs[2]))
    if hp < 10:
        hp = 10
    if shadow:
        atk *= SHADOW_ATK
        dfn *= SHADOW_DEF
    return {"atk": atk, "def": dfn, "hp": hp, "level": level}

def default_combo(p, target_cp):
    """Port of the engine DefaultCombo / pvpoke initialize(true) 'gamemaster'.
    Returns (ivs, level)."""
    ivs = (15, 15, 15)
    level = 50.0
    if target_cp != 10000:
        row = None
        di = p.get("defaultIVs") or {}
        if target_cp == 500:
            row = di.get("cp500")
        elif target_cp == 1500:
            row = di.get("cp1500")
        elif target_cp == 2500:
            row = di.get("cp2500")
        if row and len(row) == 4:
            level = min(50.0, row[0])
            ivs = (int(row[1]), int(row[2]), int(row[3]))
    return ivs, level

def default_stats(p, target_cp, shadow):
    """Re-derive the meta's default-IV battle stats (mirrors Go DefaultStats)."""
    ivs, level = default_combo(p, target_cp)
    base = p["baseStats"]
    m = cp_for_level(level)
    atk = m * (base["atk"] + ivs[0])
    dfn = m * (base["def"] + ivs[1])
    hp = int(m * (base["hp"] + ivs[2]))
    if hp < 10:
        hp = 10
    if shadow:
        atk *= SHADOW_ATK
        dfn *= SHADOW_DEF
    return {"atk": atk, "def": dfn, "hp": float(hp), "level": level}

def generate_iv_combinations(p, target_cp):
    """Port of generateIVCombinations (now recomputes CP after the step-back,
    matching the validated Go engine / pvpoke_ref.py)."""
    base = p["baseStats"]
    level_cap = 50.0
    blf = 1.0
    if p.get("levelFloor"):
        blf = p["levelFloor"]
    floor = iv_floor(p)
    combos = []
    for hp in range(15, floor-1, -1):
        for df in range(15, floor-1, -1):
            for at in range(15, floor-1, -1):
                level = 0.5 if target_cp <= 500 else blf
                calc = 0
                while level < level_cap and calc < target_cp:
                    level += 0.5
                    calc = calc_cp(base, cp_for_level(level), at, df, hp)
                if calc > target_cp:
                    level -= 0.5
                    calc = calc_cp(base, cp_for_level(level), at, df, hp)
                if calc > target_cp:
                    continue
                m = cp_for_level(level)
                a = m * (base["atk"] + at)
                d = m * (base["def"] + df)
                raw = math.floor(m * (base["hp"] + hp))
                overall = (raw * a) * d
                combos.append({"atk": a, "def": d, "rawhp": raw, "overall": overall,
                               "ivs": (at, df, hp), "level": level})
    combos.sort(key=lambda c: c["overall"], reverse=True)
    return combos

def damage(atk, dfn, power, stab, eff):
    return int(math.floor(power * stab * (atk/dfn) * eff * 0.5 * BONUS)) + 1

def move_effective_dmg(mv, user_types, opp_types, atk, dfn):
    s = stab(mv["type"], user_types)
    eff = effectiveness(mv["type"], opp_types)
    return damage(atk, dfn, mv["power"], s, eff)

def base_id(sid):
    i = sid.rfind("_")
    return sid[:i] if i > 0 else sid

def is_shadow(sid):
    l = sid.lower()
    return "_shadow" in l or l.startswith("shadow_")

def main():
    gm = json.load(open(sys.argv[1]))
    meta = json.load(open(sys.argv[2]))
    name = sys.argv[3]
    cp = int(sys.argv[4])
    atk, df, hp = int(sys.argv[5]), int(sys.argv[6]), int(sys.argv[7])
    shadow = (sys.argv[8] if len(sys.argv) > 8 else "0") == "1"
    go_states = [float(x) for x in sys.argv[9].split()] if len(sys.argv) > 9 else None

    moves = {m["moveId"]: m for m in gm["moves"]}
    by_id = {p["speciesId"]: p for p in gm["pokemon"]}
    by_name = {}
    for p in gm["pokemon"]:
        by_name.setdefault(p["speciesName"].lower(), []).append(p)

    user = by_id.get(name)
    if user is None:
        cands = by_name.get(name.lower(), [])
        non_shadow = [p for p in cands if "_shadow" not in p["speciesId"]]
        user = (non_shadow or cands or [None])[0]
    if user is None:
        print("no pokemon found for", name); sys.exit(1)

    f = iv_floor(user)
    # Our own Python IV states — computed to CROSS-CHECK the Go states line
    # (CP walk). Not used for the damage output; Go's states are authoritative.
    rank1 = generate_iv_combinations(user, cp)
    r1ivs = rank1[0]["ivs"] if rank1 else (15, 15, 15)
    st_states = [stats(user, (atk, df, hp), cp, shadow),
                 stats(user, r1ivs, cp, shadow),
                 stats(user, (15, f, f), cp, shadow),
                 stats(user, (f, 15, f), cp, shadow)]
    py_states = [s["atk"] for s in st_states] + [s["def"] for s in st_states]

    # Authoritative attacker atk + def per state (Go is authoritative).
    if go_states and len(go_states) >= 8:
        user_states_atk = go_states[:4]
        user_states_def = go_states[4:8]
        states_line = sys.argv[9]
    else:
        user_states_atk = py_states[:4]
        user_states_def = py_states[4:8]
        states_line = " ".join(repr(a) for a in py_states)

    user_types = user.get("types", [])
    out = []
    out.append("STATES " + states_line)

    for i, e in enumerate(meta["top"]):
        sid = e["speciesId"]
        shadowed = is_shadow(sid)
        opp = by_id.get(sid) or by_id.get(base_id(sid))
        opp_types = opp.get("types", []) if opp else []
        # Opponent (meta) stats re-derived from the defaultIVs table, exactly as
        # the Go side does — NOT the rounded stats baked into the meta file.
        if opp:
            dst = default_stats(opp, cp, shadowed)
            oatk, odef = dst["atk"], dst["def"]
        else:  # species not in local game data: fall back to the published stats
            oatk = e["stats"]["atk"] * (SHADOW_ATK if shadowed else 1.0)
            odef = e["stats"]["def"] * (SHADOW_DEF if shadowed else 1.0)
        sh = 1 if shadowed else 0
        out.append(f"OPP {i+1} {sh} {sid}")
        mids = user.get("fastMoves", []) + user.get("chargedMoves", [])
        for mid in mids:
            mv = moves.get(mid)
            if not mv:
                continue
            vals = [move_effective_dmg(mv, user_types, opp_types, a, odef) for a in user_states_atk]
            fast = 1 if mid in user.get("fastMoves", []) else 0
            out.append(f"{i+1} {sh} {mid} {fast} {vals[0]} {vals[1]} {vals[2]} {vals[3]}")
        # Bulkpoint side: the meta's primary fast move hitting the user.
        if e.get("fastMoves"):
            bmid = e["fastMoves"][0]
            bmv = moves.get(bmid)
            if bmv:
                bvals = [damage(oatk, d, bmv["power"], stab(bmv["type"], opp_types),
                                effectiveness(bmv["type"], user_types)) for d in user_states_def]
                out.append(f"{i+1} {sh} BULK {bmid} {bvals[0]} {bvals[1]} {bvals[2]} {bvals[3]}")

    print("\n".join(out))

if __name__ == "__main__":
    main()
