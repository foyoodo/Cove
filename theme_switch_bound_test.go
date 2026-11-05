package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"cove/themes"
)

func TestThemeSwitchBoundDecision(t *testing.T) {
	ref := 50 * time.Millisecond
	if themeSwitchMainFail(50*time.Microsecond, 40*time.Microsecond) {
		t.Fatal("a few dozen microseconds on the main thread is within 16ms")
	}
	if !themeSwitchMainFail(themeSwitchMainBound+time.Millisecond, 0) {
		t.Fatal("prime over the main-thread cap must fail")
	}
	if !themeSwitchMainFail(0, themeSwitchMainBound+time.Millisecond) {
		t.Fatal("switch over the main-thread cap must fail")
	}
	if themeSwitchGenFail(time.Duration(float64(ref)*1.2), ref) {
		t.Fatal("1.2× the reference is inside the CI spread and must pass")
	}
	if !themeSwitchGenFail(2*ref, ref) {
		t.Fatal("2× the reference must fail")
	}
	limit := generationReadyLimit(ref)
	if limit != 75*time.Millisecond {
		t.Fatalf("limit %s, want 75ms", limit)
	}
}

func TestThemeSwitchRetryLog(t *testing.T) {
	ref := 50 * time.Millisecond
	var logs []string
	logf := func(format string, args ...any) {
		logs = append(logs, strings.TrimSpace(sprintfTheme(format, args...)))
	}
	samples := []time.Duration{2 * ref, time.Duration(float64(ref) * 1.1)}
	n := 0
	_, err := runThemeSwitchAttempts(logf, "TestThemeSwitchMainThreadBound", ref, func() (time.Duration, time.Duration, time.Duration, error) {
		shown := samples[n]
		n++
		return 20 * time.Microsecond, 30 * time.Microsecond, shown, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("attempts %d, want 2", n)
	}
	retry := perfRetryLine(logs)
	if retry == "" {
		t.Fatalf("missing PERF-RETRY: %v", logs)
	}
	for _, want := range []string{`test="TestThemeSwitchMainThreadBound"`, "durations="} {
		if !strings.Contains(retry, want) {
			t.Fatalf("line %q missing %q", retry, want)
		}
	}

	logs = nil
	n = 0
	_, err = runThemeSwitchAttempts(logf, "TestThemeSwitchMainThreadBound", ref, func() (time.Duration, time.Duration, time.Duration, error) {
		n++
		return 20 * time.Microsecond, 30 * time.Microsecond, 2 * ref, nil
	})
	if err == nil {
		t.Fatal("two generations at 2× the reference returned success")
	}
	if n != 2 {
		t.Fatalf("attempts %d, want 2", n)
	}
	if strings.Count(strings.Join(logs, "\n"), "PERF-RETRY:") != 1 {
		t.Fatalf("PERF-RETRY count in %v", logs)
	}

	logs = nil
	n = 0
	_, err = runThemeSwitchAttempts(logf, "TestThemeSwitchMainThreadBound", ref, func() (time.Duration, time.Duration, time.Duration, error) {
		n++
		return themeSwitchMainBound + time.Millisecond, 0, ref, nil
	})
	if err == nil || !strings.Contains(err.Error(), "main thread") {
		t.Fatalf("error %v", err)
	}
	if n != 1 {
		t.Fatalf("main-thread failure retried (%d attempts)", n)
	}
	if perfRetryLine(logs) != "" {
		t.Fatalf("main-thread failure logged a retry: %v", logs)
	}
}

func perfRetryLine(logs []string) string {
	for _, line := range logs {
		if strings.Contains(line, "PERF-RETRY:") {
			return line
		}
	}
	return ""
}

func sprintfTheme(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}

// TestThemeSwitchGenerationRegression slows only the measured generation
// by one reference duration. The reference itself is unslowed, so this is
// a 2× regression of the bitmap under test and must fail the ratio gate.
func TestThemeSwitchGenerationRegression(t *testing.T) {
	const w, h = 2880, 1800
	reference := referenceGeneration(w, h)
	resetGradientCache()
	gradSync.Store(false)
	gradMotion.Store(true)
	now := time.Now()
	presentGradient(themeLook(themes.ThemeWarm, false), w, h, now)
	awaitGradients()

	gateMu.Lock()
	gradRenderGate = func() { time.Sleep(reference) }
	gateMu.Unlock()
	defer func() {
		gateMu.Lock()
		gradRenderGate = nil
		gateMu.Unlock()
	}()

	visible := time.Now()
	presentGradient(themeLook(themes.ThemeTide, false), w, h, now)
	awaitGradients()
	shown := time.Since(visible)
	t.Logf("slowed generation %s reference %s ratio %.2f", shown, reference, float64(shown)/float64(reference))
	if !themeSwitchGenFail(shown, reference) {
		t.Fatalf("2× slowdown %s against reference %s was accepted", shown, reference)
	}
}
