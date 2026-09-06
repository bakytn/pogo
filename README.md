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

## Discord bot

```sh
DISCORD_TOKEN=… ./pogo bot
```

On connect it bulk-registers **one slash command per `service.Command`**
(currently `/rank`), then answers interactions through the same `rank` handler.
A bot-invite token is required; the bot registers its commands globally.

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

The initial port was **not** byte-for-byte: the level floor (`baseLevelFloor`,
39 species) was hard-coded to 1, and the `overall` term used a different
multiplication order + a clamped HP, which flipped near-ties (e.g. Volcarona
1513 vs 1512). Both were fixed; the suite is now all-green across a 170-case
sweep. (The live site uses the same `cpms`/floor logic, so outputs match; the
Python reference is the ground truth since the running site shares the file.)

Run it:

```sh
go test ./...        # engine unit tests (pin the proven values)
python3 compare.py   # Go vs reference, 20 curated cases
python3 fuzz.py 100  # Go vs reference, 100 random cases
```

## Layout

```
cmd/pogo/            CLI entry: rank / list / help / bot, arg parsing
internal/engine/     CP + CPM + IV-floor + level-walk; GameData model + loader
                     cpm.go · data.go · fetch.go · rank.go (+ tests)
internal/service/    Command interface + registry; rank command; Resolve/name logic
internal/bot/        discordgo driver; auto slash-command registration + dispatch
data/gamemaster.json local game data (offline + for tests)
pvpoke_ref.py        independent JS→Python reference (parity oracle)
compare.py, fuzz.py  Go vs reference parity harnesses
```
