// Package ui replicates the color/echo/reading helpers that live at the
// top of install_remnawave.sh (lines 10-15, 77-88, 102-104).
package ui

// Original bash (install_remnawave.sh:10-15):
//
//	COLOR_RESET="\033[0m"
//	COLOR_GREEN="\033[1;32m"
//	COLOR_YELLOW="\033[1;33m"
//	COLOR_WHITE="\033[1;37m"
//	COLOR_RED="\033[1;31m"
//	COLOR_GRAY='\033[0;90m'
const (
	ColorReset  = "\033[0m"
	ColorGreen  = "\033[1;32m"
	ColorYellow = "\033[1;33m"
	ColorWhite  = "\033[1;37m"
	ColorRed    = "\033[1;31m"
	ColorGray   = "\033[0;90m"
)
