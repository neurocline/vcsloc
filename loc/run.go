// vcsloc/loc/run.go

package loc

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"vcsloc/gsos"
	"vcsloc/vcs"
)

func NewAnalyzer(startTime time.Time, verbose bool, db *VcsDb2, ) *Analyzer {
	return &Analyzer{
		startTime: startTime,
		verbose:
		verbose,
		db: db,
		terminal: gsos.NewThrottleTerminal(100*time.Millisecond),
	}
}

type Analyzer struct {
	db *VcsDb2

	verbose bool
	startTime time.Time
	terminal gsos.Terminal
}

func (work *Analyzer) Run() {
	// Make sure our database is up-to-date with the target repo
	// (this can take a while the first time)
	work.UpdateRepo()

	// Courtesy terminate any progress message
	work.terminal.Printf("")
}

// ----------------------------------------------------------------------------------------------

func (work *Analyzer) UpdateRepo() {

	work.terminal.Force().Progressf("Checking repo...")

	// Check the size of the repo (we may want a progress bar on long repos)
	work.db.info.Load(work.db)

	numObjects, _ := vcs.GitCountObjects(work.db.hdr.repoPath)

	// Get all the refs from the repo and compare against our local refs
	work.db.refs.Load(work.db)

	var refs []vcs.Ref
	refs, _ = vcs.GitRefs(work.db.hdr.repoPath)
	var sameRefs bool

	if len(refs) == len(work.db.refs.refs) {
		sameRefs = true
		for i := 0; i < len(refs); i++ {
			if refs[i].RefHash != work.db.refs.refs[i].RefHash {
				sameRefs = false
			}
			if refs[i].Refname != work.db.refs.refs[i].Refname {
				sameRefs = false
			}
		}
	}

	// If we have the same objects and the same refs, we have all
	// the data (this is probably too strong, either is likely sufficient)
	if work.db.info.graphUpToDate && work.db.info.numRepoObjects == numObjects && sameRefs {
		work.terminal.Printf("Database up to date\n")
		return
	}

	// Something didn't match, update our data
	work.terminal.Printf("Got %d/%d objects, %d/%d refs\n",
		work.db.info.numRepoObjects, numObjects, len(work.db.refs.refs), len(refs))

	work.terminal.Printf("Updating repo...\n")

	// We already got the refs and number of objects, so save those first
	work.db.info.numRepoObjects = numObjects
	work.db.info.dirty = true

	work.db.refs.refs = refs
	work.db.refs.dirty = true

	// Now update our commits list. We just get the whole thing, it's faster
	// than trying to do it incrementally.
	work.db.commits.hashes = work.FetchAllCommitHashes()
	work.db.commits.dirty = true
	work.db.info.numRepoCommits = len(work.db.commits.commits)
	work.db.info.graphUpToDate = false // we might have changed commits, re-scan

	// Do incremental save - we'll update the other parts next
	work.db.info.Save(work.db)
	work.db.refs.Save(work.db)
	work.db.commits.Save(work.db)

	// Now see if we need to fetch more raw commits
	work.FetchMissingCommits()

	// Do incremental save
	work.db.info.Save(work.db)
	work.db.refs.Save(work.db)
	work.db.commits.Save(work.db)
}

// FetchAllCommitHashes fetches just the commit hashes. This should run at
// about 100K hashes/second.
func (work *Analyzer) FetchAllCommitHashes() []vcs.Hash {
	var hashes []vcs.Hash
	outCb := func(line string) {
		hashes = append(hashes, vcs.Hash(line))
		if work.terminal.Ready() {
			work.terminal.Progressf("Getting commit hashes (%d)...", len(hashes))
		}
	}

	cmd := []string{"log", "--all", "--pretty=%H"}
	vcs.RunGitCommandIncremental(outCb, nil, work.db.hdr.repoPath, nil, cmd...)
	work.terminal.Printf("Got %d commit hashes\n", len(hashes))

	return hashes
}

// FetchMissingCommits fetches commits that we haven't received yet. This should
// run at about 2000 commits/second without -m, and about 500 commits/sec with -m.
func (work *Analyzer) FetchMissingCommits() {
	var commits []Commit
	var i int
	outCb := func(line string) {
		if strings.HasPrefix(line, "|Commit|") {
			i = len(commits)
			commits = append(commits, Commit{})
		}
		work.ParseCommitLine(line, &commits[i])
		if work.verbose {
			fmt.Printf("%s\n", line)
		}
		if work.terminal.Ready() {
			work.terminal.Progressf("Getting commits (%d)...", len(commits))
		}
	}

	prettyFormat := "--pretty=format:|Commit| %H |Timestamp| %at |AuthorName| %aN |AuthorEmail| %aE |Parents| %P"
	cmd := []string{"log", "-c", "--numstat", "--summary", prettyFormat, "--all"}
	vcs.RunGitCommandIncremental(outCb, nil, work.db.hdr.repoPath, nil, cmd...)

	work.db.commits.commits = commits
	work.db.commits.dirty = true

	work.terminal.Printf("Got %d commits\n", len(commits))
}

// ParseCommitLine reads the commit log (our specific format) and writes
// commit data
func (work *Analyzer) ParseCommitLine(line string, c *Commit) {
	// If this is the first line of a commit, parse out the commit header info
	if cPos := strings.Index(line, "|Commit| "); cPos != -1 {
		cPos := strings.Index(line, "|Commit| ")
		tPos := strings.Index(line, "|Timestamp| ")
		anPos := strings.Index(line, "|AuthorName| ")
		aePos := strings.Index(line, "|AuthorEmail| ")
		pPos := strings.Index(line, "|Parents| ")
		if cPos == -1  || tPos == -1 || anPos == -1 || aePos == -1 || pPos == -1 {
			work.terminal.Fatalf("Bad log: %s\n", line)
		}

		commitHash := line[cPos+9:tPos-1]
		timestampS := line[tPos+12:anPos-1]
		authorName := line[anPos+13:aePos-1]
		authorEmail := line[aePos+14:pPos-1]
		parentS := strings.TrimSpace(line[pPos+10:])
		var parentHashes []string
		if parentS == "" {
			parentHashes = nil
		} else {
			parentHashes = strings.Split(line[pPos+10:], " ")
		}

		timestamp, err := strconv.Atoi(timestampS)
		if err != nil {
			work.terminal.Fatalf("Bad log (timestamp): %s\n", line)
		}

		c.hash = commitHash
		c.timestamp = timestamp
		c.authorName = authorName
		c.authorEmail = authorEmail
		c.parents = parentHashes
		c.children = nil // filled in by graph traversal

		return
	}

	// ignore blank line
	if line == "" {
		return
	}

	// Parse --numstat data
	// If it's a numstat line, it's <add>\t<del>\t<file>
	// (we count on the fact that no random line will have precisely two tabs)
	tokens := strings.Split(line, "\t")
	if len(tokens) == 3 {
		// do work
		return
	}

	// Otherwise, it must be a summary line, which refers back to
	// one of the files we have from numstat
}




// THIS IS DEAD CODE AND WILL BE REMOVED SOON

// ----------------------------------------------------------------------------------------------

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
	db.Save()

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
		ref := string(ref.RefHash)
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
