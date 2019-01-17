// vcsloc/loc/types.go

package loc

type Commit struct {

	// fetched from repo
	hash string // should be vcs.Hash
	date string
	timestamp int
	authorName string
	authorEmail string
	parents []string // should be []vcs.Hash

	// computed
	children []string // should be []vcs.Hash
}

// NonmergeStat is the list of changes for a non-merge commit
type NonmergeStat struct {
	parent string // should be vcs.Hash
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
