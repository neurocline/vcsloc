// vcsloc/loc/run.go

package loc

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vcsloc/gsos"
	"vcsloc/vcs"
)

// ----------------------------------------------------------------------------------------------

// Load opens an vcsloc database, if it exists.
func Load(dbPath string, repoPath string, vcs string) *VcsDb {
	db := &VcsDb{dbPath: dbPath, repoPath: repoPath, vcs: vcs}

	if db.dbPath == "" {
		log.Fatalf("Specify a database path with --db=<path>")
	}

	// Make sure there is no file at this location
	if fInfo, err := os.Stat(db.dbPath); err == nil && !fInfo.IsDir() {
		log.Fatalf("File in the way at '%s'\n", db.dbPath)
	}

	/* load any initial data */

	return db
}

// Save writes out any unsaved data to the vcsloc database.
func (db *VcsDb) Save() {
	// Make sure the directory exists
	if err := os.MkdirAll(db.dbPath, os.ModePerm); err != nil {
		log.Fatalf("Could not create db '%s': %s\n", db.dbPath, err)
	}

	/* save dirty data */
	db.SaveRefs()
}

type VcsDb struct {
	// Path to vcsloc database directory
	dbPath string

	// Path to repo being analyzed
	repoPath string

	// Version control type: "git", "hg", etc
	vcs string

	// numRepoObjects is the number of objects in the repo
	numRepoObjects int

	// refs is the active refs from the repo
	refs []vcs.Ref

	verbose bool
	startTime time.Time
}

func (db *VcsDb) LoadRefs() error {
	db.refs = nil

	path := filepath.Join(db.dbPath, "refs")
	if db.verbose {
		fmt.Printf("Loading %s\n", path)
	}

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	fs := bufio.NewScanner(f)
	for fs.Scan() {
		line := fs.Text()
		hash := line[:40]
		refname := line[41:]
		db.refs = append(db.refs, vcs.Ref{hash, refname})
	}
	return fs.Err()
}

func (db *VcsDb) SaveRefs() error {
	path := filepath.Join(db.dbPath, "refs")
	if db.verbose {
		fmt.Printf("Saving %d refs to %s\n", len(db.refs), path)
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	defer w.Flush()

	for _, ref := range db.refs {
		_, err = w.WriteString(fmt.Sprintf("%s %s\n", ref.Hash, ref.Refname))
		if err != nil {
			return err
		}
	}

	return nil
}

// ----------------------------------------------------------------------------------------------

type Commit struct {
	hash string
	parents []string
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
	fmt.Fprintf(os.Stderr, "elapsed: %.2f\n", elapsed)

	// Get all the refs
	var refs []vcs.Ref
	fmt.Fprintf(os.Stderr, "Fetch refs...")
	refs, elapsed = vcs.GitRefs(db.repoPath)
	if db.verbose {
		fmt.Printf("%d refs\n", len(refs))
		//for i, ref := range refs {
		//	fmt.Printf("%4d: %s = %s\n", i, ref.Refname, ref.Hash)
		//}
	}
	db.refs = refs
	fmt.Fprintf(os.Stderr, "elapsed: %.2f\n", elapsed)

	// Get all the root commits
	// TBD we can get the root commits from the next step, it's just all the commits without parents
	var roots []string
	fmt.Fprintf(os.Stderr, "Fetch root commits...")
	roots, elapsed = vcs.GitRootCommits(db.repoPath)
	if db.verbose {
		fmt.Printf("%d roots\n", len(roots))
		//for i, L := range roots {
		//	fmt.Printf("%4d: %s\n", i, L)
		//}
	}
	fmt.Fprintf(os.Stderr, "elapsed: %.2f\n", elapsed)

	// Get all the commits and their parents
	var logs []string
	fmt.Fprintf(os.Stderr, "Fetch commits...")
	logs, elapsed = vcs.GitLogAll(db.repoPath, "|Commit| %H |Parents| %P")
	if db.verbose {
		fmt.Printf("%d commits\n", len(logs))
		//for i, L := range logs {
		//	pos1 := strings.Index(L, "|Commit| ")
		//	pos2 := strings.Index(L, "|Parents| ")
		//	if pos1 == -1  || pos2 == -1 {
		//		log.Fatalf("Bad log: %s\n", L)
		//	}
		//	commitHash := L[pos1+9:pos2-1]
		//	parentHashes := strings.Split(L[pos2+10:], " ")
		//	fmt.Printf("%4d: commit=%s parents=%s\n", i, commitHash, strings.Join(parentHashes, ", "))
		//}
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
