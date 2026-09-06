// Command tarn runs, checks, formats and explores Tarn programs.
//
//	tarn run    prog.tarn [--allow http,fs,env,time,rand,proc] [--seed N] [--trace out.json] [--replay in.json] [-- args...]
//	tarn check  prog.tarn
//	tarn fmt    prog.tarn [-w]
//	tarn repl
package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/hellpuffyt/tarn-lang"
)

func usage() {
	fmt.Fprintln(os.Stderr, "usage:\n  tarn run    FILE [--allow CAPS] [--seed N] [--trace OUT] [--replay IN] [-- ARGS]\n  tarn check  FILE\n  tarn fmt    FILE [-w]\n  tarn repl")
	os.Exit(2)
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "run":
		os.Exit(run(os.Args[2:]))
	case "check":
		if len(os.Args) < 3 {
			usage()
		}
		os.Exit(check(os.Args[2]))
	case "fmt":
		if len(os.Args) < 3 {
			usage()
		}
		os.Exit(format(os.Args[2], len(os.Args) > 3 && os.Args[3] == "-w"))
	case "repl":
		repl()
	default:
		usage()
	}
}

func run(args []string) int {
	in := tarn.NewInterp()
	var file, trace string
	for i := 0; i < len(args); i++ {
		a := args[i]
		next := func() string {
			i++
			if i >= len(args) {
				fmt.Fprintf(os.Stderr, "%s needs a value\n", a)
				os.Exit(2)
			}
			return args[i]
		}
		switch a {
		case "--allow":
			for _, c := range strings.Split(next(), ",") {
				if c != "" {
					in.Tools.Allowed[c] = true
				}
			}
		case "--seed":
			n, err := strconv.ParseInt(next(), 10, 64)
			if err != nil {
				fmt.Fprintln(os.Stderr, "bad --seed")
				return 2
			}
			in.Tools.Seed = n
		case "--trace":
			trace = next()
		case "--replay":
			if err := in.Tools.LoadReplay(next()); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 2
			}
		case "--":
			tarn.ProgramArgs = args[i+1:]
			i = len(args)
		default:
			if strings.HasPrefix(a, "-") || file != "" {
				usage()
			}
			file = a
		}
	}
	if file == "" {
		usage()
	}
	err := in.RunFile(file)
	if trace != "" {
		if terr := in.Tools.SaveTrace(trace); terr != nil {
			fmt.Fprintln(os.Stderr, terr)
			return 1
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

func check(file string) int {
	src, err := os.ReadFile(file)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	m, err := tarn.Parse(file, string(src))
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	// Imports are checked by loading them for types only.
	in := tarn.NewInterp()
	types, errs := in.CheckModule(m)
	_ = types
	if len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, "error:", e)
		}
		return 1
	}
	fmt.Fprintf(os.Stderr, "%s: ok (%d functions, %d tools)\n", file, len(m.Funcs), len(m.Tools))
	return 0
}

func format(file string, write bool) int {
	src, err := os.ReadFile(file)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	m, err := tarn.Parse(file, string(src))
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	out := tarn.Format(m)
	if write {
		if out == string(src) {
			return 0
		}
		if err := os.WriteFile(file, []byte(out), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "formatted %s\n", file)
		return 0
	}
	fmt.Print(out)
	if out != string(src) {
		return 3 // "would change" — useful for CI
	}
	return 0
}

func repl() {
	fmt.Println("Tarn REPL — statements run as typed; `fn` declarations persist; Ctrl-D exits.")
	in := tarn.NewInterp()
	sc := bufio.NewScanner(os.Stdin)
	var decls []string
	for {
		fmt.Print("tarn> ")
		if !sc.Scan() {
			fmt.Println()
			return
		}
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		// Collect multi-line blocks.
		for strings.Count(line, "{") > strings.Count(line, "}") {
			fmt.Print("  ... ")
			if !sc.Scan() {
				return
			}
			line += "\n" + sc.Text()
		}
		if strings.HasPrefix(line, "fn ") || strings.HasPrefix(line, "tool ") || strings.HasPrefix(line, "import ") {
			decls = append(decls, line)
			prog := strings.Join(decls, "\n")
			if _, err := tarn.Parse("repl", prog); err != nil {
				decls = decls[:len(decls)-1]
				fmt.Println("error:", err)
			}
			continue
		}
		prog := strings.Join(decls, "\n") + "\nfn main() {\n" + line + "\n}\n"
		if !strings.HasPrefix(line, "let ") && !strings.Contains(line, "=") && !strings.HasPrefix(line, "print") {
			prog = strings.Join(decls, "\n") + "\nfn main() {\nprint(" + line + ")\n}\n"
		}
		fresh := tarn.NewInterp()
		fresh.Tools = in.Tools
		if err := fresh.RunSource("repl", prog); err != nil {
			fmt.Println("error:", err)
		}
	}
}
