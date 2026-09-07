# pogo

A Pokémon GO battle helper written in Go. It ports pvpoke's `Pokemon.js`
ranking engine to Go and exposes it as a single binary with two drivers:

- **CLI** — `pogo rank …` for quick lookups on your machine.
- **Discord bot** — the same logic, as slash commands, so a server can ask a
  bot for a Pokémon's IV rank.

Both drivers share one `service.Service`, so **adding a capability is a single
`Register` call** that is picked up by the CLI *and* the bot at once.

## What it computes

`rank` reports where a given IV combination falls among **all reachable IVs**
for a Pokémon at a league's CP ceiling — the same IV rank shown on pvpoke's
battle page. "Reachable" means: enumerate every (level, IV) combo that sits at
or below the league CP, using pvpoke's exact rules:

- **CP formula** — `floor((atk+IV)·√(def+IV)·√(hp+IV)·cpm² / 10)`.
- **CPM table** — the 109-entry `cpms[]` array from `Pokemon.js`, indexed by
  `cpm[(level-1)·2]` (0.5 level steps).
- **IV floor** — 1 for legendary/ultrabeast (non-wild), 6 if that and shadow,
  10 if untradeable, 2 if it can learn RETURN.
- **Level floor** (`baseLevelFloor`) — 1 by default; 8/15/20 for the 39
  exclusives/legendaries that carry a `levelFloor` in the game data.
- **Level cap** — the league cap (50 for all four standard leagues), walked in
  0.5 steps until the combo's CP reaches the ceiling.

The reachable set is sorted by the requested stat (default: overall) and
truncated to pvpoke's 4096; the rank is the 1-based position of the user's IVs.

## Build

```sh
go build -o pogo ./cmd/pogo
```

On first run it downloads `https://pvpoke.com/data/gamemaster.json` into
`./data/` (≈1.7k species). Use `-data DIR` to point elsewhere or `-fetch` to
force a re-download. A local copy of the game data is committed for offline use.

## CLI

```
pogo rank <pokemon> [cp] [atk def hp] [stat]
pogo list
pogo help
```

| arg | meaning |
|-----|---------|
| `<pokemon>` | name or ID, case-insensitive (e.g. `charizard`) |
| `[cp]` | league CP ceiling: 500 Little / 1500 Great / 2500 Ultra / 10000 Master (default 1500) |
| `[atk def hp]` | your IVs, 0–15 each (default 15/15/15) |
| `[stat]` | `overall` (default), `atk`, `def`, or `hp` — what to rank by |

Examples:

```
$ pogo rank charizard
Charizard (fire/flying)
**Great League league (CP ≤ 1500)**
IV rank: **1666** / 4096
Level: 18.0 · CP reached: 1486
IVs: 15 / 15 / 15
Stats: Atk 135 · Def 107 · HP 113

$ pogo rank charizard 2500 12 10 8
$ pogo rank charizard 1500 15 15 15 atk
$ pogo rank charizard 500            # Little League (level floor 0.5)
```

When a combo can't sit under the league (e.g. a 15/15/15 Mega in Ultra), it
reports `not reachable` plus the total reachable set size, the combo's CP at
the league cap, and its stats.

### `breaker` — IV breakpoints vs the top meta

```
pogo breaker <pokemon> [cp] [atk def hp] [league] [shadow] [dump]
```

For your Pokémon, against **each of the top meta Pokémon in that league** (a
static list pulled from pvpoke's published overall rankings — `data/meta/<cp>.json`),
`breaker` reports, for every one of your moves, the damage it deals in **four
IV states**:

| state | IVs | meaning |
|-------|-----|---------|
| `now` | your actual IVs | how your current build deals damage |
| `rank 1` | the highest stat-product combo reachable in the league | a "rank 1" build of the same species |
| `15-0-0` | max attack, floor def/hp | the pure-attacker extreme |
| `0-15-0` | floor atk, max def, floor hp | the pure-tank extreme |

This surfaces IV breakpoints / bulkpoints as a *damage delta*: how much your
IVs give (or cost) you against each top-10/20 meta opponent. The classic
example — a high-attack Sableye starts dealing **+1 Shadow Claw** over Charjabug
once its attack crosses into that damage tier — shows up as `15-0-0 > rank 1`.

The opponent is held **fixed at its rank-1 stats** (from the meta file) plus
shadow multipliers, so the only thing that varies across your four columns is
your own IVs. Fast moves are listed first, then charged.

```
$ pogo breaker sableye 1500 15 0 0 great
Sableye (dark/ghost) vs great (1500 CP) · IVs 15/0/0
Your stats: now A131 D114 H114 · rank 1 A118 D127 H127 · ...
1. Lickilicky ...
⚡Shadow Claw   2 / 2 / 2 / 2
  Foul Play     53 / 48 / 53 / 48   (R1 5)  best +0
...
```

- `league` may be a name (`little`/`great`/`ultra`/`master`) or a CP ceiling;
  it must match the league whose top-meta file is being used.
- `shadow` switches your Pokémon to its shadow form for the whole analysis.
- `dump` prints a machine-readable form (STATES + per-move rows) for scripting.

`rank` shows where your IVs fall in the reachable set; `breaker` shows what
those IVs *do* to your damage against the Pokémon you'll actually face.

## Discord bot

```sh
DISCORD_TOKEN=… ./pogo bot
```

On connect it bulk-registers **one slash command per `service.Command`**
(currently `/rank` and `/breaker`), then answers interactions through the same
handlers. A bot-invite token is required; the bot registers its commands globally.

## Extending

A "command" is any type implementing:

```go
type Command interface {
    Name() string        // token, e.g. "counter"
    Description() string // one-liner for help + the Discord command description
    Run(ctx context.Context, svc *Service, req Request) (string, error)
}
```

Wire it up once:

```go
s.Register(NewCounterCommand())   // in service.New or wherever you build the Service
```

…and it is immediately available as `pogo counter …` **and** as a `/counter`
slash command (the bot auto-registers whatever is in the registry). `Request`
carries `Name`, `IVs`, `CP`, `SortStat`, `Shadow`, and an open `Extra` map for
commands that need more — the type is stable as you add features.

Reach into `svc.Data` (`engine.GameData`) for the full 1.7k-species model:
`ByID`, `ByName`, `Candidates`, base stats, types, move pools, tags, and the
CP/stats/rank primitives in `internal/engine`.

## Correctness — how this was validated

The engine is a faithful port of pvpoke's `src/js/pokemon/Pokemon.js`
(`generateIVCombinations` + `getIVRank`). To prove it digit-for-digit:

- `pvpoke_ref.py` is an independent Python transcription of that same JS
  (it reads the `cpms` table **straight from the JS file**).
- `compare.py` / `fuzz.py` run the Go binary and the reference over a battery
  of species × leagues × IVs × sort-stats and assert rank, count, level, CP,
  and all three displayed stats are identical.

The breaker's damage math is a faithful port of pvpoke's
`src/js/battle/DamageCalculator.js` (type chart, STAB, shadow multipliers, the
`damage()` formula). `bp_ref.py` re-implements that damage/STAB/type math in
Python (it reads the `cpms` table from the JS) and feeds it the **same**
per-state attack values the Go engine computes, then asserts the resulting
damage integers match across all four leagues × sampled species. The CP-walk
behind those attack values is already covered by `pvpoke_ref.py` above.

The initial port was **not** byte-for-byte: the level floor (`baseLevelFloor`,
39 species) was hard-coded to 1, and the `overall` term used a different
multiplication order + a clamped HP, which flipped near-ties (e.g. Volcarona
1513 vs 1512). Both were fixed; the suite is now all-green across a 170-case
sweep. (The live site uses the same `cpms`/floor logic, so outputs match; the
Python reference is the ground truth since the running site shares the file.)

Run it:

```sh
go test ./...        # engine + breaker unit tests (pin the proven values)
python3 compare.py   # rank: Go vs reference, 20 curated cases
python3 fuzz.py 100  # rank: Go vs reference, 100 random cases
```

For the breaker's damage parity (Go vs `bp_ref.py`), diff the machine-readable
`dump` form:

```sh
s=$(./pogo breaker sableye 1500 15 0 0 great dump | sed -n 's/^STATES //p')
diff <(./pogo breaker sableye 1500 15 0 0 great dump) \
     <(python3 bp_ref.py data/gamemaster.json data/meta/1500.json sableye 1500 15 0 0 0 0 "$s")
```

## Layout

```
cmd/pogo/            CLI entry: rank / breaker / list / help / bot, arg parsing
internal/engine/     CP + CPM + IV-floor + level-walk; GameData model + loader;
                     cpm.go · data.go · fetch.go · rank.go · damage.go (+ tests)
internal/breaker/    meta loader + 4-IV-state analyzer + `breaker` command (+ tests)
internal/service/    Command interface + registry; rank command; Resolve/name logic
internal/bot/        discordgo driver; auto slash-command registration + dispatch
data/gamemaster.json local game data (offline + for tests)
data/meta/<cp>.json  per-league top-20 meta (pvpoke overall rankings)
pvpoke_ref.py        independent JS→Python rank reference (parity oracle)
bp_ref.py            independent JS→Python damage reference (breaker parity oracle)
compare.py, fuzz.py  Go vs reference parity harnesses (rank)
```
