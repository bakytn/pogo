// Command pogo is a Pokémon GO battle helper. It currently ships with the
// `rank` command (the IV rank shown on pvpoke's battle page) and a Discord
// bot driver; new commands are added by implementing service.Command.
//
// Examples:
//
//	pogo rank charizard                 # 15/15/15 in Ultra League (1500 CP)
//	pogo rank charizard 2500            # 15/15/15 in Ultra League
//	pogo rank charizard 2500 12 10 8    # custom IVs in Ultra League
//	pogo rank charizard 1500 15 15 15   # Great League, perfect IVs
//	pogo rank charizard 1500 15 15 15 atk   # rank by attack instead of overall
//	pogo list                           # list commands
//	pogo help                           # help text
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/bakytn/pogo/internal/bot"
	"github.com/bakytn/pogo/internal/breaker"
	"github.com/bakytn/pogo/internal/engine"
	"github.com/bakytn/pogo/internal/service"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usage()
	}

	// Special sub-commands.
	switch strings.ToLower(args[0]) {
	case "help", "-h", "--help":
		fmt.Print(usageText())
		return nil
	}

	// Global flag set for data path / URL.
	fs := flag.NewFlagSet("pogo", flag.ContinueOnError)
	dataDir := fs.String("data", "data", "directory containing gamemaster.json")
	fresh := fs.Bool("fetch", false, "re-download game data before running")
	dataURL := fs.String("url", engine.DefaultDataURL, "game data URL to fetch")
	fs.String("token", "", "Discord bot token (or set DISCORD_TOKEN)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	rest := fs.Args()
	if len(rest) == 0 {
		return usage()
	}

	// Ensure game data is on disk (fetch on first run or with -fetch).
	path := engine.DataPath(*dataDir, "gamemaster.json")
	if *fresh || engine.DiskSize(path) < 0 {
		n, err := engine.FetchGameData(*dataURL, path)
		if err != nil {
			return fmt.Errorf("fetch game data: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Downloaded %s (%d bytes)\n", path, n)
	}

	gd, err := engine.NewGameData(path)
	if err != nil {
		return fmt.Errorf("load game data: %w", err)
	}

	svc := service.New(gd)

	// Register the breaker command on the shared Service so `list`/help show
	// it and the Discord bot auto-registers it. The adapter bridges breaker's
	// (GameData, Loader, Request) Run onto the service.Command interface.
	breakerLoader := breaker.NewLoader("")
	svc.Register(&breakerCommand{loader: breakerLoader})

	// `list` sub-command: show registered commands.
	if strings.ToLower(rest[0]) == "list" {
		fmt.Print(svc.Help())
		return nil
	}

	// `bot` sub-command: run the Discord bot (token from DISCORD_TOKEN env or
	// -token flag). This is the same service the CLI uses, so every command
	// registered on the Service is exposed as a Discord slash command.
	if strings.ToLower(rest[0]) == "bot" {
		token := os.Getenv("DISCORD_TOKEN")
		if t := fs.Lookup("token"); t != nil {
			if v := t.Value.String(); v != "" {
				token = v
			}
		}
		if token == "" {
			return fmt.Errorf("bot requires a token: set DISCORD_TOKEN or pass -token")
		}
		return runBot(token, svc)
	}

	// Dispatch to the named command.
	cmd, ok := svc.Command(rest[0])
	if !ok {
		return fmt.Errorf("unknown command %q\n%s", rest[0], svc.Help())
	}
	req, err := parseRequest(cmd.Name(), rest[1:])
	if err != nil {
		return err
	}
	out, err := cmd.Run(context.Background(), svc, req)
	if err != nil {
		return err
	}
	fmt.Println(out)
	return nil
}

// runBot launches the Discord bot and blocks until interrupted (SIGINT/SIGTERM)
// or until the Discord session errors out.
// breakerCommand adapts the breaker package's (GameData, Loader, Request)
// command onto the service.Command interface so it is registered on the
// shared Service. This lives in main (not in breaker or service) to avoid an
// import cycle: breaker must not import service, and service must not import
// breaker.
type breakerCommand struct {
	loader *breaker.Loader
}

func (c *breakerCommand) Name() string        { return breaker.NewCommand().Name() }
func (c *breakerCommand) Description() string { return breaker.NewCommand().Description() }
func (c *breakerCommand) Run(ctx context.Context, svc *service.Service, req service.Request) (string, error) {
	ivs := req.IVs
	if ivs == nil {
		ivs = &engine.IVs{Atk: 15, Def: 15, Hp: 15}
	}
	dump := req.Extra != nil && req.Extra["dump"] == "1"
	return breaker.NewCommand().Run(svc.Data, c.loader, breaker.Request{
		Name:   req.Name,
		IVs:    *ivs,
		CP:     req.CP,
		Shadow: req.Shadow,
		League: req.League,
		Dump:   dump,
	})
}

func runBot(token string, svc *service.Service) error {
	b, err := bot.New(token, svc)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return b.Run(ctx)
}

// parseRequest turns CLI positional args into a service.Request for a command.
func parseRequest(name string, args []string) (service.Request, error) {
	req := service.Request{}

	// rank <pokemon> [cp] [atk def hp] [stat]
	// We accept a flexible argument order:
	//   first arg = pokemon name
	//   ints are assigned to cp, then atk/def/hp (three), in order of appearance
	//   a trailing word in {atk,def,hp,overall} is the sort stat
	if len(args) == 0 {
		return req, fmt.Errorf("missing Pokémon name")
	}
	req.Name = args[0]

	var ints []int
	var words []string
	for _, a := range args[1:] {
		if v, err := strconv.Atoi(a); err == nil {
			ints = append(ints, v)
			continue
		}
		words = append(words, strings.ToLower(a))
	}

	// Assign ints: if 3+ ints, first is cp, next three are IVs.
	// If fewer, infer: a single int is cp; 3 ints are IVs (cp stays default 1500).
	if len(ints) >= 4 {
		req.CP = ints[0]
		ivs := engine.IVs{Atk: ints[1], Def: ints[2], Hp: ints[3]}
		req.IVs = &ivs
	} else if len(ints) == 3 {
		ivs := engine.IVs{Atk: ints[0], Def: ints[1], Hp: ints[2]}
		req.IVs = &ivs
	} else if len(ints) == 1 {
		req.CP = ints[0]
	} else if len(ints) == 2 {
		req.CP = ints[0]
		// second int alone is ambiguous; treat as atk IV with def/hp 15
		ivs := engine.IVs{Atk: ints[0], Def: 15, Hp: 15}
		req.IVs = &ivs
	}

	for _, w := range words {
		switch w {
		case "atk", "attack":
			req.SortStat = engine.SortAtk
		case "def", "defense":
			req.SortStat = engine.SortDef
		case "hp", "health":
			req.SortStat = engine.SortHp
		case "overall":
			req.SortStat = engine.SortOverall
		case "shadow":
			req.Shadow = true
		case "little", "great", "ultra", "master":
			req.League = w
		case "dump":
			req.Extra = map[string]string{"dump": "1"}
		}
	}
	// A trailing league CP (500/1500/2500/10000) sets req.CP, which the
	// breaker command maps to a league. Already handled by the int logic above.
	return req, nil
}

func usage() error {
	fmt.Fprint(os.Stderr, usageText())
	return fmt.Errorf("no command")
}

func usageText() string {
	return `pogo — Pokémon GO battle helper

Usage:
  pogo <command> [args...] [flags]

Commands:
  rank <pokemon> [cp] [atk def hp] [stat]   Report a Pokémon's IV rank in a league
  list                                       List all commands
  help                                       Show this help

Examples:
  pogo rank charizard                       15/15/15 in Great League (1500)
  pogo rank charizard 2500                  15/15/15 in Ultra League
  pogo rank charizard 2500 12 10 8          custom IVs in Ultra League
  pogo rank charizard 1500 15 15 15 atk     rank by attack stat

Flags:
  -data DIR      directory containing gamemaster.json (default "data")
  -fetch         re-download game data before running
  -url URL       game data URL (default ` + engine.DefaultDataURL + `)

`
}
