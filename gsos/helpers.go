// vcsloc/gsos/helpers.go

package gsos

import (
	"bufio"
)


// BytesToLines takes a []byte slice and turns it into a sequence
// of lines, splitting on CRLF or LF.
// TBD preserve the iteration ability of bufio.ScanLines
func BytesToLines(data []byte) []string {

	// Turn into lines split on [CR]LF
	var output []string
	for len(data) > 0 {
		adv, token, err := bufio.ScanLines(data, true)
		if err != nil {
			break
		}
		output = append(output, string(token))
		data = data[adv:]
	}

	return output
}
