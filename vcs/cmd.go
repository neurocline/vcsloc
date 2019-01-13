// vcs-torture/vcs/cmd.go

package vcs

import (
	"bytes"
	"log"
	"os"
	"os/exec"
	"strings"

	"vcsloc/gsos"
)

// RunExternal runs an external command, returning elapsed time in seconds, stdout and stderr.
// This is a non-interactive version and is best used for commands that should
// finish quickly (e.g in under 1 second). For interactive use or for feeding
// commands stdin, use operateExternal
func RunExternal(exe string, workingDir string, env []string, params ...string) (float64, []byte, []byte) {

	// Do one-time find of the executable
	exePath := lookupPath(exe)

	var stdout, stderr bytes.Buffer
	cmdEnv := append(os.Environ(), env...)

	c := exec.Command(exePath, params...)

	c.Dir = workingDir
	c.Env = cmdEnv
	c.Stdout = &stdout
	c.Stderr = &stderr

	startTime := gsos.HighresTime()
	err := c.Run()
	cmdTime := (gsos.HighresTime() - startTime).Duration().Seconds() // TBD just return HighresTimestamp

	if err != nil {
		log.Fatalf("\n%s %s failed: %s\nstdout: %s\nstderr: %s\n",
			exe, strings.Join(params, " "), err, string(stdout.Bytes()), string(stderr.Bytes()))
	}

	return cmdTime, stdout.Bytes(), stderr.Bytes()
}

// lookupPath memoizes executable paths for better performance - some
// operating systems are slow to find executables. I suppose
// it's unreasonable to expect exec.LookPath to do this...
func lookupPath(exe string) string {
	exePath, ok := commandPaths[exe]
	if ok {
		return exePath
	}

	var err error
	exePath, err = exec.LookPath(exe)
	if err != nil {
		log.Fatalf("Not installed: %s\n", exe)
	}
	commandPaths[exe] = exePath
	return exePath
}

var commandPaths map[string]string = make(map[string]string)
