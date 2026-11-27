package modules

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Szmelc-INC/Kombinat/core/events"
)

// Run statuses.
const (
	StatusOK             = "ok"
	StatusFailed         = "failed"
	StatusTimeout        = "timeout"
	StatusRuntimeMissing = "runtime-missing"
)

// OneShot are the entrypoint kinds a run executes once and waits for.
var OneShot = []string{"cli", "api"}

// Limits of one run.
const (
	MaxArgs       = 64
	MaxArgLen     = 4096
	MaxStdin      = 1 << 20
	MaxStdout     = 1 << 20
	MaxStderr     = 64 << 10
	EventOutLimit = 4 << 10
	// MaxModuleEvents caps the events one run may emit through KOMBINAT_EVENTS.
	MaxModuleEvents = 100
	maxEventLine    = 64 << 10
)

// RunRequest asks for one entrypoint run.
type RunRequest struct {
	Entrypoint string   `json:"entrypoint"`
	Args       []string `json:"args"`
	Stdin      string   `json:"stdin"`
}

// RunResult is what a run produced; the event log keeps a capped copy.
type RunResult struct {
	Module     string `json:"module"`
	Entrypoint string `json:"entrypoint"`
	Lang       string `json:"lang"`
	Status     string `json:"status"`
	ExitCode   int    `json:"exit_code"`
	DurationMS int64  `json:"duration_ms"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	Truncated  bool   `json:"truncated,omitempty"`
	Error      string `json:"error,omitempty"`
	// Events and EventsRejected count the module's own events (KOMBINAT_EVENTS).
	Events         int      `json:"events"`
	EventsRejected []string `json:"events_rejected,omitempty"`
	EventID        int64    `json:"event_id"`
}

// Runner executes module entrypoints.
type Runner struct {
	Manager *Manager
	// CacheDir holds built Go entrypoints.
	CacheDir string
	// DataDir holds one private data directory per module (DataDir/NAME).
	DataDir string
	Timeout time.Duration
	// Path is the PATH a child sees and runtimes are looked up in.
	Path string
}

// ErrBadRequest marks a run request that can never succeed as sent.
var ErrBadRequest = errors.New("bad run request")

// Run executes one entrypoint of a running module, waits for it, and logs
// the result as core.module.run. Only a failure to log is returned as an
// error together with the result; a failing module is a result, not an error.
func (r *Runner) Run(ctx context.Context, name string, req RunRequest, actor string) (RunResult, error) {
	m, running, err := r.Manager.Running(ctx, name)
	if err != nil {
		return RunResult{}, err
	}
	if !running {
		return RunResult{}, fmt.Errorf("%w: %s is not running", ErrNotFound, name)
	}
	if req.Entrypoint == "" {
		req.Entrypoint = "cli"
	}
	if !contains(OneShot, req.Entrypoint) {
		return RunResult{}, fmt.Errorf("%w: entrypoint %q is not one-shot (want %s)", ErrBadRequest, req.Entrypoint, strings.Join(OneShot, " or "))
	}
	ep, ok := m.Entrypoints[req.Entrypoint]
	if !ok {
		return RunResult{}, fmt.Errorf("%w: %s declares no %s entrypoint", ErrBadRequest, name, req.Entrypoint)
	}
	if len(req.Args) > MaxArgs {
		return RunResult{}, fmt.Errorf("%w: more than %d args", ErrBadRequest, MaxArgs)
	}
	for _, a := range req.Args {
		if len(a) > MaxArgLen || strings.ContainsRune(a, 0) {
			return RunResult{}, fmt.Errorf("%w: an arg is longer than %d bytes or holds a NUL", ErrBadRequest, MaxArgLen)
		}
	}
	if len(req.Stdin) > MaxStdin {
		return RunResult{}, fmt.Errorf("%w: stdin larger than %d bytes", ErrBadRequest, MaxStdin)
	}

	res := RunResult{Module: name, Entrypoint: ep.Kind, Lang: ep.Lang, ExitCode: -1}
	data := filepath.Join(r.DataDir, name)
	if err := os.MkdirAll(data, 0o700); err != nil {
		return RunResult{}, fmt.Errorf("module data dir: %w", err)
	}
	evf, err := os.CreateTemp(data, ".events-*.jsonl")
	if err != nil {
		return RunResult{}, fmt.Errorf("module events file: %w", err)
	}
	evf.Close()
	defer os.Remove(evf.Name())
	start := time.Now()
	r.exec(ctx, m, ep, req, []string{"KOMBINAT_DATA=" + data, "KOMBINAT_EVENTS=" + evf.Name()}, &res)
	res.DurationMS = time.Since(start).Milliseconds()
	if err := r.moduleEvents(ctx, name, evf.Name(), actor, &res); err != nil {
		return res, err
	}

	payload := map[string]any{
		"entrypoint":  res.Entrypoint,
		"lang":        res.Lang,
		"status":      res.Status,
		"exit_code":   res.ExitCode,
		"duration_ms": res.DurationMS,
		"args":        len(req.Args),
		"stdout":      capText(res.Stdout, EventOutLimit),
		"stderr":      capText(res.Stderr, EventOutLimit/2),
		"events":      res.Events,
	}
	if len(res.EventsRejected) > 0 {
		payload["events_rejected"] = res.EventsRejected
	}
	if res.Error != "" {
		payload["error"] = res.Error
	}
	summary := fmt.Sprintf("%s %s: %s, exit %d in %d ms", name, ep.Kind, res.Status, res.ExitCode, res.DurationMS)
	e, err := r.Manager.Log.Append(ctx, events.Event{Actor: actor, Action: "core.module.run", Subject: name, Summary: summary, Payload: payload})
	if err != nil {
		return res, err
	}
	res.EventID = e.ID
	return res, nil
}

// exec runs the entrypoint and fills res.
func (r *Runner) exec(ctx context.Context, m *Module, ep Entrypoint, req RunRequest, extraEnv []string, res *RunResult) {
	argv, err := r.command(ctx, m, ep)
	if err != nil {
		res.Status, res.Error = StatusRuntimeMissing, err.Error()
		return
	}
	ctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], append(argv[1:], req.Args...)...)
	cmd.Dir = m.Dir
	cmd.Env = append(r.env(m), extraEnv...)
	cmd.Stdin = strings.NewReader(req.Stdin)
	stdout := &capped{max: MaxStdout}
	stderr := &capped{max: MaxStderr, tail: true}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	isolate(cmd)
	cmd.WaitDelay = 2 * time.Second

	err = cmd.Run()
	res.Stdout, res.Stderr = stdout.String(), stderr.String()
	res.Truncated = stdout.cut || stderr.cut
	var exitErr *exec.ExitError
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		res.Status, res.Error = StatusTimeout, fmt.Sprintf("killed after %s", r.Timeout)
	case err == nil:
		res.Status, res.ExitCode = StatusOK, 0
	case errors.As(err, &exitErr):
		res.Status, res.ExitCode = StatusFailed, exitErr.ExitCode()
	default:
		res.Status, res.Error = StatusFailed, err.Error()
	}
}

// moduleEvent is one line a module writes to KOMBINAT_EVENTS.
type moduleEvent struct {
	Action  string         `json:"action"`
	Subject string         `json:"subject"`
	Summary string         `json:"summary"`
	Payload map[string]any `json:"payload"`
}

// moduleEvents appends the events a run wrote to its events file, one JSON
// object per line, each under the module's own namespace (NAME.*), acting as
// the run's actor. Bad lines are rejected with a reason, never half-applied.
func (r *Runner) moduleEvents(ctx context.Context, name, path, actor string, res *RunResult) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("module events file: %w", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 4096), maxEventLine)
	line := 0
	reject := func(why string) {
		res.EventsRejected = append(res.EventsRejected, fmt.Sprintf("line %d: %s", line, why))
	}
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}
		if res.Events >= MaxModuleEvents {
			reject(fmt.Sprintf("more than %d events in one run", MaxModuleEvents))
			break
		}
		var me moduleEvent
		dec := json.NewDecoder(strings.NewReader(text))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&me); err != nil {
			reject("not one strict JSON event object: " + err.Error())
			continue
		}
		if !strings.HasPrefix(me.Action, name+".") {
			reject(fmt.Sprintf("action %q is outside the %s.* namespace", me.Action, name))
			continue
		}
		if me.Subject == "" {
			me.Subject = name
		}
		_, err := r.Manager.Log.Append(ctx, events.Event{Actor: actor, Action: me.Action, Subject: me.Subject, Summary: me.Summary, Payload: me.Payload})
		if errors.Is(err, events.ErrInvalid) {
			reject(err.Error())
			continue
		}
		if err != nil {
			return err
		}
		res.Events++
	}
	if err := sc.Err(); err != nil {
		line++
		reject("unreadable: " + err.Error())
	}
	return nil
}

// command returns argv for an entrypoint, or an error naming the missing runtime.
func (r *Runner) command(ctx context.Context, m *Module, ep Entrypoint) ([]string, error) {
	path := filepath.Join(m.Dir, filepath.FromSlash(ep.Path))
	switch ep.Lang {
	case "shell":
		sh, err := r.look("sh")
		if err != nil {
			return nil, err
		}
		return []string{sh, path}, nil
	case "php":
		php, err := r.look("php")
		if err != nil {
			return nil, err
		}
		return []string{php, "-f", path, "--"}, nil
	case "go":
		bin, err := r.buildGo(ctx, m, ep)
		if err != nil {
			return nil, err
		}
		return []string{bin}, nil
	}
	return nil, fmt.Errorf("unknown entrypoint language %q", ep.Lang)
}

func (r *Runner) look(prog string) (string, error) {
	for _, dir := range filepath.SplitList(r.Path) {
		if dir == "" {
			continue
		}
		p := filepath.Join(dir, prog)
		if info, err := os.Stat(p); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", fmt.Errorf("%s not found on PATH", prog)
}

// buildGo compiles a Go entrypoint into the cache; Go's build cache keeps
// repeated builds fast.
func (r *Runner) buildGo(ctx context.Context, m *Module, ep Entrypoint) (string, error) {
	goBin, err := r.look("go")
	if err != nil {
		return "", err
	}
	out := filepath.Join(r.CacheDir, m.Name+"-"+ep.Kind)
	pkg := "./" + filepath.ToSlash(filepath.Dir(ep.Path))
	cmd := exec.CommandContext(ctx, goBin, "build", "-o", out, pkg)
	cmd.Dir = m.Dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if b, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("go build %s: %v: %s", pkg, err, capText(string(b), 2048))
	}
	return out, nil
}

var envKeyRe = regexp.MustCompile(`[^A-Z0-9]+`)

// env is the child's whole environment: a minimal base plus the module's
// identity and config. The core's own environment (tokens included) never leaks in.
func (r *Runner) env(m *Module) []string {
	env := []string{"PATH=" + r.Path, "KOMBINAT_MODULE=" + m.Name}
	for _, k := range []string{"HOME", "LANG", "LC_ALL", "TMPDIR", "TZ"} {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	cfg, _ := json.Marshal(m.Config)
	env = append(env, "KOMBINAT_CONFIG="+string(cfg))
	flat := map[string]string{}
	flatten("", m.Config, flat)
	keys := make([]string, 0, len(flat))
	for k := range flat {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		env = append(env, "KOMBINAT_CFG_"+k+"="+flat[k])
	}
	return env
}

// flatten turns config keys into env names (module.name → MODULE_NAME);
// tables recurse, arrays become JSON.
func flatten(prefix string, t map[string]any, out map[string]string) {
	for k, v := range t {
		key := envKeyRe.ReplaceAllString(strings.ToUpper(k), "_")
		if prefix != "" {
			key = prefix + "_" + key
		}
		switch x := v.(type) {
		case map[string]any:
			flatten(key, x, out)
		case string:
			out[key] = x
		default:
			b, _ := json.Marshal(x)
			out[key] = string(b)
		}
	}
}

func capText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// capped keeps at most max bytes: the head, or with tail the last bytes.
type capped struct {
	buf  bytes.Buffer
	max  int
	tail bool
	cut  bool
}

func (c *capped) Write(p []byte) (int, error) {
	n := len(p)
	if c.tail {
		c.buf.Write(p)
		if over := c.buf.Len() - c.max; over > 0 {
			c.cut = true
			rest := append([]byte(nil), c.buf.Bytes()[over:]...)
			c.buf.Reset()
			c.buf.Write(rest)
		}
		return n, nil
	}
	if room := c.max - c.buf.Len(); room < len(p) {
		c.cut = true
		if room > 0 {
			c.buf.Write(p[:room])
		}
		return n, nil
	}
	c.buf.Write(p)
	return n, nil
}

func (c *capped) String() string { return c.buf.String() }
