// vcsloc/main.go
// Copyright 2019 Brian Fitzgerald <neurocline@gmail.com>
//
// Use of this source code is governed by an MIT-style license that can be found in the LICENSE file.
//
// vcs-torture is a version control system torture test

package main

import (
	"fmt"
	"os"
	"strings"
	"time"
	"unsafe"

	"vcsloc/loc"
)

func main() {
	cmd := &Command{args: os.Args[1:]}
	cmd.StartTime = time.Now()
	cmd.parse().Run()
}

func (cmd *Command) Run() {
	db := loc.Load(cmd.Db, cmd.Repo, cmd.Vcs)
	db.Analyze(cmd.StartTime, cmd.Verbose)
	db.Save()
}

// ----------------------------------------------------------------------------------------------

type Command struct {
	StartTime time.Time

	// Repo is the path to the repository to analyze
	Repo string

	// Vcs is the Repo type - git, hg, svn
	Vcs string

	// Db is the location of the database used to save analysis results and temporaries.
	// This is a directory, not a single file.
	Db string

	Help    bool
	Verbose bool

	i int
	args []string

	u *CommandUsage
}

// Command parses a command-line into parsed values. If input can't be parsed,
// a relevant error is shown.
func (cmd *Command) parse() *Command {
	cmd.u = NewCommandUsage()

	// Iterate through arglist by hand, because some argument parsing can consume
	// multiple arguments
	cmd.i = 0
	for cmd.i < len(cmd.args) {
		arg := cmd.args[cmd.i]
		cmd.i += 1

		parsebool := func(opt string, val *bool) bool { return cmd.ParseBoolArg(arg, opt, val) }
		parsestr := func(opt string, val *string, tag string) bool { return cmd.ParseStrArg(arg, opt, val, tag) }

		if true &&
			!parsestr("--repo", &cmd.Repo, "path") &&
			!parsestr("--vcs", &cmd.Vcs, "vcs-name") &&
			!parsestr("--db", &cmd.Db, "path") &&
			!parsebool("-v", &cmd.Verbose) &&
			!parsebool("--verbose", &cmd.Verbose) &&
			!parsebool("-h", &cmd.Help) &&
			!parsebool("--help", &cmd.Help) {
			fmt.Printf("unknown option: '%s'\n", arg)
			cmd.Usage(1)
		}
	}

	if cmd.Help {
		cmd.Usage(0)
	}

	return cmd
}

// ParseBoolArg auto-creates usage and checks the current arg against a
// specific boolean option.
func (cmd *Command) ParseBoolArg(arg string, opt string, val *bool) bool {
	cmd.u.MakeUsage(opt, "bool", uintptr(unsafe.Pointer(val)))

	if arg != opt {
		return false
	}

	*val = true
	return true
}

// ParseStrArg auto-creates usage and checks the current arg against a
// specific string option. Both "--opt=val" and "--opt val" forms are allowed.
func (cmd *Command) ParseStrArg(arg string, opt string, val *string, tag string) bool {
	cmd.u.MakeUsage(opt, tag, uintptr(unsafe.Pointer(val)))

	// If this is of the form --opt=val, then get the value from arg
	optlen := len(opt)
	if len(arg) > optlen && arg[:optlen] == opt && arg[optlen] == '=' {
		*val = arg[optlen+1:]
		return true
	}

	// If this is of the form --opt val, and there are more args, then
	// the next arg is the value
	if arg == opt && cmd.i < len(cmd.args) {
		*val = cmd.args[cmd.i]
		cmd.i += 1
		return true
	}

	return false
}

// Usage shows command-line usage gleaned from the command-line declarations.
func (cmd *Command) Usage(fail int) {
	fmt.Fprintf(os.Stderr, "%s\n", cmd.u.Usage("usage: vcsloc"))
	os.Exit(fail)
}

// ----------------------------------------------------------------------------------------------

type CommandUsage struct {
	usage []CommandOptionUsage
	targets map[uintptr]int
	seen map[string]bool
}

type CommandOptionUsage struct {
	opt []string
	tag string
}

func NewCommandUsage() *CommandUsage {
	u := &CommandUsage{}
	u.seen = make(map[string]bool)
	u.targets = make(map[uintptr]int)
	return u
}

// MakeUsage synthesizes usage entry for a command-line option.
// Aliases of the same logical option are gathered together.
// For bool options, pass tag="bool"
func (u *CommandUsage) MakeUsage(opt string, tag string, pval uintptr) {
	// If we've already seen the opt, then we already did the work
	if _, ok := u.seen[opt]; ok {
		return
	}

	// If we haven't seen the target, add a new usage record
	if _, ok := u.targets[pval]; !ok {
		u.targets[pval] = len(u.usage)
		u.usage = append(u.usage, CommandOptionUsage{})
	}

	// Add this specific option's information to usage
	u.seen[opt] = true
	n := u.targets[pval]
	u.usage[n].opt = append(u.usage[n].opt, opt)
	u.usage[n].tag = tag
}

// Usage shows short command-line usage and then exits.
// It groups aliases together, and shows output in the order
// that the command-line was defined by the programmer.
func (u *CommandUsage) Usage(usagePrompt string) string {
	usageLen := len(usagePrompt)
	var output string
	line := usagePrompt
	for _, v := range u.usage {
		var opt string = strings.Join(v.opt, " | ")
		var kv string
		if v.tag == "bool" {
			kv = fmt.Sprintf("[%s]", opt)
		} else {
			kv = fmt.Sprintf("[%s=<%s>]", opt, v.tag)
		}
		if len(line) + 1 + len(kv) >= 75 {
			output = output + line + "\n"
			line = strings.Repeat(" ", usageLen)
		}
		line = line + " " + kv
	}

	output = output + line
	return output
}
