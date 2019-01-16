// vcsloc/loc/run.go

package loc

import (
	"strconv"
	"strings"
	"time"

	"vcsloc/gsos"
	"vcsloc/vcs"
)

// ----------------------------------------------------------------------------------------------

type Commit struct {
	// read from repo
	hash string
	timestamp int
	authorName string
	authorEmail string
	parents []string

	// computed
	children []string
}

// TBD Vcsdb should probably split into two structs, one for just
// the database, and one for the working parameters for Analyze.

// Analyze analyzes the selected repo. This uses persisted data from
// previous runs so that it can be incremental.
func (db *VcsDb) Analyze(startTime time.Time, verbose bool) {
	db.verbose = verbose
	db.startTime = startTime
	db.terminal = gsos.NewThrottleTerminal(100*time.Millisecond)

	// Compare the saved data to the current repo. If anything meaningful
	// changed (number of objects, refs), then we need to scan the repo for new commits.
	db.LoadRefs()

	db.GetRepoInfo()

	db.FetchChangeStats()

	db.FetchDiffs()
}

// ----------------------------------------------------------------------------------------------

// GetRepoInfo loads key bits of information from the repo.
// TBD use temps so that we can compare against persisted data.
func (db *VcsDb) GetRepoInfo() {
	var elapsed float64

	// Check the size of the repo (we may want a progress bar on long repos)
	var numObjects int
	db.terminal.Force().Progressf("Count objects...")
	numObjects, elapsed = vcs.GitCountObjects(db.repoPath)
	if db.numRepoObjects != numObjects {
		db.numRepoObjects = numObjects
		db.numRepoObjectsDirty = true
	}
	db.terminal.Printf("Found %d objects in %.2f sec", numObjects, elapsed)

	// Get all the refs
	var refs []vcs.Ref
	db.terminal.Force().Progressf("Fetch refs...")
	refs, elapsed = vcs.GitRefs(db.repoPath)
	db.refs = refs
	db.refsDirty = true
	db.terminal.Printf("Found %d refs in %.2f sec", len(refs), elapsed)

	// Get all the root commits
	// TBD we can get the root commits from the next step, it's just all the commits without parents
	var roots []string
	db.terminal.Force().Progressf("Fetch root commits...")
	roots, elapsed = vcs.GitRootCommits(db.repoPath)
	db.roots = roots
	db.rootsDirty = true
	db.terminal.Printf("Found %d roots in %.2f sec", len(roots), elapsed)

	// Get all the commits and their parents
	var logs []string
	db.terminal.Force().Progressf("Fetch commits...")
	logs, elapsed = vcs.GitLogAll(db.repoPath, "|Commit| %H |Timestamp| %at |AuthorName| %aN |AuthorEmail| %aE |Parents| %P")
	db.terminal.Printf("Got %d commits in %.2f sec", len(logs), elapsed)

	// Put all the commits in a graph
	db.terminal.Force().Progressf("Make graph...")
	startTime := gsos.HighresTime()

	graph := make(map[string]Commit)
	for _, L := range logs {
		cPos := strings.Index(L, "|Commit| ")
		tPos := strings.Index(L, "|Timestamp| ")
		anPos := strings.Index(L, "|AuthorName| ")
		aePos := strings.Index(L, "|AuthorEmail| ")
		pPos := strings.Index(L, "|Parents| ")
		if cPos == -1  || tPos == -1 || anPos == -1 || aePos == -1 || pPos == -1 {
			db.terminal.Fatalf("Bad log: %s\n", L)
		}
		commitHash := L[cPos+9:tPos-1]
		timestampS := L[tPos+12:anPos-1]
		authorName := L[anPos+13:aePos-1]
		authorEmail := L[aePos+14:pPos-1]
		parentS := strings.TrimSpace(L[pPos+10:])
		var parentHashes []string
		if parentS == "" {
			parentHashes = nil
		} else {
			parentHashes = strings.Split(L[pPos+10:], " ")
		}

		timestamp, err := strconv.Atoi(timestampS)
		if err != nil {
			db.terminal.Fatalf("Bad log (timestamp): %s\n", L)
		}

		commit := Commit{hash: commitHash, timestamp: timestamp, authorName: authorName, authorEmail: authorEmail, parents: parentHashes}
		graph[commitHash] = commit
	}

	if SAVE_RAW_GRAPH {
		// Save raw graph
		rawgraph := make(map[string]Commit)
		for k, v := range graph {
			rawgraph[k] = v
		}
		db.rawgraph = rawgraph
		db.rawgraphDirty = true
	}

	db.terminal.Force().Progressf("Make graph (2)...")

	// graphTips is all the refs; every visible commit can be reached from
	// one of these, and these also are what's "published". We'll use the ref names
	// to decorate output.
	var missingRefs []string
	graphTips := make(map[string]string)
	for _, ref := range refs {

		// Evidently not all the refs point to commits in the repo. Not sure
		// how this is possible.
		refname := ref.Refname
		ref := ref.Hash
		if _, ok := graph[ref]; !ok {
			db.terminal.Printf("%s missing: %s", ref, refname)
			db.terminal.Force().Progressf("Make graph (2)...")
			missingRefs = append(missingRefs, ref)
			continue
		}
		graphTips[ref] = refname
	}
	if len(missingRefs) > 0 {
		db.terminal.Printf("%d refs missing from repo\n", len(missingRefs))
		for _, ref := range missingRefs {
			db.terminal.Printf("    %s\n", ref)
		}
		db.terminal.Force().Progressf("Make graph (2)...")
	}

	// Now visit all the refs one by one, to compute children (we only have parents
	// at the moment)
	var walkRefs []string
	for ref, _ := range graphTips {
		walkRefs = append(walkRefs, ref)
	}

	// Repeat until we've followed every commit to the end
	visited := make(map[string]bool)
	count := 2
	for len(walkRefs) > 0 {
		count += 1
		db.terminal.Progressf("Make graph (%d)...", count)

		hash := walkRefs[0]
		walkRefs = walkRefs[1:]

		// Follow this commit to the end of the parent chain
		for hash != "" {

			// Add hash as children of each parent of hash, but only once
			commit := graph[hash]
			for _, parentHash := range commit.parents {
				if _, ok := graph[parentHash]; !ok {
					db.terminal.Fatalf("wtf %s not in graph?", parentHash)
				}
				parent := graph[parentHash]
				var hasChild bool
				for _, childHash := range parent.children {
					if childHash == hash {
						hasChild = true
					}
				}
				if !hasChild {
					parent.children = append(parent.children, hash)
					graph[parentHash] = parent
				}
			}

			// Now that we've done children, if we already visited this
			// node, we're done
			if visited[hash] {
				break
			}

			visited[hash] = true

			// If there are no parents to follow, we can stop
			if len(commit.parents) == 0 {
				break
			}

			// Now follow parents. If we have more than one parent, push
			// the other parents onto the queue and walk them later
			hash = commit.parents[0]
			for _, parent := range commit.parents[1:] {
				walkRefs = append(walkRefs, parent)
			}
		}
	}

	// Save the parsed graph
	db.graph = graph
	db.graphDirty = true

	// Now go back and examine the graph tips. If any of them have
	// children, they aren't really tips, so trim it down to refs
	// that really are tips
	var tips []string
	for ref, _ := range graphTips {
		commit := graph[ref]
		if len(commit.children) == 0 {
			tips = append(tips, ref)
		}
	}
	db.tips = tips
	db.tipsDirty = true

	elapsed = (gsos.HighresTime() - startTime).Duration().Seconds() // TBD just return HighresTimestamp
	db.terminal.Printf("Make graph: %.2f\n", elapsed)
	db.terminal.Printf("visited %d out of %d commits\n", len(visited), len(graph))

	if len(visited) != len(graph) {
		for hash, _ := range graph {
			if _, ok := visited[hash]; !ok {
				db.terminal.Printf("Didn't visit %s\n", hash)
			}
		}
	}
}
