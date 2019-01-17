// vcsloc/loc/db.go

package loc

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"strconv"

	"vcsloc/vcs"
)

func NewVcsDb2(dbPath string) *VcsDb2 {
	return &VcsDb2{
		dbPath: dbPath,
		hdr: NewVcsHeader(),
		info: NewVcsBaseInfo(),
		refs: NewVcsRefs(),
		commits: NewVcsCommits(),
	}
}

// VcsDb is the in-memory representation of the vcsloc database
type VcsDb2 struct {
	dbPath string // Path to vcsloc database directory

	hdr *VcsHeader
	info *VcsBaseInfo
	refs *VcsRefs
	commits *VcsCommits

	// roots is the root commits from the repo (root commits have no parents)
	roots []vcs.Hash
	rootsDirty bool

	// tips is the endpoints of all commits in the repo (tips have no children)
	tips []vcs.Hash
	tipsDirty bool

	// rawGraph is the commit graph from the repo
	rawgraph map[vcs.Hash]Commit
	rawgraphDirty bool

	// graph is the annotated graph (adds children)
	graph map[vcs.Hash]Commit
	graphDirty bool

	// nonmergeStat contains the summary of changes for each non-merge commit
	// (there are multiple entries for merge commits)
	nonmergeStat map[vcs.Hash]NonmergeStat
	nonmergeStatdirty bool
}

// OpenDb opens an existing vcsloc database or creates a new one.
// If the database exists and repoPath or vcs are non-nil, validate them
// against the database.
func OpenDb(dbPath string, repoPath string, vcs string) *VcsDb2 {
	db := NewVcsDb2(dbPath)

	if db.dbPath == "" {
		log.Fatalf("Specify a database path with --db=<path>")
	}

	// If there is a dir at this location, read header from database.
	// If it's not a valid database, tell the user to point somewhere
	// else or fix the database.
	if fInfo, err := os.Stat(db.dbPath); err == nil && fInfo.IsDir() {
		if err = db.hdr.Load(db); err != nil {
			log.Fatalf("Database at %s is corrupt? %s", db.dbPath, err)
		}
		return db
	}

	// If there is no database here, then create a directory to hold
	// the database
	if vcs == "" {
		log.Fatalf("Specify a version control system with --vcs=<type>")
	}
	if repoPath == "" {
		log.Fatalf("Specify a repository path with --repo=<path>")
	}

	if fInfo, err := os.Stat(db.dbPath); err == nil && !fInfo.IsDir() {
		log.Fatalf("File in the way at '%s'\n", db.dbPath)
	}

	if err := os.MkdirAll(db.dbPath, os.ModePerm); err != nil {
		log.Fatalf("Could not create db '%s': %s\n", db.dbPath, err)
	}

	// Write out an initial header
	db.hdr.repoPath = repoPath
	db.hdr.vcs = vcs
	if err := db.hdr.Save(db); err != nil {
		log.Fatalf("Could not write db hdr: %s\n", err)
	}

	return db
}

// Save saves any dirty database data to disk
func (db *VcsDb2) Save() {
	if db.info.dirty {
		db.info.Save(db)
	}

	if db.refs.dirty {
		db.refs.Save(db)
	}

	if db.commits.dirty {
		db.commits.Save(db)
	}
}

// ----------------------------------------------------------------------------------------------

func NewVcsHeader() *VcsHeader {
	return &VcsHeader{name: ".header"}
}

// VcsHeader is the config information for this database.
type VcsHeader struct {
	repoPath string // Path to repo being analyzed
	vcs string // Version control type: "git", "hg", etc

	name string // filename data is persisted under
}

func (h *VcsHeader) Load(db *VcsDb2) error {

	return db.doLoadDataRequired(h.name, func(line string) error {
		if !getkvstr(line, &h.repoPath, "repoPath=") &&
			!getkvstr(line, &h.vcs, "vcs=") {
				return fmt.Errorf("invalid data in VcsHeader: %s\n", line)
			}
		return nil
	})
}

func (h *VcsHeader) Save(db *VcsDb2) error {
	var lines []string
	lines = append(lines, fmt.Sprintf("repoPath=%s\n", h.repoPath))
	lines = append(lines, fmt.Sprintf("vcs=%s\n", h.vcs))
	return db.doSaveDataLines(h.name, lines)
}

// ----------------------------------------------------------------------------------------------

func NewVcsBaseInfo() *VcsBaseInfo {
	return &VcsBaseInfo{name: ".info"}
}

// VcsBaseInfo can be used as a signature on the repo - if the
// number of objects hasn't changed and the refs match, then
// we can assume we are up-to-date in terms of the repo.
type VcsBaseInfo struct {
	numRepoObjects int // number of objects in the repo
	numRepoCommits int // number of commits in the repo
	refsSignature string // a computed signature on VcsRefs
	graphUpToDate bool // true if the graph has been fully updated

	dirty bool // true if data needs to be written to disk
	name string // filename data is persisted under
}

func (h *VcsBaseInfo) Load(db *VcsDb2) error {
	// Load numRepoObjects
	return db.doLoadData(h.name, func(line string) error {
		if !getkvint(line, &h.numRepoObjects, "numRepoObjects=") &&
			!getkvint(line, &h.numRepoCommits, "numRepoCommits=") &&
			!getkvstr(line, &h.refsSignature, "refsSignature=") &&
			!getkvbool(line, &h.graphUpToDate, "graphUpToDate=") {
			return fmt.Errorf("invalid VcsBaseInfo")
		}
		return nil
	})
}

func (h *VcsBaseInfo) Save(db *VcsDb2) error {
	var lines []string
	lines = append(lines, fmt.Sprintf("numRepoObjects=%d\n", h.numRepoObjects))
	lines = append(lines, fmt.Sprintf("numRepoCommits=%d\n", h.numRepoCommits))
	lines = append(lines, fmt.Sprintf("refsSignature=%s\n", h.refsSignature))
	lines = append(lines, fmt.Sprintf("graphUpToDate=%v\n", h.graphUpToDate))
	h.dirty = false
	return db.doSaveDataLines(h.name, lines)
}

// ----------------------------------------------------------------------------------------------

// VcsRefs is the refs (heads: branches, tags, etc) from the repo.
// A signature of this is stored in VcsBasicInfo.
type VcsRefs struct {
	refs []vcs.Ref

	dirty bool // true if data needs to be written to disk
	name string // filename data is persisted under
}

func NewVcsRefs() *VcsRefs {
	return &VcsRefs{name: "refs"}
}

func (h *VcsRefs) Load(db *VcsDb2) error {
	h.refs = nil
	h.dirty = false

	return db.doLoadData(h.name, func(line string) error {
		hash := line[:40]
		refname := line[41:]
		h.refs = append(h.refs, vcs.Ref{vcs.Hash(hash), refname})
		return nil
	})
}

func (h *VcsRefs) Save(db *VcsDb2) error {
	h.dirty = false
	i := -1
	return db.doSaveData(h.name, func() string {
		i++
		if i == len(h.refs) {
			return ""
		}
		return fmt.Sprintf("%s %s\n", string(h.refs[i].RefHash), h.refs[i].Refname)
	})
}

// ----------------------------------------------------------------------------------------------

// VcsCommits is the commits from the repo, in "log --all" order.
// c.hashes is just the commit hashes, and c.commits is the commit data;
// both are in the same order, and c.hashes is just a convenience.
// A signature of c.hashes is stored in VcsBasicInfo.
type VcsCommits struct {
	hashes []vcs.Hash
	commits []Commit

	dirty bool // true if data needs to be written to disk
	name string // filename data is persisted under
}

func NewVcsCommits() *VcsCommits {
	return &VcsCommits{name: "commits"}
}

func (h *VcsCommits) Load(db *VcsDb2) error {
	h.hashes = nil
	h.commits = nil
	h.dirty = false

	var modeStr string
	mode := -1
	i := 0
	return db.doLoadData(h.name, func(line string) error {
		if getkvstr(line, &modeStr, "===== hashes: ") {
			mode = 0
		} else if getkvstr(line, &modeStr, "===== commits: ") {
			mode = 1
		} else if mode == 0 {
			h.hashes = append(h.hashes, vcs.Hash(line))
		} else if mode == 1 {
			var id int
			var multival string
			if getkvint(line, &id, "-- ") {
				i := len(h.commits)
				if i != id {
					return fmt.Errorf("bad commit id %d (expected %d)", id, i)
				}
				h.commits = append(h.commits, Commit{})
			} else if !getkvstr(line, &multival, "parents=") {
				if multival != "" {
					h.commits[i].parents = strings.Split(multival, " ")
				}
			} else if !getkvstr(line, &multival, "children=") {
				if multival != "" {
					h.commits[i].children = strings.Split(multival, " ")
				}
			} else if !getkvstr(line, &h.commits[i].hash, "hash=") &&
				!getkvint(line, &h.commits[i].timestamp, "timestamp=") &&
				!getkvstr(line, &h.commits[i].date, "date=") &&
				!getkvstr(line, &h.commits[i].authorName, "authorName=") &&
				!getkvstr(line, &h.commits[i].authorEmail, "authorEmail=") {
				return fmt.Errorf("bad commit %d", i)
			}
		}
		return nil
	})
}

func (h *VcsCommits) Save(db *VcsDb2) error {
	h.dirty = false
	i := 0
	mode := 0
	return db.doSaveData(h.name, func() string {
		i++
		if mode == 0 {
			mode = 1
			i = -1
			return fmt.Sprintf("===== hashes: %s\n", len(h.hashes))
		}
		if mode == 1 && i < len(h.hashes) {
			return fmt.Sprintf("%s\n", string(h.hashes[i]))
		} else if mode == 1 && i == len(h.hashes) {
			mode = 2
			i = -1
			return fmt.Sprintf("===== commits: %s\n", len(h.commits))
		} else if mode == 2 && i < len(h.commits) {
			return fmt.Sprintf("-- %d\nhash=%s\ntimestamp=%d\nauthorName=%s\nauthorEmail=%s\nparents=%s\nchildren=%s\n",
				i, string(h.commits[i].hash), h.commits[i].timestamp,
				h.commits[i].authorName, h.commits[i].authorEmail,
				strings.Join(h.commits[i].parents, " "), strings.Join(h.commits[i].children, " "))
		} else {
			return ""
		}
	})
}

// ----------------------------------------------------------------------------------------------

func (db *VcsDb2) doLoadDataRequired(name string, callback func(line string) error) error {
	path := filepath.Join(db.dbPath, name)
	if _, err := os.Stat(path); err != nil {
		return err
	}
	return db.doLoadData(name, callback)
}

func (db *VcsDb2) doLoadData(name string, callback func(line string) error) error {
	path := filepath.Join(db.dbPath, name)

	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	fs := bufio.NewScanner(f)
	for fs.Scan() {
		if err := callback(fs.Text()); err != nil {
			return err
		}
	}
	return fs.Err()
}

func (db *VcsDb2) doSaveData(name string, callback func() string) error {
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

func (db *VcsDb2) doLoadDataLines(name string) ([]string, error) {
	path := filepath.Join(db.dbPath, name)

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	fs := bufio.NewScanner(f)
	for fs.Scan() {
		lines = append(lines, fs.Text())
	}
	return lines, fs.Err()
}

func (db *VcsDb2) doSaveDataLines(name string, lines []string) error {
	path := filepath.Join(db.dbPath, name)

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	defer w.Flush()

	for _, line := range lines {
		_, err = w.WriteString(line)
		if err != nil {
			return err
		}
	}

	return nil
}

// Get the string value of a key=value pair
func getkvstr(text string, val *string, prefix string) bool {
	n := len(prefix)
	if len(text) < n || text[0:n] != prefix {
		return false
	}
	*val = text[n:]
	return true
}

// Get the int value of a key=value pair
func getkvint(text string, val *int, prefix string) bool {
	var numStr string
	if !getkvstr(text, &numStr, prefix) {
		return false
	}
	var err error
	*val, err = strconv.Atoi(numStr)
	return err == nil
}

// Get the bool value of a key=value pair
func getkvbool(text string, val *bool, prefix string) bool {
	var boolStr string
	if !getkvstr(text, &boolStr, prefix) {
		return false
	}
	if boolStr == "true" {
		*val = true
	} else if boolStr == "false" {
		*val = false
	} else {
		return false
	}
	return true
}

// ----------------------------------------------------------------------------------------------

func FileWriteLines(path string, lines []string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	defer w.Flush()

	for _, L := range lines {
		_, err = w.WriteString(fmt.Sprintf("%s\n", L))
		if err != nil {
			return err
		}
	}

	return nil
}

// FileReadLines returns data from a file as individual lines of text
func FileReadLines(path string) (lines []string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	fs := bufio.NewScanner(f)
	for fs.Scan() {
		lines = append(lines, fs.Text())
	}
	err = fs.Err()
	return
}
