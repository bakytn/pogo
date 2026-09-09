# AGENTS.md — pogo / breaker

Working map for coding agents so you don't re-derive the data model every time.
These facts were verified against the Go sources and the committed game data.
**Read this before touching `data/` or the damage/IV code.**

## What the breaker actually reads from `data/gamemaster.json`

The Go model is a *minimal mirror* of pvpoke's compiled `gamemaster.json`
(same 15 top-level keys: timestamp, id, title, settings, rankingScenarios, cups,
formats, pokemonTags, pokemonTraits, fastMoveArchetypes, chargedMoveArchetypes,
pokemonRegions, shadowPokemon, greatLeagueIneligible, pokemon, moves).
The committed `data/gamemaster.json` is a **verbatim mirror** of pvpoke's
`src/data/gamemaster.json` — keep the `moves` and `pokemon` sections byte-identical
to pvpoke's compiled output when you update them.

But only a **subset** of each object's fields is consumed (the rest are dropped
on JSON unmarshal because the Go structs have no field for them):

- **Move** — `internal/engine/damage.go` `Attack` reads:
  `moveId, name, type, power, cooldown, energy, energyGain`.
  NOT read (cosmetic only, dropped on unmarshal): `archetype`, `unlisted`,
  `turns`, `buffs`, `buffTarget`, `buffApplyChance`, `abbreviation`.
  ⇒ **Archetype / unlisted / buff-field changes to a move do NOT affect any
  breaker output.** Only `power`, `energy`, `energyGain`, `cooldown`, `type`
  change damage. (energy/energyGain matter for the energy/turn accounting, not
  the raw damage floor.)

- **Pokémon** — `internal/engine/data.go` `Pokemon` reads:
  `dex, speciesName, speciesId, baseStats, types, fastMoves, chargedMoves, tags,
  defaultIVs, level25CP, levelFloor`.
  NOT read: `extraChargedMoves`, `eliteMoves`, `buddyDistance`, `thirdMoveCost`,
  `released`, `family`, `searchPriority`.
  ⇒ A move that lives only in `extraChargedMoves` is **not** part of the breaker's
  move pool. Only `fastMoves` + `chargedMoves` are.

- **Top meta** — `data/meta/<cp>.json` (`internal/breaker/meta.go` `TopMetaEntry`)
  reads: `speciesId, speciesName, score, fastMoves, chargedMoves, stats`
  (stats = rank-1 atk/def/hp). Opponent **defense** for damage comes from this
  meta file (rank-1 def, shadow-adjusted), not from the user's Pokémon.

## Damage math (do not "improve")

`internal/engine/damage.go` is a **verbatim port** of pvpoke's `DamageCalculator`:
`floor(power * stab * (atk/def) * effectiveness * 0.5 * BONUS) + 1`.
The `typeWeaknesses`/`typeResistances`/`typeImmunities` tables and the float
constants (`BONUS=1.2999999523162841796875`, `SUPER`, `RESISTED`, `DOUBLE`,
`STAB`, `DmgShadowAtk=1.2`, `DmgShadowDef=0.83333331`) must match pvpoke exactly.
The `Breakpoint`/`Bulkpoint` helpers derive off the same formula.

## Parity oracles (the "verify" step)

Node is broken on this box (no libsimdjson), so **Python is the oracle**, not Node:
- `pvpoke_ref.py` — IV-rank parity vs pvpoke's `Pokemon.js` (reads the `cpms[]`
  table from `/Users/bakytn/apps/pvpoke/src/js/pokemon/Pokemon.js`).
- `bp_ref.py` — independent re-derivation of the breaker damage lines
  (type chart, STAB, damage floor) from pvpoke's verbatim constants.
- `compare.py`, `fuzz.py` — diff Go output vs the Python oracle.
- `go test ./...` — unit tests (incl. `internal/breaker/breaker_test.go`).

## How to apply a pvpoke gamemaster update (movesets / move availability)

1. **Moves & availability** live in `data/gamemaster.json` `moves` + `pokemon`
   sections. Update them to match pvpoke's freshly-compiled `gamemaster.json`
   (take verbatim; do **not** hand-edit Go code for a balance/availability pass).
   Only `power/energy/energyGain/cooldown/type` on moves and `fastMoves`/
   `chargedMoves` on Pokémon change behavior — archetype/extraChargedMoves edits
   are fidelity-only.
2. **New/removed Pokémon** (e.g. `staraptor_mega` added, `cradily_b` removed) —
   add/remove the whole `pokemon` entry to match pvpoke.
3. **Meta** — regenerate from pvpoke's published rankings:
   `python3 gen_meta.py [topN=100] [pvpoke_src_dir=../pvpoke/src/data]`
   reads `rankings/all/overall/rankings-<cp>.json` → rewrites `data/meta/<cp>.json`
   (500/1500/2500/10000).
4. **Verify** — `go test ./...` and `python3 compare.py` / `fuzz.py`.

## Layout

- `cmd/pogo/main.go` — CLI entry (rank, list, breaker, …).
- `internal/engine/` — `data.go` (GameMaster load/index), `cpm.go` (CP walk + CP
  formula + IV floor), `damage.go` (Damage/Breakpoint/Bulkpoint + type chart).
- `internal/breaker/` — `meta.go` (top-meta load), `analysis.go` (break/bulk
  against the 4 IV states), `command.go`.
- `internal/service/`, `internal/bot/` — shared registry + Discord driver.
- `data/gamemaster.json` — game model (pvpoke mirror). `data/meta/*.json` — top-100.
- `pvpoke_ref.py`, `bp_ref.py`, `compare.py`, `fuzz.py` — parity tooling.
