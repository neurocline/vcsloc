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

	// Write out an initial header. Save paths as full paths.
	db.hdr.repoPath, _ = filepath.Abs(repoPath)
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

// (*VcsBaseInfo).Load reads core vars from database. These are used
// to determine if the database is up-to-date compared to the repo.
func (h *VcsBaseInfo) Load(db *VcsDb2) error {
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

// (*VcsBaseInfo).Save writes core vars to database.
func (h *VcsBaseInfo) Save(db *VcsDb2) error {
	h.dirty = false
	return db.doSaveDataLines(h.name, []string{
		fmt.Sprintf("numRepoObjects=%d\n", h.numRepoObjects),
		fmt.Sprintf("numRepoCommits=%d\n", h.numRepoCommits),
		fmt.Sprintf("refsSignature=%s\n", h.refsSignature),
		fmt.Sprintf("graphUpToDate=%v\n", h.graphUpToDate),
	})
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

// *VcsRefs).Load reads in the refs from the database.
// TBD update vars in db.info
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

// *VcsRefs).Save writes all refs to the database.
func (h *VcsRefs) Save(db *VcsDb2) error {
	h.dirty = false
	return db.doSaveDataN(h.name, len(h.refs), func(i int) string {
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

	hashFile string // name used to store hashes
	commitFiles []string // zero or more files used to store commits

	dirty bool // true if data needs to be written to disk
	name string // filename data is persisted under
	err error // error from the load or save chain
}

func NewVcsCommits() *VcsCommits {
	return &VcsCommits{name: "commits", hashFile: "commits.hashes"}
}

func (h *VcsCommits) Load(db *VcsDb2) error {
	h.dirty = false
	h.err = nil
	return h.LoadBase(db).LoadHashes(db).LoadCommits(db).err
}

func (h *VcsCommits) Save(db *VcsDb2) error {
	h.dirty = false
	h.err = nil
	return h.SaveCommits(db).SaveHashes(db).SaveBase(db).err
}

// (*VcsCommits).LoadBase reads in the commits abstract from the database.
func (h *VcsCommits) LoadBase(db *VcsDb2) *VcsCommits {
	// Load commits metadata
	if h.err == nil {
		h.err = db.doLoadData(h.name, func(line string) error {
			if !getkvstr(line, &h.hashFile, "hashFile=") &&
			   !getkvstrlist(line, &h.commitFiles, "commitFiles=") {
				return fmt.Errorf("invalid VcsCommits")
			}
			return nil
		})
	}
	return h
}

// (*VcsCommits).SaveBase writes the commits abstract to the database.
func (h *VcsCommits) SaveBase(db *VcsDb2) *VcsCommits {
	// Save commits metadata
	if h.err == nil {
		h.err = db.doSaveDataLines(h.name, []string{
			fmt.Sprintf("hashFile=%s\n", h.hashFile),
			fmt.Sprintf("commitFiles=%s\n", strings.Join(h.commitFiles, ", ")),
		})
	}
	return h
}

// (*VcsCommits).LoadHashes reads the commit hashes from the database.
// This is an ordered list, meant to be compared with commit hashes fetched
// from the repo.
func (h *VcsCommits) LoadHashes(db *VcsDb2) *VcsCommits {
	h.hashes = nil
	if h.err == nil {
		h.err = db.doLoadData(h.hashFile, func(line string) error {
			h.hashes = append(h.hashes, vcs.Hash(line))
			return nil
		})
	}
	return h
}

// (*VcsCommits).SaveHashes writes the commit hashes to the database.
func (h *VcsCommits) SaveHashes(db *VcsDb2) *VcsCommits {
	if h.err == nil {
		h.err = db.doSaveDataN(h.hashFile, len(h.hashes), func(i int) string {
			return fmt.Sprintf("%s\n", string(h.hashes[i]))
		})
	}
	return h
}

// (*VcsCommits).LoadCommits reads the commit data from the database.
// This is not ordered, but it is treated as an append-only list.
func (h *VcsCommits) LoadCommits(db *VcsDb2) *VcsCommits {
	h.commits = nil
	for _, file := range h.commitFiles {
		if h.err != nil {
			break
		}
		var i int
		h.err = db.doLoadData(file, func(line string) error {
			var index int
			if getkvint(line, &index, "-- ") {
				i = len(h.commits)
				h.commits = append(h.commits, Commit{})
				if i != index {
					return fmt.Errorf("invalid VcsCommits.commits: saw %d but wanted %d", index, i)
				}
			}
			if !getkvstr(line, &h.commits[i].hash, "hash=") &&
				!getkvint(line, &h.commits[i].timestamp, "timestamp=") &&
				!getkvstr(line, &h.commits[i].authorName, "authorName=") &&
				!getkvstr(line, &h.commits[i].authorEmail, "authorEmail=") &&
				!getkvstrlist(line, &h.commits[i].parents, "parents=") &&
				!getkvstrlist(line, &h.commits[i].children, "children=") {
					return fmt.Errorf("invalid VcsCommits.commits: %d", i)
				}
			return nil
		})
	}
	return h
}

// (*VcsCommits).SaveCommits writes the commit data tp the database.
func (h *VcsCommits) SaveCommits(db *VcsDb2) *VcsCommits {
	if h.err == nil {
		// for now, put it all in one file
		h.commitFiles = []string{h.name+".commits"}
		var sb strings.Builder
		h.err = db.doSaveDataN(h.commitFiles[0], len(h.commits), func(i int) string {
			sb.WriteString(fmt.Sprintf("-- %d\n", i))
			sb.WriteString(fmt.Sprintf("hash=%s\n", string(h.commits[i].hash)))
			sb.WriteString(fmt.Sprintf("timestamp=%d\n", h.commits[i].timestamp))
			sb.WriteString(fmt.Sprintf("authorName=%s\n", h.commits[i].authorName))
			sb.WriteString(fmt.Sprintf("authorEmail=%s\n", h.commits[i].authorEmail))
			sb.WriteString(fmt.Sprintf("parents=%s\n", strings.Join(h.commits[i].parents, " ")))
			sb.WriteString(fmt.Sprintf("children=%s\n", strings.Join(h.commits[i].children, " ")))
			joined := sb.String()
			sb.Reset()
			return joined
		})
	}
	return h
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

func (db *VcsDb2) doSaveDataN(name string, N int, callback func(int) string) error {
	path := filepath.Join(db.dbPath, name)

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	defer w.Flush()

	for i := 0; i < N; i++ {
		_, err = w.WriteString(callback(i))
	}

	return nil
}

func (db *VcsDb2) doSaveDataLines(name string, lines []string) error {
	return db.doSaveDataWorker(name, func(w *bufio.Writer) error {
		for _, line := range lines {
			if _, err := w.WriteString(line); err != nil {
				return err
			}
		}
		return nil
	})
}

func (db *VcsDb2) doSaveDataWorker(name string, worker func(w *bufio.Writer) error) error {
	path := filepath.Join(db.dbPath, name)

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	defer w.Flush()

	return worker(w)
}

// Get the stringlist value of a key=value pair
func getkvstrlist(text string, val *[]string, prefix string) bool {
	n := len(prefix)
	if len(text) < n || text[0:n] != prefix {
		return false
	}
	strlist := text[n:]
	if strlist != "" {
		*val = strings.Split(strlist, ", ")
	} else {
		*val = nil
	}
	return true
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
