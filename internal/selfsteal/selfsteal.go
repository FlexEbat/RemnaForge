// Package selfsteal is a port of src/modules/selfsteal_templates.sh
// (150 lines): downloads one of three community template repos, picks a
// random page from it, lightly obfuscates it (randomized ids/classes/meta
// tags so every install looks different), and installs it to
// /var/www/html.
//
// Two deliberate improvements over the literal bash flow (per project
// decision to not force a 1:1 port where a better option exists):
//   - Download+unzip is done in-memory via net/http + archive/zip instead
//     of shelling out to wget/unzip and writing main.zip to disk.
//   - The bash version's `spinner $$ ... &` background process is replaced
//     with a simple "please wait" message; no functional spinner animation
//     is reproduced since spinner() itself lives in install_remnawave.sh
//     and hasn't been ported (nor does the terminal's non-blocking spinner
//     translate well into Go without pulling in a TTY animation library
//     for a purely cosmetic effect).
package selfsteal

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/remnawave/remnawave-reverse-proxy-go/internal/i18n"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/ui"
)

const optDir = "/opt"
const webRoot = "/var/www/html"

// templateURLs mirrors template_urls (src/modules/selfsteal_templates.sh:29-33).
var templateURLs = []string{
	"https://github.com/eGamesAPI/simple-web-templates/archive/refs/heads/main.zip",
	"https://github.com/distillium/sni-templates/archive/refs/heads/main.zip",
	"https://github.com/prettyleaf/nothing-sni/archive/refs/heads/main.zip",
}

// Original bash (src/modules/selfsteal_templates.sh:4-14): show_template_source_options().
func showTemplateSourceOptions() {
	fmt.Println()
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("CHOOSE_TEMPLATE_SOURCE"), ui.ColorReset)
	fmt.Println()
	fmt.Printf("%s1. %s%s\n", ui.ColorYellow, i18n.T("SIMPLE_WEB_TEMPLATES"), ui.ColorReset)
	fmt.Printf("%s2. %s%s\n", ui.ColorYellow, i18n.T("SNI_TEMPLATES"), ui.ColorReset)
	fmt.Printf("%s3. %s%s\n", ui.ColorYellow, i18n.T("NOTHING_TEMPLATES"), ui.ColorReset)
	fmt.Println()
	fmt.Printf("%s0. %s%s\n", ui.ColorYellow, i18n.T("EXIT"), ui.ColorReset)
	fmt.Println()
}

// ManageSelfstealTemplates is the menu-facing entry point for "Install
// random template for node". Despite the menu label (LANG[MENU_4],
// unchanged from the original's "random"), this now lets the user pick a
// specific page within the chosen source instead of always picking one
// at random. See InteractiveInstall.
func ManageSelfstealTemplates() {
	showTemplateSourceOptions()
	option := ui.Reading(i18n.T("CHOOSE_TEMPLATE_OPTION"))

	var source string
	switch option {
	case "1":
		source = "simple"
	case "2":
		source = "sni"
	case "3":
		source = "nothing"
	case "0":
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("EXIT"), ui.ColorReset)
		return
	default:
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("INVALID_CHOICE"), ui.ColorReset)
		return
	}

	if err := InteractiveInstall(source); err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, err.Error(), ui.ColorReset)
	}
}

// templateSet is a downloaded, extracted template repo with its list of
// installable pages, not yet narrowed down to one.
type templateSet struct {
	selectedURL string
	templateDir string
	names       []string
}

func (ts *templateSet) randomName() string {
	return ts.names[rand.Intn(len(ts.names))]
}

// Original bash (src/modules/selfsteal_templates.sh:16-150): randomhtml().
// prepareTemplateSet covers its setup half, lines 21-77: clean up
// leftovers, download, extract, and list the pages available to choose
// from, without picking one yet. installTemplate (below) covers the rest.
func prepareTemplateSet(templateSource string) (*templateSet, error) {
	// Lines 21-22: clean up any leftovers from a previous run.
	_ = os.RemoveAll(filepath.Join(optDir, "main.zip"))
	_ = os.RemoveAll(filepath.Join(optDir, "simple-web-templates-main"))
	_ = os.RemoveAll(filepath.Join(optDir, "sni-templates-main"))
	_ = os.RemoveAll(filepath.Join(optDir, "nothing-sni-main"))

	// Lines 24-25.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("RANDOM_TEMPLATE"), ui.ColorReset)
	time.Sleep(1 * time.Second)
	fmt.Printf("%s%s...%s\n", ui.ColorGray, i18n.T("WAITING"), ui.ColorReset)

	// Lines 35-47: pick which repo to download.
	var selectedURL string
	switch templateSource {
	case "":
		selectedURL = templateURLs[rand.Intn(len(templateURLs))]
	case "simple":
		selectedURL = templateURLs[0]
	case "sni":
		selectedURL = templateURLs[1]
	case "nothing":
		selectedURL = templateURLs[2]
	default:
		selectedURL = templateURLs[1]
	}

	// Lines 49-55: download with retry (in-memory instead of wget->disk).
	zipData, err := downloadWithRetry(selectedURL)
	if err != nil {
		return nil, fmt.Errorf("%s", i18n.T("UNPACK_ERROR"))
	}

	// Line 54: unzip main.zip (in-memory instead of the `unzip` binary).
	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, fmt.Errorf("%s", i18n.T("UNPACK_ERROR"))
	}
	if err := extractZip(zr, optDir); err != nil {
		return nil, fmt.Errorf("%s", i18n.T("UNPACK_ERROR"))
	}

	// Lines 57-66: cd into the extracted folder + drop unwanted files.
	var templateDir string
	switch {
	case strings.Contains(selectedURL, "eGamesAPI"):
		templateDir = filepath.Join(optDir, "simple-web-templates-main")
		removeAll(templateDir, "assets", ".gitattributes", "README.md", "_config.yml")
	case strings.Contains(selectedURL, "nothing-sni"):
		templateDir = filepath.Join(optDir, "nothing-sni-main")
		removeAll(templateDir, ".github", "README.md")
	default:
		templateDir = filepath.Join(optDir, "sni-templates-main")
		removeAll(templateDir, "assets", "README.md", "index.html")
	}

	// Lines 68-77: list the pages/files available within the repo.
	var names []string
	if strings.Contains(selectedURL, "nothing-sni") {
		for i := 1; i <= 8; i++ {
			names = append(names, strconv.Itoa(i)+".html")
		}
	} else {
		entries, err := os.ReadDir(templateDir)
		if err != nil {
			return nil, fmt.Errorf("%s", i18n.T("UNPACK_ERROR"))
		}
		for _, e := range entries {
			if e.IsDir() {
				names = append(names, e.Name())
			}
		}
		if len(names) == 0 {
			return nil, fmt.Errorf("%s", i18n.T("UNPACK_ERROR"))
		}
	}

	return &templateSet{selectedURL: selectedURL, templateDir: templateDir, names: names}, nil
}

// installTemplate covers randomhtml()'s remaining lines, 79-149: resolve
// the distillium "503 error pages" v1/v2 variant, obfuscate, copy into
// /var/www/html, verify the obfuscation took, and clean up. pickVariant
// supplies the v1/v2 choice for that one template: RandomHTML rolls it
// randomly, InteractiveInstall asks the user.
func installTemplate(ts *templateSet, chosen string, pickVariant func(options []string) string) error {
	// Lines 79-85: special-case the distillium "503 error pages" template,
	// which itself has v1/v2 sub-variants.
	if strings.Contains(ts.selectedURL, "distillium") && chosen == "503 error pages" {
		chosen = filepath.Join(chosen, pickVariant([]string{"v1", "v2"}))
	}

	selectedPath := filepath.Join(ts.templateDir, chosen)

	// Lines 87-103: generate randomized tokens used to obfuscate the page.
	randomMetaID := randHex(16)
	randomComment := randHex(8)
	randomClassSuffix := randHex(4)
	randomTitleSuffix := randHex(4)
	randomFooterText := "Designed by RandomSite_" + randomTitleSuffix
	randomIDSuffix := randHex(4)

	metaNames := []string{"viewport-id", "session-id", "track-id", "render-id", "page-id", "config-id"}
	metaUsernames := []string{"Payee6296", "UserX1234", "AlphaBeta", "GammaRay", "DeltaForce", "EchoZulu", "Foxtrot99", "HotelCalifornia", "IndiaInk", "JulietBravo"}
	randomMetaName := metaNames[rand.Intn(len(metaNames))]
	randomUsername := metaUsernames[rand.Intn(len(metaUsernames))]

	classPrefixes := []string{"style", "data", "ui", "layout", "theme", "view"}
	randomClassPrefix := classPrefixes[rand.Intn(len(classPrefixes))]
	randomClass := randomClassPrefix + "-" + randomClassSuffix
	randomTitle := "Page_" + randomTitleSuffix

	// Lines 105-121: apply the obfuscation to every .html/.css file found.
	if err := obfuscateHTMLFiles(selectedPath, htmlTokens{
		metaName:   randomMetaName,
		metaID:     randomMetaID,
		comment:    randomComment,
		class:      randomClass,
		title:      randomTitle,
		footerText: randomFooterText,
		idSuffix:   randomIDSuffix,
		username:   randomUsername,
	}); err != nil {
		return err
	}
	obfuscateCSSFiles(selectedPath, randomComment, randomClass)

	// Line 127.
	fmt.Printf("%s %s\n", i18n.T("SELECT_TEMPLATE"), chosen)

	// Lines 129-141: install into /var/www/html.
	info, statErr := os.Stat(selectedPath)
	switch {
	case statErr == nil && info.IsDir():
		if _, err := os.Stat(webRoot); os.IsNotExist(err) {
			if err := os.MkdirAll(webRoot, 0755); err != nil {
				return fmt.Errorf("failed to create %s", webRoot)
			}
		}
		_ = clearDir(webRoot)
		if err := copyDir(selectedPath, webRoot); err != nil {
			return fmt.Errorf("%s", i18n.T("UNPACK_ERROR"))
		}
		fmt.Println(i18n.T("TEMPLATE_COPY"))
	case statErr == nil:
		if err := copyFile(selectedPath, filepath.Join(webRoot, "index.html")); err != nil {
			return fmt.Errorf("%s", i18n.T("UNPACK_ERROR"))
		}
		fmt.Println(i18n.T("TEMPLATE_COPY"))
	default:
		return fmt.Errorf("%s", i18n.T("UNPACK_ERROR"))
	}

	// Lines 143-146: sanity check the obfuscation actually took.
	if !anyHTMLContains(webRoot, randomMetaName) {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("FAILED_TO_MODIFY_HTML_FILES"), ui.ColorReset)
		return nil
	}

	// Lines 148-149: final cleanup.
	_ = os.RemoveAll(filepath.Join(optDir, "simple-web-templates-main"))
	_ = os.RemoveAll(filepath.Join(optDir, "sni-templates-main"))
	_ = os.RemoveAll(filepath.Join(optDir, "nothing-sni-main"))

	return nil
}

// randomVariant picks a v1/v2-style variant without asking, used by
// RandomHTML and by InteractiveInstall's own fallback when the user asks
// for a random page.
func randomVariant(options []string) string {
	return options[rand.Intn(len(options))]
}

// RandomHTML installs a fully-random page from a fully-random (or, if
// templateSource is set, a specific) repo, with no interactive prompts.
// Install flows (nginxnode, panelonly, panelfull) call this mid-install,
// where stopping to ask the user which decoy page to use would not fit.
func RandomHTML(templateSource string) error {
	ts, err := prepareTemplateSet(templateSource)
	if err != nil {
		return err
	}
	return installTemplate(ts, ts.randomName(), randomVariant)
}

// InteractiveInstall is the Go equivalent of randomhtml(), extended to
// let the user pick which page to install instead of always rolling one
// at random. This is not in the original bash, which never offers this
// choice. ManageSelfstealTemplates is the only caller.
func InteractiveInstall(templateSource string) error {
	ts, err := prepareTemplateSet(templateSource)
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.AvailableTemplates(), ui.ColorReset)
	fmt.Println()
	for i, name := range ts.names {
		fmt.Printf("%s%d. %s%s\n", ui.ColorYellow, i+1, name, ui.ColorReset)
	}
	fmt.Println()
	fmt.Printf("%s0. Random%s\n", ui.ColorYellow, ui.ColorReset)
	fmt.Println()

	choice := ui.Reading(fmt.Sprintf("Select action (0-%d):", len(ts.names)))

	chosen := ts.randomName()
	if idx, convErr := strconv.Atoi(choice); convErr == nil && idx >= 1 && idx <= len(ts.names) {
		chosen = ts.names[idx-1]
	}

	pickVariant := func(options []string) string {
		fmt.Println()
		fmt.Printf("%s%s: %s vs %s%s\n", ui.ColorYellow, chosen, options[0], options[1], ui.ColorReset)
		reply := ui.Reading(fmt.Sprintf("Select action (1-%d):", len(options)))
		if idx, convErr := strconv.Atoi(reply); convErr == nil && idx >= 1 && idx <= len(options) {
			return options[idx-1]
		}
		return randomVariant(options)
	}

	return installTemplate(ts, chosen, pickVariant)
}

// downloadWithRetry is the Go equivalent of:
//
//	while ! wget -q --timeout=30 --tries=10 --retry-connrefused "$selected_url"; do
//	    echo "${LANG[DOWNLOAD_FAIL]}"
//	    sleep 3
//	done
func downloadWithRetry(url string) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	for {
		resp, err := client.Get(url)
		if err == nil && resp.StatusCode == http.StatusOK {
			data, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr == nil {
				return data, nil
			}
		}
		if resp != nil {
			resp.Body.Close()
		}
		fmt.Println(i18n.T("DOWNLOAD_FAIL"))
		time.Sleep(3 * time.Second)
	}
}

// BUG FIX (not a 1:1 port): the original shells out to `unzip`, which
// refuses by default to write a path that escapes the extraction
// directory ("Zip Slip"). filepath.Join alone doesn't reject a
// malicious f.Name like "../../etc/cron.d/x", so this checks the
// cleaned, joined path stays under dest before writing anything.
// templateURLs are fixed, trusted GitHub archives, so this is
// defense-in-depth against a compromised upstream repo or a
// tampered-with download, not a fix for a currently-reachable exploit.
func extractZip(zr *zip.Reader, dest string) error {
	destClean := filepath.Clean(dest) + string(filepath.Separator)
	for _, f := range zr.File {
		path := filepath.Join(dest, f.Name)
		if !strings.HasPrefix(filepath.Clean(path)+string(filepath.Separator), destClean) {
			return fmt.Errorf("%s: illegal file path outside extraction directory", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(path, f.Mode()); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, rc)
		out.Close()
		rc.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	return nil
}

func removeAll(baseDir string, names ...string) {
	for _, n := range names {
		_ = os.RemoveAll(filepath.Join(baseDir, n))
	}
}

type htmlTokens struct {
	metaName   string
	metaID     string
	comment    string
	class      string
	title      string
	footerText string
	idSuffix   string
	username   string
}

var titleTagRE = regexp.MustCompile(`(?s)<title>.*?</title>`)

// obfuscateHTMLFiles replicates the `find ... -exec sed -i ...` pipeline at
// src/modules/selfsteal_templates.sh:105-116, one substitution per line
// there, applied to every .html file under root (root may itself be a
// single .html file, matching find's behavior when given a file path).
func obfuscateHTMLFiles(root string, t htmlTokens) error {
	return walkFiles(root, ".html", func(path string) error {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		s := string(data)

		s = strings.ReplaceAll(s, `<!-- Website template by freewebsitetemplates.com -->`, "")
		s = strings.ReplaceAll(s, `<!-- Theme by: WebThemez.com -->`, "")
		s = strings.ReplaceAll(s, `<a href="http://freewebsitetemplates.com">Free Website Templates</a>`, fmt.Sprintf("<span>%s</span>", t.footerText))
		s = strings.ReplaceAll(s, `<a href="http://webthemez.com" alt="webthemez">WebThemez.com</a>`, fmt.Sprintf("<span>%s</span>", t.footerText))
		s = strings.ReplaceAll(s, `id="Content"`, fmt.Sprintf(`id="rnd_%s"`, t.idSuffix))
		s = strings.ReplaceAll(s, `id="subscribe"`, fmt.Sprintf(`id="sub_%s"`, t.idSuffix))
		s = titleTagRE.ReplaceAllString(s, fmt.Sprintf("<title>%s</title>", t.title))
		s = strings.Replace(s, "</head>", fmt.Sprintf("<meta name=\"%s\" content=\"%s\">\n<!-- %s -->\n</head>", t.metaName, t.metaID, t.comment), 1)
		s = strings.Replace(s, "<body", fmt.Sprintf(`<body class="%s"`, t.class), 1)
		s = strings.ReplaceAll(s, "CHANGEMEPLS", t.username)

		return os.WriteFile(path, []byte(s), 0644)
	})
}

// obfuscateCSSFiles replicates src/modules/selfsteal_templates.sh:118-121:
// prepend a comment line and a tiny throwaway class rule to every .css file.
func obfuscateCSSFiles(root, comment, class string) {
	_ = walkFiles(root, ".css", func(path string) error {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		prefix := fmt.Sprintf("/* %s */\n.%s { display: block; }\n", comment, class)
		return os.WriteFile(path, append([]byte(prefix), data...), 0644)
	})
}

// walkFiles applies fn to every file under root with the given extension.
// If root itself is a file (not a directory) matching ext, fn runs on it
// directly. This mirrors how `find "./$RandomHTML" -type f -name "*.ext"`
// behaves whether $RandomHTML is a directory or a single file, the
// nothing-sni "N.html" case.
func walkFiles(root, ext string, fn func(path string) error) error {
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		if strings.HasSuffix(root, ext) {
			return fn(root)
		}
		return nil
	}
	return filepath.Walk(root, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !fi.IsDir() && strings.HasSuffix(path, ext) {
			return fn(path)
		}
		return nil
	})
}

func anyHTMLContains(root, needle string) bool {
	found := false
	_ = filepath.Walk(root, func(path string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() || !strings.HasSuffix(path, ".html") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr == nil && strings.Contains(string(data), needle) {
			found = true
		}
		return nil
	})
	return found
}

func clearDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if fi.IsDir() {
			return os.MkdirAll(target, fi.Mode())
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}
