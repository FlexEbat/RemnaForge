package sysinfo

import (
	"fmt"
	"strings"

	"github.com/FlexEbat/RemnaForge/internal/i18n"
	"github.com/FlexEbat/RemnaForge/internal/ui"
)

// Severity of a finding.
type Severity int

const (
	// Warn lets the install go on.
	Warn Severity = iota
	// Refuse stops the install.
	Refuse
)

// Finding is one problem the host check found.
type Finding struct {
	Severity Severity
	Message  string
}

// FormatBytes writes a size in GiB with one decimal.
func FormatBytes(b uint64) string {
	return fmt.Sprintf("%.1f GiB", float64(b)/float64(1<<30))
}

// Evaluate compares the host with the thresholds. A value that could not be
// read (RAM 0, a disk error, an unparsable kernel) is skipped, not judged.
func Evaluate(info Info) []Finding {
	var out []Finding
	add := func(s Severity, key string, args ...any) {
		out = append(out, Finding{Severity: s, Message: fmt.Sprintf(i18n.T(key), args...)})
	}
	switch {
	case info.RAM == 0:
	case info.RAM < MinRAM:
		add(Refuse, "SYSINFO_RAM_REFUSE", FormatBytes(info.RAM), FormatBytes(MinRAM))
	case info.RAM < WarnRAM:
		add(Warn, "SYSINFO_RAM_WARN", FormatBytes(info.RAM), FormatBytes(WarnRAM))
	}
	for _, d := range info.Disks {
		if d.Err == nil && d.Free < MinFreeDisk {
			add(Warn, "SYSINFO_DISK_WARN", d.Path, FormatBytes(d.Free), FormatBytes(MinFreeDisk))
		}
	}
	if major, minor, ok := ParseKernel(info.Kernel); ok {
		if major < MinKernelMajor || (major == MinKernelMajor && minor < MinKernelMinor) {
			add(Warn, "SYSINFO_KERNEL_WARN", info.Kernel, fmt.Sprintf("%d.%d", MinKernelMajor, MinKernelMinor))
		}
	}
	if !info.Systemd {
		add(Warn, "SYSINFO_NO_SYSTEMD")
	}
	if r := info.Repo; r != nil {
		if r.UpdateErr != nil {
			add(Warn, "SYSINFO_REPO_UPDATE_FAILED", r.UpdateErr)
		}
		if len(r.NoCandidate) > 0 {
			add(Warn, "SYSINFO_REPO_NO_CANDIDATE", strings.Join(r.NoCandidate, " "))
		}
	}
	return out
}

// Lines renders the host as the block printed at the start of an install
// flow.
func Lines(info Info) []string {
	name := info.OSName
	if name == "" {
		name = strings.TrimSpace(info.OSID + " " + info.OSVersion)
	}
	pm := info.PackageManager
	if pm == "" {
		pm = "-"
	}
	lines := []string{
		fmt.Sprintf(i18n.T("SYSINFO_LINE_SYSTEM"), name, info.Arch, info.Kernel),
		fmt.Sprintf(i18n.T("SYSINFO_LINE_TOOLS"), pm, yesNo(info.Systemd)),
	}
	if info.RAM > 0 {
		lines = append(lines, fmt.Sprintf(i18n.T("SYSINFO_LINE_RAM"), FormatBytes(info.RAM)))
	}
	var disks []string
	for _, d := range info.Disks {
		if d.Err == nil {
			disks = append(disks, d.Path+" "+FormatBytes(d.Free))
		}
	}
	if len(disks) > 0 {
		lines = append(lines, fmt.Sprintf(i18n.T("SYSINFO_LINE_DISK"), strings.Join(disks, ", ")))
	}
	if r := info.Repo; r != nil {
		state := i18n.T("SYSINFO_REPO_OK")
		if r.UpdateErr != nil || len(r.NoCandidate) > 0 {
			state = i18n.T("SYSINFO_REPO_PROBLEM")
		}
		lines = append(lines, fmt.Sprintf(i18n.T("SYSINFO_LINE_REPO"), state))
	}
	return lines
}

func yesNo(b bool) string {
	if b {
		return i18n.T("SYSINFO_YES")
	}
	return i18n.T("SYSINFO_NO")
}

// Print writes the block and the findings to the terminal and returns an
// error when a finding refuses the install.
func Print(info Info) error {
	fmt.Println()
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("SYSINFO_TITLE"), ui.ColorReset)
	for _, l := range Lines(info) {
		fmt.Printf("%s%s%s\n", ui.ColorGray, l, ui.ColorReset)
	}
	var refused []string
	for _, f := range Evaluate(info) {
		if f.Severity == Refuse {
			fmt.Printf("%s%s%s\n", ui.ColorRed, f.Message, ui.ColorReset)
			refused = append(refused, f.Message)
			continue
		}
		fmt.Printf("%s%s%s\n", ui.ColorYellow, f.Message, ui.ColorReset)
	}
	fmt.Println()
	if len(refused) > 0 {
		return fmt.Errorf("%s", strings.Join(refused, "; "))
	}
	return nil
}
