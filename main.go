// vcsloc/main.go
// Copyright 2019 Brian Fitzgerald <neurocline@gmail.com>
//
// Use of this source code is governed by an MIT-style license that can be found in the LICENSE file.
//
// vcs-torture is a version control system torture test

package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"
	"unsafe"

	"vcsloc/gsos"
	"vcsloc/vcs"
)

func main() {
	cmd := &Command{args: os.Args[1:]}
	cmd.StartTime = time.Now()
	cmd.parse().Run()
}

type Commit struct {
	hash string
	parents []string
	children []string
}

func (cmd *Command) Run() {
	var elapsed float64

	// Check the size of the repo (we may want a progress bar on long repos)
	var numObjects int
	fmt.Fprintf(os.Stderr, "Count objects...")
	numObjects, elapsed = vcs.GitCountObjects(cmd.Repo)
	if cmd.Verbose {
		fmt.Printf("%d objects\n", numObjects)
	}
	fmt.Fprintf(os.Stderr, "elapsed: %.2f\n", elapsed)

	// Get all the refs
	var refs [][]string
	fmt.Fprintf(os.Stderr, "Fetch refs...")
	refs, elapsed = vcs.GitRefs(cmd.Repo)
	if cmd.Verbose {
		fmt.Printf("%d refs\n", len(refs))
		for i, L := range refs {
			fmt.Printf("%4d: %s = %s\n", i, L[0], L[1])
		}
	}
	fmt.Fprintf(os.Stderr, "elapsed: %.2f\n", elapsed)

	// Get all the root commits
	// TBD we can get the root commits from the next step, it's just all the commits without parents
	var roots []string
	fmt.Fprintf(os.Stderr, "Fetch root commits...")
	roots, elapsed = vcs.GitRootCommits(cmd.Repo)
	if cmd.Verbose {
		fmt.Printf("%d roots\n", len(roots))
		for i, L := range roots {
			fmt.Printf("%4d: %s\n", i, L)
		}
	}
	fmt.Fprintf(os.Stderr, "elapsed: %.2f\n", elapsed)

	// Get all the commits and their parents
	var logs []string
	fmt.Fprintf(os.Stderr, "Fetch commits...")
	logs, elapsed = vcs.GitLogAll(cmd.Repo, "|Commit| %H |Parents| %P")
	if cmd.Verbose {
		fmt.Printf("%d commits\n", len(logs))
		for i, L := range logs {
			pos1 := strings.Index(L, "|Commit| ")
			pos2 := strings.Index(L, "|Parents| ")
			if pos1 == -1  || pos2 == -1 {
				log.Fatalf("Bad log: %s\n", L)
			}
			commitHash := L[pos1+9:pos2-1]
			parentHashes := strings.Split(L[pos2+10:], " ")
			fmt.Printf("%4d: commit=%s parents=%s\n", i, commitHash, strings.Join(parentHashes, ", "))
		}
	}
	fmt.Fprintf(os.Stderr, "elapsed: %.2f\n", elapsed)

	// Put all the commits in a graph
	fmt.Fprintf(os.Stderr, "Make graph...")
	startTime := gsos.HighresTime()

	graph := make(map[string]Commit)
	for _, L := range logs {
		pos1 := strings.Index(L, "|Commit| ")
		pos2 := strings.Index(L, "|Parents| ")
		if pos1 == -1  || pos2 == -1 {
			log.Fatalf("Bad log: %s\n", L)
		}
		commitHash := L[pos1+9:pos2-1]
		parentHashes := strings.Split(L[pos2+10:], " ")

		commit := Commit{hash: commitHash, parents: parentHashes}
		graph[commitHash] = commit
	}

	fmt.Fprintf(os.Stderr, "\rMake graph (2)...")

	// graphTips is all the refs; every visible commit can be reached from
	// one of these, and these also are what's "published". We'll use the ref names
	// to decorate output.
	var missingRefs int
	graphTips := make(map[string]string)
	for _, L := range refs {

		// Evidently not all the refs point to commits in the repo. Not sure
		// how this is possible.
		refname := L[0]
		ref := L[1]
		if _, ok := graph[ref]; !ok {
			fmt.Fprintf(os.Stderr, "\r%s missing: %s\nMake graph (2)...", ref, refname)
			missingRefs += 1
			continue
		}
		graphTips[ref] = refname
	}
	if missingRefs > 0 {
		fmt.Fprintf(os.Stderr, "\r%d refs missing from repo\n", missingRefs)
		fmt.Fprintf(os.Stderr, "Make graph (2)...")
	}

	// Now visit all the refs one by one, to compute children (we only have parents
	// at the moment)
	linksToFollow := [][]string{}
	for ref, _ := range graphTips {
		linksToFollow = append(linksToFollow, []string{ref, ""})
	}

	// Repeat until we've followed every commit to the end
	visited := make(map[string]bool)
	count := 2
	for len(linksToFollow) > 0 {
		count += 1
		fmt.Fprintf(os.Stderr, "\rMake graph (%d)...", count)

		hash, parentHash := linksToFollow[0][0], linksToFollow[0][1]
		linksToFollow = linksToFollow[1:]

		// Follow this commit to the end of the parent chain
		for hash != "" {
			commit := graph[hash]

			// Add to children of parent
			if parentHash != "" {
				parent := graph[parentHash]
				var hasChild bool
				for _, c := range parent.children {
					if c == hash {
						hasChild = true
					}
				}
				if !hasChild {
					parent.children = append(parent.children, hash)
				}
			}

			// Now that we've done children, if we've already visited
			// this node, we don't need to keep going
			if visited[hash] {
				break
			}
			visited[hash] = true
			if _, ok := graph[hash]; !ok {
				log.Fatalf("\nUnexpected commit hash: %s\n", hash)
			}

			// Now follow parents. If we have more than one parent, push
			// the other parents onto the queue
			parentHash = hash
			if len(commit.parents) == 0 {
				hash = ""
			} else {
				hash = commit.parents[0]
				for _, v := range commit.parents[1:] {
					linksToFollow = append(linksToFollow, []string{v, hash})
				}
			}
		}
	}
	elapsed = (gsos.HighresTime() - startTime).Duration().Seconds() // TBD just return HighresTimestamp
	fmt.Fprintf(os.Stderr, "elapsed: %.2f\n", elapsed)
	fmt.Fprintf(os.Stderr, "visited %d out of %d commits\n", len(visited), len(graph))
}

// ----------------------------------------------------------------------------------------------

type Command struct {
	StartTime time.Time

	// Repo is the path to the repository to analyze
	Repo string

	// Vcs is the Repo type - git, hg, svn
	Vcs string

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
