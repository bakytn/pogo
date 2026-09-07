package engine

import "math"

// DamageMultiplier constants — verbatim from pvpoke's src/js/battle/DamageCalculator.js.
// The long decimals are the exact binary32->binary64 values the site uses, so
// damage lands on the same integer floor as pvpoke for any given stat pair.
const (
	DmgBonus          = 1.2999999523162841796875
	DmgSuperEffective = 1.60000002384185791015625
	DmgResisted       = 0.625
	DmgDoubleResisted = 0.390625
	DmgStab           = 1.2000000476837158203125
	DmgShadowAtk      = 1.2
	DmgShadowDef      = 0.83333331
)

// Attack is a move with the attributes the damage formula needs.
type Attack struct {
	ID         string  `json:"moveId"`
	Name       string  `json:"name"`
	Type       string  `json:"type"`
	Power      float64 `json:"power"`
	Cooldown   float64 `json:"cooldown"`
	Energy     float64 `json:"energy"`
	EnergyGain float64 `json:"energyGain"`
	Stab       float64 `json:"-"` // set from the user's Pokémon types at call time
}

// BattleState is the battle-ready form of a Pokémon for one (IV, league)
// combo: the CP-walked level, the CPM-derived atk/def/hp, and the stat
// product (matching pvpoke's stats.product scale, /1000).
type BattleState struct {
	Level   float64
	IVs     IVs
	Atk     float64
	Def     float64
	Hp      float64
	Product float64
}

// typeTraits is pvpoke's getTypeTraits table (weaknesses take priority over
// resistances, resistances over immunities) as per-type sets.
var typeWeaknesses = map[string][]string{
	"normal":   {"fighting"},
	"fighting": {"flying", "psychic", "fairy"},
	"flying":   {"rock", "electric", "ice"},
	"poison":   {"ground", "psychic"},
	"ground":   {"water", "grass", "ice"},
	"rock":     {"fighting", "ground", "steel", "water", "grass"},
	"bug":      {"flying", "rock", "fire"},
	"ghost":    {"ghost", "dark"},
	"steel":    {"fighting", "ground", "fire"},
	"fire":     {"ground", "rock", "water"},
	"water":    {"grass", "electric"},
	"grass":    {"flying", "poison", "bug", "fire", "ice"},
	"electric": {"ground"},
	"psychic":  {"bug", "ghost", "dark"},
	"ice":      {"fighting", "fire", "steel", "rock"},
	"dragon":   {"dragon", "ice", "fairy"},
	"dark":     {"fighting", "fairy", "bug"},
	"fairy":    {"poison", "steel"},
}

var typeResistances = map[string][]string{
	"normal":   {},
	"fighting": {"rock", "bug", "dark"},
	"flying":   {"fighting", "bug", "grass"},
	"poison":   {"fighting", "poison", "bug", "fairy", "grass"},
	"ground":   {"poison", "rock"},
	"rock":     {"normal", "flying", "poison", "fire"},
	"bug":      {"fighting", "ground", "grass"},
	"ghost":    {"poison", "bug"},
	"steel":    {"normal", "flying", "rock", "bug", "steel", "grass", "psychic", "ice", "dragon", "fairy"},
	"fire":     {"bug", "steel", "fire", "grass", "ice", "fairy"},
	"water":    {"steel", "fire", "water", "ice"},
	"grass":    {"ground", "water", "grass", "electric"},
	"electric": {"flying", "steel", "electric"},
	"psychic":  {"fighting", "psychic"},
	"ice":      {"ice"},
	"dragon":   {"fire", "water", "grass", "electric"},
	"dark":     {"ghost", "dark"},
	"fairy":    {"fighting", "bug", "dark"},
}

var typeImmunities = map[string][]string{
	"normal":   {"ghost"},
	"fighting": {},
	"flying":   {"ground"},
	"poison":   {},
	"ground":   {"electric"},
	"rock":     {},
	"bug":      {},
	"ghost":    {"normal", "fighting"},
	"steel":    {"poison"},
	"fire":     {},
	"water":    {},
	"grass":    {},
	"electric": {},
	"psychic":  {},
	"ice":      {},
	"dragon":   {},
	"dark":     {"psychic"},
	"fairy":    {"dragon"},
}

// containsSet reports whether s is in the slice.
func containsSet(set []string, s string) bool {
	for _, v := range set {
		if v == s {
			return true
		}
	}
	return false
}

// Effectiveness ports DamageCalculator.getEffectiveness: folds the defender's
// type traits for a move type, using the site's exact multipliers.
func Effectiveness(moveType string, defenderTypes []string) float64 {
	eff := 1.0
	for _, dt := range defenderTypes {
		if containsSet(typeWeaknesses[dt], moveType) {
			eff *= DmgSuperEffective
		} else if containsSet(typeResistances[dt], moveType) {
			eff *= DmgResisted
		} else if containsSet(typeImmunities[dt], moveType) {
			eff *= DmgDoubleResisted
		}
	}
	return eff
}

// Stab ports Pokemon.getStab: 1.2 if the move matches either of the
// attacker's types, else 1.
func Stab(moveType string, types []string) float64 {
	if len(types) > 0 && moveType == types[0] {
		return DmgStab
	}
	if len(types) > 1 && moveType == types[1] {
		return DmgStab
	}
	return 1.0
}

// Damage ports DamageCalculator.damage for the standard damage method:
//
//	floor(power * stab * (atk/def) * effectiveness * charge * 0.5 * BONUS) + 1
//
// getEffectiveStat is folded in: shadow forms apply the 1.2 atk / 0.83333331
// def multipliers inside the atk/def values. (Form-specific edge cases such
// as aegislash_shield and percentMaxHP moves are not ported: they do not
// affect the standard IV delta / top-meta use case.)
func Damage(atk, def, power, stab, effectiveness float64) int {
	return int(math.Floor(power*stab*(atk/def)*effectiveness*0.5*DmgBonus)) + 1
}

// DamageForMove computes a move's damage against a defender. attack/defense
// are the *effective* stats (shadow/buff multipliers already folded in).
func DamageForMove(move Attack, types []string, defenderTypes []string, attack, defense float64) int {
	stab := Stab(move.Type, types)
	eff := Effectiveness(move.Type, defenderTypes)
	return Damage(attack, defense, move.Power, stab, eff)
}

// Breakpoint ports DamageCalculator.breakpoint: the minimum effective attack
// needed for the move to deal exactly `damage` to a defender with the given
// effective defense.
func Breakpoint(damage, defense, power, stab, effectiveness float64) float64 {
	return (float64(damage) - 1) * defense / (power * stab * effectiveness * 0.5 * DmgBonus)
}

// Bulkpoint ports DamageCalculator.bulkpoint: the minimum effective defense
// that holds the attacker to exactly `damage`.
func Bulkpoint(damage, attack, power, stab, effectiveness float64) float64 {
	return (power * stab * effectiveness * 0.5 * DmgBonus * attack) / float64(damage)
}
