// Package config is the config registry: the core defaults plus every
// module's config.default.toml, overlaid by the Kombinat root's
// config/<name>.toml (tracked, team) and config/<name>.local.toml
// (gitignored, personal). Overrides are validated against the defaults:
// an unknown key or a value of another type is an error naming file and key,
// and nothing from that file is applied.
package config

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

//go:embed core.default.toml
var coreDefaults []byte

// CoreName is the namespace of the core config; no module may use it.
const CoreName = "core"

// Dir is the directory, relative to the root, that holds config overrides.
const Dir = "config"

// DefaultsFile is the file every module ships with its exhaustive defaults.
const DefaultsFile = "config.default.toml"

// Table is one parsed TOML table.
type Table = map[string]any

var nameRe = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// ValidName reports whether name is a valid kebab-case module name.
func ValidName(name string) bool { return nameRe.MatchString(name) && name != CoreName }

// Error is one config problem, located by file and dotted key.
type Error struct {
	File string
	Key  string
	Msg  string
}

func (e *Error) Error() string {
	if e.Key == "" {
		return e.File + ": " + e.Msg
	}
	return e.File + ": " + e.Key + ": " + e.Msg
}

// Errors is a list of config problems; it is an error when non-empty.
type Errors []*Error

func (es Errors) Error() string {
	lines := make([]string, len(es))
	for i, e := range es {
		lines[i] = e.Error()
	}
	return strings.Join(lines, "\n")
}

// Registry is the merged configuration of one Kombinat root.
type Registry struct {
	Root    string
	Core    Table
	Modules map[string]Table
	// Problems lists module config errors; a module with a problem is left
	// out of Modules, while the core and every other module stay usable.
	Problems Errors
}

// Load reads and validates the core config and every module config under root.
// A core config problem is returned as Errors; module problems land in
// Registry.Problems.
func Load(root string) (*Registry, error) {
	defaults, err := parse("core.default.toml (built in)", coreDefaults)
	if err != nil {
		return nil, err
	}
	core, errs := overlay(root, CoreName, defaults)
	if len(errs) > 0 {
		return nil, errs
	}
	reg := &Registry{Root: root, Core: core, Modules: map[string]Table{}}

	dir := String(core, "modules.dir")
	entries, err := os.ReadDir(reg.Path(dir))
	if errors.Is(err, os.ErrNotExist) {
		return reg, nil
	}
	if err != nil {
		return nil, Errors{{File: dir, Msg: err.Error()}}
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		rel := filepath.ToSlash(filepath.Join(dir, name, DefaultsFile))
		if !ValidName(name) {
			reg.Problems = append(reg.Problems, &Error{File: rel, Msg: "module directory name must be kebab-case and not \"core\""})
			continue
		}
		data, err := os.ReadFile(reg.Path(filepath.Join(dir, name, DefaultsFile)))
		if err != nil {
			reg.Problems = append(reg.Problems, &Error{File: rel, Msg: "cannot read module defaults: " + errMsg(err)})
			continue
		}
		defs, err := parse(rel, data)
		if err != nil {
			reg.Problems = append(reg.Problems, err.(*Error))
			continue
		}
		merged, errs := overlay(root, name, defs)
		if len(errs) > 0 {
			reg.Problems = append(reg.Problems, errs...)
			continue
		}
		reg.Modules[name] = merged
	}
	return reg, nil
}

// Path resolves p against the root unless it is absolute.
func (r *Registry) Path(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(r.Root, p)
}

// overlay applies config/<name>.toml, then config/<name>.local.toml, onto defaults.
func overlay(root, name string, defaults Table) (Table, Errors) {
	merged := defaults
	var errs Errors
	for _, file := range []string{name + ".toml", name + ".local.toml"} {
		rel := Dir + "/" + file
		data, err := os.ReadFile(filepath.Join(root, Dir, file))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			errs = append(errs, &Error{File: rel, Msg: errMsg(err)})
			continue
		}
		over, err := parse(rel, data)
		if err != nil {
			errs = append(errs, err.(*Error))
			continue
		}
		if ve := validate(rel, "", defaults, over); len(ve) > 0 {
			errs = append(errs, ve...)
			continue
		}
		merged = merge(merged, over)
	}
	return merged, errs
}

func errMsg(err error) string {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}

func parse(file string, data []byte) (Table, error) {
	t := Table{}
	if _, err := toml.Decode(string(data), &t); err != nil {
		var pe toml.ParseError
		if errors.As(err, &pe) {
			return nil, &Error{File: file, Key: pe.LastKey, Msg: fmt.Sprintf("line %d: %s", pe.Position.Line, pe.Message)}
		}
		return nil, &Error{File: file, Msg: err.Error()}
	}
	return t, nil
}

// kind names a TOML value type the way errors show it.
func kind(v any) string {
	switch v.(type) {
	case string:
		return "string"
	case int64:
		return "integer"
	case float64:
		return "float"
	case bool:
		return "boolean"
	case time.Time: // offset and local date-times alike
		return "datetime"
	case []map[string]any:
		return "array of tables"
	case []any:
		return "array"
	case map[string]any:
		return "table"
	}
	return fmt.Sprintf("%T", v)
}

func sortedKeys(t Table) []string {
	keys := make([]string, 0, len(t))
	for k := range t {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// validate checks over against the shape and types of def.
func validate(file, prefix string, def, over Table) Errors {
	var errs Errors
	for _, k := range sortedKeys(over) {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		v := over[k]
		d, ok := def[k]
		if !ok {
			errs = append(errs, &Error{File: file, Key: key, Msg: "unknown key"})
			continue
		}
		if dk, vk := kind(d), kind(v); dk != vk {
			errs = append(errs, &Error{File: file, Key: key, Msg: "want " + dk + ", got " + vk})
			continue
		}
		switch dv := d.(type) {
		case map[string]any:
			errs = append(errs, validate(file, key, dv, v.(map[string]any))...)
		case []any:
			if len(dv) == 0 {
				continue
			}
			want := kind(dv[0])
			for i, e := range v.([]any) {
				if got := kind(e); got != want {
					errs = append(errs, &Error{File: file, Key: fmt.Sprintf("%s[%d]", key, i), Msg: "want " + want + ", got " + got})
				}
			}
		case []map[string]any:
			if len(dv) == 0 {
				continue
			}
			for i, e := range v.([]map[string]any) {
				errs = append(errs, validate(file, fmt.Sprintf("%s[%d]", key, i), dv[0], e)...)
			}
		}
	}
	return errs
}

// merge returns dst overlaid by src: tables merge, arrays and scalars replace.
func merge(dst, src Table) Table {
	out := make(Table, len(dst))
	for k, v := range dst {
		out[k] = v
	}
	for k, v := range src {
		if st, ok := v.(map[string]any); ok {
			if dt, ok := out[k].(map[string]any); ok {
				out[k] = merge(dt, st)
				continue
			}
		}
		out[k] = v
	}
	return out
}

// Lookup returns the value at a dotted key.
func Lookup(t Table, dotted string) (any, bool) {
	var cur any = t
	for _, part := range strings.Split(dotted, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = m[part]; !ok {
			return nil, false
		}
	}
	return cur, true
}

// String returns the string at a dotted key; validated keys always exist.
func String(t Table, dotted string) string {
	v, _ := Lookup(t, dotted)
	s, _ := v.(string)
	return s
}

// Int returns the integer at a dotted key; validated keys always exist.
func Int(t Table, dotted string) int64 {
	v, _ := Lookup(t, dotted)
	n, _ := v.(int64)
	return n
}
