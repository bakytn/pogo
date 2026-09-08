package bot

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/bakytn/pogo/internal/engine"
	"github.com/bakytn/pogo/internal/service"
)

// Bot drives a Discord application using the shared service. It registers one
// Discord slash command per registered service.Command, so adding a new
// capability to the Service is automatically exposed in Discord.
type Bot struct {
	dg    *discordgo.Session
	svc   *service.Service
	appID string
}

// New builds a Bot around the given token and shared service.
func New(token string, svc *service.Service) (*Bot, error) {
	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("init discord: %w", err)
	}
	// discordgo.New already sets Intents to IntentsAllWithoutPrivileged,
	// which includes the privileged MessageContent intent. Keep it.
	return &Bot{dg: dg, svc: svc}, nil
}

// Run starts the session, registers slash commands on ready, and blocks until
// ctx is cancelled or the connection closes.
func (b *Bot) Run(ctx context.Context) error {
	b.dg.AddHandler(b.onReady)
	b.dg.AddHandler(b.onInteraction)

	if err := b.dg.Open(); err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer b.dg.Close()

	log.Printf("logged in as %s — bot ready", b.dg.State.User.Username)

	select {
	case <-ctx.Done():
		return nil
	}
}

// onReady registers slash commands once the application id is known.
func (b *Bot) onReady(_ *discordgo.Session, r *discordgo.Ready) {
	b.appID = r.User.ID
	if err := b.registerCommands(); err != nil {
		log.Printf("failed to register slash commands: %v", err)
	}
}

// registerCommands creates one application command per registered service
// command. guildID "" registers them globally.
func (b *Bot) registerCommands() error {
	var cmds []*discordgo.ApplicationCommand
	for _, name := range b.svc.Commands() {
		cmd, ok := b.svc.Command(name)
		if !ok {
			continue
		}
		cmds = append(cmds, b.buildCommand(name, cmd.Description()))
	}
	if len(cmds) == 0 {
		return nil
	}
	_, err := b.dg.ApplicationCommandBulkOverwrite(b.appID, "", cmds)
	return err
}

// buildCommand translates a service command into Discord options. The rank
// command gets rich typed options; other commands still get the same options
// so they keep working.
func (b *Bot) buildCommand(name, description string) *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Type:        discordgo.ChatApplicationCommand,
		Name:        name,
		Description: description,
		Options: []*discordgo.ApplicationCommandOption{
			stringOpt("pokemon", "Pokémon name (e.g. charizard)", true),
			intOpt("cp", "League CP ceiling (500 / 1500 / 2500 / 10000)", false, 10, 10000),
			intOpt("atk", "Attack IV (0–15)", false, 0, 15),
			intOpt("def", "Defense IV (0–15)", false, 0, 15),
			intOpt("hp", "HP IV (0–15)", false, 0, 15),
			choiceOpt("stat", "Rank by which stat", []string{"overall", "atk", "def", "hp"}, false),
			boolOpt("shadow", "Shadow form (e.g. /breaker charizard shadow)", false),
			boolOpt("fast", "Report fast moves only (default: all moves)", false),
			stringOpt("move", "Specific move to report (e.g. shadow_claw); implies fast", false),
		},
	}
}

func stringOpt(name, desc string, required bool) *discordgo.ApplicationCommandOption {
	return &discordgo.ApplicationCommandOption{
		Type: discordgo.ApplicationCommandOptionString,
		Name: name, Description: desc, Required: required,
	}
}

func boolOpt(name, desc string, required bool) *discordgo.ApplicationCommandOption {
	return &discordgo.ApplicationCommandOption{
		Type: discordgo.ApplicationCommandOptionBoolean,
		Name: name, Description: desc, Required: required,
	}
}

func intOpt(name, desc string, required bool, min, max int) *discordgo.ApplicationCommandOption {
	mf, mf2 := float64(min), float64(max)
	return &discordgo.ApplicationCommandOption{
		Type: discordgo.ApplicationCommandOptionInteger,
		Name: name, Description: desc, Required: required,
		MinValue: &mf, MaxValue: mf2,
	}
}

func choiceOpt(name, desc string, choices []string, required bool) *discordgo.ApplicationCommandOption {
	vals := make([]*discordgo.ApplicationCommandOptionChoice, len(choices))
	for i, c := range choices {
		vals[i] = &discordgo.ApplicationCommandOptionChoice{Name: c, Value: c}
	}
	return &discordgo.ApplicationCommandOption{
		Type: discordgo.ApplicationCommandOptionString,
		Name: name, Description: desc, Required: required, Choices: vals,
	}
}

// onInteraction handles chat (slash) command invocations.
func (b *Bot) onInteraction(_ *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}
	// Acknowledge immediately so Discord doesn't time out with "bot failed".
	if err := b.dg.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: "⏳ Calculating…"},
	}); err != nil {
		log.Printf("respond: %v", err)
	}

	data := i.ApplicationCommandData()
	name := strings.ToLower(data.Name)

	cmd, ok := b.svc.Command(name)
	if !ok {
		b.followup(i, fmt.Sprintf("Unknown command `%s`.\n%s", name, b.svc.Help()))
		return
	}

	req := b.buildRequest(data)
	out, err := cmd.Run(context.Background(), b.svc, req)
	if err != nil {
		out = "❌ " + err.Error()
	}
	b.followup(i, out)
}

// buildRequest maps slash-command option values into a service.Request.
func (b *Bot) buildRequest(d discordgo.ApplicationCommandInteractionData) service.Request {
	req := service.Request{}

	getStr := func(key string) string {
		for _, o := range d.Options {
			if o.Name == key && o.Type == discordgo.ApplicationCommandOptionString {
				v, _ := o.Value.(string)
				return v
			}
		}
		return ""
	}
	getInt := func(key string) (int, bool) {
		for _, o := range d.Options {
			if o.Name == key && o.Type == discordgo.ApplicationCommandOptionInteger {
				v, _ := o.Value.(float64)
				return int(v), true
			}
		}
		return 0, false
	}
	getBool := func(key string) (bool, bool) {
		for _, o := range d.Options {
			if o.Name == key && o.Type == discordgo.ApplicationCommandOptionBoolean {
				v, _ := o.Value.(bool)
				return v, true
			}
		}
		return false, false
	}

	req.Name = getStr("pokemon")
	if cp, ok := getInt("cp"); ok {
		req.CP = cp
	}
	av, hasAtk := getInt("atk")
	dv, hasDef := getInt("def")
	hv, hasHp := getInt("hp")
	if hasAtk || hasDef || hasHp {
		ivs := engine.IVs{Atk: 15, Def: 15, Hp: 15}
		if hasAtk {
			ivs.Atk = av
		}
		if hasDef {
			ivs.Def = dv
		}
		if hasHp {
			ivs.Hp = hv
		}
		req.IVs = &ivs
	}
	if stat := getStr("stat"); stat != "" {
		req.SortStat = engine.SortStat(stat)
	}
	if shadow, ok := getBool("shadow"); ok && shadow {
		req.Shadow = true
	}
	if req.Extra == nil {
		req.Extra = map[string]string{}
	}
	// fast (bool) and move (string) both drive the breaker's report filters.
	fast, fastSet := getBool("fast")
	move := getStr("move")
	if fastSet && fast || move != "" {
		req.Extra["fast"] = "1"
	}
	if move != "" {
		req.Extra["move"] = move
	}
	return req
}

func (b *Bot) followup(i *discordgo.InteractionCreate, content string) {
	if content == "" {
		content = "_(no result)_"
	}
	if len(content) > 1900 {
		content = content[:1897] + "…"
	}
	_, err := b.dg.FollowupMessageCreate(b.appID, i.Interaction, true, &discordgo.WebhookParams{Content: content})
	if err != nil {
		log.Printf("followup: %v", err)
	}
}
