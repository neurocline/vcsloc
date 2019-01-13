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
)

func main() {
	cmd := &Command{args: os.Args[1:]}
	cmd.StartTime = time.Now()
	cmd.parse().Run()
}

func (cmd *Command) Run() {

}

// ----------------------------------------------------------------------------------------------

type Command struct {
	StartTime time.Time

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

		if true &&
			!parsebool("-v", &cmd.Verbose) &&
			!parsebool("--verbose", &cmd.Verbose) &&
			!parsebool("-h", &cmd.Help) &&
			!parsebool("--help", &cmd.Help) {
			fmt.Printf("unknown option: '%s'\n", arg)
			cmd.Usage(1)
		}
	}

	return cmd
}

// ParseBoolArg auto-creates usage and checks the current arg against a
// specific boolean option.
func (cmd *Command) ParseBoolArg(arg string, opt string, val *bool) bool {
	cmd.MakeBoolUsage(opt, (uintptr)(unsafe.Pointer(val)))

	if arg != opt {
		return false
	}

	*val = true
	return true
}

// Usage shows short command-line usage and then exits.
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
			usage = "         "
		}
		usage = usage + " " + kv
	}

	fmt.Fprintf(os.Stderr, "%s\n", usage)
	os.Exit(fail)
}

// MakeArgUsage synthesizes usage entry for a command-line option.
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
