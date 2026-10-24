package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func quietFrames(n int, wall time.Duration) []streamSample {
	xs := make([]streamSample, n)
	for i := range xs {
		xs[i] = streamSample{wall: wall, build: wall / 4, layout: wall / 4, paint: wall / 4, present: wall / 4, tokens: i + 1}
	}
	return xs
}

// oneSlowPresent is a pass whose p95 stays under the cap and whose max is
// one present stall.
func oneSlowPresent() []streamSample {
	xs := quietFrames(40, time.Millisecond)
	xs[7] = streamSample{
		wall: 50 * time.Millisecond, build: 100 * time.Microsecond, layout: 200 * time.Microsecond,
		paint: 300 * time.Microsecond, present: 49 * time.Millisecond, tokens: 8,
	}
	return xs
}

func TestPerfRetryHelper(t *testing.T) {
	t.Run("pass on first try", func(t *testing.T) {
		var logs []string
		calls := 0
		err := checkStreamPasses(func(format string, args ...any) {
			logs = append(logs, fmt.Sprintf(format, args...))
		}, "after resize", func() []streamSample {
			calls++
			return quietFrames(40, time.Millisecond)
		})
		if err != nil {
			t.Fatal(err)
		}
		if calls != 1 {
			t.Fatalf("measured %d times, want 1", calls)
		}
		for _, line := range logs {
			if strings.Contains(line, "PERF-RETRY:") {
				t.Fatalf("logged a retry on a clean pass: %s", line)
			}
		}
	})

	t.Run("fail then pass", func(t *testing.T) {
		var logs []string
		calls := 0
		err := checkStreamPasses(func(format string, args ...any) {
			logs = append(logs, fmt.Sprintf(format, args...))
		}, "after resize", func() []streamSample {
			calls++
			if calls == 1 {
				return oneSlowPresent()
			}
			return quietFrames(40, time.Millisecond)
		})
		if err != nil {
			t.Fatal(err)
		}
		if calls != 2 {
			t.Fatalf("measured %d times, want 2", calls)
		}
		var retry string
		for _, line := range logs {
			if strings.Contains(line, "PERF-RETRY:") {
				retry = line
			}
		}
		if retry == "" {
			t.Fatalf("missing PERF-RETRY log: %v", logs)
		}
		for _, want := range []string{
			`test="after resize"`,
			"p95=",
			"max=",
			"frame=7",
			"build=",
			"layout=",
			"paint=",
			"present=",
		} {
			if !strings.Contains(retry, want) {
				t.Fatalf("PERF-RETRY line %q missing %q", retry, want)
			}
		}
	})

	t.Run("fail then fail", func(t *testing.T) {
		var logs []string
		calls := 0
		err := checkStreamPasses(func(format string, args ...any) {
			logs = append(logs, fmt.Sprintf(format, args...))
		}, "after resize", func() []streamSample {
			calls++
			return oneSlowPresent()
		})
		if err == nil {
			t.Fatal("second pass over the cap returned success")
		}
		if calls != 2 {
			t.Fatalf("measured %d times, want 2", calls)
		}
		if !strings.Contains(err.Error(), "second pass") {
			t.Fatalf("error %q does not name the second pass", err)
		}
		retries := 0
		for _, line := range logs {
			if strings.Contains(line, "PERF-RETRY:") {
				retries++
			}
		}
		if retries != 1 {
			t.Fatalf("PERF-RETRY lines = %d, want 1", retries)
		}
	})
}
