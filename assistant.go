package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"

	"dev.vimukthid/brewctl/internal/assist"
	"dev.vimukthid/brewctl/internal/keychain"
)

const (
	// maxToolRounds bounds one answer's back-and-forth with the tools.
	maxToolRounds  = 12
	assistantEvent = "assistant"
)

var apiKeyItem = keychain.Item{Service: "dev.vimukthid.brewctl.anthropic", Account: "api-key"}

// AssistantService is the AI assistant: a conversation with Claude that can
// look things up through Brewctl's own data and suggest changes, which run
// only when the user approves them.
type AssistantService struct {
	brew *BrewService

	storeOnce sync.Once
	settings  *assist.Store

	mu      sync.Mutex
	history []anthropic.BetaMessageParam
	system  []anthropic.BetaTextBlockParam // frozen per conversation, so it stays cached
	turn    int
	cancel  context.CancelFunc // set while an answer is being written
	cost    float64            // this conversation so far, in US dollars
}

// AssistantStatus tells the UI whether the assistant can be used.
type AssistantStatus struct {
	HasKey    bool           `json:"hasKey"`
	KeySource string         `json:"keySource"` // "keychain", "environment" or ""
	Model     string         `json:"model"`     // the chosen model's id
	Models    []assist.Model `json:"models"`
	Busy      bool           `json:"busy"`
	Cost      float64        `json:"cost"` // this conversation, in US dollars
}

// ActionCard is a change Claude suggests. It runs only if the user approves.
type ActionCard struct {
	ID       string      `json:"id"`     // the tool call's id
	Action   string      `json:"action"` // install, upgrade, uninstall, start_service, …
	Title    string      `json:"title"`  // "Install tableplus"
	Packages []ActionPkg `json:"packages"`
	Reason   string      `json:"reason"`
	Command  string      `json:"command"` // the equivalent Terminal command
}

// ActionPkg is one package an action card acts on.
type ActionPkg struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Version string `json:"version,omitempty"` // installed, or to be installed
	Latest  string `json:"latest,omitempty"`  // for upgrades
	Desc    string `json:"desc,omitempty"`
}

// AssistantEvent streams one piece of an answer to the UI.
type AssistantEvent struct {
	Turn  int    `json:"turn"`
	Kind  string `json:"kind"` // "text", "tool", "toolDone", "done", "error"
	Text  string `json:"text,omitempty"`
	Tool  string `json:"tool,omitempty"` // tool call id
	Label string `json:"label,omitempty"`
	// Failed marks a tool call that returned an error.
	Failed bool `json:"failed,omitempty"`
	// Stop says how a turn ended: "end_turn", "stopped", "refusal" or "max_tokens".
	Stop string  `json:"stop,omitempty"`
	Cost float64 `json:"cost,omitempty"` // whole conversation, after this turn
	// Action is a suggested change (kind "action").
	Action *ActionCard `json:"action,omitempty"`
	// Refs are catalogue packages the answer may name (kind "refs"), so
	// the UI can open them even though they aren't installed.
	Refs []PkgRef `json:"refs,omitempty"`
}

func (a *AssistantService) store() *assist.Store {
	a.storeOnce.Do(func() {
		dir, err := os.UserConfigDir()
		if err != nil {
			dir = os.TempDir()
		}
		a.settings = assist.OpenStore(filepath.Join(dir, "dev.vimukthid.brewctl", "assistant.json"))
	})
	return a.settings
}

func (a *AssistantService) key(ctx context.Context) (string, string) {
	if k, err := apiKeyItem.Get(ctx); err == nil && k != "" {
		return k, "keychain"
	}
	if k := os.Getenv("ANTHROPIC_API_KEY"); k != "" {
		return k, "environment"
	}
	return "", ""
}

// Status reports whether a key is set and the conversation's cost so far.
func (a *AssistantService) Status(ctx context.Context) AssistantStatus {
	_, src := a.key(ctx)
	model := a.store().Model().ID
	a.mu.Lock()
	defer a.mu.Unlock()
	return AssistantStatus{HasKey: src != "", KeySource: src, Model: model, Models: assist.Models, Busy: a.cancel != nil, Cost: a.cost}
}

// SetModel chooses the model for the next question. The conversation
// carries on; only its cached prefix starts afresh on the new model.
func (a *AssistantService) SetModel(id string) error {
	return a.store().SetModel(id)
}

// Usage totals what the assistant has used, by day and by model.
func (a *AssistantService) Usage() assist.UsageSummary {
	return a.store().Summary(time.Now())
}

// SetAPIKey checks a key against the API, then keeps it in the Keychain.
func (a *AssistantService) SetAPIKey(ctx context.Context, key string) error {
	key = strings.TrimSpace(key)
	if !strings.HasPrefix(key, "sk-ant-") {
		return errors.New("That doesn't look like an Anthropic API key. Keys start with “sk-ant-”.")
	}
	client := anthropic.NewClient(option.WithAPIKey(key), option.WithMaxRetries(1))
	tctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if _, err := client.Models.Get(tctx, a.store().Model().ID, anthropic.ModelGetParams{}); err != nil {
		var apiErr *anthropic.Error
		if errors.As(err, &apiErr) && apiErr.StatusCode == 401 {
			return errors.New("Anthropic didn't accept this key. Check that it was copied in full and hasn't been revoked.")
		}
		return errors.New(friendlyError(err))
	}
	return apiKeyItem.Set(ctx, key)
}

// RemoveAPIKey deletes the key from the Keychain.
func (a *AssistantService) RemoveAPIKey(ctx context.Context) error {
	return apiKeyItem.Delete(ctx)
}

// NewChat forgets the conversation.
func (a *AssistantService) NewChat() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
	a.history, a.system, a.cost = nil, nil, 0
}

// Stop cancels the answer being written.
func (a *AssistantService) Stop() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		a.cancel()
	}
}

// Ask sends the user's message and streams the answer as "assistant"
// events. It returns the turn number at once; the answer arrives after.
// Notes are things Brewctl tells Claude alongside the message, such as
// what became of the changes it suggested.
func (a *AssistantService) Ask(ctx context.Context, text string, notes []string) (int, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, errors.New("nothing to send")
	}
	key, _ := a.key(ctx)
	if key == "" {
		return 0, errors.New("Add an Anthropic API key first.")
	}

	a.mu.Lock()
	if a.cancel != nil {
		a.mu.Unlock()
		return 0, errors.New("Still answering the last message.")
	}
	a.turn++
	turn := a.turn
	tctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	needSystem := a.system == nil
	a.mu.Unlock()
	model := a.store().Model()

	go func() {
		defer cancel()
		if needSystem {
			sys, err := a.buildSystem(tctx)
			if err != nil {
				a.finish(turn, 0, AssistantEvent{Kind: "error", Text: "Couldn't read your Homebrew packages: " + err.Error()})
				return
			}
			a.mu.Lock()
			a.system = sys
			a.mu.Unlock()
		}
		a.run(tctx, turn, key, model, userMessage(text, notes))
	}()
	return turn, nil
}

func (a *AssistantService) send(turn int, ev AssistantEvent) {
	ev.Turn = turn
	emit(assistantEvent, ev)
}

// finish ends a turn: on anything but a clean answer the turn is taken back
// out of the history (from index start), so the conversation stays valid.
func (a *AssistantService) finish(turn, start int, ev AssistantEvent) {
	a.mu.Lock()
	if ev.Kind == "error" || ev.Stop != "end_turn" {
		if start <= len(a.history) {
			a.history = a.history[:start]
		}
	}
	a.cancel = nil
	ev.Cost = a.cost
	a.mu.Unlock()
	a.send(turn, ev)
}

// userMessage puts Brewctl's notes ahead of what the user typed, marked so
// Claude can tell the two apart.
func userMessage(text string, notes []string) anthropic.BetaMessageParam {
	var blocks []anthropic.BetaContentBlockParamUnion
	var kept []string
	for _, n := range notes {
		if n = strings.TrimSpace(n); n != "" && len(kept) < 20 {
			if len(n) > 2000 {
				n = n[:2000] + "…"
			}
			kept = append(kept, "- "+n)
		}
	}
	if len(kept) > 0 {
		blocks = append(blocks, anthropic.NewBetaTextBlock("<brewctl_notes>\n"+strings.Join(kept, "\n")+"\n</brewctl_notes>"))
	}
	blocks = append(blocks, anthropic.NewBetaTextBlock(text))
	return anthropic.NewBetaUserMessage(blocks...)
}

// requestParams builds one request. Haiku has no server-side fallback, so
// only the other models ask for one.
func requestParams(m assist.Model, system []anthropic.BetaTextBlockParam, msgs []anthropic.BetaMessageParam) anthropic.BetaMessageNewParams {
	p := anthropic.BetaMessageNewParams{
		Model:     m.ID,
		MaxTokens: 64000,
		System:    system,
		Messages:  msgs,
		Tools:     toolParams(),
		// Cache the growing conversation too, not only the system prompt.
		CacheControl: anthropic.NewBetaCacheControlEphemeralParam(),
		OutputConfig: anthropic.BetaOutputConfigParam{Effort: anthropic.BetaOutputConfigEffortMedium},
	}
	if m.Fallback {
		// If a safety check declines a request, let the API retry it on
		// a suitable model instead of failing the answer.
		p.Fallbacks = anthropic.BetaFallbacksParamOfDefault()
		p.Betas = []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01}
	}
	return p
}

func (a *AssistantService) run(ctx context.Context, turn int, key string, model assist.Model, user anthropic.BetaMessageParam) {
	client := anthropic.NewClient(option.WithAPIKey(key))

	a.mu.Lock()
	start := len(a.history)
	a.history = append(a.history, user)
	system := a.system
	a.mu.Unlock()

	for round := 0; round < maxToolRounds; round++ {
		a.mu.Lock()
		msgs := append([]anthropic.BetaMessageParam(nil), a.history...)
		a.mu.Unlock()

		stream := client.Beta.Messages.NewStreaming(ctx, requestParams(model, system, msgs))
		msg := anthropic.BetaMessage{}
		for stream.Next() {
			ev := stream.Current()
			if err := msg.Accumulate(ev); err != nil {
				a.finish(turn, start, AssistantEvent{Kind: "error", Text: "The answer couldn't be read: " + err.Error()})
				return
			}
			if d, ok := ev.AsAny().(anthropic.BetaRawContentBlockDeltaEvent); ok {
				if t, ok := d.Delta.AsAny().(anthropic.BetaTextDelta); ok && t.Text != "" {
					a.send(turn, AssistantEvent{Kind: "text", Text: t.Text})
				}
			}
		}
		a.record(model, assist.Usage{
			Input:      msg.Usage.InputTokens,
			CacheWrite: msg.Usage.CacheCreationInputTokens,
			CacheRead:  msg.Usage.CacheReadInputTokens,
			Output:     msg.Usage.OutputTokens,
		}, round == 0)
		if err := stream.Err(); err != nil {
			if ctx.Err() != nil {
				a.finish(turn, start, AssistantEvent{Kind: "done", Stop: "stopped"})
			} else {
				a.finish(turn, start, AssistantEvent{Kind: "error", Text: friendlyError(err)})
			}
			return
		}

		switch msg.StopReason {
		case anthropic.BetaStopReasonRefusal:
			a.finish(turn, start, AssistantEvent{Kind: "done", Stop: "refusal"})
			return
		case anthropic.BetaStopReasonMaxTokens:
			a.finish(turn, start, AssistantEvent{Kind: "done", Stop: "max_tokens"})
			return
		case anthropic.BetaStopReasonToolUse:
		default:
			a.mu.Lock()
			a.history = append(a.history, msg.ToParam())
			a.mu.Unlock()
			a.finish(turn, start, AssistantEvent{Kind: "done", Stop: "end_turn"})
			return
		}

		// Run every tool call from this response, then answer them together.
		a.mu.Lock()
		a.history = append(a.history, msg.ToParam())
		a.mu.Unlock()
		var results []anthropic.BetaContentBlockParamUnion
		for _, block := range msg.Content {
			use, ok := block.AsAny().(anthropic.BetaToolUseBlock)
			if !ok {
				continue
			}
			results = append(results, a.callTool(ctx, turn, use))
		}
		if ctx.Err() != nil {
			a.finish(turn, start, AssistantEvent{Kind: "done", Stop: "stopped"})
			return
		}
		a.mu.Lock()
		a.history = append(a.history, anthropic.NewBetaUserMessage(results...))
		a.mu.Unlock()
	}
	a.finish(turn, start, AssistantEvent{Kind: "error", Text: "This took too many steps, so I stopped. Try asking something narrower."})
}

// record adds one request's usage to the conversation and the usage record.
func (a *AssistantService) record(m assist.Model, u assist.Usage, question bool) {
	if u == (assist.Usage{}) {
		return
	}
	cost := m.RequestCost(u)
	a.mu.Lock()
	a.cost += cost
	a.mu.Unlock()
	_ = a.store().Record(time.Now(), m.ID, u, cost, question)
}

func (a *AssistantService) callTool(ctx context.Context, turn int, use anthropic.BetaToolUseBlock) anthropic.BetaContentBlockParamUnion {
	raw, _ := json.Marshal(use.Input)
	t, ok := findTool(use.Name)
	if !ok {
		return anthropic.NewBetaToolResultBlock(use.ID, "Unknown tool "+use.Name, true)
	}
	in, err := parseInput(t, raw)
	if err != nil {
		bad, _ := json.Marshal(map[string]string{"INVALID_JSON": string(raw)})
		return anthropic.NewBetaToolResultBlock(use.ID, string(bad), true)
	}
	in.turn, in.callID = turn, use.ID
	label := t.label(in)
	a.send(turn, AssistantEvent{Kind: "tool", Tool: use.ID, Label: label})

	tctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	out, err := t.run(a, tctx, in)
	if err != nil {
		a.send(turn, AssistantEvent{Kind: "toolDone", Tool: use.ID, Label: label, Failed: true})
		return anthropic.NewBetaToolResultBlock(use.ID, err.Error(), true)
	}
	a.send(turn, AssistantEvent{Kind: "toolDone", Tool: use.ID, Label: label})
	var body string
	if s, ok := out.(string); ok {
		body = s
	} else {
		b, _ := json.Marshal(out)
		body = string(b)
	}
	if len(body) > 60000 {
		body = body[:60000] + "\n…(cut off)"
	}
	return anthropic.NewBetaToolResultBlock(use.ID, body, false)
}

func toolParams() []anthropic.BetaToolUnionParam {
	out := make([]anthropic.BetaToolUnionParam, 0, len(assistantTools))
	for _, t := range assistantTools {
		p := anthropic.BetaToolParam{
			Name:        t.name,
			Description: anthropic.String(t.desc),
			InputSchema: anthropic.BetaToolInputSchemaParam{Properties: t.props, Required: t.required},
			// Inputs stream as they are written; parseInput validates them.
			EagerInputStreaming: anthropic.Bool(true),
		}
		out = append(out, anthropic.BetaToolUnionParam{OfTool: &p})
	}
	return out
}

const assistantPrompt = `You are the assistant inside Brewctl, a Mac app for managing Homebrew. You help one person understand and look after the Homebrew packages on their own Mac.

How to answer:
- Be brief and concrete. Lead with the answer in plain language, then the detail that supports it. Use short paragraphs or a short list; no headings unless the answer is long.
- Write package names in backticks, exactly as Homebrew names them (` + "`openssl@3`, `firefox`" + `).
- Base answers on this Mac's real state. The inventory below was taken when this conversation started; call the tools for details and anything that may have changed. Never guess versions, sizes, dependencies or vulnerabilities - look them up.
- Before saying something is safe to remove, check what needs it with package_details.
- For security questions, rely on security_findings; don't invent CVEs or severities.

Making changes:
- You can't change anything yourself. When a change would help - installing, upgrading or uninstalling packages, starting, stopping or restarting a service, or tidying up - call suggest_action. Brewctl shows it as a card with Approve and Decline, and nothing runs unless the user approves.
- Suggest a change only when the user asked for one or it clearly helps with what they asked. One card per change; put related packages of the same action on one card. Check first (package_details, list_services) so the card is right.
- After suggesting, say in a sentence what the card does and why. Never say a change was made, or describe it as done, unless a Brewctl note says it finished.
- Uninstalling something that other installed packages need isn't possible; explain that instead of suggesting it.
- For finding software, use search_catalog, compare the good options briefly, and suggest installing the best fit if the user wants it.
- For things Brewctl can't do (pinning, editing files, anything outside Homebrew), say where or how it's done instead; give the Terminal command in a code block when that helps.

Brewctl notes:
- A user message may start with a <brewctl_notes> block. Brewctl wrote it, not the user: it reports what happened to your suggestions (approved and finished, failed, declined) and other changes. Trust it over the inventory, and take it into account without quoting it back.`

func (a *AssistantService) buildSystem(ctx context.Context) ([]anthropic.BetaTextBlockParam, error) {
	snap, err := a.brew.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	host, _ := os.Hostname()
	inv := fmt.Sprintf("This Mac (%s), inventory taken %s:\n\n%s", host, time.Now().Format("2 January 2006 15:04"), assist.Inventory(snap))
	return []anthropic.BetaTextBlockParam{
		{Text: assistantPrompt},
		{Text: inv, CacheControl: anthropic.NewBetaCacheControlEphemeralParam()},
	}, nil
}

// friendlyError turns an API failure into something to show a person.
func friendlyError(err error) string {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.StatusCode == 401:
			return "Anthropic didn't accept the API key. Check it in the assistant's settings."
		case apiErr.StatusCode == 403:
			return "This API key isn't allowed to use this model. Try another one from the model menu."
		case apiErr.StatusCode == 429:
			return "Too many requests right now. Wait a moment and try again."
		case apiErr.StatusCode == 529 || apiErr.StatusCode >= 500:
			return "Anthropic's service is busy or having trouble. Try again in a moment."
		case apiErr.StatusCode == 400 && strings.Contains(strings.ToLower(apiErr.Error()), "credit"):
			return "The Anthropic account is out of credit."
		}
		return fmt.Sprintf("The request failed (%d).", apiErr.StatusCode)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "Anthropic took too long to answer. Try again."
	}
	return "Couldn't reach Anthropic. Check your internet connection."
}
