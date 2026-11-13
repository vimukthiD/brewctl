package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/v2"

	"dev.vimukthid/brewctl/internal/assist"
)

func TestParseInput(t *testing.T) {
	pd, _ := findTool("package_details")
	out, _ := findTool("activity_output")
	search, _ := findTool("search_catalog")
	cases := []struct {
		t   tool
		raw string
		ok  bool
	}{
		{pd, `{"name":"wget"}`, true},
		{pd, `{"name":"homebrew/core/openssl@3","kind":"formula"}`, true},
		{pd, `{"name":"wget","kind":"app"}`, false},
		{pd, `{"name":"; rm -rf /"}`, false},
		{pd, `{}`, false},
		{pd, `{"name":"wg`, false},
		{out, `{"id":3}`, true},
		{out, `{"id":0}`, false},
		{search, `{"query":"  "}`, false},
		{search, `{"query":"image resize"}`, true},
	}
	for _, c := range cases {
		_, err := parseInput(c.t, []byte(c.raw))
		if (err == nil) != c.ok {
			t.Errorf("%s %s: err = %v, want ok=%v", c.t.name, c.raw, err, c.ok)
		}
	}
}

// The request must carry the settings the assistant relies on, and only
// models with a server-side fallback may ask for one.
func TestRequestShape(t *testing.T) {
	msgs := []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("hi"))}
	for _, m := range assist.Models {
		b, err := json.Marshal(requestParams(m, nil, msgs))
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		for _, want := range []string{`"model":"` + m.ID + `"`, `"eager_input_streaming":true`, `"effort":"medium"`, `"name":"package_details"`, `"name":"suggest_action"`, `"required":["name"]`, `"cache_control":{"type":"ephemeral"}`} {
			if !strings.Contains(s, want) {
				t.Errorf("%s: request missing %s", m.ID, want)
			}
		}
		if got := strings.Contains(s, `"fallbacks":"default"`); got != m.Fallback {
			t.Errorf("%s: fallbacks sent = %v, want %v", m.ID, got, m.Fallback)
		}
		if strings.Contains(s, `"thinking"`) {
			t.Errorf("%s: thinking should be left to the model's default", m.ID)
		}
	}
	if len(assistantTools) != len(toolParams()) {
		t.Error("tool count mismatch")
	}
	if _, ok := assist.FindModel(assist.DefaultModel); !ok {
		t.Error("default model isn't in the list")
	}
}

func TestSuggestActionInput(t *testing.T) {
	sa, _ := findTool("suggest_action")
	cases := []struct {
		raw string
		ok  bool
	}{
		{`{"action":"install","packages":[{"name":"tableplus","kind":"cask"}],"reason":"A SQLite browser."}`, true},
		{`{"action":"cleanup","reason":"Frees 2 GB."}`, true},
		{`{"action":"install","packages":[{"name":"x"}]}`, false},                           // no reason
		{`{"action":"reboot","reason":"why not"}`, false},                                   // unknown action
		{`{"action":"install","packages":[{"name":"$(id)"}],"reason":"r"}`, false},          // bad name
		{`{"action":"install","packages":[{"name":"x","kind":"app"}],"reason":"r"}`, false}, // bad kind
		{`{"action":"install","packages":[{"name":"x"}],"reason":"  "}`, false},
	}
	for _, c := range cases {
		_, err := parseInput(sa, []byte(c.raw))
		if (err == nil) != c.ok {
			t.Errorf("%s: err = %v, want ok=%v", c.raw, err, c.ok)
		}
	}
}

func TestDescribeAction(t *testing.T) {
	pkgs := []ActionPkg{{Name: "wget", Kind: "formula"}, {Name: "firefox", Kind: "cask"}, {Name: "jq", Kind: "formula"}}
	title, cmd := describeAction("install", pkgs)
	if title != "Install wget, firefox and jq" {
		t.Errorf("title = %q", title)
	}
	if cmd != "brew install wget jq\nbrew install --cask firefox" {
		t.Errorf("command = %q", cmd)
	}
	title, cmd = describeAction("restart_service", []ActionPkg{{Name: "postgresql@17", Kind: "formula"}})
	if title != "Restart the postgresql@17 service" || cmd != "brew services restart postgresql@17" {
		t.Errorf("service: %q, %q", title, cmd)
	}
	if got := listNames([]string{"a", "b", "c", "d", "e"}); got != "a, b and 3 more" {
		t.Errorf("listNames = %q", got)
	}
}

func TestUserMessageNotes(t *testing.T) {
	b, _ := json.Marshal(userMessage("thanks", []string{"The user approved “Install jq”; it finished.", " "}))
	s := string(b)
	if !strings.Contains(s, `brewctl_notes\u003e\n- The user approved`) || !strings.Contains(s, `"text":"thanks"`) {
		t.Errorf("message = %s", s)
	}
	b, _ = json.Marshal(userMessage("hi", nil))
	if strings.Contains(string(b), "brewctl_notes") {
		t.Error("empty notes should add nothing")
	}
}

func TestHumanBytes(t *testing.T) {
	for n, want := range map[int64]string{999: "999 B", 1500: "1.5 kB", 11_500_000: "11.5 MB", 2_300_000_000: "2.3 GB"} {
		if got := humanBytes(n); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", n, got, want)
		}
	}
}
