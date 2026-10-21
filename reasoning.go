package main

import "strings"

// Effort values stored on Config.Reasoning. Empty is Default: the request
// omits every reasoning field so a server that rejects unknown parameters
// still answers.
const (
	effortOff     = "off"
	effortMinimal = "minimal"
	effortLow     = "low"
	effortMedium  = "medium"
	effortHigh    = "high"
)

// effortOptions is the order shown in the settings select and the composer.
var effortOptions = []string{"Off", "Default", "Minimal", "Low", "Medium", "High"}

// reasoningStyles is what a Custom provider can ask Cove to send.
var reasoningStyles = []string{"None", "OpenAI", "OpenRouter", "xAI", "DeepSeek", "Ollama"}

func effortLabel(value string) string {
	switch normalizeEffort(value) {
	case effortOff:
		return "Off"
	case effortMinimal:
		return "Minimal"
	case effortLow:
		return "Low"
	case effortMedium:
		return "Medium"
	case effortHigh:
		return "High"
	default:
		return "Default"
	}
}

func effortValue(label string) string {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "off", "none", "disabled":
		return effortOff
	case "minimal":
		return effortMinimal
	case "low":
		return effortLow
	case "medium":
		return effortMedium
	case "high":
		return effortHigh
	default:
		return ""
	}
}

// normalizeEffort maps stored and typed values onto the six levels.
// Anything else is Default, which sends nothing.
func normalizeEffort(value string) string {
	return effortValue(value)
}

// reasoningStyle is the encoding named providers use, or the Custom
// provider's chosen style. "" means send nothing.
func reasoningStyle(cfg Config) string {
	name := cfg.Provider
	if cfg.Provider == "Custom" || !knownProvider(cfg.Provider) {
		name = cfg.ReasoningStyle
	}
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "openai":
		return "openai"
	case "openrouter":
		return "openrouter"
	case "xai":
		return "xai"
	case "deepseek":
		return "deepseek"
	case "ollama":
		return "ollama"
	default:
		return ""
	}
}

// reasoningUnsupported reports models that reject reasoning parameters.
// Only clear cases are listed: OpenAI chat models such as gpt-4o and
// gpt-4.1. o-series and gpt-5 stay enabled. Anything else is left alone
// so a relay with its own model id still sends what the user picked.
func reasoningUnsupported(cfg Config) bool {
	if reasoningStyle(cfg) != "openai" {
		return false
	}
	id := strings.ToLower(strings.TrimSpace(cfg.Model))
	switch {
	case strings.HasPrefix(id, "gpt-5"), strings.HasPrefix(id, "o1"), strings.HasPrefix(id, "o3"), strings.HasPrefix(id, "o4"):
		return false
	case strings.HasPrefix(id, "gpt-4"), strings.HasPrefix(id, "gpt-3.5"), strings.HasPrefix(id, "chatgpt-"):
		return true
	default:
		return false
	}
}

// deepseekEffort maps Cove's levels onto the documented chat-completions
// values low, high, and max. minimal and low become low; medium and high
// become high. Off is not handled here.
func deepseekEffort(effort string) string {
	switch effort {
	case effortMinimal, effortLow:
		return effortLow
	default:
		return effortHigh
	}
}

// reasoningParams is the extra JSON for cfg's effort. Nil means the body
// stays a plain chat completion.
//
// The shapes follow the providers' chat-completions docs as of 2026:
//   - OpenAI and xAI: top-level reasoning_effort (none, minimal, low,
//     medium, high). xAI's Grok 4.5 and newer may reject none.
//   - OpenRouter: reasoning object with effort and enabled. exclude is
//     left unset so the stream still carries the reasoning text.
//   - DeepSeek: thinking.type enabled|disabled, and, when thinking is on,
//     a sibling reasoning_effort. Nesting the effort inside thinking is
//     ignored. Off sends only {"thinking":{"type":"disabled"}}, the shape
//     the official API honors. It does not add reasoning_effort and it
//     does not rewrite the model id. A relay that drops thinking is
//     handled by Extra body, which is merged afterwards.
//   - Ollama's /v1/chat/completions documents reasoning_effort (none
//     disables). think is Ollama's own option (boolean or a level); Cove
//     sends both so a server that understands either one follows the choice.
func reasoningParams(cfg Config) map[string]any {
	effort := normalizeEffort(cfg.Reasoning)
	if effort == "" || reasoningUnsupported(cfg) {
		return nil
	}
	switch reasoningStyle(cfg) {
	case "openai", "xai":
		return map[string]any{"reasoning_effort": openAIEffort(effort)}
	case "openrouter":
		if effort == effortOff {
			return map[string]any{"reasoning": map[string]any{"effort": "none", "enabled": false}}
		}
		return map[string]any{"reasoning": map[string]any{"effort": effort, "enabled": true}}
	case "deepseek":
		if effort == effortOff {
			// Official API: thinking.type disabled, and no reasoning_effort.
			return map[string]any{"thinking": map[string]any{"type": "disabled"}}
		}
		return map[string]any{
			"thinking":         map[string]any{"type": "enabled"},
			"reasoning_effort": deepseekEffort(effort),
		}
	case "ollama":
		if effort == effortOff {
			return map[string]any{"reasoning_effort": "none", "think": false}
		}
		think := effort
		if effort == effortMinimal {
			// Models without a named "minimal" level treat it as low.
			think = effortLow
		}
		return map[string]any{"reasoning_effort": effort, "think": think}
	default:
		return nil
	}
}

func openAIEffort(effort string) string {
	if effort == effortOff {
		return "none"
	}
	return effort
}

// thoughtLabel is the disclosure title. While the reply is still streaming
// it reads "Thinking · 4s"; afterwards it names how long reasoning took.
func thoughtLabel(m *Message, live bool) string {
	if m == nil {
		return ""
	}
	if live {
		sec := m.ThinkingMS / 1000
		if sec < 1 {
			sec = 1
		}
		return "Thinking · " + itoa(sec) + "s"
	}
	if m.ThinkingMS <= 0 {
		return "Thinking"
	}
	sec := m.ThinkingMS / 1000
	if sec < 1 {
		return "Thought for 1s"
	}
	return "Thought for " + itoa(sec) + "s"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
