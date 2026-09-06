#!/usr/bin/env python3
"""Compare Go `pogo rank` vs pvpoke_ref.py (exact JS port), field by field:
rank, count, level, actual CP, and displayed atk/def/hp."""
import subprocess, re, sys, os

GO  = "/Users/bakytn/apps/pogo_discord/pogo"
REF = "/Users/bakytn/apps/pogo_discord/pvpoke_ref.py"
GM  = "/Users/bakytn/apps/pogo_discord/data/gamemaster.json"
os.chdir("/Users/bakytn/apps/pogo_discord")

def run_go(name, cp, ivs, stat):
    args = [GO, "-data", "data", "rank", name]
    if cp: args.append(str(cp))
    args += [str(x) for x in ivs]
    if stat: args.append(stat)
    r = subprocess.run(args, capture_output=True, text=True)
    if r.returncode != 0:
        return None
    out = r.stdout
    m = re.search(r"IV rank: \*\*(\d+)\*\* / (\d+)", out)
    rank = int(m.group(1)) if m else 0
    if m:
    	count = int(m.group(2))
    else:
    	c = re.search(r"Reachable IV combinations: \*\*(\d+)\*\*", out)
    	count = int(c.group(1)) if c else -1
    lvl = re.search(r"Level: ([\d.]+)", out)
    cp  = re.search(r"CP reached: (\d+)", out)
    atk = re.search(r"Stats: Atk ([\d.]+)", out)
    dfn = re.search(r"Def ([\d.]+)", out)
    hp  = re.search(r"HP ([\d.]+)", out)
    return dict(rank=rank, count=count,
                level=float(lvl.group(1)) if lvl else None,
                cp=int(cp.group(1)) if cp else None,
                atk=float(atk.group(1)) if atk else None,
                dfn=float(dfn.group(1)) if dfn else None,
                hp=float(hp.group(1)) if hp else None)

def run_ref(name, cp, ivs, stat):
    r = subprocess.run(["python3", REF, GM, name, str(cp)] + [str(x) for x in ivs] + ([stat] if stat else []),
                        capture_output=True, text=True)
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
    if a["rank"] != b["rank"]: return False
    if a["count"] != b["count"]: return False
    if a["level"] != b["level"]: return False
    if a["cp"] != b["cp"]: return False
    for k in ("atk","dfn","hp"):
        if abs(a[k] - b[k]) > 0.5: return False   # displayed stats are %.0f
    return True

def fmt(x):
    if x is None: return "   ?"
    return f"{x:7.1f}" if isinstance(x, float) else f"{x:7d}"

cases = [
    ("Charizard",        1500, [15,15,15], ""),
    ("Charizard",        1500, [15,15,15], "atk"),
    ("Charizard",        1500, [15,15,15], "def"),
    ("Charizard",        1500, [15,15,15], "hp"),
    ("Charizard",        2500, [12,10,8],  ""),
    ("Charizard",         500, [15,15,15], ""),
    ("Charizard",        1500, [0,0,0],    ""),
    ("Charizard Shadow", 1500, [15,15,15], ""),
    ("Pikachu",          1500, [15,15,15], ""),
    ("Snorlax",         10000,[15,15,15], ""),
    ("Snorlax",         10000,[15,15,15], "def"),
    ("Mewtwo (Mega X)",  2500, [15,15,15], ""),
    ("Mewtwo (Mega Y)",  2500, [0,0,0],   ""),
    ("Tauros (Aqua)",    1500, [15,15,15], ""),
    ("Larvesta",         1500, [15,15,15], ""),
    ("Volcarona",        2500, [10,10,10], ""),
    ("Heatran",          2500, [15,15,15], ""),
    ("Heatran (Shadow)", 2500, [15,15,15], ""),
    ("Darkrai",          2500, [7,7,7],   ""),
    ("Giratina (Origin)",2500, [15,15,15], ""),
]

allok, n = True, 0
for name, cp, ivs, stat in cases:
    g = run_go(name, cp, ivs, stat)
    r = run_ref(name, cp, ivs, stat)
    ok = cmp(g, r); n += 1
    if not ok: allok = False
    def line(x):
        if x is None: return "    (none)"
        return (f"rank {x['rank']:>4}/{x['count']:>5} L{fmt(x['level'])[1:]} "
                f"cp {x['cp'] if x['cp'] is not None else '?':>5} "
                f"a/d/h {x['atk']:.0f}/{x['dfn']:.0f}/{x['hp']:.0f}")
    print(f"[{'OK ' if ok else 'DIFF'}] {name:18} cp{cp:5} {'/'.join(map(str,ivs)):8} {stat or 'overall':6}")
    print(f"           GO {line(g)}")
    print(f"           JS {line(r)}")

print(f"\n{'*** MISMATCH ***' if not allok else 'ALL IDENTICAL'}  — {n} cases")
sys.exit(0 if allok else 1)
