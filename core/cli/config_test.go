package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestConfigShow(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "modules/hello/config.default.toml", "enabled = false\n[module]\ngreeting = \"hello\"\n")
	writeFile(t, root, "config/hello.local.toml", "[module]\ngreeting = \"czesc\"\n")
	env := map[string]string{"KOMBINAT_ROOT": root}

	r := run(t, env, false, "config", "show")
	if r.code != ExitOK {
		t.Fatalf("code %d: %s", r.code, r.stderr)
	}
	var got struct {
		Core    map[string]any            `json:"core"`
		Modules map[string]map[string]any `json:"modules"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, r.stdout)
	}
	if got.Modules["hello"]["module"].(map[string]any)["greeting"] != "czesc" {
		t.Fatalf("merged config: %s", r.stdout)
	}
	if r := run(t, env, false, "config", "show", "hello"); !strings.Contains(r.stdout, "czesc") {
		t.Fatalf("config show hello: %s", r.stdout)
	}
	if r := run(t, env, false, "config", "show", "core"); !strings.Contains(r.stdout, `"dir": "modules"`) {
		t.Fatalf("config show core: %s", r.stdout)
	}
	if r := run(t, env, false, "config", "show", "nope"); r.code != ExitError {
		t.Fatalf("config show nope: code %d", r.code)
	}
}

func TestConfigInvalidValueExits1(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "config/core.toml", "[modules]\ndir = 5\n")
	r := run(t, map[string]string{"KOMBINAT_ROOT": root}, false, "config", "show")
	if r.code != ExitError || r.stdout != "" {
		t.Fatalf("code %d stdout %q", r.code, r.stdout)
	}
	if !strings.Contains(r.stderr, "config/core.toml: modules.dir: want string, got integer") {
		t.Fatalf("stderr must name file and key: %q", r.stderr)
	}
}

func TestConfigNeedsSubcommand(t *testing.T) {
	if r := run(t, nil, false, "config"); r.code != ExitUsage {
		t.Fatalf("config: code %d", r.code)
	}
	if r := run(t, nil, false, "config", "nope"); r.code != ExitUsage {
		t.Fatalf("config nope: code %d", r.code)
	}
}
