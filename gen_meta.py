#!/usr/bin/env python3
"""Regenerate data/meta/<cp>.json from pvpoke's published overall rankings.

Top-N (default 100) per league, keeping the exact file shape the breaker
consumes: {league, cp, top: [{speciesId, speciesName, score, fastMoves,
chargedMoves, stats}]}. The opponent stats the breaker re-derives from the
gamemaster defaultIVs table are not stored here; `stats` is kept for
parity/reference with pvpoke's published numbers.

Usage: python3 gen_meta.py [topN] [pvpoke_src_dir]
  topN          default 100
  pvpoke_src_dir default ../pvpoke/src/data
"""
import json, os, sys

LEAGUES = [("little", 500), ("great", 1500), ("ultra", 2500), ("master", 10000)]

def main():
    top_n = int(sys.argv[1]) if len(sys.argv) > 1 else 100
    here = os.path.dirname(os.path.abspath(__file__))
    src = sys.argv[2] if len(sys.argv) > 2 else os.path.normpath(os.path.join(
        here, "..", "pvpoke", "src", "data"))
    outdir = os.path.join(here, "data", "meta")
    os.makedirs(outdir, exist_ok=True)

    for league, cp in LEAGUES:
        path = os.path.join(src, "rankings", "all", "overall", f"rankings-{cp}.json")
        rank = json.load(open(path))
        top = []
        for e in rank[:top_n]:
            top.append({
                "speciesId": e["speciesId"],
                "speciesName": e["speciesName"],
                "score": e["score"],
                "fastMoves": [m["moveId"] for m in e.get("moves", {}).get("fastMoves", [])],
                "chargedMoves": [m["moveId"] for m in e.get("moves", {}).get("chargedMoves", [])],
                "stats": e["stats"],
            })
        out = {"league": league, "cp": cp, "top": top}
        dest = os.path.join(outdir, f"{cp}.json")
        with open(dest, "w") as f:
            json.dump(out, f, indent=1)
            f.write("\n")
        print(f"{cp} ({league}): {len(top)} entries")

if __name__ == "__main__":
    main()
