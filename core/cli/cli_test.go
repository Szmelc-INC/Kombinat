package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

type result struct {
	code           int
	stdout, stderr string
}

func run(t *testing.T, env map[string]string, tty bool, args ...string) result {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(args, IO{
		Stdout: &out,
		Stderr: &errb,
		Getenv: func(k string) string { return env[k] },
		TTY:    tty,
	})
	return result{code, out.String(), errb.String()}
}

func TestVersion(t *testing.T) {
	r := run(t, nil, false, "version")
	if r.code != ExitOK || r.stdout != "kombinat "+version+"\n" {
		t.Fatalf("version: code %d, stdout %q", r.code, r.stdout)
	}
	r = run(t, nil, false, "--json", "version")
	var got map[string]string
	if err := json.Unmarshal([]byte(r.stdout), &got); err != nil || got["version"] != version {
		t.Fatalf("version --json: %v, %q", err, r.stdout)
	}
	if r := run(t, nil, false, "version", "extra"); r.code != ExitUsage {
		t.Fatalf("version extra: code %d, want %d", r.code, ExitUsage)
	}
}

func TestUnknownCommandIsUsageError(t *testing.T) {
	r := run(t, nil, false, "nope")
	if r.code != ExitUsage {
		t.Fatalf("code %d, want %d", r.code, ExitUsage)
	}
	if r.stdout != "" || !strings.Contains(r.stderr, "unknown command: nope") || !strings.Contains(r.stderr, "Usage") {
		t.Fatalf("usage must go to stderr only; stdout %q stderr %q", r.stdout, r.stderr)
	}
}

func TestNoArgsIsUsageError(t *testing.T) {
	if r := run(t, nil, false); r.code != ExitUsage || r.stdout != "" {
		t.Fatalf("no args: code %d stdout %q", r.code, r.stdout)
	}
	if r := run(t, nil, false, "--help"); r.code != ExitOK || !strings.Contains(r.stdout, "Commands") {
		t.Fatalf("--help: code %d stdout %q", r.code, r.stdout)
	}
}

func TestHelpListsEveryCommand(t *testing.T) {
	r := run(t, nil, false, "help")
	for _, c := range Root().Sub {
		if !strings.Contains(r.stdout, "  "+c.Name) {
			t.Errorf("help misses %q:\n%s", c.Name, r.stdout)
		}
	}
	if r := run(t, nil, false, "help", "version"); !strings.Contains(r.stdout, "kombinat version") {
		t.Errorf("help version: %q", r.stdout)
	}
	if r := run(t, nil, false, "version", "--help"); r.code != ExitOK || !strings.Contains(r.stdout, "Print the core version") {
		t.Errorf("version --help: code %d %q", r.code, r.stdout)
	}
	if r := run(t, nil, false, "help", "nope"); r.code != ExitUsage {
		t.Errorf("help nope: code %d", r.code)
	}
}

func TestColor(t *testing.T) {
	esc := "\x1b["
	if r := run(t, nil, true, "help"); !strings.Contains(r.stdout, esc) {
		t.Error("tty help should be colored")
	}
	if r := run(t, nil, false, "help"); strings.Contains(r.stdout, esc) {
		t.Error("non-tty help must be plain")
	}
	if r := run(t, map[string]string{"NO_COLOR": "1"}, true, "help"); strings.Contains(r.stdout, esc) {
		t.Error("NO_COLOR must disable colors")
	}
	if r := run(t, nil, true, "--no-color", "help"); strings.Contains(r.stdout, esc) {
		t.Error("--no-color must disable colors")
	}
}

func TestCompletionShells(t *testing.T) {
	for _, sh := range shells {
		r := run(t, nil, false, "completion", sh)
		if r.code != ExitOK {
			t.Fatalf("completion %s: code %d %s", sh, r.code, r.stderr)
		}
		for _, c := range Root().Sub {
			if !strings.Contains(r.stdout, c.Name) {
				t.Errorf("completion %s misses %q", sh, c.Name)
			}
		}
	}
	if r := run(t, nil, false, "completion", "tcsh"); r.code != ExitUsage {
		t.Errorf("completion tcsh: code %d", r.code)
	}
	if r := run(t, nil, false, "completion"); r.code != ExitUsage {
		t.Errorf("completion without shell: code %d", r.code)
	}
}

// Completion scripts quote only simple tokens; keep names shell-safe.
func TestCommandNamesAreShellSafe(t *testing.T) {
	safe := regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	for _, n := range walk(Root(), &Env{root: Root()}) {
		for _, v := range n.values {
			if !safe.MatchString(v.word) {
				t.Errorf("unsafe completion word %q at %q", v.word, n.path)
			}
		}
	}
}

// The committed completion files must match the generator (run `make completions`).
func TestCompletionFilesInSync(t *testing.T) {
	files := map[string]string{"bash": "kombinat.bash", "zsh": "_kombinat", "fish": "kombinat.fish"}
	for sh, name := range files {
		want, ok := Completion(Root(), sh, &Env{root: Root()})
		if !ok {
			t.Fatalf("no generator for %s", sh)
		}
		got, err := os.ReadFile(filepath.Join("..", "..", "completions", name))
		if err != nil {
			t.Fatalf("read completions/%s: %v (run make completions)", name, err)
		}
		if string(got) != want {
			t.Errorf("completions/%s is stale; run make completions", name)
		}
	}
}
