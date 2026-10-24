package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Delta is one piece of a streaming reply. Content and Reasoning are the
// new text in this event, not the whole reply.
type Delta struct {
	Content   string
	Reasoning string
}

// StatusError is a non-2xx response. Body is the response body, shortened.
type StatusError struct {
	Status int
	Body   string
}

func (e *StatusError) Error() string {
	msg := strings.TrimSpace(e.Body)
	if extracted := errorMessage([]byte(msg)); extracted != "" {
		msg = extracted
	}
	msg = strings.Join(strings.Fields(msg), " ")
	if r := []rune(msg); len(r) > 500 {
		msg = string(r[:500]) + "…"
	}
	if msg == "" {
		return fmt.Sprintf("The API returned HTTP %d.", e.Status)
	}
	return fmt.Sprintf("The API returned HTTP %d: %s", e.Status, msg)
}

// completionsURL builds POST {base}/chat/completions. A base that already
// ends in that path is left alone, and any query (Azure's api-version) is
// kept.
func completionsURL(base string) (string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		return "", errors.New("Set an API base URL in Settings.")
	}
	if !strings.Contains(base, "://") {
		base = "https://" + base
	}
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		return "", fmt.Errorf("The API base URL %q is not an http(s) URL.", base)
	}
	// Split off the query so path cleaning does not drop it.
	var query string
	if i := strings.IndexByte(base, '?'); i >= 0 {
		query = base[i:]
		base = base[:i]
	}
	base = strings.TrimRight(base, "/")
	if !strings.HasSuffix(base, "/chat/completions") {
		base += "/chat/completions"
	}
	return base + query, nil
}

func validateConfig(cfg Config) error {
	if strings.TrimSpace(cfg.Model) == "" {
		return errors.New("Set a model id in Settings.")
	}
	if _, err := completionsURL(cfg.BaseURL); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.APIKey) == "" && !allowsEmptyKey(cfg) {
		return errors.New("Add an API key in Settings. Local servers such as Ollama can leave the key empty.")
	}
	return nil
}

type apiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func historyForAPI(cfg Config, msgs []Message) []apiMessage {
	out := make([]apiMessage, 0, len(msgs)+1)
	if s := strings.TrimSpace(cfg.SystemPrompt); s != "" {
		out = append(out, apiMessage{Role: "system", Content: s})
	}
	for _, m := range msgs {
		if m.Role != roleUser && m.Role != roleAssistant {
			continue
		}
		content := m.Content
		if m.Role == roleUser {
			content = quoteRequestText(m.Quotes, m.Content)
		}
		if strings.TrimSpace(content) == "" {
			continue
		}
		// Content only. Reasoning is shown in the chat and must not be sent
		// back, including DeepSeek's reasoning_content. Quotes are prefixed
		// onto the user text; they are not a separate message.
		out = append(out, apiMessage{Role: m.Role, Content: content})
	}
	return out
}

// chatRequestBody is the Chat Completions JSON. Reasoning fields are added
// only when the effort is not Default. Extra body is merged last.
func chatRequestBody(cfg Config, history []Message) ([]byte, error) {
	msgs := historyForAPI(cfg, history)
	if len(msgs) == 0 {
		return nil, errors.New("There is no message to send.")
	}
	payload := map[string]any{
		"model":    strings.TrimSpace(cfg.Model),
		"messages": msgs,
		"stream":   true,
	}
	if cfg.Temperature != nil {
		payload["temperature"] = *cfg.Temperature
	}
	if cfg.MaxTokens > 0 {
		if cfg.Provider == "OpenAI" {
			payload["max_completion_tokens"] = cfg.MaxTokens
		} else {
			payload["max_tokens"] = cfg.MaxTokens
		}
	}
	for k, v := range reasoningParams(cfg) {
		payload[k] = v
	}
	if err := mergeExtraBody(payload, cfg.ExtraBody); err != nil {
		return nil, err
	}
	return json.Marshal(payload)
}

// mergeExtraBody copies a JSON object onto payload. The user's keys replace
// Cove's, including reasoning fields. null removes a key. messages and
// stream cannot be overridden. An empty string is the usual body.
func mergeExtraBody(payload map[string]any, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var extra map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &extra); err != nil || extra == nil {
		return errors.New("Extra body must be a JSON object, for example {\"reasoning_effort\":\"none\"}.")
	}
	for k, rawV := range extra {
		if k == "messages" || k == "stream" {
			return fmt.Errorf("Extra body cannot override %q.", k)
		}
		if string(rawV) == "null" {
			delete(payload, k)
			continue
		}
		var v any
		if err := json.Unmarshal(rawV, &v); err != nil {
			return err
		}
		payload[k] = v
	}
	return nil
}

func prettyJSON(b []byte) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, b, "", "  "); err != nil {
		return string(b)
	}
	return buf.String()
}

// streamIdle cancels a body that receives no bytes for this long. Relays
// sometimes leave the socket open after they have stopped sending.
var streamIdle = 90 * time.Second

// errIncomplete is a clean EOF with no [DONE] and no finish_reason.
var errIncomplete = errors.New("incomplete — connection closed")

// errIdle is the idle-read watchdog.
var errIdle = errors.New("The connection went quiet and was closed.")

// errFinish ends parseSSE when the stream is complete.
var errFinish = errors.New("finish")

// jsonCompletionLimit caps a non-streaming response body.
var jsonCompletionLimit int64 = 8 << 20

// httpClient has no overall timeout: a reply may stream for a long time.
// ResponseHeaderTimeout still fails a server that never answers.
var httpClient = &http.Client{
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ResponseHeaderTimeout: 60 * time.Second,
		IdleConnTimeout:       90 * time.Second,
	},
}

// StreamChat calls the OpenAI-compatible Chat Completions API with
// stream:true and reports each delta. ctx cancels the request.
func StreamChat(ctx context.Context, cfg Config, history []Message, onDelta func(Delta)) error {
	if err := validateConfig(cfg); err != nil {
		return err
	}
	endpoint, err := completionsURL(cfg.BaseURL)
	if err != nil {
		return err
	}
	body, err := chatRequestBody(cfg, history)
	if err != nil {
		return err
	}
	reqCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var idle atomic.Bool
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", "Cove/"+appVersion)
	key := strings.TrimSpace(cfg.APIKey)
	if key != "" {
		if strings.Contains(strings.ToLower(endpoint), "openai.azure.com") {
			req.Header.Set("api-key", key)
		} else {
			req.Header.Set("Authorization", "Bearer "+key)
		}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return fmt.Errorf("Could not reach the API: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return &StatusError{Status: resp.StatusCode, Body: string(b)}
	}
	ctype := strings.ToLower(resp.Header.Get("Content-Type"))
	bodyReader := newIdleReader(resp.Body, streamIdle, func() {
		idle.Store(true)
		cancel()
	})
	defer bodyReader.Close()
	read := func(err error) error {
		if idle.Load() && ctx.Err() == nil {
			return errIdle
		}
		return err
	}
	if strings.Contains(ctype, "application/json") && !strings.Contains(ctype, "text/event-stream") {
		return read(readJSONCompletion(bodyReader, onDelta))
	}
	err = parseSSE(bodyReader, func(data string) error {
		d, done, finished, err := decodeSSEData(data)
		if err != nil {
			return err
		}
		if d.Content != "" || d.Reasoning != "" {
			onDelta(d)
		}
		if done || finished {
			return errFinish
		}
		return nil
	})
	return read(err)
}

// completeChat posts one non-streaming chat completion and returns the
// assistant text. Title generation uses it so a title does not stream
// into the transcript.
func completeChat(ctx context.Context, cfg Config, body []byte) (string, error) {
	if err := validateConfig(cfg); err != nil {
		return "", err
	}
	endpoint, err := completionsURL(cfg.BaseURL)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Cove/"+appVersion)
	key := strings.TrimSpace(cfg.APIKey)
	if key != "" {
		if strings.Contains(strings.ToLower(endpoint), "openai.azure.com") {
			req.Header.Set("api-key", key)
		} else {
			req.Header.Set("Authorization", "Bearer "+key)
		}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", err
		}
		return "", fmt.Errorf("Could not reach the API: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return "", &StatusError{Status: resp.StatusCode, Body: string(b)}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, jsonCompletionLimit))
	if err != nil {
		return "", fmt.Errorf("Could not read the API response: %v", err)
	}
	d, _, _, err := decodeSSEData(string(b))
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(d.Content) == "" {
		return "", errors.New("The model returned an empty title.")
	}
	return d.Content, nil
}

func readJSONCompletion(r io.Reader, onDelta func(Delta)) error {
	b, err := io.ReadAll(io.LimitReader(r, jsonCompletionLimit))
	if err != nil {
		return fmt.Errorf("Could not read the API response: %v", err)
	}
	d, _, _, err := decodeSSEData(string(b))
	if err != nil {
		return err
	}
	if d.Content != "" || d.Reasoning != "" {
		onDelta(d)
	}
	return nil
}

// parseSSE reads Server-Sent Events. Each event's data field, joined with
// newlines when it spans several data: lines, is passed to fn. A blank
// line ends an event. Lines beginning with ':' are comments. A clean EOF
// that never saw [DONE] or a finish_reason is errIncomplete: relays often
// drop the tail of a stream without an error status.
func parseSSE(r io.Reader, fn func(data string) error) error {
	sc := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 4<<20)
	var data []string
	finished := false
	flush := func() error {
		if len(data) == 0 {
			return nil
		}
		payload := strings.Join(data, "\n")
		data = data[:0]
		if err := fn(payload); err != nil {
			if errors.Is(err, errFinish) {
				finished = true
				return errFinish
			}
			return err
		}
		return nil
	}
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" {
			if err := flush(); err != nil {
				if errors.Is(err, errFinish) {
					return nil
				}
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		if strings.HasPrefix(value, " ") {
			value = value[1:]
		}
		if field == "data" {
			data = append(data, value)
		}
	}
	if err := sc.Err(); err != nil {
		if errors.Is(err, context.Canceled) {
			return err
		}
		return fmt.Errorf("The stream ended early: %v", err)
	}
	if err := flush(); err != nil && !errors.Is(err, errFinish) {
		return err
	}
	if !finished {
		return errIncomplete
	}
	return nil
}

// idleReader cancels via onIdle when Read is quiet for idle.
type idleReader struct {
	rc     io.ReadCloser
	idle   time.Duration
	onIdle func()
	mu     sync.Mutex
	last   time.Time
	stop   chan struct{}
}

func newIdleReader(rc io.ReadCloser, idle time.Duration, onIdle func()) *idleReader {
	ir := &idleReader{rc: rc, idle: idle, onIdle: onIdle, last: time.Now(), stop: make(chan struct{})}
	go ir.watch()
	return ir
}

func (r *idleReader) watch() {
	t := time.NewTicker(200 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-r.stop:
			return
		case <-t.C:
			r.mu.Lock()
			quiet := time.Since(r.last) >= r.idle
			r.mu.Unlock()
			if quiet {
				if r.onIdle != nil {
					r.onIdle()
				}
				return
			}
		}
	}
}

func (r *idleReader) Read(p []byte) (int, error) {
	n, err := r.rc.Read(p)
	if n > 0 {
		r.mu.Lock()
		r.last = time.Now()
		r.mu.Unlock()
	}
	return n, err
}

func (r *idleReader) Close() error {
	select {
	case <-r.stop:
	default:
		close(r.stop)
	}
	return r.rc.Close()
}

// decodeSSEData reads one SSE data payload. done is true for [DONE].
// finished is true when a choice carries a finish_reason.
func decodeSSEData(data string) (Delta, bool, bool, error) {
	data = strings.TrimSpace(data)
	if data == "" {
		return Delta{}, false, false, nil
	}
	if data == "[DONE]" {
		return Delta{}, true, true, nil
	}
	var env struct {
		Error   json.RawMessage `json:"error"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Delta        struct {
				Content          json.RawMessage `json:"content"`
				ReasoningContent json.RawMessage `json:"reasoning_content"`
				Reasoning        json.RawMessage `json:"reasoning"`
			} `json:"delta"`
			Message struct {
				Content          json.RawMessage `json:"content"`
				ReasoningContent json.RawMessage `json:"reasoning_content"`
				Reasoning        json.RawMessage `json:"reasoning"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(data), &env); err != nil {
		return Delta{}, false, false, fmt.Errorf("The API sent data that is not JSON: %v", err)
	}
	if msg := errorMessage(env.Error); msg != "" {
		return Delta{}, false, false, errors.New(msg)
	}
	var d Delta
	finished := false
	for _, ch := range env.Choices {
		if strings.TrimSpace(ch.FinishReason) != "" {
			finished = true
		}
		d.Content += rawString(ch.Delta.Content) + rawString(ch.Message.Content)
		think := rawString(ch.Delta.ReasoningContent)
		if think == "" {
			think = rawString(ch.Delta.Reasoning)
		}
		if think == "" {
			think = rawString(ch.Message.ReasoningContent)
		}
		if think == "" {
			think = rawString(ch.Message.Reasoning)
		}
		d.Reasoning += think
	}
	return d, false, finished, nil
}

func explainAPIError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, errIncomplete) {
		return errIncomplete.Error()
	}
	var se *StatusError
	if !errors.As(err, &se) {
		return err.Error()
	}
	msg := se.Error()
	low := strings.ToLower(se.Body + " " + msg)
	if se.Status == 400 && (strings.Contains(low, "reasoning_effort") || strings.Contains(low, "thinking")) {
		return msg + " This model doesn't support reasoning effort; set Reasoning to Default."
	}
	if strings.Contains(low, "context_length") || strings.Contains(low, "maximum context") || strings.Contains(low, "context length") || strings.Contains(low, "too many tokens") {
		return msg + " The conversation is longer than this model accepts."
	}
	return msg
}

func rawString(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

func errorMessage(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}
	var obj struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		return strings.TrimSpace(obj.Message)
	}
	return strings.TrimSpace(string(raw))
}
