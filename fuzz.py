#!/usr/bin/env python3
"""Scale fuzz: compare Go `pogo rank` vs pvpoke_ref.py across a large random
sample of species x leagues x IVs. Assert rank + count + level + CP match.
Surfaces any edge case in the 1742-species space the hand-picked 20 missed."""
import subprocess, re, os, random, json, sys

random.seed(1337)
os.chdir("/Users/bakytn/apps/pogo_discord")
GO  = "/Users/bakytn/apps/pogo_discord/pogo"
REF = "/Users/bakytn/apps/pogo_discord/pvpoke_ref.py"
GM  = "/Users/bakytn/apps/pogo_discord/data/gamemaster.json"

d = json.load(open(GM))
pk = d["pokemon"]
names = [p["speciesName"] for p in pk]
LEAGUES = [500, 1500, 2500, 10000]
STATS = ["", "atk", "def", "hp"]

def run_go(name, cp, ivs, stat):
    a = [GO, "-data", "data", "rank", name]
    if cp: a.append(str(cp))
    a += [str(x) for x in ivs]
    if stat: a.append(stat)
    r = subprocess.run(a, capture_output=True, text=True)
    if r.returncode != 0:
        return None
    out = r.stdout
    m = re.search(r"IV rank: \*\*(\d+)\*\* / (\d+)", out)
    if m:
        rank, count = int(m.group(1)), int(m.group(2))
    else:
        c = re.search(r"Reachable IV combinations: \*\*(\d+)\*\*", out)
        rank, count = 0, (int(c.group(1)) if c else -1)
    lvl = re.search(r"Level: ([\d.]+)", out)
    cpv = re.search(r"CP reached: (\d+)", out)
    atk = re.search(r"Stats: Atk ([\d.]+)", out)
    dfn = re.search(r"Def ([\d.]+)", out)
    hp  = re.search(r"HP ([\d.]+)", out)
    return dict(rank=rank, count=count, level=float(lvl.group(1)) if lvl else None,
                cp=int(cpv.group(1)) if cpv else None,
                atk=float(atk.group(1)) if atk else None,
                dfn=float(dfn.group(1)) if dfn else None,
                hp=float(hp.group(1)) if hp else None)

def run_ref(name, cp, ivs, stat):
    a = ["python3", REF, GM, name, str(cp)] + [str(x) for x in ivs] + ([stat] if stat else [])
    r = subprocess.run(a, capture_output=True, text=True)
    if r.returncode != 0 or "not found" in r.stdout:
        return None
    f = r.stdout.strip().split("|")
    if len(f) < 9:
        return None
    return dict(rank=int(f[1]), count=int(f[2]), level=float(f[3]), cp=int(f[4]),
                atk=float(f[6]), dfn=float(f[7]), hp=float(f[8]))

def cmp(a, b):
    if a is None or b is None:
        return a is None and b is None
    return (a["rank"] == b["rank"] and a["count"] == b["count"]
            and a["level"] == b["level"] and a["cp"] == b["cp"]
            and all(abs(a[k]-b[k]) <= 0.5 for k in ("atk","dfn","hp")))

N = int(sys.argv[1]) if len(sys.argv) > 1 else 250
# Weight toward interesting species: level-floor species, legendaries, shadows, megas
interesting = [p["speciesName"] for p in pk
               if "levelFloor" in p or "shadow" in str(p.get("speciesId",""))
               or p.get("speciesId","").endswith("_shadow") or "(Mega" in p["speciesName"]
               or "shadow" in p.get("tags",[])]
pool = interesting * 3 + names   # oversample the edge species

bad = 0
for i in range(N):
    name = random.choice(pool)
    cp   = random.choice(LEAGUES)
    # Random IVs, but sometimes perfect / sometimes floor-ish
    mode = random.random()
    if mode < 0.4:
        ivs = [15,15,15]
    elif mode < 0.55:
        ivs = [random.randrange(16) for _ in range(3)]
    else:
        lo = random.choice([0,2,6,10,15])
        ivs = [random.randrange(lo, 16) for _ in range(3)]
    stat = random.choice(STATS)
    g = run_go(name, cp, ivs, stat)
    r = run_ref(name, cp, ivs, stat)
    if not cmp(g, r):
        bad += 1
        def ln(x):
            return "none" if x is None else f"rank {x['rank']}/{x['count']} L{fmtx(x['level'])} cp{x['cp']} a/d/h {x['atk']:.0f}/{x['dfn']:.0f}/{x['hp']:.0f}"
        print(f"MISMATCH {name} cp{cp} {'/'.join(map(str,ivs))} {stat or 'overall'}\n  GO {ln(g)}\n  JS {ln(r)}")

print(f"\n{N} random cases: {N-bad} identical, {bad} mismatches")
sys.exit(1 if bad else 0)

def fmtx(x):
    return f"{x:.1f}"
