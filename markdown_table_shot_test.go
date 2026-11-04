package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func tableInlineMarkdown() string {
	return strings.Join([]string{
		"Inline markup in table cells:",
		"",
		"| Kind | Sample |",
		"| --- | --- |",
		"| Inline code | Use `cancel()` |",
		"| Link | See [docs](https://pkg.go.dev/context) |",
		"| Autolink | https://example.com |",
		"| Bold | **bold** |",
		"| Italic strike | *italic* ~~strike~~ |",
		"| Chinese | 中文 |",
		"| Emoji | 🎉 done |",
		"| Escaped pipe | `a\\|b` |",
		"| Literal Yes | Yes |",
	}, "\n")
}

func tableWideMarkdown() string {
	heads := make([]string, 10)
	align := make([]string, 10)
	row := make([]string, 10)
	for i := range heads {
		n := itoa(i + 1)
		heads[i] = "Column " + n + " heading"
		align[i] = "---"
		row[i] = "value-" + n + "-overflow"
	}
	return "| " + strings.Join(heads, " | ") + " |\n| " + strings.Join(align, " | ") + " |\n| " + strings.Join(row, " | ") + " |\n"
}

func tableWrapMarkdown() string {
	return strings.Join([]string{
		"Three settings control retries. Defaults are safe for most endpoints:",
		"",
		"| Setting | What it does | Default | Docs |",
		"| --- | --- | --- | --- |",
		"| `MaxRetries` | How many times a failed delivery is retried before the event is parked in the dead-letter queue. Retries use exponential backoff with full jitter, capped at **30 s** between attempts. | `3` | [Retry policy](https://example.com/retry) |",
		"| **Backoff** | Exponential with jitter; ~~linear backoff~~ was removed in v2. | `500ms` | ![backoff-curve.png](https://example.com/backoff-curve.png) |",
		"| Dead-letter queue | Parked events stay for 7 days. | On | [DLQ guide](https://example.com/dlq) |",
		"",
		"Pin the release you tested against:",
		"",
		"| Artifact | Source / checksum |",
		"| --- | --- |",
		"| webhook v2.4.1 | [https://pkg.go.dev/github.com/acme/webhook/v2@v2.4.1/retry#*Exponential*BackoffWithJitter](https://pkg.go.dev/github.com/acme/webhook/v2@v2.4.1/retry#ExponentialBackoffWithJitter) |",
		"| webhook-darwin-arm64.tar.gz | sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08 |",
	}, "\n")
}

func tableAlignMarkdown() string {
	return strings.Join([]string{
		"All four regions passed. Latency is p95 over the 20-minute run:",
		"",
		"| Region | Status | p95 latency | Requests | Error rate |",
		"| --- | :---: | ---: | ---: | --- |",
		"| us-east-1 | Pass | 182 ms | 1,204,331 | 0.02% |",
		"| eu-west-1 | Pass | 211 ms | 986,120 | 0.04% |",
		"| ap-northeast-1 | **Degraded** | **468 ms** | 402,877 | 0.31% |",
		"| sa-east-1 | Pass | 297 ms | 88,409 | 0.05% |",
		"",
		"Status is centered and the two numeric columns are right-aligned because the table says `:---:` and `---:`; *Error rate* has no marker, so it stays left.",
	}, "\n")
}

func tableDeliveriesMarkdown() string {
	return strings.Join([]string{
		"Seven deliveries failed between 06:00 and 09:00. The table is wider than the message, so it scrolls sideways:",
		"",
		"| Endpoint | Attempts | First try | Last try | Last status | Next action | Event |",
		"| --- | ---: | --- | --- | --- | --- | --- |",
		"| https://hooks.example.com/billing/v2 | 4 | 06:02:11 | 06:09:40 | 503 Service Unavailable | Retry at 09:30 | `evt_billing_1` |",
		"| https://hooks.example.com/billing/v2 | 4 | 06:14:55 | 06:22:03 | 503 Service Unavailable | Retry at 09:30 | `evt_billing_2` |",
		"| https://crm.internal.acme/webhooks | 2 | 07:41:30 | 07:41:52 | 401 Unauthorized | Parked: rotate secret | `evt_crm_1` |",
		"| https://ops.example.org/alerts | 5 | 08:58:02 | 09:12:47 | Timeout after 10 s | Parked: check VPN | `evt_ops_1` |",
		"",
		"The two billing failures share an outage window; retrying them after 09:30 should be enough.",
	}, "\n")
}

func tableShotApp(t *testing.T, body string) *App {
	t.Helper()
	return tableShotAppPrompt(t, "Table cells", "Show a table with inline markup.", body)
}

func tableShotAppPrompt(t *testing.T, title, prompt, body string) *App {
	t.Helper()
	a := newTestApp(t)
	now := time.Date(2026, 11, 4, 9, 52, 0, 0, time.UTC)
	connectBuiltin(a, "Anthropic", "sk-anthropic")
	cfg := DefaultConfig()
	cfg.Provider = "Anthropic"
	cfg.Model = "claude-sonnet-4.5"
	cfg.ConnectionID = "conn-anthropic"
	a.data.Conversations = []Conversation{{
		ID: "t", Title: title, Config: cfg,
		CreatedAt: now, UpdatedAt: now,
		Messages: []Message{
			{ID: "u", Role: roleUser, Content: prompt, Done: true, CreatedAt: now},
			{ID: "a", Role: roleAssistant, Content: body, Done: true, CreatedAt: now},
		},
	}}
	a.data.ActiveID = "t"
	return a
}

func TestTableInlineShots(t *testing.T) {
	dir := testArtifactDir(t, "table-inline")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	phase := os.Getenv("COVE_TABLE_SHOT_PHASE")
	if phase == "" {
		phase = "after"
	}
	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		a := tableShotApp(t, tableInlineMarkdown())
		tt := ui.NewTester(a.view, shotW, shotH)
		if dark {
			tt.SetDark(true)
		}
		tt.Frame()
		table := mustFind(t, tt, "table scroller")
		writePNG(t, filepath.Join(dir, "table-inline-"+phase+"-"+mode+".png"), tt.Image())
		writePNG(t, filepath.Join(dir, "table-inline-"+phase+"-"+mode+"-crop.png"), cropRect(tt.Image(), table, 8))

		wide := tableShotApp(t, tableWideMarkdown())
		tw := ui.NewTester(wide.view, 720, 520)
		if dark {
			tw.SetDark(true)
		}
		tw.Frame()
		scroller := mustFind(t, tw, "table scroller")
		scrollFarRight(tw, scroller.X+24, scroller.Y+20)
		scroller = mustFind(t, tw, "table scroller")
		writePNG(t, filepath.Join(dir, "table-wide-scrolled-"+phase+"-"+mode+".png"), tw.Image())
		writePNG(t, filepath.Join(dir, "table-wide-scrolled-"+phase+"-"+mode+"-crop.png"), cropRect(tw.Image(), scroller, 8))
	}
}

func TestTableDesignShots(t *testing.T) {
	dir := testArtifactDir(t, "table-design")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	fixtures := []struct {
		name, title, prompt, body string
		wide                      bool
	}{
		{"table-wrap", "Cancel HTTP on close", "Which retry settings does the webhook client expose?", tableWrapMarkdown(), false},
		{"table-align", "Cancel HTTP on close", "How did each region do in last night's load test?", tableAlignMarkdown(), false},
		{"table-wide-scroll", "Cancel HTTP on close", "List the failed deliveries from this morning.", tableDeliveriesMarkdown(), true},
	}
	for _, fx := range fixtures {
		for _, dark := range []bool{false, true} {
			mode := "light"
			if dark {
				mode = "dark"
			}
			a := tableShotAppPrompt(t, fx.title, fx.prompt, fx.body)
			tt := ui.NewTester(a.view, shotW, shotH)
			if dark {
				tt.SetDark(true)
			}
			tt.Frame()
			if fx.wide {
				placeHScroll(t, a, tt, "middle")
			}
			table := mustFind(t, tt, "table scroller")
			switch fx.name {
			case "table-wrap":
				assertWrapShotGeometry(t, tt, table)
			case "table-align":
				assertAlignShotGeometry(t, tt, table)
			case "table-wide-scroll":
				assertWideShotGeometry(t, a, tt, table)
			}
			writePNG(t, filepath.Join(dir, fx.name+"-"+mode+".png"), tt.Image())
			writePNG(t, filepath.Join(dir, fx.name+"-"+mode+"-crop.png"), cropRect(tt.Image(), table, 8))
		}
	}
}

func assertWrapShotGeometry(t *testing.T, tt *ui.Tester, table ui.Rect) {
	t.Helper()
	if !tt.HasText("Exponential") || !tt.HasText("linear backoff") {
		t.Fatalf("wrap fixture missing Exponential/linear backoff: %q", tt.Texts())
	}
	cols := tt.FindAll("table-col-1")
	if len(cols) == 0 {
		t.Fatalf("wrap What it does column missing")
	}
	if cols[0].W > tableMaxCol+1 {
		t.Fatalf("wrap column 1 exceeded 320: %.1f", cols[0].W)
	}
	if table.H <= tableMinRowH*4 {
		t.Fatalf("wrap table did not grow with wrapped cells: %v", table)
	}
}

func assertAlignShotGeometry(t *testing.T, tt *ui.Tester, table ui.Rect) {
	t.Helper()
	if !tt.HasText("Degraded") || !tt.HasText("468 ms") {
		t.Fatalf("align fixture missing Degraded/468 ms: %q", tt.Texts())
	}
	want := float32(tableMinRowH * 5) // header + 4 body rows
	if abs32(table.H-want) > 1 {
		t.Fatalf("align table height %.1f, want %.1f", table.H, want)
	}
}

func assertWideShotGeometry(t *testing.T, a *App, tt *ui.Tester, table ui.Rect) {
	t.Helper()
	if !tt.HasText("evt_billing_1") {
		t.Fatalf("wide fixture missing evt_ code: %q", tt.Texts())
	}
	if len(tt.FindAll("table-col-6")) == 0 {
		t.Fatalf("wide table is not 7 columns")
	}
	if len(tt.FindAll("table-col-7")) != 0 {
		t.Fatalf("wide table has more than 7 columns")
	}
	var slot *hScrollSlot
	for _, s := range a.hScrolls {
		if s.st.MaxX > 1 {
			slot = s
			break
		}
	}
	if slot == nil {
		t.Fatalf("wide table did not overflow: table %v", table)
	}
}
