// vcs-torture/vcs/cmd.go

package vcs

import (
	"bufio"
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

// RunExternalIncremental runs an external command incrementally, returning elapsed time.
// The stdout and stderr are provided through a callback as individual lines. The stdout
// callback is mandatory, but the stderr callback is optional.
// TBD add stdin that gets fed to the external program
func RunExternalIncremental(outCb, errCb func(string),
	exe string, workingDir string, env []string, params ...string) float64 {

	// Do one-time find of the executable
	exePath := lookupPath(exe)

	// Prepare the command
	c := exec.Command(exePath, params...)
	c.Dir = workingDir
	c.Env = append(os.Environ(), env...)

	stdoutPipe, _ := c.StdoutPipe()
	stdout := bufio.NewScanner(stdoutPipe)

	stderrPipe, _ := c.StderrPipe()
	stderr := bufio.NewScanner(stderrPipe)

	done := make(chan struct{})

	// Start the command. We can fetch stdout in the current thread, and
	// defer stderr to a goroutine. This should be performant.
	startTime := gsos.HighresTime()
	c.Start()

	go func() {
		for stderr.Scan() {
			line := stderr.Text()
			if errCb != nil {
				errCb(line)
			}
		}
		done <- struct{}{} // prevent race, although this could slow us down on really quick externals
	}()

	for stdout.Scan() {
		outCb(stdout.Text())
	}

	// Now wait for all the output. Hopefully our stderr will be consumed before
	// we exit.
	<-done
	err := c.Wait()
	cmdTime := (gsos.HighresTime() - startTime).Duration().Seconds() // TBD just return HighresTimestamp

	if err != nil {
		log.Fatalf("\n%s %s failed: %s\n", exe, strings.Join(params, " "), err)
	}

	return cmdTime
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
