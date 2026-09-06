#!/usr/bin/env python3
"""Exact Python port of pvpoke's Pokemon.generateIVCombinations + getIVRank
(src/js/pokemon/Pokemon.js lines 498-659). Used as the digit-for-digit ground
truth to validate the Go engine in internal/engine/rank.go.

The cpms table is read directly from the JS file so it is byte-identical.

Usage:
  python3 pvpoke_ref.py <gamemaster.json> <pokemon> <cp> [atk def hp] [stat]
Prints: rank count level cp atk def hp overall  (the same fields the Go rank command reports)
"""
import json, math, sys, re

JS = "/Users/bakytn/apps/pvpoke/src/js/pokemon/Pokemon.js"

def load_cpms(path=JS):
    src = open(path).read()
    m = re.search(r"var cpms = \[(.*?)\];", src, re.S)
    return [float(x) for x in re.findall(r"-?\d+\.\d+", m.group(1))]

CPMS = load_cpms()

def cp_for_level(level):
    idx = int((level - 1) * 2)
    if idx < 0: idx = 0
    if idx >= len(CPMS): idx = len(CPMS) - 1
    return CPMS[idx]

def calc_cp(base, cpm, atk_iv, def_iv, hp_iv):
    return int(( (base["atk"] + atk_iv)
              * math.sqrt(base["def"] + def_iv)
              * math.sqrt(base["hp"]  + hp_iv)
              * cpm * cpm ) / 10)

def iv_floor(p, iv_floor_arg=None):
    floor = 0
    tags = set(p.get("tags", []))
    if (("legendary" in tags) or ("ultrabeast" in tags)) and ("wildlegendary" not in tags):
        floor = 1
    if (("legendary" in tags) or ("ultrabeast" in tags)) and (("shadow" in tags) or ("shadow" in tags)):
        floor = 6
    if iv_floor_arg:
        floor = iv_floor_arg
    if "untradeable" in tags:
        floor = 10
    if "RETURN" in (p.get("chargedMoves", []) + p.get("fastMoves", [])):
        floor = 2
    return floor

def generate_iv_combinations(p, sort_stat, sort_direction=1, result_count=4096, target_cp=1500):
    base = p["baseStats"]
    level_cap = 50                    # battle default (all 4 standard leagues = 50)
    base_level_floor = 1
    if p.get("levelFloor"):
        base_level_floor = p["levelFloor"]
    floor = iv_floor(p)

    combos = []
    hp_iv = 15
    while hp_iv >= floor:
        def_iv = 15
        while def_iv >= floor:
            atk_iv = 15
            while atk_iv >= floor:
                if target_cp > 500:
                    level = base_level_floor
                else:
                    level = 0.5
                calccp = 0
                while (level < level_cap) and (calccp < target_cp):
                    level += 0.5
                    cpm = cp_for_level(level)
                    calccp = calc_cp(base, cpm, atk_iv, def_iv, hp_iv)
                if calccp > target_cp:
                    level -= 0.5
                    cpm = cp_for_level(level)
                    calccp = calc_cp(base, cpm, atk_iv, def_iv, hp_iv)
                if calccp <= target_cp:
                    atk = cpm * (base["atk"] + atk_iv)
                    dfn = cpm * (base["def"] + def_iv)
                    hp = math.floor(cpm * (base["hp"] + hp_iv))   # raw, no max-10 (line 583)
                    overall = hp * atk * dfn
                    combos.append({
                        "level": level,
                        "atk": atk_iv, "def": def_iv, "hp": hp_iv,
                        "statAtk": atk, "statDef": dfn, "statHp": hp,
                        "overall": overall, "cp": calccp,
                    })
                atk_iv -= 1
            def_iv -= 1
        hp_iv -= 1

    def key(c):
        return {"atk": c["statAtk"], "def": c["statDef"], "hp": c["statHp"],
                "overall": c["overall"]}[sort_stat]
    # pvpoke: (a>b)? -dir : (b>a)? dir : 0   -> ascending by key when dir=1 means best first
    combos.sort(key=key, reverse=(sort_direction == 1))
    return combos[:result_count]

def get_iv_rank(p, sort_stat="overall", target_cp=1500, ivs=None):
    combos = generate_iv_combinations(p, sort_stat, 1, 4096, target_cp)
    if ivs is None:
        ivs = [15, 15, 15]
    rank = 0
    for i, c in enumerate(combos):
        if c["atk"] == ivs[0] and c["def"] == ivs[1] and c["hp"] == ivs[2]:
            rank = i + 1
            break
    return rank, len(combos), combos

def find_combo(combos, ivs):
    for c in combos:
        if c["atk"] == ivs[0] and c["def"] == ivs[1] and c["hp"] == ivs[2]:
            return c
    return None

def main():
    path = sys.argv[1]
    d = json.load(open(path))
    pkl = d.get("pokemon", d if isinstance(d, list) else [])
    query = sys.argv[2].strip().lower()
    cp = int(sys.argv[3])
    ivs = [15, 15, 15]
    stat = "overall"
    rest = sys.argv[4:]
    ints = [int(x) for x in rest if x.lstrip("-").isdigit()]
    words = [x for x in rest if not x.lstrip("-").isdigit()]
    if len(ints) >= 3:
        ivs = ints[:3]
    if words:
        stat = {"atk": "atk", "def": "def", "hp": "hp"}.get(words[-1], stat)
    byname = {p["speciesName"].lower(): p for p in pkl}
    p = byname.get(query)
    if p is None:
        print("not found: " + sys.argv[2])
        sys.exit(1)
    rank, count, combos = get_iv_rank(p, stat, cp, ivs)
    c = find_combo(combos, ivs)
    if c is not None:
        lvl, actcp = c["level"], c["cp"]
        # display stats use setStats semantics (max-10) to match the pokebox
        cpm = cp_for_level(lvl)
        base = p["baseStats"]
        dAtk = cpm * (base["atk"] + ivs[0])
        dDef = cpm * (base["def"] + ivs[1])
        dHp = max(int(cpm * (base["hp"] + ivs[2])), 10)
    else:
        # Not in the reachable set. Go's GetIVRank synthesizes a display
        # level/CP: the combo's CP at the league level cap (50). pvpoke's raw
        # getIVRank returns only {rank:0, count}; Go extends the display.
        lvl = 50.0
        cpm = cp_for_level(50)
        base = p["baseStats"]
        actcp = calc_cp(base, cpm, ivs[0], ivs[1], ivs[2])
        dAtk = cpm * (base["atk"] + ivs[0])
        dDef = cpm * (base["def"] + ivs[1])
        dHp = max(int(cpm * (base["hp"] + ivs[2])), 10)
    print(f"{p['speciesName']}|{rank}|{count}|{lvl:.1f}|{actcp}|{ivs[0]}/{ivs[1]}/{ivs[2]}|{dAtk:.0f}|{dDef:.0f}|{dHp}")

if __name__ == "__main__":
    main()
