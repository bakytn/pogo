package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/bakytn/pogo/internal/engine"
)

// RankCommand implements the `rank` command: it reports where a given IV
// combination falls among all reachable IVs for the Pokémon in a league,
// mirroring the IV rank shown on pvpoke's battle/pokebox UI.
type RankCommand struct{}

// NewRankCommand returns a new rank command.
func NewRankCommand() *RankCommand { return &RankCommand{} }

func (c *RankCommand) Name() string        { return "rank" }
func (c *RankCommand) Description() string {
	return "Report a Pokémon's IV rank in a league (e.g. `rank charizard 2500`)."
}

func (c *RankCommand) Run(ctx context.Context, svc *Service, req Request) (string, error) {
	if req.Name == "" {
		return "", fmt.Errorf("usage: rank <pokemon> [iv atk,def,hp] [cp]")
	}

	poke, note, err := svc.Resolve(req)
	if err != nil {
		return "", err
	}

	// Apply the requested IVs onto the Pokémon before ranking.
	ivs := svc.defaultIVs(req)
	poke.IVs = ivs

	cp := req.CP
	if cp == 0 {
		cp = 1500 // default to Ultra League when none given
	}

	sortStat := req.SortStat
	if sortStat == "" {
		sortStat = engine.SortOverall
	}

	res := poke.GetIVRank(sortStat, cp)

	var b strings.Builder
	fmt.Fprintf(&b, "%s", res.Species)
	if t := engine.FormatTypes(res.Types); t != "" {
		fmt.Fprintf(&b, " (%s)", t)
	}
	if note != "" {
		fmt.Fprintf(&b, "\n_%s_", note)
	}
	fmt.Fprintf(&b, "\n**%s league (CP ≤ %d)**\n", leagueName(cp), cp)
	if res.Rank > 0 {
		fmt.Fprintf(&b, "IV rank: **%d** / %d\n", res.Rank, res.Count)
	} else {
		fmt.Fprintf(&b, "IV combination %d/%d/%d is not reachable at CP ≤ %d (e.g. shadow-IV floor).\n",
			res.IVs.Atk, res.IVs.Def, res.IVs.Hp, cp)
		fmt.Fprintf(&b, "Reachable IV combinations: **%d**\n", res.Count)
	}
	fmt.Fprintf(&b, "Level: %s · CP reached: %d\n", engine.FormatLevel(res.Level), res.ActualCP)
	fmt.Fprintf(&b, "IVs: %d / %d / %d\n", res.IVs.Atk, res.IVs.Def, res.IVs.Hp)
	fmt.Fprintf(&b, "Stats: Atk %.0f · Def %.0f · HP %.0f", res.Stats.Atk, res.Stats.Def, res.Stats.Hp)

	return b.String(), nil
}

// leagueName maps a league CP to its common name.
func leagueName(cp int) string {
	switch {
	case cp <= 500:
		return "Little League"
	case cp <= 1500:
		return "Great League"
	case cp <= 2500:
		return "Ultra League"
	default:
		return "Master League"
	}
}
