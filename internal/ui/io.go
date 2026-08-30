package ui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

// LogFile is the destination for tee-style logging.
var LogFile = "/usr/local/remnawave_reverse/remnawave_reverse.log"

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// stdinScanner is shared across all Reading() calls. Using a single
// long-lived scanner (instead of a fresh one per call) is important:
// bufio.Scanner reads stdin in chunks, so a new scanner per call would
// silently swallow whatever line was buffered but unread by the previous
// scanner, losing input.
var stdinScanner = bufio.NewScanner(os.Stdin)

// Question formats a prompt string with the standard "[?]" marker.
func Question(msg string) string {
	return fmt.Sprintf("%s[?]%s %s%s%s", ColorGreen, ColorReset, ColorYellow, msg, ColorReset)
}

// Reading prints prompt via Question and reads a line of input.
func Reading(prompt string) string {
	fmt.Printf(" %s", Question(prompt))
	stdinScanner.Scan()
	return stdinScanner.Text()
}

// ErrorExit prints msg in red and exits with status 1.
func ErrorExit(msg string) {
	fmt.Printf("%s%s%s\n", ColorRed, msg, ColorReset)
	Exit(1)
}

// cleanupFileLogging is set by EnableFileLogging and cleared once run.
// Exit calls it before terminating the process.
var cleanupFileLogging func()

// Exit is the only place in this codebase that should call os.Exit.
// os.Exit skips deferred functions, so a bare os.Exit call anywhere after
// EnableFileLogging would risk losing whatever output is still sitting in
// the tee pipe's buffer: the reader goroutine that copies it to the
// terminal and log file never gets scheduled again once the process
// starts tearing down. Exit runs that cleanup synchronously first, so the
// last lines a user sees (often an error message) are not the ones lost.
func Exit(code int) {
	if cleanupFileLogging != nil {
		cleanupFileLogging()
		cleanupFileLogging = nil
	}
	os.Exit(code)
}

// Println prints a line already colored by the caller.
func Println(msg string) {
	fmt.Println(msg)
}

// EnableFileLogging tees everything the process writes to stdout or
// stderr, including from child processes started with cmd.Stdout =
// os.Stdout (docker, certbot, ufw), to both the terminal and LogFile.
// It creates a pipe, points os.Stdout and os.Stderr at the write end,
// and copies everything read from it to both the original terminal and
// the log file.
//
// Call this once, as early as possible in main(), before anything else
// prints. It returns a cleanup function that restores the original
// os.Stdout/os.Stderr and waits for the copy goroutine to drain; call it
// before the process exits so buffered output is not lost.
func EnableFileLogging() (cleanup func(), err error) {
	if err := os.MkdirAll(filepath.Dir(LogFile), 0755); err != nil {
		return func() {}, err
	}
	logFile, err := os.OpenFile(LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return func() {}, err
	}

	realStdout := os.Stdout
	realStderr := os.Stderr

	pipeReader, pipeWriter, err := os.Pipe()
	if err != nil {
		logFile.Close()
		return func() {}, err
	}

	os.Stdout = pipeWriter
	os.Stderr = pipeWriter

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(io.MultiWriter(realStdout, logFile), pipeReader)
	}()

	closed := false
	cleanup = func() {
		if closed {
			return
		}
		closed = true
		os.Stdout = realStdout
		os.Stderr = realStderr
		_ = pipeWriter.Close()
		<-done
		_ = pipeReader.Close()
		_ = logFile.Close()
	}
	cleanupFileLogging = cleanup
	return cleanup, nil
}

// LogClear strips ANSI escape codes from the log file, done natively
// instead of shelling out to sed.
func LogClear() error {
	data, err := os.ReadFile(LogFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	cleaned := ansiEscape.ReplaceAll(data, nil)
	return os.WriteFile(LogFile, cleaned, 0644)
}
