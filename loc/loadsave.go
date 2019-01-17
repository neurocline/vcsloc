// vcsloc/loc/loadsave.go

package loc

import (
	"bufio"
	"container/heap"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"strconv"
	"time"

	"vcsloc/gsos"
	"vcsloc/vcs"
)

// This code is obsolete and will be removed soon.

const SAVE_RAW_GRAPH = false

// TBD make types for everything, stop using string
// TBD maybe tips should be called heads

// VcsDb is the in-memory representation of the vcsloc database
type VcsDb struct {
	// Path to vcsloc database directory
	dbPath string

	// Path to repo being analyzed
	repoPath string

	// Version control type: "git", "hg", etc
	vcs string

	// numRepoObjects is the number of objects in the repo
	numRepoObjects int
	numRepoObjectsDirty bool

	// refs is the active refs from the repo
	refs []vcs.Ref
	refsDirty bool

	// roots is the root commits from the repo (root commits have no parents)
	roots []string
	rootsDirty bool

	// tips is the endpoints of all commits in the repo (tips have no children)
	tips []string
	tipsDirty bool

	// rawGraph is the commit graph from the repo
	rawgraph map[string]Commit
	rawgraphDirty bool

	// graph is the annotated graph (adds children)
	graph map[string]Commit
	graphDirty bool

	// nonmergeStat contains the summary of changes for each non-merge commit
	// (there are multiple entries for merge commits)
	nonmergeStat map[string]NonmergeStat
	nonmergeStatdirty bool

	verbose bool
	startTime time.Time
	terminal gsos.Terminal
}

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
	db.LoadNumRepoObjects()

	return db
}

// Save writes out any unsaved data to the vcsloc database.
func (db *VcsDb) Save() {
	// Make sure the directory exists
	if err := os.MkdirAll(db.dbPath, os.ModePerm); err != nil {
		log.Fatalf("Could not create db '%s': %s\n", db.dbPath, err)
	}

	if db.numRepoObjectsDirty {
		db.SaveNumRepoObjects()
	}
	if db.refsDirty {
		db.SaveRefs()
	}
	if db.rootsDirty {
		db.SaveRoots()
	}
	if db.tipsDirty {
		db.SaveTips()
	}
	if SAVE_RAW_GRAPH {
		if db.rawgraphDirty {
			db.SaveRawGraph()
		}
	}
	if db.graphDirty {
		db.SaveGraph()
	}
}

// ----------------------------------------------------------------------------------------------

func (db *VcsDb) doLoadData(name string, callback func(line string)) error {
	path := filepath.Join(db.dbPath, name)

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	fs := bufio.NewScanner(f)
	for fs.Scan() {
		callback(fs.Text())
	}
	return fs.Err()
}

func (db *VcsDb) doSaveData(name string, callback func() string) error {
	path := filepath.Join(db.dbPath, name)

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	defer w.Flush()

	line := callback()
	for line != "" {
		_, err = w.WriteString(line)
		if err != nil {
			return err
		}
		line = callback()
	}

	return nil
}

// ----------------------------------------------------------------------------------------------

func (db *VcsDb) LoadNumRepoObjects() error {
	db.numRepoObjectsDirty = true
	path := filepath.Join(db.dbPath, "repoObjects")
	if db.verbose {
		fmt.Printf("Loading %s\n", path)
	}

	lines, err := FileReadLines(path)
	if err == nil {
		db.numRepoObjects, err = strconv.Atoi(lines[0])
		db.numRepoObjectsDirty = false
	}

	return err
}

func (db *VcsDb) SaveNumRepoObjects() error {
	path := filepath.Join(db.dbPath, "repoObjects")
	if db.verbose {
		fmt.Printf("Saving %s\n", path)
	}

	lines := []string{fmt.Sprintf("%d", db.numRepoObjects)}
	err := FileWriteLines(path, lines)
	if err == nil {
		db.numRepoObjectsDirty = false
	}
	return err
}

// ----------------------------------------------------------------------------------------------

func (db *VcsDb) LoadRefs() error {
	db.refs = nil

	fn := func(line string) {
		hash := line[:40]
		refname := line[41:]
		db.refs = append(db.refs, vcs.Ref{vcs.Hash(hash), refname})
	}
	err := db.doLoadData("refs", fn)

	db.refsDirty = false
	return err
}

func (db *VcsDb) SaveRefs() error {
	i := -1
	fn := func() string {
		i++
		if i == len(db.refs) {
			return ""
		}
		return fmt.Sprintf("%s %s\n", db.refs[i].RefHash, db.refs[i].Refname)
	}
	err := db.doSaveData("refs", fn)

	db.refsDirty = false
	return err
}

// ----------------------------------------------------------------------------------------------

func (db *VcsDb) LoadRoots() error {
	db.roots = nil

	fn := func(line string) {
		db.roots = append(db.roots, line)
	}
	err := db.doLoadData("roots", fn)

	db.rootsDirty = false
	return err
}

func (db *VcsDb) SaveRoots() error {
	i := -1
	fn := func() string {
		i++
		if i == len(db.roots) {
			return ""
		}
		return fmt.Sprintf("%s\n", db.roots[i])
	}
	err := db.doSaveData("roots", fn)

	db.rootsDirty = false
	return err
}

// ----------------------------------------------------------------------------------------------

func (db *VcsDb) LoadTips() error {
	db.tips = nil

	fn := func(line string) {
		db.tips = append(db.tips, line)
	}
	err := db.doLoadData("tips", fn)

	db.tipsDirty = false
	return err
}

func (db *VcsDb) SaveTips() error {
	i := -1
	fn := func() string {
		i++
		if i == len(db.tips) {
			return ""
		}
		return fmt.Sprintf("%s\n", db.tips[i])
	}
	err := db.doSaveData("tips", fn)

	db.tipsDirty = false
	return err
}

// ----------------------------------------------------------------------------------------------

func (db *VcsDb) LoadRawGraph() error {
	var err error
	db.rawgraph, err = db.LoadOneGraph("rawgraph")
	if err != nil {
		db.rawgraphDirty = false
	}
	return err
}

func (db *VcsDb) LoadGraph() error {
	var err error
	db.graph, err = db.LoadOneGraph("graph")
	if err != nil {
		db.graphDirty = false
	}
	return err
}

func (db *VcsDb) LoadOneGraph(graphFile string) (map[string]Commit, error) {
	graph := make(map[string]Commit)

	path := filepath.Join(db.dbPath, graphFile)
	if db.verbose {
		fmt.Printf("Loading %s\n", path)
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := bufio.NewScanner(f)
	var fail bool
	var id int
	for i := 0; r.Scan(); i++ {

		// Parse an graph entry - marker, commit hash, timestamp, date, author, subject
		fail = true
		var c Commit
		var parentsS, childrenS, noteS string
		if !getint(r, &id, "-- ") || id != i ||
			!sgetstr(r, &c.hash, "hash=") ||
			!sgetint(r, &c.timestamp, "timestamp=") ||
			!sgetstr(r, &c.authorName, "name=") ||
			!sgetstr(r, &c.authorEmail, "email=") ||
			!sgetstr(r, &noteS, "notes=") ||
			!sgetstr(r, &parentsS, "parents=") ||
			!sgetstr(r, &childrenS, "children=") {
			break
		}
		// we don't keep the notes, they are a save-file artifact
		if parentsS == "" {
			c.parents = nil
		} else {
			c.parents = strings.Split(parentsS, " ")
		}
		if childrenS == "" {
			c.children = nil
		} else {
			c.children = strings.Split(childrenS, " ")
		}

		// Save parsed entry
		fail = false
		graph[c.hash] = c
	}

	if fail {
		log.Fatalf("Bad data in %s at i=%d: %s\n", path, id, r.Text())
	}

	return graph, r.Err()
}

func (db *VcsDb) SaveRawGraph() error {
	err := db.SaveOneGraph("rawgraph", db.rawgraph)
	if err == nil {
		db.rawgraphDirty = false
	}
	return err
}

func (db *VcsDb) SaveGraph() error {
	err := db.SaveOneGraph("graph", db.graph)
	if err == nil {
		db.graphDirty = false
	}
	return err
}

func (db *VcsDb) SaveOneGraph(graphFile string, graph map[string]Commit) error {

	path := filepath.Join(db.dbPath, graphFile)
	if db.verbose {
		fmt.Printf("Saving graph (len %d) to %s\n", len(graph), path)
	}

	// Output the graph in a stable order. For now, that means ordered
	// by author timestamp
	h := &CommitHeap{}
	heap.Init(h)
	for _, e := range graph {
		heap.Push(h, e)
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	defer w.Flush()

	var i int
	//for _, e := range graph {
	for h.Len() > 0 {
		e := heap.Pop(h).(Commit)
		var notes []string
		if len(e.parents) > 1 {
			notes = append(notes, "merge")
		}
		if len(e.children) > 1 {
			notes = append(notes, "branch")
		}
		w.WriteString(fmt.Sprintf("-- %d\n", i))
		w.WriteString(fmt.Sprintf("hash=%s\n", e.hash))
		w.WriteString(fmt.Sprintf("timestamp=%d\n", e.timestamp))
		w.WriteString(fmt.Sprintf("name=%s\n", e.authorName))
		w.WriteString(fmt.Sprintf("email=%s\n", e.authorEmail))
		w.WriteString(fmt.Sprintf("notes=%s\n", strings.Join(notes, ", ")))
		w.WriteString(fmt.Sprintf("parents=%s\n", strings.Join(e.parents, " ")))
		w.WriteString(fmt.Sprintf("children=%s\n", strings.Join(e.children, " ")))
		i++
	}

	db.tipsDirty = false
	return nil
}

type CommitHeap []Commit
func (h CommitHeap) Len() int { return len(h) }
func (h CommitHeap) Less(i, j int) bool {
	return h[i].timestamp < h[j].timestamp
}
func (h CommitHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}
func (h *CommitHeap) Push(x interface{}) {
	*h = append(*h, x.(Commit))
}
func (h *CommitHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0:n-1]
	return x
}

// Advance the scanner and then get a string value
func sgetstr(r *bufio.Scanner, s *string, prefix string) bool {
	if !r.Scan() {
		return false
	}
	return getstr(r, s, prefix)
}

// Advance the scanner and then get an int value
func sgetint(r *bufio.Scanner, i *int, prefix string) bool {
	if !r.Scan() {
		return false
	}
	return getint(r, i, prefix)
}

// Get the string value of a key=value pair
func getstr(r *bufio.Scanner, s *string, prefix string) bool {
	text := r.Text()
	e := len(prefix)
	if len(text) < e || text[0:e] != prefix {
		return false
	}
	*s = text[e:]
	return true
}

// Get the int value of a key=value pair
func getint(r *bufio.Scanner, i *int, prefix string) bool {
	var text string
	if !getstr(r, &text, prefix) {
		return false
	}
	var err error
	*i, err = strconv.Atoi(text)
	if err != nil {
		return false
	}
	return true
}
