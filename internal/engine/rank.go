package engine

import (
	"fmt"
	"math"
)

// SortStat names a sortable ranking metric for IV combinations.
type SortStat string

const (
	SortOverall SortStat = "overall"
	SortAtk     SortStat = "atk"
	SortDef     SortStat = "def"
	SortHp      SortStat = "hp"
)

// Combination is a single (level, IVs) candidate with its derived stats.
type Combination struct {
	Level   float64
	IVs     IVs
	Atk     float64
	Def     float64
	Hp      int     // display HP: floor(cpm*(baseHp+hpIV)) clamped to >=10
	RawHp   float64 // raw floor(cpm*(baseHp+hpIV)), no clamp — mirrors pvpoke line 583
	Overall float64 // (rawHp*atk)*def, same op order as pvpoke line 584
	CP      int
}

// RankResult is the result of ranking a Pokémon's IV combination.
type RankResult struct {
	Species   string
	SpeciesID string
	Types     []string
	CP        int      // target league CP used for the rank
	Level     float64  // level at which the Pokémon sits at the given CP/IVs
	IVs       IVs      // the IV combination that was ranked
	Stats     Stats    // derived battle stats
	ActualCP  int      // the CP the ranked IV combo actually reaches at its level
	Rank      int      // 1-based rank (1 = best); 0 if the IV combo wasn't reachable
	Count     int      // total reachable IV combinations for this Pokémon/CP
}

// ivFloor mirrors the IV floor rules from pvpoke's generateIVCombinations:
//   - legendary/ultrabeast (not wildlegendary): 1
//   - legendary/ultrabeast that is shadow: 6
//   - untradeable: 10
//   - has RETURN: 2
func (p *Pokemon) ivFloor() int {
	floor := 0
	if (p.HasTag("legendary") || p.HasTag("ultrabeast")) && !p.HasTag("wildlegendary") {
		floor = 1
	}
	if (p.HasTag("legendary") || p.HasTag("ultrabeast")) && (p.IsShadow() || p.HasTag("shadow")) {
		floor = 6
	}
	if p.HasTag("untradeable") {
		floor = 10
	}
	if p.HasMove("RETURN") {
		floor = 2
	}
	return floor
}

// levelCapForCP returns the level cap for the given league CP. pvpoke uses a
// fixed level cap of 50 for all leagues; the CP ceiling is the binding
// constraint — the enumeration loop stops once the CP reaches the target.
// For legendaries the level cap is 40 (set in game data), but a cap of 50
// is safe and correct: any combo that "overachieves" at level > 40 for a
// legendary will be filtered by the CP target, so the results are identical.
func levelCapForCP(_ int) float64 {
	return 50
}

// GenerateIVCombinations enumerates every reachable (IV, level) combination
// for a Pokémon at the given CP, sorted descending by the given sortStat.
// This is a faithful Go port of pvpoke's Pokemon.generateIVCombinations.
func (p *Pokemon) GenerateIVCombinations(sortStat SortStat, targetCP int) []Combination {
	levelCap := levelCapForCP(targetCP)
	// baseLevelFloor: the minimum level a combo may sit at. Most Pokémon are 1;
	// exclusives/legendaries carry 8/15/20 in the game data. (pvpoke:
	// this.baseLevelFloor = 1, then if(data.levelFloor) baseLevelFloor = data.levelFloor)
	baseLevelFloor := 1.0
	if p.LevelFloor > 0 {
		baseLevelFloor = p.LevelFloor
	}
	floor := p.ivFloor()

	combinations := make([]Combination, 0, 4096)
	// The pvpoke source starts at 15 and loops down; we replicate the
	// same enumeration order (hp → def → atk, 15..floor).
	for hpIV := 15; hpIV >= floor; hpIV-- {
		for defIV := 15; defIV >= floor; defIV-- {
			for atkIV := 15; atkIV >= floor; atkIV-- {
				ivs := IVs{Atk: atkIV, Def: defIV, Hp: hpIV}

				// Little Cup (targetCP <= 500) ignores the level floor;
				// everything else starts at baseLevelFloor. (pvpoke lines 542-546)
				level := 0.5
				if targetCP > 500 {
					level = baseLevelFloor
				}

				calcCP := 0
				for level < levelCap && calcCP < targetCP {
					level += 0.5
					m := cpForLevel(level)
					calcCP = p.CPAt(m, ivs)
				}
				if calcCP > targetCP {
					level -= 0.5
				}
				m := cpForLevel(level)
				calcCP = p.CPAt(m, ivs)

				if calcCP > targetCP {
					continue
				}

				atk := m * (p.BaseStats.Atk + float64(atkIV))
				def := m * (p.BaseStats.Def + float64(defIV))
				// pvpoke line 583: hp is the RAW floor, no max-10 clamp; the clamp
				// is applied only to the *display* hp, not to the overall used for
				// ranking. Line 584: overall = (hp * atk * def), which JS evaluates
				// left-to-right as (hp*atk)*def. We must keep both the raw hp AND the
				// op order identical or near-ties in overall reorder the rank.
				rawHp := math.Floor(m * (p.BaseStats.Hp + float64(hpIV)))
				hp := int(rawHp)
				if hp < 10 {
					hp = 10
				}
				overall := (rawHp * atk) * def

				combinations = append(combinations, Combination{
					Level:   level,
					IVs:     ivs,
					Atk:     atk,
					Def:     def,
					Hp:      hp,
					RawHp:   rawHp,
					Overall: overall,
					CP:      calcCP,
				})
			}
		}
	}

	sortCombinations(combinations, sortStat, 1)
	return combinations
}

func sortCombinations(combos []Combination, stat SortStat, dir int) {
	for i := 1; i < len(combos); i++ {
		for j := i; j > 0; j-- {
			a := val(combos[j], stat)
			b := val(combos[j-1], stat)
			if a == b {
				break
			}
			// dir=1 means descending (best first)
			if dir == 1 && a < b {
				break
			}
			if dir == -1 && a > b {
				break
			}
			combos[j], combos[j-1] = combos[j-1], combos[j]
		}
	}
}

func val(c Combination, stat SortStat) float64 {
	switch stat {
	case SortAtk:
		return c.Atk
	case SortDef:
		return c.Def
	case SortHp:
		return float64(c.Hp)
	default:
		return c.Overall
	}
}

// GetIVRank returns the 1-based rank of the Pokémon's current IV combination
// within all reachable IVs for the given target CP, sorted by sortStat
// descending (best first). This mirrors pvpoke's Pokemon.getIVRank.
func (p *Pokemon) GetIVRank(sortStat SortStat, targetCP int) RankResult {
	combinations := p.GenerateIVCombinations(sortStat, targetCP)

	// The user's IV combo (already set on p.IVs).
	target := p.IVs

	rank := 0
	for i, c := range combinations {
		if c.IVs == target {
			rank = i + 1
			break
		}
	}

	// Find the level/CP for the user's IV combo for display purposes.
	displayCP := 0
	displayLevel := 0.0
	if rank > 0 {
		displayCP = combinations[rank-1].CP
		displayLevel = combinations[rank-1].Level
	} else {
		// The exact combo wasn't in the reachable set (e.g. a shadow form
		// with 0-0-0 on a cup whose floor is 6). Report the best CP/level
		// the combo does reach.
		m := cpForLevel(levelCapForCP(targetCP))
		displayCP = p.CPAt(m, target)
		displayLevel = levelCapForCP(targetCP)
	}

	st := p.SetStats(cpForLevel(displayLevel), target)

	result := RankResult{
		Species:   p.SpeciesName,
		SpeciesID: p.SpeciesID,
		Types:     p.Types,
		CP:        targetCP,
		Level:     displayLevel,
		IVs:       target,
		Stats:     st,
		ActualCP:  displayCP,
		Rank:      rank,
		Count:     len(combinations),
	}
	return result
}

func pct(v, max float64) float64 {
	if max == 0 {
		return 0
	}
	return v / max * 100
}

// FormatLevel renders a level in 0.5 increments the way the site does.
func FormatLevel(l float64) string {
	return fmt.Sprintf("%.1f", l)
}

// FormatTypes joins the Pokémon's types for display.
func FormatTypes(types []string) string {
	out := ""
	for i, t := range types {
		if i > 0 {
			out += "/"
		}
		out += t
	}
	return out
}

