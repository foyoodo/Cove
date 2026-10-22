package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// preferredArtifactRoot is where screenshot and measurement dumps go on
// this machine. GitHub-hosted runners cannot create it.
const preferredArtifactRoot = "/opt/cursor/artifacts"

var (
	artifactTemps   sync.Map // *testing.T → temp root
	artifactRootLog sync.Once
)

// testArtifactDir returns a writable directory for screenshots and
// measurement dumps. COVE_TEST_ARTIFACTS selects the root when it is
// set. Otherwise the root is /opt/cursor/artifacts when that directory
// can be created, and t.TempDir when it cannot. sub is a relative path
// under the root; an empty sub is the root itself.
func testArtifactDir(t *testing.T, sub string) string {
	t.Helper()
	root := testArtifactRoot(t)
	if sub == "" {
		return root
	}
	dir := filepath.Join(root, filepath.FromSlash(sub))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create artifact dir %s: %v", dir, err)
	}
	return dir
}

func testArtifactRoot(t *testing.T) string {
	t.Helper()
	if v, ok := artifactTemps.Load(t); ok {
		return v.(string)
	}
	var candidates []string
	if dir := os.Getenv("COVE_TEST_ARTIFACTS"); dir != "" {
		candidates = append(candidates, dir)
	}
	candidates = append(candidates, preferredArtifactRoot)
	var failed []string
	for _, dir := range candidates {
		err := os.MkdirAll(dir, 0o755)
		if err == nil {
			return dir
		}
		failed = append(failed, fmt.Sprintf("%s (%v)", dir, err))
	}
	artifactRootLog.Do(func() {
		t.Logf("artifact roots not writable: %s; writing to a temp dir", strings.Join(failed, "; "))
	})
	dir := t.TempDir()
	artifactTemps.Store(t, dir)
	t.Cleanup(func() { artifactTemps.Delete(t) })
	return dir
}
