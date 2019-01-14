// vcsloc/loc/run.go

package loc

import (
	"fmt"
	"log"
	"os"
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

func (db *VcsDb) Analyze(startTime time.Time, verbose bool) {
	db.verbose = verbose
	db.startTime = startTime

	// Compare the saved data to the current repo. If anything meaningful
	// changed (number of objects, refs), then we need to scan the repo for new commits.
	db.LoadRefs()

	db.GetRepoInfo()
}

func (db *VcsDb) GetRepoInfo() {
	var elapsed float64

	// Check the size of the repo (we may want a progress bar on long repos)
	var numObjects int
	fmt.Fprintf(os.Stderr, "Count objects...")
	numObjects, elapsed = vcs.GitCountObjects(db.repoPath)
	if db.verbose {
		fmt.Printf("%d objects\n", numObjects)
	}
	if db.numRepoObjects != numObjects {
		db.numRepoObjects = numObjects
		db.numRepoObjectsDirty = true
	}
	fmt.Fprintf(os.Stderr, "elapsed: %.2f\n", elapsed)

	// Get all the refs
	var refs []vcs.Ref
	fmt.Fprintf(os.Stderr, "Fetch refs...")
	refs, elapsed = vcs.GitRefs(db.repoPath)
	if db.verbose {
		fmt.Printf("%d refs\n", len(refs))
	}
	db.refs = refs
	db.refsDirty = true
	fmt.Fprintf(os.Stderr, "elapsed: %.2f\n", elapsed)

	// Get all the root commits
	// TBD we can get the root commits from the next step, it's just all the commits without parents
	var roots []string
	fmt.Fprintf(os.Stderr, "Fetch root commits...")
	roots, elapsed = vcs.GitRootCommits(db.repoPath)
	if db.verbose {
		fmt.Printf("%d roots\n", len(roots))
	}
	db.roots = roots
	db.rootsDirty = true
	fmt.Fprintf(os.Stderr, "elapsed: %.2f\n", elapsed)

	// Get all the commits and their parents
	var logs []string
	fmt.Fprintf(os.Stderr, "Fetch commits...")
	logs, elapsed = vcs.GitLogAll(db.repoPath, "|Commit| %H |Timestamp| %at |AuthorName| %aN |AuthorEmail| %aE |Parents| %P")
	if db.verbose {
		fmt.Printf("%d commits\n", len(logs))
	}
	fmt.Fprintf(os.Stderr, "elapsed: %.2f\n", elapsed)

	// Put all the commits in a graph
	fmt.Fprintf(os.Stderr, "Make graph...")
	startTime := gsos.HighresTime()

	graph := make(map[string]Commit)
	for _, L := range logs {
		cPos := strings.Index(L, "|Commit| ")
		tPos := strings.Index(L, "|Timestamp| ")
		anPos := strings.Index(L, "|AuthorName| ")
		aePos := strings.Index(L, "|AuthorEmail| ")
		pPos := strings.Index(L, "|Parents| ")
		if cPos == -1  || tPos == -1 || anPos == -1 || aePos == -1 || pPos == -1 {
			log.Fatalf("Bad log: %s\n", L)
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
			log.Fatalf("Bad log (timestamp): %s\n", L)
		}

		commit := Commit{hash: commitHash, timestamp: timestamp, authorName: authorName, authorEmail: authorEmail, parents: parentHashes}
		graph[commitHash] = commit
	}

	// Save raw graph
	rawgraph := make(map[string]Commit)
	for k, v := range graph {
		rawgraph[k] = v
	}
	db.rawgraph = rawgraph
	db.rawgraphDirty = true

	fmt.Fprintf(os.Stderr, "\rMake graph (2)...")

	// graphTips is all the refs; every visible commit can be reached from
	// one of these, and these also are what's "published". We'll use the ref names
	// to decorate output.
	var missingRefs int
	graphTips := make(map[string]string)
	for _, ref := range refs {

		// Evidently not all the refs point to commits in the repo. Not sure
		// how this is possible.
		refname := ref.Refname
		ref := ref.Hash
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
	var walkRefs []string
	for ref, _ := range graphTips {
		walkRefs = append(walkRefs, ref)
	}

	// Repeat until we've followed every commit to the end
	visited := make(map[string]bool)
	count := 2
	for len(walkRefs) > 0 {
		count += 1
		fmt.Fprintf(os.Stderr, "\rMake graph (%d)...", count)

		hash := walkRefs[0]
		walkRefs = walkRefs[1:]

		// Follow this commit to the end of the parent chain
		for hash != "" {

			// Add hash as children of each parent of hash, but only once
			commit := graph[hash]
			for _, parentHash := range commit.parents {
				if _, ok := graph[parentHash]; !ok {
					log.Fatalf("wtf %s not in graph?", parentHash)
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

			// Now that we've done children, if we've already visited
			// this node, or there are no parents to follow, we can stop
			if visited[hash] || len(commit.parents) == 0 {
				break
			}
			visited[hash] = true

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
	fmt.Fprintf(os.Stderr, "elapsed: %.2f\n", elapsed)
	fmt.Fprintf(os.Stderr, "visited %d out of %d commits\n", len(visited), len(graph))
}
