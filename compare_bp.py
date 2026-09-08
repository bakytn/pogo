#!/usr/bin/env python3
"""Cross-language parity check for the breaker (Go dump vs bp_ref.py oracle).

For a given (pokemon, ivs, cp, league, shadow):
  1. Run `pogo breaker ... dump` -> the Go report (STATES + OPP + moves + BULK).
  2. Feed the Go STATES line to bp_ref.py -> the independent Python oracle.
  3. Diff the two line-for-line; report the max per-stat divergence.

The Go STATES line is authoritative for the attacker stats (it carries the full
CP walk); bp_ref.py re-derives the opponent default-IV stats and recomputes
every damage value from pvpoke's verbatim constants/type tables.
"""
import subprocess, sys, os

HERE = os.path.dirname(os.path.abspath(__file__))
GOBIN = "/tmp/pogo"
GM = os.path.join(HERE, "data", "gamemaster.json")
META = os.path.join(HERE, "data", "meta")

LEAGUE_CP = {"little": 500, "great": 1500, "ultra": 2500, "master": 10000}

def run_go(poke, cp, a, d, h, league, shadow):
    cmd = [GOBIN, "breaker", poke, str(cp), str(a), str(d), str(h), league]
    if shadow:
        cmd.append("shadow")
    cmd.append("dump")
    out = subprocess.run(cmd, cwd=HERE, capture_output=True, text=True)
    if out.returncode != 0:
        return None, out.stderr
    return out.stdout, None

def states_from(go_text):
    for line in go_text.splitlines():
        if line.startswith("STATES "):
            return line[len("STATES "):]
    return None

def run_py(poke, cp, a, d, h, league, shadow, go_states):
    meta_path = os.path.join(META, f"{cp}.json")
    cmd = [sys.executable, os.path.join(HERE, "bp_ref.py"),
           GM, meta_path, poke, str(cp), str(a), str(d), str(h)]
    cmd.append("1" if shadow else "0")
    cmd.append(go_states)
    out = subprocess.run(cmd, cwd=HERE, capture_output=True, text=True)
    if out.returncode != 0:
        return None, out.stderr
    return out.stdout, None

def norm(text):
    return [l.split() for l in text.splitlines() if l.strip()]

def check(poke, a, d, h, league, shadow):
    cp = LEAGUE_CP[league]
    go, err = run_go(poke, cp, a, d, h, league, shadow)
    if go is None:
        return f"{poke} {a}/{d}/{h} {league}{' shadow' if shadow else ''}: GO ERR {err}"
    gs = states_from(go)
    py, err = run_py(poke, cp, a, d, h, league, shadow, gs)
    if py is None:
        return f"{poke} {a}/{d}/{h} {league}{' shadow' if shadow else ''}: PY ERR {err}"
    # Compare OPP/move/BULK lines (skip the STATES line: py echoes gs verbatim).
    go_lines = [l for l in norm(go) if l and l[0] != "STATES"]
    py_lines = [l for l in norm(py) if l and l[0] != "STATES"]
    if len(go_lines) != len(py_lines):
        return f"{poke} {a}/{d}/{h} {league}: line count go={len(go_lines)} py={len(py_lines)}"
    maxdiff = 0
    worst = None
    for gl, pl in zip(go_lines, py_lines):
        if gl[0] != pl[0] or gl[1] != pl[1] or gl[2] != pl[2]:
            return f"{poke} {a}/{d}/{h} {league}: ID MISMATCH go={gl[:3]} py={pl[:3]}"
        for x, y in zip(gl[3:], pl[3:]):
            try:
                diff = abs(int(x) - int(y))
            except ValueError:
                if x != y:
                    return f"{poke} {a}/{d}/{h} {league}: TEXT MISMATCH {gl} vs {pl}"
                continue
            if diff > maxdiff:
                maxdiff = diff
                worst = (gl[:3], x, y)
    if maxdiff > 0:
        return f"{poke} {a}/{d}/{h} {league}: MAX DIFF {maxdiff} at {worst}"
    return None

if __name__ == "__main__":
    # args: <poke[:shadow] ...>   — a trailing ":shadow" suffix requests the
    # shadow form (base name + shadow flag), which the base-name-only form
    # cannot express (a bare "shadow" token is a Pokémon name).
    specs = []
    for tok in sys.argv[1:]:
        shadow = tok.endswith(":shadow")
        name = tok[: -len(":shadow")] if shadow else tok
        specs.append((name, shadow))
    if not specs:
        specs = [(p, False) for p in ["charizard", "mewtwo", "gengar", "lapras", "dragonite", "alakazam"]]
    ivsets = [(15, 15, 15), (15, 0, 0), (0, 15, 0), (1, 1, 1), (7, 3, 12), (15, 8, 5)]
    leagues = ["little", "great", "ultra", "master"]
    for (p, sh) in specs:
        for (a, d, h) in ivsets:
            for lg in leagues:
                for shadow in ([sh] if sh else [False]):
                    r = check(p, a, d, h, lg, shadow)
                    if r:
                        print("FAIL", r)
    print("done")
