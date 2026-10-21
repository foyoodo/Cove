package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCompletionsURL(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"https://api.openai.com/v1", "https://api.openai.com/v1/chat/completions"},
		{"https://api.openai.com/v1/", "https://api.openai.com/v1/chat/completions"},
		{"https://api.openai.com/v1/chat/completions", "https://api.openai.com/v1/chat/completions"},
		{"http://127.0.0.1:11434/v1/", "http://127.0.0.1:11434/v1/chat/completions"},
		{"api.openai.com/v1", "https://api.openai.com/v1/chat/completions"},
		{"https://example.openai.azure.com/openai/deployments/x/chat/completions?api-version=2024-02-01",
			"https://example.openai.azure.com/openai/deployments/x/chat/completions?api-version=2024-02-01"},
	}
	for _, tc := range cases {
		got, err := completionsURL(tc.in)
		if err != nil {
			t.Errorf("%q: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%q: got %q, want %q", tc.in, got, tc.want)
		}
	}
	if _, err := completionsURL(""); err == nil {
		t.Error("empty base url was accepted")
	}
	if _, err := completionsURL("ftp://example.com/v1"); err == nil {
		t.Error("ftp url was accepted")
	}
}

func TestParseSSEChunks(t *testing.T) {
	const stream = "" +
		": keep-alive\n" +
		"\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"Hel\"}}]}\n" +
		"\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"lo\"}}]}\n" +
		"data: ignored-because-same-event\n" +
		"\n" +
		"event: error\n" +
		"data: {\"error\":{\"message\":\"nope\"}}\n" +
		"\n"
	// One byte at a time, so a line split across reads still parses.
	var got []string
	err := parseSSE(&chunkReader{s: stream, n: 1}, func(data string) error {
		got = append(got, data)
		return nil
	})
	if err != nil && !errors.Is(err, errIncomplete) {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("events %q", got)
	}
	if got[0] != `{"choices":[{"delta":{"content":"Hel"}}]}` {
		t.Fatalf("first event %q", got[0])
	}
	if !strings.Contains(got[1], "lo") || !strings.Contains(got[1], "ignored-because-same-event") {
		t.Fatalf("joined data: %q", got[1])
	}
	if _, _, _, err := decodeSSEData(got[2]); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("error event: %v", err)
	}
}

func TestDecodeSSEData(t *testing.T) {
	d, done, _, err := decodeSSEData(`{"choices":[{"delta":{"content":"Hi","reasoning_content":"think"}}]}`)
	if err != nil || done || d.Content != "Hi" || d.Reasoning != "think" {
		t.Fatalf("delta %+v done %v err %v", d, done, err)
	}
	d, done, _, err = decodeSSEData("{\"choices\":[{\"delta\":{\"reasoning\":\"alt\",\"content\":null}}]}")
	if err != nil || done || d.Content != "" || d.Reasoning != "alt" {
		t.Fatalf("null content: %+v done %v err %v", d, done, err)
	}
	if _, done, _, err = decodeSSEData("[DONE]"); err != nil || !done {
		t.Fatalf("DONE done %v err %v", done, err)
	}
	if _, _, _, err = decodeSSEData(`{"error":{"message":"bad key"}}`); err == nil || !strings.Contains(err.Error(), "bad key") {
		t.Fatalf("error payload: %v", err)
	}
	d, _, _, err = decodeSSEData(`{"choices":[{"message":{"content":"whole"}}]}`)
	if err != nil || d.Content != "whole" {
		t.Fatalf("non-stream body: %+v %v", d, err)
	}
}

func TestStreamChat(t *testing.T) {
	var sawAuth, sawModel, sawUser bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			http.Error(w, `{"error":{"message":"bad key"}}`, http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		text := string(body)
		sawAuth = true
		sawModel = strings.Contains(text, `"model":"demo-model"`)
		sawUser = strings.Contains(text, "Hello there") && strings.Contains(text, `"role":"system"`)
		if !strings.Contains(text, `"stream":true`) || !strings.Contains(text, `"temperature":0.4`) || !strings.Contains(text, `"max_tokens":32`) {
			t.Errorf("request body %s", text)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"hmm\",\"content\":\" world\"}}]}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	cfg := Config{
		Model: "demo-model", BaseURL: srv.URL + "/v1", APIKey: "secret",
		SystemPrompt: "Be brief", Temperature: f64p(0.4), MaxTokens: 32,
	}
	var content, reasoning strings.Builder
	err := StreamChat(context.Background(), cfg, []Message{{Role: roleUser, Content: "Hello there", Done: true}}, func(d Delta) {
		content.WriteString(d.Content)
		reasoning.WriteString(d.Reasoning)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sawAuth || !sawModel || !sawUser {
		t.Fatalf("request was not what the server expected: auth %v model %v user %v", sawAuth, sawModel, sawUser)
	}
	if content.String() != "Hello world" || reasoning.String() != "hmm" {
		t.Fatalf("content %q reasoning %q", content.String(), reasoning.String())
	}
}

func TestStreamChatHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":{"message":"Incorrect API key provided"}}`)
	}))
	defer srv.Close()
	cfg := Config{Model: "m", BaseURL: srv.URL + "/v1", APIKey: "nope"}
	err := StreamChat(context.Background(), cfg, []Message{{Role: roleUser, Content: "Hi"}}, nil)
	var status *StatusError
	if !errors.As(err, &status) {
		t.Fatalf("got %T %v", err, err)
	}
	if status.Status != 401 || !strings.Contains(err.Error(), "Incorrect API key") || !strings.Contains(err.Error(), "401") {
		t.Fatalf("error %v", err)
	}
}

func TestStreamChatJSONBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"content":"all at once"}}]}`)
	}))
	defer srv.Close()
	cfg := Config{Model: "m", BaseURL: srv.URL, APIKey: "k"}
	var got string
	err := StreamChat(context.Background(), cfg, []Message{{Role: roleUser, Content: "Hi"}}, func(d Delta) {
		got += d.Content
	})
	if err != nil || got != "all at once" {
		t.Fatalf("got %q err %v", got, err)
	}
}

func TestStreamChatCancel(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hi\"}}]}\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		close(started)
		<-r.Context().Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cfg := Config{Model: "m", BaseURL: srv.URL, APIKey: "k"}
	done := make(chan error, 1)
	go func() {
		done <- StreamChat(ctx, cfg, []Message{{Role: roleUser, Content: "Hi"}}, func(Delta) {
			cancel()
		})
	}()
	<-started
	err := <-done
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
}

func TestParseSSEFixtures(t *testing.T) {
	long := strings.Repeat("x", 70*1024)
	longJSON := `{"choices":[{"delta":{"content":"` + long + `"}}]}`
	cases := []struct {
		name    string
		stream  string
		wantErr error
		want    []string
	}{
		{
			name:   "crlf",
			stream: "data: {\"choices\":[{\"delta\":{\"content\":\"A\"}}]}\r\n\r\ndata: [DONE]\r\n\r\n",
			want:   []string{`{"choices":[{"delta":{"content":"A"}}]}`, "[DONE]"},
		},
		{
			name:   "no space after colon",
			stream: "data:{\"choices\":[{\"finish_reason\":\"stop\",\"delta\":{\"content\":\"B\"}}]}\n\n",
			want:   []string{`{"choices":[{"finish_reason":"stop","delta":{"content":"B"}}]}`},
		},
		{
			name:   "comments",
			stream: ": keep-alive\n\ndata: {\"choices\":[{\"delta\":{\"reasoning_content\":\"think\"}}]}\n\ndata: [DONE]\n\n",
			want:   []string{`{"choices":[{"delta":{"reasoning_content":"think"}}]}`, "[DONE]"},
		},
		{
			name:    "eof without done",
			stream:  "data: {\"choices\":[{\"delta\":{\"content\":\"cut\"}}]}\n\n",
			wantErr: errIncomplete,
			want:    []string{`{"choices":[{"delta":{"content":"cut"}}]}`},
		},
		{
			name:   "line over 64KB",
			stream: "data: " + longJSON + "\n\ndata: [DONE]\n\n",
			want:   []string{longJSON, "[DONE]"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			err := parseSSE(strings.NewReader(tc.stream), func(data string) error {
				got = append(got, data)
				_, done, finished, decErr := decodeSSEData(data)
				if decErr != nil {
					return decErr
				}
				if done || finished {
					return errFinish
				}
				return nil
			})
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("events %d: %q", len(got), got)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("event %d\n got %q\nwant %q", i, got[i], tc.want[i])
				}
			}
		})
	}
	d, _, finished, err := decodeSSEData(`{"choices":[{"delta":{"reasoning":"r","reasoning_content":"c"},"finish_reason":"stop"}]}`)
	if err != nil || !finished || d.Reasoning != "c" {
		t.Fatalf("reasoning decode %+v finished %v err %v", d, finished, err)
	}
}

func TestStreamEndsIncomplete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hi\"}}]}\n\n")
	}))
	defer srv.Close()
	var got string
	err := StreamChat(context.Background(), Config{Model: "m", BaseURL: srv.URL, APIKey: "k"}, []Message{{Role: roleUser, Content: "Hi"}}, func(d Delta) {
		got += d.Content
	})
	if !errors.Is(err, errIncomplete) {
		t.Fatalf("err %v", err)
	}
	if got != "Hi" {
		t.Fatalf("content %q", got)
	}
	if explainAPIError(err) != "incomplete — connection closed" {
		t.Fatalf("hint %q", explainAPIError(err))
	}
}

func TestStreamIdleWatchdog(t *testing.T) {
	old := streamIdle
	streamIdle = 40 * time.Millisecond
	t.Cleanup(func() { streamIdle = old })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-r.Context().Done()
	}))
	defer srv.Close()
	err := StreamChat(context.Background(), Config{Model: "m", BaseURL: srv.URL, APIKey: "k"}, []Message{{Role: roleUser, Content: "Hi"}}, func(Delta) {})
	if !errors.Is(err, errIdle) {
		t.Fatalf("err %v", err)
	}
}

func TestExplainAPIErrorHints(t *testing.T) {
	reason := explainAPIError(&StatusError{Status: 400, Body: `{"error":{"message":"Unrecognized request argument: reasoning_effort"}}`})
	if !strings.Contains(reason, "set Reasoning to Default") {
		t.Fatalf("reasoning hint: %s", reason)
	}
	think := explainAPIError(&StatusError{Status: 400, Body: `{"error":{"message":"unknown field thinking"}}`})
	if !strings.Contains(think, "set Reasoning to Default") {
		t.Fatalf("thinking hint: %s", think)
	}
	ctxErr := explainAPIError(&StatusError{Status: 400, Body: `{"error":{"message":"maximum context length exceeded"}}`})
	if !strings.Contains(ctxErr, "longer than this model accepts") {
		t.Fatalf("context hint: %s", ctxErr)
	}
}

func TestReadJSONCompletionStops(t *testing.T) {
	old := jsonCompletionLimit
	jsonCompletionLimit = 32
	t.Cleanup(func() { jsonCompletionLimit = old })
	body := `{"choices":[{"message":{"content":"ok"}}]}`
	r := &capReader{s: body, left: len(body) + 200}
	err := readJSONCompletion(r, func(Delta) {})
	if err == nil {
		t.Fatal("a truncated body was accepted")
	}
	if r.read > int(jsonCompletionLimit) {
		t.Fatalf("read %d, limit %d", r.read, jsonCompletionLimit)
	}
	if r.read == 0 {
		t.Fatal("read nothing")
	}
}

func TestValidateConfig(t *testing.T) {
	if err := validateConfig(Config{Model: "m", BaseURL: "https://example.com/v1"}); err == nil {
		t.Fatal("missing key was accepted")
	}
	if err := validateConfig(Config{Model: "m", BaseURL: "http://127.0.0.1:11434/v1", Provider: "Ollama"}); err != nil {
		t.Fatal(err)
	}
	if err := validateConfig(Config{}); err == nil {
		t.Fatal("empty config was accepted")
	}
}

// capReader yields s and then keeps offering bytes until left is exhausted.
// A reader without a limit would pull the extra bytes.
type capReader struct {
	s    string
	left int
	read int
}

func (c *capReader) Read(p []byte) (int, error) {
	if c.left <= 0 {
		return 0, io.EOF
	}
	n := len(p)
	if n > c.left {
		n = c.left
	}
	if c.read < len(c.s) {
		n = copy(p[:n], c.s[c.read:])
	} else {
		for i := 0; i < n; i++ {
			p[i] = ' '
		}
	}
	c.read += n
	c.left -= n
	return n, nil
}

type chunkReader struct {
	s string
	n int
}

func (c *chunkReader) Read(p []byte) (int, error) {
	if len(c.s) == 0 {
		return 0, io.EOF
	}
	n := c.n
	if n > len(p) {
		n = len(p)
	}
	if n > len(c.s) {
		n = len(c.s)
	}
	copy(p, c.s[:n])
	c.s = c.s[n:]
	return n, nil
}
