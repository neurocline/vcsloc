// vcsloc/loc/fetch.go

package loc

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"vcsloc/gsos"
	"vcsloc/vcs"
)

// NonmergeStat is the list of changes for a non-merge commit
type NonmergeStat struct {
	parent string
	changes []Change
}

type Change struct {
	path string
	add int
	remove int
	binary bool

	create bool
	delete bool
	rename bool
	oldPath string
}

// ----------------------------------------------------------------------------------------------

func (db *VcsDb) FetchChangeStats() {
	stats := make(map[string]NonmergeStat)
	seen := make(map[string]bool)

	var walkRefs [][]string
	for _, ref := range db.roots {
		walkRefs = append(walkRefs, []string{ref, ""})
	}

	count := 0
	for len(walkRefs) > 0 {
		hash := walkRefs[0][0]
		parent := walkRefs[0][1]
		walkRefs = walkRefs[1:]
		fmt.Fprintf(os.Stderr, "\rNext in commit chain: %s           \n", hash)

		for hash != "" {
			commit := db.graph[hash]

			// Get info if we haven't seen this hash
			if _, ok := seen[hash]; !ok {
				fmt.Fprintf(os.Stderr, "\r%d: git log...%s", count, hash)
				count += 1
				if len(commit.parents) <= 1 {
					sh := db.GetNonmergeStat(hash, parent)
					stats[hash] = sh
				}
			}
			seen[hash] = true

			// Go to the next child. If there are multiple children, pick the leftmost
			// one and queue the rest
			if len(commit.children) == 0 {
				break
			}

			parent = hash
			hash = commit.children[0]
			for _, h := range commit.children[1:] {
				walkRefs = append(walkRefs, []string{h, parent})
			}
		}
	}

	db.nonmergeStat = stats
}

func (db *VcsDb) GetNonmergeStat(hash, parent string) NonmergeStat {
	commitRange := hash
	if parent != "" {
		commitRange = parent + ".." + hash
	}
	cmd := []string{"log", "--numstat", "--summary", "--pretty=format:%H", commitRange}

	_, stdout, _ := vcs.RunGitCommand(db.repoPath, nil, cmd...)
	text := gsos.BytesToLines(stdout)
	fmt.Printf("\ngit %s", strings.Join(cmd, " "))
	fmt.Printf("%s\n", strings.Join(text, "\n"))

	changes := make(map[string]Change)
	for _, L := range(text[1:]) {

		// If it's a numstat line, it's <add>\t<del>\t<file>
		tokens := strings.Split(L, "\t")
		if len(tokens) == 3 {
			filepath := tokens[2]
			var add, del int
			var isBinary bool
			if tokens[0] == "-" && tokens[1] == "-" {
				isBinary = true
			} else {
				add, _ = strconv.Atoi(tokens[0])
				del, _ = strconv.Atoi(tokens[1])
			}
			// If the filepath has a " => " in the middle of it, it's a rename
			pos := strings.Index(filepath, " => ")
			var oldPath string
			if pos != -1 {
				//fmt.Printf("filepath: '%s', pos: %d\n", filepath, pos)
				oldPath = filepath[pos+4:]
				filepath = filepath[:pos]
				changes[filepath] = Change{path: filepath, add: add, remove: del, binary: isBinary, rename: true, oldPath: oldPath}
			} else {
				// Not a rename, a regular add/remove line
				changes[filepath] = Change{path: filepath, add: add, remove: del, binary: isBinary}
			}

		} else if L[7:13] == " mode " {
			//  examplar line: " create mode 100644 .gitattributes"
			verb := L[0:7]
			access := L[13:20]
			filepath := L[20:]
			if !(access == "100644 " || access == "100755 " || access == "120000 ") {
				log.Fatalf("%s don't understand access '%s' line: '%s'", commitRange, access, L)
			}
			change := changes[filepath]
			if verb == " create" {
				change.create = true
			} else if verb == " delete" {
				change.delete = true
			}
			changes[filepath] = change
		} else if L[0:8] == " rename " {
			//  examplar line: " rename test.sh => t/test.sh (100%)"
			pos1 := 8
			pos2 := strings.Index(L, " => ")
			pos3 := strings.LastIndex(L, "(")
			if pos2 == -1 || pos3 == -1 {
				log.Fatalf("%s don't understand rename: '%s'", commitRange, L)
			}
			oldPath := L[pos1:pos2]
			newPath := L[pos2+4:pos3-1]
			change := changes[newPath]
			change.rename = true
			change.oldPath = oldPath
			changes[newPath] = change
		} else if L[0:13] == " mode change " {
			//  examplar line: " mode change 100644 => 100755 t/t7409-submodule-detached-worktree.sh
			// We don't care
			fmt.Printf("Ignoring %s\n", L)
		} else {
			log.Fatalf("%s don't understand: '%s'", commitRange, L)
		}
	}

	var nchanges []Change
	for _, v := range changes {
		nchanges = append(nchanges, v)
	}
	return NonmergeStat{parent: parent, changes: nchanges}
}

// ----------------------------------------------------------------------------------------------

func (db *VcsDb) FetchDiffs() {
}
