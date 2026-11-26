// Package modules is the module loader: it discovers modules/*/module.json,
// validates every module against the Ultimate Module Format, and runs the
// lifecycle (enable, disable, start, stop). An invalid module is skipped with
// its reason; the core and every other module stay up.
package modules

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Szmelc-INC/Kombinat/core/config"
)

// ManifestFile is the manifest every module ships.
const ManifestFile = "module.json"

// Kinds are the entrypoint kinds a manifest declares, in display order.
var Kinds = []string{"api", "cli", "ui", "worker"}

// Languages maps an entrypoint file layout to its runtime, in lookup order:
// src/<kind>.sh (POSIX shell), src/<kind>.php (PHP), src/<kind>/main.go (Go).
var Languages = []struct{ Lang, Pattern string }{
	{"shell", "src/%s.sh"},
	{"php", "src/%s.php"},
	{"go", "src/%s/main.go"},
}

// requiredFiles every module carries besides the manifest (AGENTS.md, Module format).
var requiredFiles = []string{"MODULE.md", "SKILL.md", "ASSERTIONS.md", config.DefaultsFile, "src"}

// Manifest is a module.json.
type Manifest struct {
	Name        string          `json:"name"`
	DisplayName string          `json:"display_name"`
	Version     string          `json:"version"`
	Description string          `json:"description"`
	Depends     []string        `json:"depends"`
	Provides    Provides        `json:"provides"`
	Entrypoints map[string]bool `json:"entrypoints"`
	Locales     []string        `json:"locales"`
}

// Provides lists what a module offers to the mesh.
type Provides struct {
	API          []string `json:"api"`
	Events       []string `json:"events"`
	Capabilities []string `json:"capabilities"`
	Config       []string `json:"config"`
}

// Entrypoint is one resolved entrypoint file.
type Entrypoint struct {
	Kind string `json:"kind"`
	Lang string `json:"lang"`
	Path string `json:"path"` // relative to the module directory
}

// Module is one discovered module directory.
type Module struct {
	Name        string                `json:"name"`
	Dir         string                `json:"-"`
	Manifest    *Manifest             `json:"manifest,omitempty"`
	Entrypoints map[string]Entrypoint `json:"entrypoints,omitempty"`
	Config      config.Table          `json:"-"`
	// Problems is why the module is skipped; empty means valid.
	Problems []string `json:"problems,omitempty"`
}

// Valid reports whether the module passed every check.
func (m *Module) Valid() bool { return len(m.Problems) == 0 }

// Enabled is the config default for the enabled state (`enabled` key).
func (m *Module) Enabled() bool {
	v, _ := m.Config["enabled"].(bool)
	return v
}

func (m *Module) problem(format string, a ...any) {
	m.Problems = append(m.Problems, fmt.Sprintf(format, a...))
}

var (
	semverRe = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)
	localeRe = regexp.MustCompile(`^[a-z]{2}(-[A-Z]{2})?$`)
	itemRe   = regexp.MustCompile(`^[a-z][a-z0-9-]*(\.[a-z0-9_-]+)+$`)
)

// Discover finds and validates every module under the registry's modules.dir,
// sorted by name. Invalid modules are included with their problems.
func Discover(reg *config.Registry) ([]*Module, error) {
	dir := reg.Path(config.String(reg.Core, "modules.dir"))
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("modules: %w", err)
	}
	var out []*Module
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		m := &Module{Name: e.Name(), Dir: filepath.Join(dir, e.Name())}
		for _, p := range reg.ModuleProblems[m.Name] {
			m.problem("%s", p.Error())
		}
		m.Config = reg.Modules[m.Name]
		load(m)
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	checkDepends(out)
	return out, nil
}

// load reads and validates one module directory.
func load(m *Module) {
	data, err := os.ReadFile(filepath.Join(m.Dir, ManifestFile))
	if err != nil {
		m.problem("%s: cannot read: %v", ManifestFile, unwrap(err))
		return
	}
	man, err := decodeManifest(data)
	if err != nil {
		m.problem("%s: %v", ManifestFile, err)
		return
	}
	m.Manifest = man
	checkManifest(m)
	for _, f := range requiredFiles {
		if _, err := os.Stat(filepath.Join(m.Dir, f)); err != nil {
			m.problem("missing required file: %s", f)
		}
	}
	checkSkill(m)
	checkLocales(m)
	checkEntrypoints(m)
}

func unwrap(err error) error {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}

// decodeManifest parses strict JSON: every key present, no unknown key.
func decodeManifest(data []byte) (*Manifest, error) {
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		return nil, fmt.Errorf("invalid JSON: %v", err)
	}
	for _, k := range []string{"name", "display_name", "version", "description", "depends", "provides", "entrypoints", "locales"} {
		if _, ok := keys[k]; !ok {
			return nil, fmt.Errorf("missing key: %s", k)
		}
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var man Manifest
	if err := dec.Decode(&man); err != nil {
		return nil, fmt.Errorf("invalid manifest: %v", err)
	}
	return &man, nil
}

func checkManifest(m *Module) {
	man := m.Manifest
	if man.Name != m.Name {
		m.problem("name %q must equal the directory name %q", man.Name, m.Name)
	}
	if !config.ValidName(man.Name) {
		m.problem("name %q must be kebab-case and not \"core\"", man.Name)
	}
	if strings.TrimSpace(man.DisplayName) == "" || strings.ContainsAny(man.DisplayName, "<>") {
		m.problem("display_name must be filled")
	}
	if strings.TrimSpace(man.Description) == "" || strings.ContainsAny(man.Description, "<>") {
		m.problem("description must be filled")
	}
	if !semverRe.MatchString(man.Version) {
		m.problem("version %q is not SemVer", man.Version)
	}
	for _, d := range man.Depends {
		if !config.ValidName(d) || d == man.Name {
			m.problem("depends: %q is not another module's name", d)
		}
	}
	prefix := man.Name + "."
	for _, list := range []struct {
		field string
		items []string
	}{{"provides.api", man.Provides.API}, {"provides.events", man.Provides.Events}} {
		for _, it := range list.items {
			if !itemRe.MatchString(it) || !strings.HasPrefix(it, prefix) {
				m.problem("%s: %q must be namespaced as %s<name>", list.field, it, prefix)
			}
		}
	}
	for _, key := range man.Provides.Config {
		if _, ok := config.Lookup(m.Config, key); m.Config != nil && !ok {
			m.problem("provides.config: %q is not a key in %s", key, config.DefaultsFile)
		}
	}
	for k := range man.Entrypoints {
		if !contains(Kinds, k) {
			m.problem("entrypoints: unknown kind %q (want %s)", k, strings.Join(Kinds, ", "))
		}
	}
	for _, k := range Kinds {
		if _, ok := man.Entrypoints[k]; !ok {
			m.problem("entrypoints: missing kind %q", k)
		}
	}
	if !contains(man.Locales, "en") || !contains(man.Locales, "pl") {
		m.problem("locales must include en and pl")
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

var skillNameRe = regexp.MustCompile(`(?m)^name:\s*(\S+)\s*$`)

func checkSkill(m *Module) {
	data, err := os.ReadFile(filepath.Join(m.Dir, "SKILL.md"))
	if err != nil {
		return // reported as a missing required file
	}
	if got := skillNameRe.FindSubmatch(data); got == nil || string(got[1]) != m.Name {
		m.problem("SKILL.md: frontmatter name must be %q", m.Name)
	}
}

// checkLocales: every listed locale exists, holds only module.<name>.* string
// keys, and all locales share one key set.
func checkLocales(m *Module) {
	if m.Manifest == nil {
		return
	}
	prefix := "module." + m.Name + "."
	var first string
	var firstKeys map[string]bool
	for _, loc := range m.Manifest.Locales {
		if !localeRe.MatchString(loc) {
			m.problem("locales: %q is not a locale code", loc)
			continue
		}
		file := "locales/" + loc + ".json"
		data, err := os.ReadFile(filepath.Join(m.Dir, file))
		if err != nil {
			m.problem("%s: cannot read: %v", file, unwrap(err))
			continue
		}
		var strs map[string]string
		if err := json.Unmarshal(data, &strs); err != nil {
			m.problem("%s: want a JSON object of strings: %v", file, err)
			continue
		}
		keys := map[string]bool{}
		for k := range strs {
			keys[k] = true
			if !strings.HasPrefix(k, prefix) {
				m.problem("%s: key %q must start with %s", file, k, prefix)
			}
		}
		if firstKeys == nil {
			first, firstKeys = loc, keys
			continue
		}
		for k := range firstKeys {
			if !keys[k] {
				m.problem("%s: missing key %q (present in %s)", file, k, first)
			}
		}
		for k := range keys {
			if !firstKeys[k] {
				m.problem("%s: extra key %q (absent from %s)", file, k, first)
			}
		}
	}
}

// checkEntrypoints resolves every declared entrypoint to exactly one file.
func checkEntrypoints(m *Module) {
	if m.Manifest == nil {
		return
	}
	m.Entrypoints = map[string]Entrypoint{}
	for _, kind := range Kinds {
		var found []Entrypoint
		for _, l := range Languages {
			rel := fmt.Sprintf(l.Pattern, kind)
			if _, err := os.Stat(filepath.Join(m.Dir, rel)); err == nil {
				found = append(found, Entrypoint{Kind: kind, Lang: l.Lang, Path: rel})
			}
		}
		declared := m.Manifest.Entrypoints[kind]
		switch {
		case declared && len(found) == 0:
			m.problem("entrypoints.%s is true but no src/%s.sh, src/%s.php or src/%s/main.go exists", kind, kind, kind, kind)
		case declared && len(found) > 1:
			m.problem("entrypoints.%s: more than one file (%s, %s)", kind, found[0].Path, found[1].Path)
		case !declared && len(found) > 0:
			m.problem("entrypoints.%s is false but %s exists", kind, found[0].Path)
		case declared:
			m.Entrypoints[kind] = found[0]
		}
	}
	// A Go entrypoint builds standalone, from the module's own go.mod.
	for _, ep := range m.Entrypoints {
		if ep.Lang == "go" {
			if _, err := os.Stat(filepath.Join(m.Dir, "go.mod")); err != nil {
				m.problem("Go entrypoint %s needs the module's own go.mod", ep.Path)
			}
			break
		}
	}
}

// checkDepends marks modules whose dependencies are missing or invalid.
func checkDepends(mods []*Module) {
	byName := map[string]*Module{}
	for _, m := range mods {
		byName[m.Name] = m
	}
	for _, m := range mods {
		if m.Manifest == nil {
			continue
		}
		for _, d := range m.Manifest.Depends {
			if dep, ok := byName[d]; !ok {
				m.problem("depends on %q, which is not installed", d)
			} else if dep.Manifest == nil {
				m.problem("depends on %q, which is invalid", d)
			}
		}
	}
}
