package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

const helloDefaults = `
enabled = false
[module]
greeting = "hello"
count = 1
ratio = 0.5
tags = ["a"]
`

func TestDefaultsOnly(t *testing.T) {
	reg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got := String(reg.Core, "modules.dir"); got != "modules" {
		t.Fatalf("modules.dir = %q", got)
	}
	if len(reg.Modules) != 0 || len(reg.Problems) != 0 {
		t.Fatalf("modules %v problems %v", reg.Modules, reg.Problems)
	}
}

func TestOverlayOrder(t *testing.T) {
	root := t.TempDir()
	write(t, root, "modules/hello/config.default.toml", helloDefaults)
	write(t, root, "config/hello.toml", "[module]\ngreeting = \"team\"\ncount = 2\n")
	write(t, root, "config/hello.local.toml", "[module]\ncount = 3\ntags = [\"x\", \"y\"]\n")
	reg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	m := reg.Modules["hello"]
	if String(m, "module.greeting") != "team" || Int(m, "module.count") != 3 {
		t.Fatalf("merged: %v", m)
	}
	if tags, _ := Lookup(m, "module.tags"); len(tags.([]any)) != 2 {
		t.Fatalf("arrays replace, got %v", tags)
	}
	if v, _ := Lookup(m, "enabled"); v != false {
		t.Fatalf("untouched default lost: %v", v)
	}
}

func TestCoreInvalidValueNamesFileAndKey(t *testing.T) {
	root := t.TempDir()
	write(t, root, "config/core.toml", "[modules]\ndir = 5\n")
	_, err := Load(root)
	if err == nil || err.Error() != "config/core.toml: modules.dir: want string, got integer" {
		t.Fatalf("err = %v", err)
	}
}

func TestUnknownKey(t *testing.T) {
	root := t.TempDir()
	write(t, root, "config/core.local.toml", "[modules]\ndirr = \"x\"\n")
	_, err := Load(root)
	if err == nil || err.Error() != "config/core.local.toml: modules.dirr: unknown key" {
		t.Fatalf("err = %v", err)
	}
}

func TestParseErrorNamesLine(t *testing.T) {
	root := t.TempDir()
	write(t, root, "config/core.toml", "[modules]\ndir = \n")
	_, err := Load(root)
	if err == nil || !strings.HasPrefix(err.Error(), "config/core.toml: ") || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("err = %v", err)
	}
}

func TestModuleProblemsLeaveOthersUsable(t *testing.T) {
	root := t.TempDir()
	write(t, root, "modules/hello/config.default.toml", helloDefaults)
	write(t, root, "modules/other/config.default.toml", "enabled = true\n")
	write(t, root, "config/hello.toml", "[module]\nratio = \"high\"\ntags = [1]\n")
	write(t, root, "modules/Bad_Name/config.default.toml", "enabled = true\n")
	write(t, root, "modules/nodefaults/MODULE.md", "# x\n")
	reg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Modules["hello"]; ok {
		t.Error("an invalid module must be left out")
	}
	if _, ok := reg.Modules["other"]; !ok {
		t.Error("a valid module must stay")
	}
	got := reg.Problems.Error()
	for _, want := range []string{
		"config/hello.toml: module.ratio: want float, got string",
		"config/hello.toml: module.tags[0]: want string, got integer",
		"modules/Bad_Name/config.default.toml: module directory name must be kebab-case",
		"modules/nodefaults/config.default.toml: cannot read module defaults",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("problems miss %q:\n%s", want, got)
		}
	}
}

func TestCustomModulesDir(t *testing.T) {
	root := t.TempDir()
	write(t, root, "config/core.toml", "[modules]\ndir = \"mods\"\n")
	write(t, root, "mods/hello/config.default.toml", helloDefaults)
	reg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Modules["hello"]; !ok {
		t.Fatalf("modules.dir override ignored: %v", reg.Modules)
	}
}

func TestValidName(t *testing.T) {
	for name, want := range map[string]bool{"hello": true, "memory-5": true, "a1": true, "core": false, "Hello": false, "-x": false, "x-": false, "a--b": false, "": false} {
		if ValidName(name) != want {
			t.Errorf("ValidName(%q) != %v", name, want)
		}
	}
}
