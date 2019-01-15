// vcs-torture/vcs/git.go

package vcs

import (
	"fmt"
	"strings"
	"strconv"

	"vcsloc/gsos"
)

type Ref struct {
	Hash string
	Refname string
}

// Run a Git command, returning elapsed time and stdout and stderr
func RunGitCommand(repodir string, env []string, cmd ...string) (float64, []byte, []byte) {

	return RunExternal("git", repodir, env, cmd...)
}

// ----------------------------------------------------------------------------------------------

// GitLog does "git log --all --pretty=format:<format>"
func GitLogAll(repodir string, format string) ([]string, float64) {
	prettyFormat := fmt.Sprintf("--pretty=format:%s", format)
	elapsed, stdout, _ := RunGitCommand(repodir, nil, "log", "--all", "--full-history", prettyFormat)
	return gsos.BytesToLines(stdout), elapsed
}

// GitLogNumstat does "git log --numstat --pretty=format:<format> stopHash..startHash"
func GitLogNumstat(repodir string, startHash, stopHash string, format string) ([]string, float64) {
	prettyFormat := fmt.Sprintf("--pretty=format:%s", format)
	commitRange := startHash
	if stopHash != "" {
		commitRange = stopHash + ".." + startHash
	}
	elapsed, stdout, _ := RunGitCommand(repodir, nil, "log", "--numstat", prettyFormat, commitRange)
	return gsos.BytesToLines(stdout), elapsed
}

// GitRootCommits finds the root commits, e.g. commits without parents.
// Every Git repo has at least one root commit, but it can multiple
// (the git repo itself has 9)
func GitRootCommits(repodir string) ([]string, float64) {
	elapsed, stdout, _ := RunGitCommand(repodir, nil, "rev-list", "--max-parents=0", "--all")
	return gsos.BytesToLines(stdout), elapsed
}

// GitRefs collects all the refs from the repo, in pairs of
// ref-name, ref-hash. We use --dereference to make tags show
// their commits, because that's what we really care about.
func GitRefs(repodir string) ([]Ref, float64) {
	elapsed, stdout, _ := RunGitCommand(repodir, nil, "show-ref", "--dereference")

	// Turn output into refnames and hashes, collapsing tag refnames
	// to their pointed-to commits
	var refs []Ref
	refnames := make(map[string]string)
	for _, L := range gsos.BytesToLines(stdout) {
		hash := L[:40]
		refname := L[41:]
		if strings.HasSuffix(refname, "^{}") {
			refname = refname[:len(refname)-3]
		}
		refnames[refname] = hash
	}

	for refname, hash := range refnames {
		refs = append(refs, Ref{hash, refname})
	}

	return refs, elapsed
}

// GitCountObjects returns the number of objects in the repo
// (useful to know if another Git command might take a long time)
func GitCountObjects(repodir string) (int, float64) {
	elapsed, stdout, _ := RunGitCommand(repodir, nil, "count-objects", "-v")
	var numObjects int
	for _, L := range gsos.BytesToLines(stdout) {
		if strings.Index(L, "count: ") == 0 {
			v, _ := strconv.Atoi(L[7:])
			numObjects += v
		} else if strings.Index(L, "in-pack: ") == 0 {
			v, _ := strconv.Atoi(L[9:])
			numObjects += v
		}
	}
	return numObjects, elapsed
}
