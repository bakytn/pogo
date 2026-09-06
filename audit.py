#!/usr/bin/env python3
"""Focused parity audit for the not-found + tie-order edge cases.
1) For 'not reachable' IVs: print Go's FULL output line and the reference's
   reachable-set count, so we can confirm the set sizes agree (not a -1 artifact).
2) For Volcarona cp2500: confirm the off-by-one is a tie-order artifact, not a
   wrong set (dump the reachable set, the target's neighbors, and both ranks).
"""
import subprocess, re, os, json
os.chdir("/Users/bakytn/apps/pogo_discord")
GO, REF, GM = "/Users/bakytn/apps/pogo_discord/pogo", "/Users/bakytn/apps/pogo_discord/pvpoke_ref.py", "/Users/bakytn/apps/pogo_discord/data/gamemaster.json"

def go_full(name, cp, ivs, stat=""):
    a = [GO, "-data", "data", "rank", name]
    if cp: a.append(str(cp))
    a += [str(x) for x in ivs]
    if stat: a.append(stat)
    r = subprocess.run(a, capture_output=True, text=True)
    return r.stdout, r.returncode

def ref_raw(name, cp, ivs, stat=""):
    a = ["python3", REF, GM, name, str(cp)] + [str(x) for x in ivs] + ([stat] if stat else [])
    r = subprocess.run(a, capture_output=True, text=True)
    return r.stdout.strip()

print("=== NOT-FOUND edge cases: Go output vs reference count ===")
for name, cp, ivs in [
    ("Mewtwo (Mega X)", 2500, [15,15,15]),
    ("Mewtwo (Mega Y)", 2500, [0,0,0]),
    ("Tauros (Aqua)",   1500, [15,15,15]),
    ("Darkrai",         2500, [7,7,7]),
]:
    out, rc = go_full(name, cp, ivs)
    ref = ref_raw(name, cp, ivs)
    print(f"\n{name} cp{cp} ivs {'/'.join(map(str,ivs))}  (go rc={rc})")
    print("  GO:", " | ".join(l for l in out.splitlines() if l.strip())[:200])
    print("  JS:", ref)

print("\n\n=== Volcarona cp2500 tie-order check ===")
out, rc = go_full("Volcarona", 2500, [10,10,10])
print("GO:", " | ".join(l for l in out.splitlines() if l.strip())[:200])
ref = ref_raw("Volcarona", 2500, [10,10,10])
print("JS:", ref)
# Does the reference expose combos? add a tiny probe: rank of a neighboring IV
for ivs in ([10,10,11],[10,11,10],[11,10,10],[10,10,9],[10,11,11]):
    r2 = ref_raw("Volcarona", 2500, ivs)
    g2, _ = go_full("Volcarona", 2500, ivs)
    gr = re.search(r"rank: \*\*(\d+)\*\*", g2)
    print(f"  ivs {'/'.join(map(str,ivs)):8}  GO rank {gr.group(1) if gr else '?':>5}   JS {r2}")
