package modules

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Szmelc-INC/Kombinat/core/config"
	"github.com/Szmelc-INC/Kombinat/core/events"
	"github.com/Szmelc-INC/Kombinat/core/store"
)

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const manifestTmpl = `{
  "name": "NAME",
  "display_name": "Name",
  "version": "0.1.0",
  "description": "A test module.",
  "depends": DEPENDS,
  "provides": {"api": [], "events": ["NAME.done"], "capabilities": [], "config": ["enabled"]},
  "entrypoints": {"api": false, "cli": true, "ui": false, "worker": false},
  "locales": ["en", "pl"]
}`

// fixture writes a complete valid module; edit files afterwards to break it.
func fixture(t *testing.T, root, name, depends string) {
	t.Helper()
	dir := "modules/" + name + "/"
	m := strings.ReplaceAll(manifestTmpl, "NAME", name)
	write(t, root, dir+"module.json", strings.ReplaceAll(m, "DEPENDS", depends))
	write(t, root, dir+"MODULE.md", "# x\n")
	write(t, root, dir+"SKILL.md", "---\nname: "+name+"\n---\n")
	write(t, root, dir+"ASSERTIONS.md", "# x\n")
	write(t, root, dir+"config.default.toml", "enabled = false\n")
	write(t, root, dir+"src/cli.sh", "echo hi\n")
	write(t, root, dir+"locales/en.json", `{"module.`+name+`.title": "X"}`)
	write(t, root, dir+"locales/pl.json", `{"module.`+name+`.title": "X"}`)
}

func discover(t *testing.T, root string) []*Module {
	t.Helper()
	reg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	mods, err := Discover(reg)
	if err != nil {
		t.Fatal(err)
	}
	return mods
}

func byName(mods []*Module, name string) *Module {
	for _, m := range mods {
		if m.Name == name {
			return m
		}
	}
	return nil
}

func TestValidModule(t *testing.T) {
	root := t.TempDir()
	fixture(t, root, "hello", "[]")
	mods := discover(t, root)
	if len(mods) != 1 || !mods[0].Valid() {
		t.Fatalf("problems: %v", mods[0].Problems)
	}
	if ep := mods[0].Entrypoints["cli"]; ep.Lang != "shell" || ep.Path != "src/cli.sh" {
		t.Fatalf("cli entrypoint: %+v", ep)
	}
}

func TestRepoModulesAreValid(t *testing.T) {
	mods := discover(t, filepath.Join("..", ".."))
	if len(mods) == 0 {
		t.Fatal("the repo ships at least the hello module")
	}
	for _, m := range mods {
		if !m.Valid() {
			t.Errorf("%s: %v", m.Name, m.Problems)
		}
	}
}

func TestInvalidModules(t *testing.T) {
	root := t.TempDir()
	cases := map[string]struct {
		edit func()
		want string
	}{
		"badjson":   {func() { write(t, root, "modules/badjson/module.json", "{") }, "invalid JSON"},
		"missing":   {func() { write(t, root, "modules/missing/module.json", `{"name":"missing"}`) }, "missing key: display_name"},
		"unknown":   {func() { replace(t, root, "unknown", `"locales"`, `"extra": 1, "locales"`) }, "unknown field"},
		"rename":    {func() { replace(t, root, "rename", `"name": "rename"`, `"name": "other"`) }, "must equal the directory name"},
		"semver":    {func() { replace(t, root, "semver", `"0.1.0"`, `"1.0"`) }, "not SemVer"},
		"events":    {func() { replace(t, root, "events", `"events.done"`, `"core.done"`) }, "must be namespaced"},
		"noentry":   {func() { os.Remove(filepath.Join(root, "modules/noentry/src/cli.sh")) }, "entrypoints.cli is true"},
		"twoentry":  {func() { write(t, root, "modules/twoentry/src/cli.php", "<?php\n") }, "more than one file"},
		"undecl":    {func() { write(t, root, "modules/undecl/src/worker.sh", "x\n") }, "entrypoints.worker is false"},
		"locales":   {func() { write(t, root, "modules/locales/locales/pl.json", `{"module.locales.other": "X"}`) }, "missing key"},
		"prefix":    {func() { write(t, root, "modules/prefix/locales/en.json", `{"title": "X"}`) }, "must start with module.prefix."},
		"skill":     {func() { write(t, root, "modules/skill/SKILL.md", "---\nname: x\n---\n") }, "frontmatter name"},
		"noassert":  {func() { os.Remove(filepath.Join(root, "modules/noassert/ASSERTIONS.md")) }, "missing required file: ASSERTIONS.md"},
		"badconfig": {func() { write(t, root, "config/badconfig.toml", "enabled = \"yes\"\n") }, "want boolean, got string"},
		"cfgkey":    {func() { replace(t, root, "cfgkey", `["enabled"]`, `["enabled", "nope"]`) }, `provides.config: "nope"`},
		"ghostdep":  {func() { replace(t, root, "ghostdep", `"depends": []`, `"depends": ["ghost"]`) }, `depends on "ghost", which is not installed`},
	}
	for name, c := range cases {
		fixture(t, root, name, "[]")
		c.edit()
	}
	mods := discover(t, root)
	for name, c := range cases {
		m := byName(mods, name)
		if m == nil {
			t.Errorf("%s: not discovered", name)
			continue
		}
		if m.Valid() || !strings.Contains(strings.Join(m.Problems, "\n"), c.want) {
			t.Errorf("%s: problems %q, want one containing %q", name, m.Problems, c.want)
		}
	}
}

func replace(t *testing.T, root, name, old, repl string) {
	t.Helper()
	p := filepath.Join(root, "modules", name, "module.json")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), old) {
		t.Fatalf("%s: %q not in manifest", name, old)
	}
	if err := os.WriteFile(p, []byte(strings.Replace(string(b), old, repl, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func manager(t *testing.T, root string) (*Manager, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "k.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return &Manager{Modules: discover(t, root), Store: st, Log: events.New(st)}, st
}

func TestLifecycle(t *testing.T) {
	root := t.TempDir()
	fixture(t, root, "base", "[]")
	fixture(t, root, "app", `["base"]`)
	mg, st := manager(t, root)
	ctx := context.Background()

	if _, running, _ := mg.Running(ctx, "base"); running {
		t.Fatal("config default enabled = false must start disabled")
	}
	if _, err := mg.Enable(ctx, "app", "test"); !errors.Is(err, ErrConflict) {
		t.Fatalf("enable app before base: %v", err)
	}
	if r, err := mg.Enable(ctx, "base", "test"); err != nil || !r.Changed || r.Status != "running" {
		t.Fatalf("enable base: %+v %v", r, err)
	}
	if r, err := mg.Enable(ctx, "base", "test"); err != nil || r.Changed {
		t.Fatalf("enable twice must be a no-op: %+v %v", r, err)
	}
	if _, err := mg.Enable(ctx, "app", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := mg.Disable(ctx, "base", "test"); !errors.Is(err, ErrConflict) {
		t.Fatalf("disable base while app needs it: %v", err)
	}
	if r, err := mg.Stop(ctx, "app", "test"); err != nil || r.Status != "stopped" {
		t.Fatalf("stop: %+v %v", r, err)
	}
	if _, running, _ := mg.Running(ctx, "app"); running {
		t.Fatal("a stopped module must not be running")
	}
	if boot, err := mg.Boot(ctx, "test"); err != nil || len(boot) != 1 || boot[0].Module != "app" {
		t.Fatalf("boot restarts what is enabled: %+v %v", boot, err)
	}
	if _, err := mg.Disable(ctx, "app", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := mg.Start(ctx, "app", "test"); !errors.Is(err, ErrConflict) {
		t.Fatalf("start a disabled module: %v", err)
	}
	if _, err := mg.Enable(ctx, "ghost", "test"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("enable unknown: %v", err)
	}

	list, err := st.ListEvents(ctx, events.Query{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range list {
		got = append(got, e.Action+" "+e.Subject)
	}
	want := "core.module.enable base,core.module.enable app,core.module.stop app,core.module.start app,core.module.disable app"
	if strings.Join(got, ",") != want {
		t.Fatalf("events:\n got %s\nwant %s", strings.Join(got, ","), want)
	}
}

func eventsQuery(n int) events.Query { return events.Query{Limit: n} }

func TestInvalidModuleCannotEnable(t *testing.T) {
	root := t.TempDir()
	fixture(t, root, "bad", "[]")
	write(t, root, "modules/bad/module.json", "{")
	mg, st := manager(t, root)
	ctx := context.Background()
	if _, err := mg.Enable(ctx, "bad", "test"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("enable invalid: %v", err)
	}
	if list, _ := st.ListEvents(ctx, events.Query{Limit: 10}); len(list) != 0 {
		t.Fatalf("a rejected call must log nothing: %v", list)
	}
}
