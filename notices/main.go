// Command notices writes third_party_notices.txt from the module cache.
// Run it from the repo root with `go generate`.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// friendly names for the modules Cove actually links. Anything else in
// `go list -m all` is a tool or a test dependency and is left out.
var names = map[string]string{
	"github.com/egoist/mygo":         "MyGo",
	"golang.org/x/image":             "Go image",
	"github.com/ebitengine/purego":   "PureGo",
	"github.com/go-text/typesetting": "go-text typesetting",
	"golang.org/x/text":              "Go text",
}

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	mods, err := listModules(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	byPath := map[string]module{}
	for _, m := range mods {
		byPath[m.path] = m
	}
	order := []string{
		"github.com/egoist/mygo",
		"golang.org/x/image",
		"github.com/ebitengine/purego",
		"github.com/go-text/typesetting",
		"golang.org/x/text",
	}
	var b strings.Builder
	for _, path := range order {
		m, ok := byPath[path]
		if !ok {
			fmt.Fprintf(os.Stderr, "missing module %s\n", path)
			os.Exit(1)
		}
		text, err := os.ReadFile(filepath.Join(m.dir, "LICENSE"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", m.path, err)
			os.Exit(1)
		}
		writeNotice(&b, names[path], m.path, m.version, string(text))
	}
	writeGo(&b)
	out := filepath.Join(root, "third_party_notices.txt")
	if err := os.WriteFile(out, []byte(b.String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type module struct {
	path, version, dir string
}

func listModules(root string) ([]module, error) {
	cmd := exec.Command("go", "list", "-m", "-f", "{{.Path}}\t{{.Version}}\t{{.Dir}}", "all")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var mods []module
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.Split(line, "\t")
		if len(parts) != 3 || parts[2] == "" {
			continue
		}
		mods = append(mods, module{path: parts[0], version: parts[1], dir: parts[2]})
	}
	return mods, nil
}

func writeGo(b *strings.Builder) {
	goroot := strings.TrimSpace(os.Getenv("GOROOT"))
	if goroot == "" {
		out, err := exec.Command("go", "env", "GOROOT").Output()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		goroot = strings.TrimSpace(string(out))
	}
	verOut, err := exec.Command("go", "env", "GOVERSION").Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	text, err := os.ReadFile(filepath.Join(goroot, "LICENSE"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	writeNotice(b, "Go", "std", strings.TrimSpace(string(verOut)), string(text))
}

func writeNotice(b *strings.Builder, name, module, version, text string) {
	fmt.Fprintf(b, "@@NAME %s\n@@MODULE %s\n@@VERSION %s\n@@LICENSE\n%s\n@@END\n", name, module, version, strings.TrimSpace(text))
}
