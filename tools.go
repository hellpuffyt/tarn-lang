package tarn

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ----- tools: typed, capability-gated, traced host functions -----

// A ToolImpl is a host function behind a capability. Tool namespaces map
// to capabilities: `http.*` needs "http", `fs.*` needs "fs", and so on.
type ToolImpl func(args []any) (any, error)

// TraceEntry records one tool call. A trace makes a run replayable
// without the outside world: `--replay` serves recorded results and
// refuses to run if the program asks for something the trace doesn't have.
type TraceEntry struct {
	Seq    int    `json:"seq"`
	Tool   string `json:"tool"`
	Args   []any  `json:"args"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

type ToolHost struct {
	impls   map[string]ToolImpl
	caps    map[string]string // tool → capability
	decls   map[string]*ToolDecl
	Allowed map[string]bool
	Trace   []TraceEntry
	Replay  []TraceEntry
	replayI int
	Seed    int64
	rng     uint64
	Clock   func() time.Time
	mu      sync.Mutex
}

func NewToolHost() *ToolHost {
	h := &ToolHost{impls: map[string]ToolImpl{}, caps: map[string]string{}, decls: map[string]*ToolDecl{}, Allowed: map[string]bool{}, Clock: time.Now}
	h.Register("http.get", "http", toolHTTPGet)
	h.Register("fs.read", "fs", func(a []any) (any, error) {
		b, err := os.ReadFile(a[0].(string))
		return string(b), err
	})
	h.Register("fs.write", "fs", func(a []any) (any, error) {
		return nil, os.WriteFile(a[0].(string), []byte(a[1].(string)), 0o644)
	})
	h.Register("fs.list", "fs", func(a []any) (any, error) {
		es, err := os.ReadDir(a[0].(string))
		if err != nil {
			return nil, err
		}
		l := &List{}
		for _, e := range es {
			l.Items = append(l.Items, e.Name())
		}
		return l, nil
	})
	h.Register("env.get", "env", func(a []any) (any, error) { return os.Getenv(a[0].(string)), nil })
	h.Register("time.now", "time", func(a []any) (any, error) { return h.Clock().UnixMilli(), nil })
	h.Register("rand.int", "rand", func(a []any) (any, error) {
		n := a[0].(int64)
		if n <= 0 {
			return nil, fmt.Errorf("rand.int needs a positive bound")
		}
		return int64(h.next() % uint64(n)), nil
	})
	h.Register("proc.args", "proc", func(a []any) (any, error) {
		l := &List{}
		for _, s := range ProgramArgs {
			l.Items = append(l.Items, s)
		}
		return l, nil
	})
	return h
}

// ProgramArgs is what `proc.args()` returns; set by the CLI.
var ProgramArgs []string

// Register adds (or overrides) a host tool. Tests use it to fake tools.
func (h *ToolHost) Register(name, capability string, impl ToolImpl) {
	h.impls[name] = impl
	h.caps[name] = capability
}

func (h *ToolHost) next() uint64 {
	if h.rng == 0 {
		h.rng = uint64(h.Seed)*0x9E3779B97F4A7C15 + 0x2545F4914F6CDD1D
	}
	h.rng ^= h.rng << 13
	h.rng ^= h.rng >> 7
	h.rng ^= h.rng << 17
	return h.rng
}

func (h *ToolHost) declare(d *ToolDecl) error {
	name := d.NS + "." + d.Name
	if _, ok := h.impls[name]; !ok {
		return fmt.Errorf("no host tool named %q (available: %s)", name, strings.Join(h.names(), ", "))
	}
	h.decls[name] = d
	return nil
}

func (h *ToolHost) names() []string {
	var n []string
	for k := range h.impls {
		n = append(n, k)
	}
	sort.Strings(n)
	return n
}

func (h *ToolHost) lookup(name string) *ToolDecl { return h.decls[name] }

// Call type-checks arguments against the declaration, enforces the
// capability (or serves from the replay), records the trace entry.
func (h *ToolHost) Call(d *ToolDecl, p Pos, args []any) (any, error) {
	name := d.NS + "." + d.Name
	if len(args) != len(d.Params) {
		return nil, errAt(p, "%s expects %d argument(s), got %d", name, len(d.Params), len(args))
	}
	for i, pr := range d.Params {
		if err := checkValue(pr.Type, args[i]); err != nil {
			return nil, errAt(p, "%s argument %q: %v", name, pr.Name, err)
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	seq := len(h.Trace) + 1
	var result any
	var err error
	if h.Replay != nil {
		if h.replayI >= len(h.Replay) {
			return nil, errAt(p, "replay: trace ended but program called %s", name)
		}
		rec := h.Replay[h.replayI]
		h.replayI++
		if rec.Tool != name || !Equal(fromJSON(rec.Args), toList(args)) {
			return nil, errAt(p, "replay: expected call #%d %s(%s), program called %s(%s)", rec.Seq, rec.Tool, Show(fromJSON(rec.Args)), name, Show(toList(args)))
		}
		if rec.Error != "" {
			err = fmt.Errorf("%s", rec.Error)
		} else {
			result = fromJSON(rec.Result)
		}
	} else {
		capability := h.caps[name]
		if !h.Allowed[capability] {
			return nil, errAt(p, "capability %q is required to call %s (run with --allow %s)", capability, name, capability)
		}
		result, err = h.impls[name](args)
	}
	entry := TraceEntry{Seq: seq, Tool: name, Args: toJSON(toList(args)).([]any)}
	if err != nil {
		entry.Error = err.Error()
	} else {
		entry.Result = toJSON(result)
	}
	h.Trace = append(h.Trace, entry)
	if err != nil {
		return nil, errAt(p, "%s: %v", name, err)
	}
	if err := checkValue(d.Ret, result); err != nil {
		return nil, errAt(p, "%s returned the wrong type: %v", name, err)
	}
	return result, nil
}

func toList(args []any) *List { return &List{Items: args} }

// toJSON converts a Tarn value to a plain Go value for encoding/json.
func toJSON(v any) any {
	switch x := v.(type) {
	case *List:
		out := make([]any, len(x.Items))
		for i, it := range x.Items {
			out[i] = toJSON(it)
		}
		return out
	case *Map:
		out := map[string]any{}
		for k, it := range x.M {
			out[k] = toJSON(it)
		}
		return out
	case int64:
		return float64(x)
	}
	return v
}

// fromJSON converts decoded JSON back into Tarn values.
func fromJSON(v any) any {
	switch x := v.(type) {
	case []any:
		l := &List{}
		for _, it := range x {
			l.Items = append(l.Items, fromJSON(it))
		}
		return l
	case map[string]any:
		m := &Map{M: map[string]any{}}
		for k, it := range x {
			m.M[k] = fromJSON(it)
		}
		return m
	case float64:
		if x == float64(int64(x)) {
			return int64(x)
		}
		return x
	}
	return v
}

func toolHTTPGet(a []any) (any, error) {
	url := a[0].(string)
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return nil, fmt.Errorf("http.get: url must start with http:// or https://")
	}
	c := &http.Client{Timeout: 30 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("http.get: %s returned %d", url, resp.StatusCode)
	}
	return string(b), nil
}

// ----- builtins -----

var builtins map[string]*Builtin

func init() {
	builtins = map[string]*Builtin{}
	def := func(name string, fn func(in *Interp, p Pos, a []any) (any, error)) {
		builtins[name] = &Builtin{Name: name, Fn: fn}
	}
	argc := func(p Pos, name string, a []any, n int) error {
		if len(a) != n {
			return errAt(p, "%s expects %d argument(s), got %d", name, n, len(a))
		}
		return nil
	}
	def("print", func(in *Interp, p Pos, a []any) (any, error) {
		parts := make([]string, len(a))
		for i, v := range a {
			parts[i] = Show(v)
		}
		in.Out(strings.Join(parts, " ") + "\n")
		return nil, nil
	})
	def("len", func(in *Interp, p Pos, a []any) (any, error) {
		if err := argc(p, "len", a, 1); err != nil {
			return nil, err
		}
		switch x := a[0].(type) {
		case *List:
			return int64(len(x.Items)), nil
		case *Map:
			return int64(len(x.M)), nil
		case string:
			return int64(len([]rune(x))), nil
		}
		return nil, errAt(p, "len needs a List, Map or Str, got %s", typeName(a[0]))
	})
	def("str", func(in *Interp, p Pos, a []any) (any, error) {
		parts := make([]string, len(a))
		for i, v := range a {
			parts[i] = Show(v)
		}
		return strings.Join(parts, ""), nil
	})
	def("int", func(in *Interp, p Pos, a []any) (any, error) {
		if err := argc(p, "int", a, 1); err != nil {
			return nil, err
		}
		switch x := a[0].(type) {
		case int64:
			return x, nil
		case bool:
			if x {
				return int64(1), nil
			}
			return int64(0), nil
		case string:
			n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
			if err != nil {
				return nil, errAt(p, "int: %q is not an integer", x)
			}
			return n, nil
		}
		return nil, errAt(p, "int cannot convert %s", typeName(a[0]))
	})
	def("type", func(in *Interp, p Pos, a []any) (any, error) {
		if err := argc(p, "type", a, 1); err != nil {
			return nil, err
		}
		return typeName(a[0]), nil
	})
	def("push", func(in *Interp, p Pos, a []any) (any, error) {
		if err := argc(p, "push", a, 2); err != nil {
			return nil, err
		}
		l, ok := a[0].(*List)
		if !ok {
			return nil, errAt(p, "push needs a List")
		}
		l.Items = append(l.Items, a[1])
		return nil, nil
	})
	def("pop", func(in *Interp, p Pos, a []any) (any, error) {
		if err := argc(p, "pop", a, 1); err != nil {
			return nil, err
		}
		l, ok := a[0].(*List)
		if !ok || len(l.Items) == 0 {
			return nil, errAt(p, "pop needs a non-empty List")
		}
		v := l.Items[len(l.Items)-1]
		l.Items = l.Items[:len(l.Items)-1]
		return v, nil
	})
	def("keys", func(in *Interp, p Pos, a []any) (any, error) {
		if err := argc(p, "keys", a, 1); err != nil {
			return nil, err
		}
		m, ok := a[0].(*Map)
		if !ok {
			return nil, errAt(p, "keys needs a Map")
		}
		l := &List{}
		for _, k := range sortedKeys(m) {
			l.Items = append(l.Items, k)
		}
		return l, nil
	})
	def("values", func(in *Interp, p Pos, a []any) (any, error) {
		if err := argc(p, "values", a, 1); err != nil {
			return nil, err
		}
		m, ok := a[0].(*Map)
		if !ok {
			return nil, errAt(p, "values needs a Map")
		}
		l := &List{}
		for _, k := range sortedKeys(m) {
			l.Items = append(l.Items, m.M[k])
		}
		return l, nil
	})
	def("has", func(in *Interp, p Pos, a []any) (any, error) {
		if err := argc(p, "has", a, 2); err != nil {
			return nil, err
		}
		m, ok := a[0].(*Map)
		k, ok2 := a[1].(string)
		if !ok || !ok2 {
			return nil, errAt(p, "has needs a Map and a Str key")
		}
		_, found := m.M[k]
		return found, nil
	})
	def("range", func(in *Interp, p Pos, a []any) (any, error) {
		var lo, hi int64
		switch len(a) {
		case 1:
			hi, _ = a[0].(int64)
		case 2:
			lo, _ = a[0].(int64)
			hi, _ = a[1].(int64)
		default:
			return nil, errAt(p, "range takes 1 or 2 Int arguments")
		}
		if hi-lo > 10_000_000 {
			return nil, errAt(p, "range too large")
		}
		l := &List{}
		for i := lo; i < hi; i++ {
			l.Items = append(l.Items, i)
		}
		return l, nil
	})
	def("join", func(in *Interp, p Pos, a []any) (any, error) {
		if err := argc(p, "join", a, 2); err != nil {
			return nil, err
		}
		l, ok := a[0].(*List)
		sep, ok2 := a[1].(string)
		if !ok || !ok2 {
			return nil, errAt(p, "join needs a List and a Str separator")
		}
		parts := make([]string, len(l.Items))
		for i, it := range l.Items {
			parts[i] = Show(it)
		}
		return strings.Join(parts, sep), nil
	})
	def("split", func(in *Interp, p Pos, a []any) (any, error) {
		if err := argc(p, "split", a, 2); err != nil {
			return nil, err
		}
		l := &List{}
		for _, s := range strings.Split(a[0].(string), a[1].(string)) {
			l.Items = append(l.Items, s)
		}
		return l, nil
	})
	def("contains", func(in *Interp, p Pos, a []any) (any, error) {
		if err := argc(p, "contains", a, 2); err != nil {
			return nil, err
		}
		switch x := a[0].(type) {
		case string:
			s, ok := a[1].(string)
			return ok && strings.Contains(x, s), nil
		case *List:
			for _, it := range x.Items {
				if Equal(it, a[1]) {
					return true, nil
				}
			}
			return false, nil
		}
		return nil, errAt(p, "contains needs a Str or List")
	})
	def("upper", func(in *Interp, p Pos, a []any) (any, error) { return strings.ToUpper(a[0].(string)), nil })
	def("lower", func(in *Interp, p Pos, a []any) (any, error) { return strings.ToLower(a[0].(string)), nil })
	def("trim", func(in *Interp, p Pos, a []any) (any, error) { return strings.TrimSpace(a[0].(string)), nil })
	def("map", func(in *Interp, p Pos, a []any) (any, error) {
		if err := argc(p, "map", a, 2); err != nil {
			return nil, err
		}
		l, ok := a[0].(*List)
		if !ok {
			return nil, errAt(p, "map needs a List and a function")
		}
		out := &List{}
		for _, it := range l.Items {
			v, err := in.call(p, a[1], []any{it})
			if err != nil {
				return nil, err
			}
			out.Items = append(out.Items, v)
		}
		return out, nil
	})
	def("filter", func(in *Interp, p Pos, a []any) (any, error) {
		if err := argc(p, "filter", a, 2); err != nil {
			return nil, err
		}
		l, ok := a[0].(*List)
		if !ok {
			return nil, errAt(p, "filter needs a List and a function")
		}
		out := &List{}
		for _, it := range l.Items {
			v, err := in.call(p, a[1], []any{it})
			if err != nil {
				return nil, err
			}
			if b, ok := v.(bool); ok && b {
				out.Items = append(out.Items, it)
			}
		}
		return out, nil
	})
	def("reduce", func(in *Interp, p Pos, a []any) (any, error) {
		if err := argc(p, "reduce", a, 3); err != nil {
			return nil, err
		}
		l, ok := a[0].(*List)
		if !ok {
			return nil, errAt(p, "reduce needs a List, an initial value and a function")
		}
		acc := a[1]
		for _, it := range l.Items {
			v, err := in.call(p, a[2], []any{acc, it})
			if err != nil {
				return nil, err
			}
			acc = v
		}
		return acc, nil
	})
	def("sort", func(in *Interp, p Pos, a []any) (any, error) {
		if err := argc(p, "sort", a, 1); err != nil {
			return nil, err
		}
		l, ok := a[0].(*List)
		if !ok {
			return nil, errAt(p, "sort needs a List")
		}
		out := &List{Items: append([]any{}, l.Items...)}
		var bad error
		sort.SliceStable(out.Items, func(i, j int) bool {
			switch x := out.Items[i].(type) {
			case int64:
				if y, ok := out.Items[j].(int64); ok {
					return x < y
				}
			case string:
				if y, ok := out.Items[j].(string); ok {
					return x < y
				}
			}
			bad = errAt(p, "sort needs a List of Int or Str")
			return false
		})
		return out, bad
	})
	def("slice", func(in *Interp, p Pos, a []any) (any, error) {
		if err := argc(p, "slice", a, 3); err != nil {
			return nil, err
		}
		lo, _ := a[1].(int64)
		hi, _ := a[2].(int64)
		switch x := a[0].(type) {
		case *List:
			if lo < 0 || hi > int64(len(x.Items)) || lo > hi {
				return nil, errAt(p, "slice bounds out of range")
			}
			return &List{Items: append([]any{}, x.Items[lo:hi]...)}, nil
		case string:
			rs := []rune(x)
			if lo < 0 || hi > int64(len(rs)) || lo > hi {
				return nil, errAt(p, "slice bounds out of range")
			}
			return string(rs[lo:hi]), nil
		}
		return nil, errAt(p, "slice needs a List or Str")
	})
	def("all", func(in *Interp, p Pos, a []any) (any, error) {
		if err := argc(p, "all", a, 1); err != nil {
			return nil, err
		}
		return a[0], nil
	})
	def("assert", func(in *Interp, p Pos, a []any) (any, error) {
		if len(a) < 1 {
			return nil, errAt(p, "assert needs a condition")
		}
		if b, ok := a[0].(bool); !ok || !b {
			msg := "assertion failed"
			if len(a) > 1 {
				msg += ": " + Show(a[1])
			}
			return nil, errAt(p, "%s", msg)
		}
		return nil, nil
	})
	def("error", func(in *Interp, p Pos, a []any) (any, error) {
		return nil, errAt(p, "%s", Show(a[0]))
	})
	def("min", func(in *Interp, p Pos, a []any) (any, error) {
		x, y := a[0].(int64), a[1].(int64)
		if x < y {
			return x, nil
		}
		return y, nil
	})
	def("max", func(in *Interp, p Pos, a []any) (any, error) {
		x, y := a[0].(int64), a[1].(int64)
		if x > y {
			return x, nil
		}
		return y, nil
	})
	def("abs", func(in *Interp, p Pos, a []any) (any, error) {
		x := a[0].(int64)
		if x < 0 {
			return -x, nil
		}
		return x, nil
	})
	def("json", func(in *Interp, p Pos, a []any) (any, error) {
		if err := argc(p, "json", a, 1); err != nil {
			return nil, err
		}
		b, err := json.Marshal(toJSON(a[0]))
		if err != nil {
			return nil, errAt(p, "json: %v", err)
		}
		return string(b), nil
	})
	def("parse_json", func(in *Interp, p Pos, a []any) (any, error) {
		var v any
		if err := json.Unmarshal([]byte(a[0].(string)), &v); err != nil {
			return nil, errAt(p, "parse_json: %v", err)
		}
		return fromJSON(v), nil
	})
	// par_map runs fn over the list in real goroutines; results come back
	// in input order. Tool calls inside are traced in completion order,
	// so a par_map program is deterministic in output but its trace order
	// may vary — replay of such traces requires the same completion order.
	def("par_map", func(in *Interp, p Pos, a []any) (any, error) {
		if err := argc(p, "par_map", a, 2); err != nil {
			return nil, err
		}
		l, ok := a[0].(*List)
		if !ok {
			return nil, errAt(p, "par_map needs a List and a function")
		}
		out := make([]any, len(l.Items))
		errs := make([]error, len(l.Items))
		var wg sync.WaitGroup
		var mu sync.Mutex
		for i, it := range l.Items {
			wg.Add(1)
			go func(i int, it any) {
				defer wg.Done()
				sub := &Interp{Out: func(s string) { mu.Lock(); in.Out(s); mu.Unlock() }, Tools: in.Tools, modules: in.modules, types: in.types, MaxDepth: in.MaxDepth}
				out[i], errs[i] = sub.call(p, a[1], []any{it})
			}(i, it)
		}
		wg.Wait()
		for _, e := range errs {
			if e != nil {
				return nil, e
			}
		}
		return &List{Items: out}, nil
	})
}

// SaveTrace writes the recorded trace as JSON.
func (h *ToolHost) SaveTrace(path string) error {
	b, err := json.MarshalIndent(h.Trace, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// LoadReplay reads a trace to replay from.
func (h *ToolHost) LoadReplay(path string) error {
	b, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return err
	}
	var t []TraceEntry
	if err := json.Unmarshal(b, &t); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	h.Replay = t
	return nil
}
