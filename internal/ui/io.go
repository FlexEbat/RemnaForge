package ui

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
)

// LogFile is the destination for tee-style logging, mirroring the
// LOGFILE variable set up in install_remnawave.sh:106-110 (log_entry()).
var LogFile = "/usr/local/remnawave_reverse/remnawave_reverse.log"

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// stdinScanner is shared across all Reading() calls. Using a single
// long-lived scanner (instead of a fresh one per call) is important:
// bufio.Scanner reads stdin in chunks, so a new scanner per call would
// silently swallow whatever line was buffered but unread by the previous
// scanner - losing input, unlike bash's `read`, which re-reads stdin
// line-by-line with no such buffering surprise.
var stdinScanner = bufio.NewScanner(os.Stdin)

// Original bash (install_remnawave.sh:77-79):
//
//	question() {
//	    echo -e "${COLOR_GREEN}[?]${COLOR_RESET} ${COLOR_YELLOW}$*${COLOR_RESET}"
//	}
func Question(msg string) string {
	return fmt.Sprintf("%s[?]%s %s%s%s", ColorGreen, ColorReset, ColorYellow, msg, ColorReset)
}

// Original bash (install_remnawave.sh:81-83):
//
//	reading() {
//	    read -rp " $(question "$1")" "$2"
//	}
//
// Go doesn't have bash's "read into a named variable", so Reading returns
// the entered string instead of writing into a caller-named variable.
func Reading(prompt string) string {
	fmt.Printf(" %s", Question(prompt))
	stdinScanner.Scan()
	return stdinScanner.Text()
}

// Original bash (install_remnawave.sh:85-88):
//
//	error() {
//	    echo -e "${COLOR_RED}$*${COLOR_RESET}"
//	    exit 1
//	}
func ErrorExit(msg string) {
	fmt.Printf("%s%s%s\n", ColorRed, msg, ColorReset)
	os.Exit(1)
}

// Println prints a line already colored by the caller, equivalent to the
// many `echo -e "${COLOR_X}...${COLOR_RESET}"` calls throughout the scripts.
func Println(msg string) {
	fmt.Println(msg)
}

// Original bash (install_remnawave.sh:102-104):
//
//	log_clear() {
//	  sed -i -e 's/\x1b\[[0-9;]*[a-zA-Z]//g' "$LOGFILE"
//	}
//
// Rather than shelling out to sed, we strip ANSI escape codes from the log
// file natively.
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
