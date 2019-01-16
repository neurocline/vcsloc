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
	usage map[string]string
	targets map[uintptr]string
}

func (cmd *Command) parse() *Command {
	cmd.usage = make(map[string]string)
	cmd.targets = make(map[uintptr]string)

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
	cmd.MakeBoolUsage(opt, uintptr(unsafe.Pointer(val)))

	if arg != opt {
		return false
	}

	*val = true
	return true
}

func (cmd *Command) ParseStrArg(arg string, opt string, val *string, tag string) bool {
	cmd.MakeStrUsage(opt, tag)

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

// Usage shows short command-line usage and then exits.
// TBD put this into some sort of order, either alphabetical
// or by declaration in code
func (cmd *Command) Usage(fail int) {
	usage := "usage: vcsloc"
	for k, v := range cmd.usage {
		var kv string
		if v == "bool" {
			kv = fmt.Sprintf("[%s]", k)
		} else {
			kv = fmt.Sprintf("[%s=<%s>]", k, v)
		}
		if len(usage) + 1 + len(kv) >= 75 {
			fmt.Fprintf(os.Stderr, "%s\n", usage)
			usage = "             "
		}
		usage = usage + " " + kv
	}

	fmt.Fprintf(os.Stderr, "%s\n", usage)
	os.Exit(fail)
}

// MakeBoolUsage synthesizes usage entry for a boolean command-line option.
// Aliases of the same logical option are gathered together.
func (cmd *Command) MakeBoolUsage(opt string, pval uintptr) {

	// Do we already have an option sharing the value
	if optAlias, ok := cmd.targets[pval]; ok {
		// Yes, so create a new synthetic option with the new alias
		// replacing the old one
		delete(cmd.usage, optAlias)
		optAlias = optAlias + " | " + opt
		cmd.usage[optAlias] = "bool"
		cmd.targets[pval] = optAlias

	} else if _, ok := cmd.usage[opt]; !ok {
		// Otherwise, add it if it doesn't already exist
		cmd.usage[opt] = "bool"
		cmd.targets[pval] = opt
	}
}

// MakeStrUsage synthesizes usage entry for a string command-line option.
// We assume string args have no alias
func (cmd *Command) MakeStrUsage(opt string, tag string) {
	if _, ok := cmd.usage[opt]; !ok {
		cmd.usage[opt] = tag
	}
}
