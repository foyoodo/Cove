package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func decodeRequest(t *testing.T, cfg Config, history []Message) map[string]any {
	t.Helper()
	body, err := chatRequestBody(cfg, history)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "SECRET-REASONING") {
		t.Fatalf("reasoning was sent back to the API: %s", body)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func baseCfg(provider string) Config {
	return Config{
		Provider: provider, Model: "m", BaseURL: "https://example.com/v1",
		APIKey: "k", Temperature: f64p(0.2),
	}
}

func TestReasoningParamsOmittedByDefault(t *testing.T) {
	history := []Message{{
		Role: roleUser, Content: "Hi", Reasoning: "SECRET-REASONING", Done: true,
	}, {
		Role: roleAssistant, Content: "Yo", Reasoning: "SECRET-REASONING", Done: true,
	}}
	for _, provider := range []string{"OpenAI", "DeepSeek", "OpenRouter", "xAI", "Ollama", "Custom"} {
		m := decodeRequest(t, baseCfg(provider), history)
		for _, key := range []string{"reasoning_effort", "reasoning", "thinking", "think"} {
			if _, ok := m[key]; ok {
				t.Errorf("%s default included %s: %#v", provider, key, m[key])
			}
		}
		msgs, _ := m["messages"].([]any)
		if len(msgs) != 2 {
			t.Errorf("%s messages: %#v", provider, msgs)
		}
	}
}

func TestReasoningParamsByProvider(t *testing.T) {
	history := []Message{{Role: roleUser, Content: "Hi", Reasoning: "SECRET-REASONING"}}
	cases := []struct {
		name string
		cfg  Config
		want func(t *testing.T, m map[string]any)
	}{
		{
			name: "openai high",
			cfg:  Config{Provider: "OpenAI", Reasoning: effortHigh},
			want: func(t *testing.T, m map[string]any) {
				if m["reasoning_effort"] != "high" {
					t.Fatalf("%#v", m["reasoning_effort"])
				}
			},
		},
		{
			name: "openai off",
			cfg:  Config{Provider: "OpenAI", Reasoning: effortOff},
			want: func(t *testing.T, m map[string]any) {
				if m["reasoning_effort"] != "none" {
					t.Fatalf("%#v", m["reasoning_effort"])
				}
			},
		},
		{
			name: "xai minimal",
			cfg:  Config{Provider: "xAI", Reasoning: effortMinimal},
			want: func(t *testing.T, m map[string]any) {
				if m["reasoning_effort"] != "minimal" {
					t.Fatalf("%#v", m["reasoning_effort"])
				}
			},
		},
		{
			name: "openrouter medium",
			cfg:  Config{Provider: "OpenRouter", Reasoning: effortMedium},
			want: func(t *testing.T, m map[string]any) {
				obj, _ := m["reasoning"].(map[string]any)
				if obj["effort"] != "medium" || obj["enabled"] != true {
					t.Fatalf("%#v", obj)
				}
				if _, ok := obj["exclude"]; ok {
					t.Fatal("exclude would hide the reasoning text")
				}
			},
		},
		{
			name: "openrouter off",
			cfg:  Config{Provider: "OpenRouter", Reasoning: effortOff},
			want: func(t *testing.T, m map[string]any) {
				obj, _ := m["reasoning"].(map[string]any)
				if obj["effort"] != "none" || obj["enabled"] != false {
					t.Fatalf("%#v", obj)
				}
			},
		},
		{
			name: "deepseek high",
			cfg:  Config{Provider: "DeepSeek", Reasoning: effortHigh},
			want: func(t *testing.T, m map[string]any) {
				think, _ := m["thinking"].(map[string]any)
				if think["type"] != "enabled" || m["reasoning_effort"] != "high" {
					t.Fatalf("thinking %#v effort %#v", think, m["reasoning_effort"])
				}
			},
		},
		{
			name: "deepseek off",
			cfg:  Config{Provider: "DeepSeek", Reasoning: effortOff},
			want: func(t *testing.T, m map[string]any) {
				think, _ := m["thinking"].(map[string]any)
				if think["type"] != "disabled" {
					t.Fatalf("%#v", think)
				}
				if _, ok := m["reasoning_effort"]; ok {
					t.Fatal("off should not also send reasoning_effort")
				}
			},
		},
		{
			name: "ollama low",
			cfg:  Config{Provider: "Ollama", Reasoning: effortLow},
			want: func(t *testing.T, m map[string]any) {
				if m["reasoning_effort"] != "low" || m["think"] != "low" {
					t.Fatalf("effort %#v think %#v", m["reasoning_effort"], m["think"])
				}
			},
		},
		{
			name: "ollama minimal",
			cfg:  Config{Provider: "Ollama", Reasoning: effortMinimal},
			want: func(t *testing.T, m map[string]any) {
				if m["reasoning_effort"] != "minimal" || m["think"] != "low" {
					t.Fatalf("effort %#v think %#v", m["reasoning_effort"], m["think"])
				}
			},
		},
		{
			name: "ollama off",
			cfg:  Config{Provider: "Ollama", Reasoning: effortOff},
			want: func(t *testing.T, m map[string]any) {
				if m["reasoning_effort"] != "none" || m["think"] != false {
					t.Fatalf("effort %#v think %#v", m["reasoning_effort"], m["think"])
				}
			},
		},
		{
			name: "custom none",
			cfg:  Config{Provider: "Custom", Reasoning: effortHigh, ReasoningStyle: "None"},
			want: func(t *testing.T, m map[string]any) {
				for _, key := range []string{"reasoning_effort", "reasoning", "thinking", "think"} {
					if _, ok := m[key]; ok {
						t.Fatalf("custom none sent %s", key)
					}
				}
			},
		},
		{
			name: "custom openai",
			cfg:  Config{Provider: "Custom", Reasoning: effortLow, ReasoningStyle: "OpenAI"},
			want: func(t *testing.T, m map[string]any) {
				if m["reasoning_effort"] != "low" {
					t.Fatalf("%#v", m["reasoning_effort"])
				}
			},
		},
		{
			name: "custom deepseek",
			cfg:  Config{Provider: "Custom", Reasoning: "disabled", ReasoningStyle: "DeepSeek"},
			want: func(t *testing.T, m map[string]any) {
				think, _ := m["thinking"].(map[string]any)
				if think["type"] != "disabled" {
					t.Fatalf("%#v", m)
				}
				if _, ok := m["reasoning_effort"]; ok {
					t.Fatal("off sends thinking only, with no reasoning_effort")
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := baseCfg(tc.cfg.Provider)
			cfg.Reasoning = tc.cfg.Reasoning
			cfg.ReasoningStyle = tc.cfg.ReasoningStyle
			tc.want(t, decodeRequest(t, cfg, history))
		})
	}
}

func TestReasoningParamsExactJSON(t *testing.T) {
	history := []Message{{Role: roleUser, Content: "Hi"}}
	cases := []struct {
		name                           string
		provider, model, effort, style string
		want                           map[string]any // nil means those keys are absent
	}{
		{"openai default", "OpenAI", "gpt-4o-mini", "", "", nil},
		{"openai high on gpt-4o-mini", "OpenAI", "gpt-4o-mini", effortHigh, "", nil},
		{"openai high on o3", "OpenAI", "o3", effortHigh, "", map[string]any{"reasoning_effort": "high"}},
		{"openai off", "OpenAI", "o3", effortOff, "", map[string]any{"reasoning_effort": "none"}},
		{"xai medium", "xAI", "grok-4", effortMedium, "", map[string]any{"reasoning_effort": "medium"}},
		{"openrouter low", "OpenRouter", "openai/gpt-4o-mini", effortLow, "", map[string]any{"reasoning": map[string]any{"effort": "low", "enabled": true}}},
		{"openrouter off", "OpenRouter", "m", effortOff, "", map[string]any{"reasoning": map[string]any{"effort": "none", "enabled": false}}},
		{"deepseek off", "DeepSeek", "deepseek-flash", effortOff, "", map[string]any{"thinking": map[string]any{"type": "disabled"}}},
		{"deepseek minimal", "DeepSeek", "deepseek-flash", effortMinimal, "", map[string]any{"thinking": map[string]any{"type": "enabled"}, "reasoning_effort": "low"}},
		{"deepseek low", "DeepSeek", "deepseek-flash", effortLow, "", map[string]any{"thinking": map[string]any{"type": "enabled"}, "reasoning_effort": "low"}},
		{"deepseek medium", "DeepSeek", "deepseek-flash", effortMedium, "", map[string]any{"thinking": map[string]any{"type": "enabled"}, "reasoning_effort": "high"}},
		{"deepseek high", "DeepSeek", "deepseek-flash", effortHigh, "", map[string]any{"thinking": map[string]any{"type": "enabled"}, "reasoning_effort": "high"}},
		{"ollama off", "Ollama", "llama3.2", effortOff, "", map[string]any{"reasoning_effort": "none", "think": false}},
		{"custom none", "Custom", "gpt-4o-mini", effortHigh, "None", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := baseCfg(tc.provider)
			cfg.Model = tc.model
			cfg.Reasoning = tc.effort
			cfg.ReasoningStyle = tc.style
			m := decodeRequest(t, cfg, history)
			for _, key := range []string{"reasoning_effort", "reasoning", "thinking", "think"} {
				got, ok := m[key]
				want, wantOK := tc.want[key]
				if ok != wantOK {
					t.Errorf("key %s present %v, want %v (body key %#v)", key, ok, wantOK, got)
					continue
				}
				if !wantOK {
					continue
				}
				if !jsonEqual(got, want) {
					t.Errorf("key %s = %#v, want %#v", key, got, want)
				}
			}
			if tc.provider == "DeepSeek" && tc.effort == effortOff {
				if _, ok := m["reasoning_effort"]; ok {
					t.Fatal("DeepSeek off included reasoning_effort")
				}
			}
		})
	}
}

func jsonEqual(a, b any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	var ax, bx any
	if json.Unmarshal(ab, &ax) != nil || json.Unmarshal(bb, &bx) != nil {
		return false
	}
	ab, _ = json.Marshal(ax)
	bb, _ = json.Marshal(bx)
	return string(ab) == string(bb)
}

func TestSamplingOmittedUnlessSet(t *testing.T) {
	history := []Message{{Role: roleUser, Content: "Hi"}}
	cfg := baseCfg("OpenAI")
	cfg.Temperature = nil
	cfg.MaxTokens = 0
	m := decodeRequest(t, cfg, history)
	if _, ok := m["temperature"]; ok {
		t.Fatalf("nil temperature was sent: %#v", m["temperature"])
	}
	if _, ok := m["max_tokens"]; ok {
		t.Fatal("sent max_tokens")
	}
	if _, ok := m["max_completion_tokens"]; ok {
		t.Fatal("sent max_completion_tokens")
	}

	cfg.MaxTokens = 64
	m = decodeRequest(t, cfg, history)
	if m["max_completion_tokens"] != float64(64) {
		t.Fatalf("openai max %#v", m["max_completion_tokens"])
	}
	if _, ok := m["max_tokens"]; ok {
		t.Fatal("openai also sent max_tokens")
	}

	cfg.Provider = "DeepSeek"
	m = decodeRequest(t, cfg, history)
	if m["max_tokens"] != float64(64) {
		t.Fatalf("deepseek max %#v", m["max_tokens"])
	}
	if _, ok := m["max_completion_tokens"]; ok {
		t.Fatal("deepseek sent max_completion_tokens")
	}

	zero := 0.0
	cfg.Temperature = &zero
	m = decodeRequest(t, cfg, history)
	if m["temperature"] != float64(0) {
		t.Fatalf("explicit 0 was omitted: %#v", m["temperature"])
	}
}

func TestExtraBodyNullDeletesAndRefusesReserved(t *testing.T) {
	history := []Message{{Role: roleUser, Content: "Hi"}}
	cfg := baseCfg("OpenAI")
	cfg.Model = "o3"
	cfg.Reasoning = effortHigh
	cfg.ExtraBody = `{"temperature":null,"reasoning_effort":null}`
	m := decodeRequest(t, cfg, history)
	if _, ok := m["temperature"]; ok {
		t.Fatalf("null did not delete temperature: %#v", m["temperature"])
	}
	if _, ok := m["reasoning_effort"]; ok {
		t.Fatalf("null did not delete reasoning_effort: %#v", m["reasoning_effort"])
	}
	if _, ok := m["messages"]; !ok {
		t.Fatal("messages was removed")
	}
	if m["stream"] != true {
		t.Fatalf("stream %#v", m["stream"])
	}
	for _, raw := range []string{`{"messages":[]}`, `{"stream":false}`, `{"stream":null}`} {
		cfg.ExtraBody = raw
		if _, err := chatRequestBody(cfg, history); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestExtraBodyMergesLast(t *testing.T) {
	history := []Message{{Role: roleUser, Content: "Hi", Reasoning: "SECRET-REASONING"}}
	cfg := baseCfg("DeepSeek")
	cfg.Model = "my-relay-model"
	cfg.Reasoning = effortOff
	cfg.ExtraBody = `{"reasoning_effort":"none","temperature":0}`
	m := decodeRequest(t, cfg, history)
	think, _ := m["thinking"].(map[string]any)
	if think["type"] != "disabled" {
		t.Fatalf("thinking %#v", think)
	}
	if m["reasoning_effort"] != "none" {
		t.Fatalf("extra reasoning_effort %#v", m["reasoning_effort"])
	}
	if m["temperature"] != float64(0) {
		t.Fatalf("temperature %#v", m["temperature"])
	}
	if m["model"] != "my-relay-model" {
		t.Fatalf("model %#v", m["model"])
	}

	cfg.ExtraBody = ""
	m = decodeRequest(t, cfg, history)
	if _, ok := m["reasoning_effort"]; ok {
		t.Fatal("deepseek off without extra body sent reasoning_effort")
	}

	cfg.ExtraBody = `{"thinking":{"type":"enabled"}}`
	m = decodeRequest(t, cfg, history)
	think, _ = m["thinking"].(map[string]any)
	if think["type"] != "enabled" {
		t.Fatalf("extra body should override thinking: %#v", think)
	}
}

func TestExtraBodyRejectsNonObjects(t *testing.T) {
	history := []Message{{Role: roleUser, Content: "Hi"}}
	for _, raw := range []string{`[]`, `"none"`, `null`, `not json`, `{"a":1}{"b":2}`} {
		cfg := baseCfg("OpenAI")
		cfg.ExtraBody = raw
		if _, err := chatRequestBody(cfg, history); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	cfg := baseCfg("OpenAI")
	cfg.ExtraBody = "  "
	if _, err := chatRequestBody(cfg, history); err != nil {
		t.Fatal(err)
	}
	cfg.ExtraBody = "{}"
	if _, err := chatRequestBody(cfg, history); err != nil {
		t.Fatal(err)
	}
}

func TestOffKeepsModelID(t *testing.T) {
	a := newTestApp(t)
	a.data.Conversations = []Conversation{{
		ID: "c", Title: "DeepSeek",
		Config: Config{
			Provider: "DeepSeek", Model: "custom-id", BaseURL: "https://example.com/v1",
			APIKey: "k", Reasoning: effortHigh,
		},
	}}
	a.data.ActiveID = "c"
	a.setEffort("Off")
	cfg := a.active().Config
	if cfg.Model != "custom-id" || cfg.Reasoning != effortOff {
		t.Fatalf("%+v", cfg)
	}
	m := decodeRequest(t, cfg, []Message{{Role: roleUser, Content: "Hi"}})
	if m["model"] != "custom-id" {
		t.Fatalf("model rewritten: %#v", m["model"])
	}
	think, _ := m["thinking"].(map[string]any)
	if think["type"] != "disabled" {
		t.Fatalf("%#v", think)
	}
	if _, ok := m["reasoning_effort"]; ok {
		t.Fatal("off sent reasoning_effort")
	}
}

func TestSaveFormExtraBody(t *testing.T) {
	a := newTestApp(t)
	a.newChat()
	a.active().Messages = []Message{{ID: "m", Role: roleUser, Content: "Hi", Done: true}}
	a.openSettings()
	a.form.ExtraBody = "[]"
	if a.saveForm(false) {
		t.Fatal("accepted an array")
	}
	if a.active().Config.ExtraBody != "" {
		t.Fatalf("stored %q", a.active().Config.ExtraBody)
	}
	a.form.ExtraBody = `{"reasoning_effort":"none"}`
	if !a.saveForm(false) {
		t.Fatal(a.formError)
	}
	got, err := Load(a.path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Conversations[0].Config.ExtraBody != `{"reasoning_effort":"none"}` {
		t.Fatalf("saved %#v", got.Conversations[0].Config.ExtraBody)
	}
}

func TestDeepSeekPresetUsesFlash(t *testing.T) {
	cfg := Config{Provider: "DeepSeek", Model: "leave-me", BaseURL: "https://old.example/v1"}
	applyPreset(&cfg)
	if cfg.Model != "deepseek-flash" || cfg.BaseURL != "https://api.deepseek.com/v1" {
		t.Fatalf("%+v", cfg)
	}
}

func TestHistorySkipsReasoningOnlyTurns(t *testing.T) {
	msgs := historyForAPI(Config{}, []Message{
		{Role: roleUser, Content: "Hi"},
		{Role: roleAssistant, Reasoning: "SECRET-REASONING"},
	})
	if len(msgs) != 1 || msgs[0].Content != "Hi" {
		t.Fatalf("%+v", msgs)
	}
}

func TestDecodeReasoningShapes(t *testing.T) {
	d, _, _, err := decodeSSEData(`{"choices":[{"delta":{"reasoning_content":"a"}},{"message":{"reasoning_content":"b","content":"c"}}]}`)
	if err != nil || d.Reasoning != "ab" || d.Content != "c" {
		t.Fatalf("%+v %v", d, err)
	}
	d, _, _, err = decodeSSEData(`{"choices":[{"delta":{"reasoning":{"text":"object"},"content":""}}]}`)
	if err != nil || d.Reasoning != "" {
		t.Fatalf("non-string reasoning should be ignored: %+v %v", d, err)
	}
	d, _, _, err = decodeSSEData(`{"choices":[{"message":{"content":"answer","reasoning":"final thought"}}]}`)
	if err != nil || d.Content != "answer" || d.Reasoning != "final thought" {
		t.Fatalf("%+v %v", d, err)
	}
}

func TestThoughtLabel(t *testing.T) {
	m := &Message{ThinkingMS: 12000}
	if thoughtLabel(m, true) != "Thinking · 12s" {
		t.Fatal(thoughtLabel(m, true))
	}
	if thoughtLabel(m, false) != "Thought for 12s" {
		t.Fatal(thoughtLabel(m, false))
	}
	m.ThinkingMS = 400
	if thoughtLabel(m, false) != "Thought for 1s" {
		t.Fatal(thoughtLabel(m, false))
	}
	if thoughtLabel(&Message{}, false) != "Thinking" {
		t.Fatal("missing duration")
	}
}

func TestDateBucket(t *testing.T) {
	now := time.Date(2026, 10, 18, 15, 0, 0, 0, time.UTC)
	if dateBucket(now.Add(-time.Hour), now) != "Today" {
		t.Fatal(dateBucket(now.Add(-time.Hour), now))
	}
	if dateBucket(now.Add(-30*time.Hour), now) != "Yesterday" {
		t.Fatal(dateBucket(now.Add(-30*time.Hour), now))
	}
	if dateBucket(now.Add(-72*time.Hour), now) != "Previous 7 Days" {
		t.Fatal(dateBucket(now.Add(-72*time.Hour), now))
	}
	if dateBucket(now.Add(-240*time.Hour), now) != "Older" {
		t.Fatal(dateBucket(now.Add(-240*time.Hour), now))
	}
}
