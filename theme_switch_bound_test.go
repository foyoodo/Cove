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
	first := 90 * time.Millisecond
	second := 40 * time.Millisecond
	_, err = runThemeSwitchAttempts(logf, "TestThemeSwitchMainThreadBound", ref, func() (time.Duration, time.Duration, time.Duration, error) {
		n++
		if n == 1 {
			return 20 * time.Microsecond, 30 * time.Microsecond, first, nil
		}
		return themeSwitchMainBound + time.Millisecond, 0, second, nil
	})
	if err == nil || !strings.Contains(err.Error(), "main thread") {
		t.Fatalf("error %v", err)
	}
	if n != 2 {
		t.Fatalf("retry of a later main-thread failure measured %d times, want 2", n)
	}
	retry = perfRetryLine(logs)
	if retry == "" {
		t.Fatalf("failed retry did not log PERF-RETRY: %v", logs)
	}
	for _, want := range []string{first.String(), second.String()} {
		if !strings.Contains(retry, want) {
			t.Fatalf("PERF-RETRY line %q missing duration %s", retry, want)
		}
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

// TestThemeSwitchGenerationRegression slows renderGradient itself, on
// both the synchronous reference and the async switch, by about 2×.
// The CPU calibration does not call renderGradient, so that gate fails.
func TestThemeSwitchGenerationRegression(t *testing.T) {
	const w, h = 2880, 1800
	calib := cpuCalibration()
	unslowed := referenceGeneration(w, h)
	if themeSwitchCalibFail(unslowed, calib) {
		t.Fatalf("unslowed reference %s is already %.2f× calibration %s", unslowed, float64(unslowed)/float64(calib), calib)
	}

	gateMu.Lock()
	gradRenderGate = func() { time.Sleep(unslowed) }
	gateMu.Unlock()
	defer func() {
		gateMu.Lock()
		gradRenderGate = nil
		gateMu.Unlock()
	}()

	slowedRef := referenceGeneration(w, h)
	resetGradientCache()
	gradSync.Store(false)
	gradMotion.Store(true)
	now := time.Now()
	presentGradient(themeLook(themes.ThemeWarm, false), w, h, now)
	awaitGradients()
	visible := time.Now()
	presentGradient(themeLook(themes.ThemeTide, false), w, h, now)
	awaitGradients()
	shown := time.Since(visible)
	t.Logf("slowed reference %s slowed switch %s calibration %s ratios %.2f and %.2f",
		slowedRef, shown, calib, float64(slowedRef)/float64(calib), float64(shown)/float64(calib))
	if !themeSwitchCalibFail(slowedRef, calib) {
		t.Fatalf("2× renderGradient reference %s against calibration %s was accepted", slowedRef, calib)
	}
	if !themeSwitchCalibFail(shown, calib) {
		t.Fatalf("2× renderGradient switch %s against calibration %s was accepted", shown, calib)
	}
}
