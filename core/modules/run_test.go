package modules

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// needRuntime skips when prog is missing, unless KOMBINAT_REQUIRE_RUNTIMES is
// set (CI sets it), so CI can never pass by skipping a language.
func needRuntime(t *testing.T, prog string) {
	t.Helper()
	if _, err := exec.LookPath(prog); err != nil {
		if os.Getenv("KOMBINAT_REQUIRE_RUNTIMES") != "" {
			t.Fatalf("%s is required (KOMBINAT_REQUIRE_RUNTIMES) but missing", prog)
		}
		t.Skipf("%s not installed", prog)
	}
}

// runFixture enables module name (already written under root) and returns a runner.
func runFixture(t *testing.T, root, name string, timeout time.Duration) *Runner {
	t.Helper()
	mg, _ := manager(t, root)
	m := byName(mg.Modules, name)
	if m == nil || !m.Valid() {
		t.Fatalf("fixture %s invalid: %+v", name, m)
	}
	if _, err := mg.Enable(context.Background(), name, "test"); err != nil {
		t.Fatal(err)
	}
	return &Runner{Manager: mg, CacheDir: filepath.Join(t.TempDir(), "cache"), DataDir: filepath.Join(t.TempDir(), "data"), Timeout: timeout, Path: os.Getenv("PATH")}
}

func shellModule(t *testing.T, root, name, script string) {
	t.Helper()
	fixture(t, root, name, "[]")
	write(t, root, "modules/"+name+"/config.default.toml", "enabled = false\n[module]\nname = \"world\"\n")
	write(t, root, "modules/"+name+"/src/cli.sh", script)
}

func TestRunShell(t *testing.T) {
	root := t.TempDir()
	shellModule(t, root, "greet", `printf 'hi %s %s\n' "$1" "$KOMBINAT_CFG_MODULE_NAME"; printf 'cfg=%s\n' "$KOMBINAT_CONFIG"; printf 'leak=%s\n' "${KOMBINAT_TEST_SECRET:-none}"; echo warn >&2`)
	t.Setenv("KOMBINAT_TEST_SECRET", "s3cret")
	r := runFixture(t, root, "greet", 5*time.Second)
	res, err := r.Run(context.Background(), "greet", RunRequest{Args: []string{"there; rm -rf /"}}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusOK || res.ExitCode != 0 || res.Lang != "shell" {
		t.Fatalf("result: %+v", res)
	}
	if !strings.HasPrefix(res.Stdout, "hi there; rm -rf / world\n") {
		t.Fatalf("args must arrive verbatim, config via env: %q", res.Stdout)
	}
	if !strings.Contains(res.Stdout, `"name":"world"`) || !strings.Contains(res.Stdout, "leak=none") {
		t.Fatalf("env: %q", res.Stdout)
	}
	if res.Stderr != "warn\n" || res.EventID == 0 {
		t.Fatalf("stderr %q event %d", res.Stderr, res.EventID)
	}
}

func TestRunLogsResult(t *testing.T) {
	root := t.TempDir()
	shellModule(t, root, "fail", "echo nope; exit 3\n")
	r := runFixture(t, root, "fail", 5*time.Second)
	res, err := r.Run(context.Background(), "fail", RunRequest{}, "test")
	if err != nil || res.Status != StatusFailed || res.ExitCode != 3 {
		t.Fatalf("result %+v err %v", res, err)
	}
	list, _ := r.Manager.Log.List(context.Background(), eventsQuery(1))
	e := list[0]
	if e.Action != "core.module.run" || e.Subject != "fail" || e.Payload["exit_code"] != float64(3) || e.Payload["stdout"] != "nope\n" {
		t.Fatalf("event: %+v", e)
	}
	if _, ok := e.Payload["duration_ms"]; !ok {
		t.Fatalf("event lacks duration: %+v", e.Payload)
	}
}

func TestRunTimeoutKillsTheGroup(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(t.TempDir(), "orphan")
	shellModule(t, root, "slow", "(sleep 1; touch '"+marker+"') &\nsleep 30\n")
	r := runFixture(t, root, "slow", 300*time.Millisecond)
	start := time.Now()
	res, err := r.Run(context.Background(), "slow", RunRequest{}, "test")
	if err != nil || res.Status != StatusTimeout {
		t.Fatalf("result %+v err %v", res, err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("timeout took %s", time.Since(start))
	}
	time.Sleep(1500 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a child of the timed-out run survived and ran")
	}
}

func TestRunPHP(t *testing.T) {
	root := t.TempDir()
	fixture(t, root, "php-mod", "[]")
	os.Remove(filepath.Join(root, "modules/php-mod/src/cli.sh"))
	write(t, root, "modules/php-mod/src/cli.php", "<?php\necho 'php ', $argv[1] ?? '-', ' ', getenv('KOMBINAT_MODULE'), \"\\n\";\nexit(2);\n")
	r := runFixture(t, root, "php-mod", 10*time.Second)

	r.Path = "" // no runtime on PATH: the run reports runtime-missing, the core is fine
	res, err := r.Run(context.Background(), "php-mod", RunRequest{}, "test")
	if err != nil || res.Status != StatusRuntimeMissing || !strings.Contains(res.Error, "php not found") {
		t.Fatalf("missing php: %+v %v", res, err)
	}

	needRuntime(t, "php")
	r.Path = os.Getenv("PATH")
	res, err = r.Run(context.Background(), "php-mod", RunRequest{Args: []string{"x"}}, "test")
	if err != nil || res.Status != StatusFailed || res.ExitCode != 2 || res.Stdout != "php x php-mod\n" {
		t.Fatalf("php run: %+v %v", res, err)
	}
}

func TestRunGo(t *testing.T) {
	root := t.TempDir()
	fixture(t, root, "go-mod", "[]")
	dir := "modules/go-mod/"
	os.Remove(filepath.Join(root, dir+"src/cli.sh"))
	write(t, root, dir+"src/cli/main.go", "package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\nfunc main() { fmt.Println(\"go\", os.Args[1], os.Getenv(\"KOMBINAT_MODULE\")) }\n")
	mods := discover(t, root)
	if m := byName(mods, "go-mod"); m.Valid() {
		t.Fatal("a Go entrypoint without its own go.mod must be invalid")
	}
	write(t, root, dir+"go.mod", "module example.com/gomod\n\ngo 1.22\n")
	r := runFixture(t, root, "go-mod", 2*time.Minute)

	r.Path = ""
	if res, _ := r.Run(context.Background(), "go-mod", RunRequest{}, "test"); res.Status != StatusRuntimeMissing {
		t.Fatalf("missing go: %+v", res)
	}

	needRuntime(t, "go")
	r.Path = os.Getenv("PATH")
	res, err := r.Run(context.Background(), "go-mod", RunRequest{Args: []string{"y"}}, "test")
	if err != nil || res.Status != StatusOK || res.Stdout != "go y go-mod\n" {
		t.Fatalf("go run: %+v %v", res, err)
	}
}

func TestRunDataDirAndModuleEvents(t *testing.T) {
	root := t.TempDir()
	script := `n=$(cat "$KOMBINAT_DATA/count" 2>/dev/null || echo 0); n=$((n + 1)); echo "$n" >"$KOMBINAT_DATA/count"
echo '{"action":"notes.added","summary":"note","payload":{"n":1,"token":"s3cret"}}' >>"$KOMBINAT_EVENTS"
echo '{"action":"core.module.enable","summary":"forged"}' >>"$KOMBINAT_EVENTS"
echo 'not json' >>"$KOMBINAT_EVENTS"
echo '{"action":"notes.x","extra":1}' >>"$KOMBINAT_EVENTS"
echo "count $n"
`
	shellModule(t, root, "notes", script)
	r := runFixture(t, root, "notes", 5*time.Second)
	ctx := context.Background()
	for want := 1; want <= 2; want++ {
		res, err := r.Run(ctx, "notes", RunRequest{}, "test")
		if err != nil || res.Status != StatusOK || res.Stdout != fmt.Sprintf("count %d\n", want) {
			t.Fatalf("run %d: %+v %v", want, res, err)
		}
		if res.Events != 1 || len(res.EventsRejected) != 3 {
			t.Fatalf("run %d events: %d accepted, rejected %q", want, res.Events, res.EventsRejected)
		}
	}
	if info, err := os.Stat(filepath.Join(r.DataDir, "notes")); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("data dir: %v %v", info, err)
	}
	if left, _ := filepath.Glob(filepath.Join(r.DataDir, "notes", ".events-*")); len(left) != 0 {
		t.Fatalf("events files left behind: %v", left)
	}
	list, _ := r.Manager.Log.List(ctx, eventsQuery(10))
	var got []string
	for _, e := range list {
		got = append(got, e.Action+"/"+e.Actor)
		if e.Action == "notes.added" && e.Payload["token"] != "[redacted]" {
			t.Fatalf("module event payload not redacted: %v", e.Payload)
		}
	}
	want := "core.module.enable/test,notes.added/test,core.module.run/test,notes.added/test,core.module.run/test"
	if strings.Join(got, ",") != want {
		t.Fatalf("log:\n got %s\nwant %s", strings.Join(got, ","), want)
	}
}

func TestRunRejects(t *testing.T) {
	root := t.TempDir()
	shellModule(t, root, "greet", "echo hi\n")
	shellModule(t, root, "idle", "echo hi\n")
	r := runFixture(t, root, "greet", time.Second)
	ctx := context.Background()
	for name, c := range map[string]struct {
		module string
		req    RunRequest
		want   error
	}{
		"not running":  {"idle", RunRequest{}, ErrNotFound},
		"unknown":      {"ghost", RunRequest{}, ErrNotFound},
		"undeclared":   {"greet", RunRequest{Entrypoint: "api"}, ErrBadRequest},
		"worker":       {"greet", RunRequest{Entrypoint: "worker"}, ErrBadRequest},
		"nul arg":      {"greet", RunRequest{Args: []string{"a\x00b"}}, ErrBadRequest},
		"too many arg": {"greet", RunRequest{Args: make([]string, MaxArgs+1)}, ErrBadRequest},
	} {
		if _, err := r.Run(ctx, c.module, c.req, "test"); !errors.Is(err, c.want) {
			t.Errorf("%s: err %v, want %v", name, err, c.want)
		}
	}
}

func TestCapped(t *testing.T) {
	head := &capped{max: 4}
	head.Write([]byte("abcdef"))
	tail := &capped{max: 4, tail: true}
	tail.Write([]byte("abc"))
	tail.Write([]byte("def"))
	if head.String() != "abcd" || !head.cut || tail.String() != "cdef" || !tail.cut {
		t.Fatalf("head %q tail %q", head.String(), tail.String())
	}
}
