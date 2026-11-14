package cfgfile

import (
	"regexp"
	"sort"
	"strings"
)

func ensureNewline(s string) string {
	if s != "" && !strings.HasSuffix(s, "\n") {
		return s + "\n"
	}
	return s
}

var envKeyRE = regexp.MustCompile(`^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=`)

// MergeEnv keeps every line of the existing .env and adds the lines of the
// proposed one whose key is missing. An existing value is never replaced.
func MergeEnv(existing, proposed string) string {
	have := map[string]bool{}
	for _, line := range strings.Split(existing, "\n") {
		if m := envKeyRE.FindStringSubmatch(line); m != nil {
			have[m[1]] = true
		}
	}
	out := ensureNewline(existing)
	for _, line := range strings.Split(proposed, "\n") {
		m := envKeyRE.FindStringSubmatch(line)
		if m == nil || have[m[1]] {
			continue
		}
		have[m[1]] = true
		out += line + "\n"
	}
	return out
}

// Block is a top-level `header { ... }` block of an nginx.conf or a
// Caddyfile.
type Block struct {
	Header string // the text before the brace, comments removed, spaces collapsed
	Start  int    // where the statement starts (after the previous statement)
	Open   int    // index of the opening brace
	End    int    // index just past the closing brace
}

// Text returns the block with its header.
func (b Block) Text(conf string) string { return strings.TrimLeft(conf[b.Start:b.End], "\n") }

// Body returns what is between the braces.
func (b Block) Body(conf string) string { return conf[b.Open+1 : b.End-1] }

// scanTopLevel walks conf at brace depth 0 and returns its blocks and the
// plain statements (ending with ';') that sit between them. Lines starting
// with '#' are skipped, so a brace in a comment is not counted.
func scanTopLevel(conf string) (blocks []Block, statements []string) {
	stmtStart := 0
	depth := 0
	for i := 0; i < len(conf); {
		c := conf[i]
		if c == '#' && lineStartsWithComment(conf, i) {
			for i < len(conf) && conf[i] != '\n' {
				i++
			}
			continue
		}
		switch {
		case c == ';' && depth == 0:
			statements = append(statements, strings.TrimSpace(stripComments(conf[stmtStart:i])))
			stmtStart = i + 1
			i++
		case c == '{':
			if depth == 0 {
				open := i
				header := collapse(stripComments(conf[stmtStart:open]))
				d := 1
				k := i + 1
				for k < len(conf) && d > 0 {
					switch conf[k] {
					case '#':
						if lineStartsWithComment(conf, k) {
							for k < len(conf) && conf[k] != '\n' {
								k++
							}
							continue
						}
					case '{':
						d++
					case '}':
						d--
					}
					k++
				}
				blocks = append(blocks, Block{Header: header, Start: stmtStart, Open: open, End: k})
				stmtStart = k
				i = k
				continue
			}
			depth++
			i++
		case c == '}':
			if depth > 0 {
				depth--
			}
			i++
		default:
			i++
		}
	}
	return blocks, statements
}

func lineStartsWithComment(s string, i int) bool {
	for j := i - 1; j >= 0 && s[j] != '\n'; j-- {
		if s[j] != ' ' && s[j] != '\t' {
			return false
		}
	}
	return true
}

func stripComments(s string) string {
	var keep []string
	for _, l := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "#") {
			continue
		}
		keep = append(keep, l)
	}
	return strings.Join(keep, "\n")
}

func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

var serverNameRE = regexp.MustCompile(`(?m)^\s*server_name\s+([^;]+);`)

// blockKey names a block so the installer's version and the operator's
// version of the same block meet. A server block is named by its server_name
// list; every other block by its header.
func blockKey(conf string, b Block) string {
	if b.Header == "server" {
		if m := serverNameRE.FindStringSubmatch(b.Body(conf)); m != nil {
			names := strings.Fields(m[1])
			sort.Strings(names)
			return "server:" + strings.Join(names, ",")
		}
	}
	return b.Header
}

func directiveName(stmt string) string {
	f := strings.Fields(stmt)
	if len(f) == 0 {
		return ""
	}
	return f[0]
}

func mergeBlocks(existing, proposed string, withDirectives bool) string {
	pBlocks, pStmts := scanTopLevel(proposed)
	eBlocks, eStmts := scanTopLevel(existing)
	have := map[string]bool{}
	for _, b := range pBlocks {
		have[blockKey(proposed, b)] = true
	}
	out := ensureNewline(proposed)
	if withDirectives {
		known := map[string]bool{}
		for _, s := range pStmts {
			known[directiveName(s)] = true
		}
		var extra []string
		for _, s := range eStmts {
			if s != "" && !known[directiveName(s)] {
				extra = append(extra, s+";")
			}
		}
		if len(extra) > 0 {
			out += "\n# Kept from the previous file\n" + strings.Join(extra, "\n") + "\n"
		}
	}
	for _, b := range eBlocks {
		if have[blockKey(existing, b)] {
			continue
		}
		out += "\n" + ensureNewline(b.Text(existing))
	}
	return out
}

// MergeNginx puts the proposed nginx.conf first and appends every top-level
// block and plain directive of the existing file that the proposed one does
// not define. A block both define, such as the server of one domain, comes
// from the proposed file.
func MergeNginx(existing, proposed string) string { return mergeBlocks(existing, proposed, true) }

// MergeCaddy does the same for a Caddyfile.
func MergeCaddy(existing, proposed string) string { return mergeBlocks(existing, proposed, false) }

var topKeyRE = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_.-]*):`)

type yamlSection struct {
	name string
	text string
}

// splitTop cuts a compose file into top-level sections.
func splitTop(s string) (head string, sections []yamlSection) {
	lines := strings.SplitAfter(s, "\n")
	cur := -1
	for _, l := range lines {
		if m := topKeyRE.FindStringSubmatch(l); m != nil {
			sections = append(sections, yamlSection{name: m[1]})
			cur = len(sections) - 1
		}
		if cur < 0 {
			head += l
		} else {
			sections[cur].text += l
		}
	}
	return head, sections
}

var childKeyRE = regexp.MustCompile(`^( +)([A-Za-z0-9_.-]+):`)

// splitChildren cuts a section into its heading line and its children, found
// by the indentation of the first child.
func splitChildren(sec string) (heading string, children []yamlSection, tail string) {
	lines := strings.SplitAfter(sec, "\n")
	if len(lines) == 0 {
		return sec, nil, ""
	}
	heading = lines[0]
	indent := ""
	cur := -1
	for _, l := range lines[1:] {
		if m := childKeyRE.FindStringSubmatch(l); m != nil && (indent == "" || m[1] == indent) {
			indent = m[1]
			children = append(children, yamlSection{name: m[2]})
			cur = len(children) - 1
		}
		if cur < 0 {
			heading += l
		} else {
			children[cur].text += l
		}
	}
	return heading, children, ""
}

// MergeCompose merges two compose files by service, network and volume: the
// proposed file's entries win, entries only the existing file has stay, and
// so do top-level sections only the existing file has.
func MergeCompose(existing, proposed string) string {
	_, eSecs := splitTop(existing)
	pHead, pSecs := splitTop(proposed)
	eByName := map[string]yamlSection{}
	for _, s := range eSecs {
		eByName[s.name] = s
	}
	var out strings.Builder
	out.WriteString(pHead)
	used := map[string]bool{}
	for _, ps := range pSecs {
		used[ps.name] = true
		es, ok := eByName[ps.name]
		if !ok || (ps.name != "services" && ps.name != "networks" && ps.name != "volumes") {
			out.WriteString(ensureNewline(ps.text))
			continue
		}
		heading, pKids, _ := splitChildren(ps.text)
		_, eKids, _ := splitChildren(es.text)
		out.WriteString(ensureNewline(heading))
		pNames := map[string]bool{}
		for _, k := range pKids {
			pNames[k.name] = true
			out.WriteString(trimBlank(k.text))
		}
		target := leadingSpaces(firstLine(pKids))
		for _, k := range eKids {
			if !pNames[k.name] {
				out.WriteString(trimBlank(reindent(k.text, target)))
			}
		}
	}
	for _, es := range eSecs {
		if !used[es.name] {
			out.WriteString("\n" + trimBlank(es.text))
		}
	}
	return ensureNewline(out.String())
}

func firstLine(kids []yamlSection) string {
	if len(kids) == 0 {
		return ""
	}
	return strings.SplitN(kids[0].text, "\n", 2)[0]
}

func leadingSpaces(line string) int { return len(line) - len(strings.TrimLeft(line, " ")) }

// reindent moves a child entry to the indentation its new siblings use. YAML
// rejects siblings indented differently, and an operator's file often uses
// four spaces where the installer writes two. Deeper lines scale with it.
func reindent(text string, target int) string {
	if target <= 0 {
		return text
	}
	lines := strings.Split(text, "\n")
	from := leadingSpaces(lines[0])
	if from == 0 || from == target {
		return text
	}
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		ind := leadingSpaces(l)
		if ind < from {
			continue
		}
		lines[i] = strings.Repeat(" ", target+(ind-from)*target/from) + strings.TrimLeft(l, " ")
	}
	return strings.Join(lines, "\n")
}

// trimBlank ends a chunk with exactly one newline, so blank lines do not pile
// up when merged chunks are joined again and again.
func trimBlank(s string) string { return strings.TrimRight(s, "\n") + "\n" }

// MergeLines keeps the existing file and adds the proposed lines it lacks.
// Comment and blank lines of the proposal are added only when something else
// is added with them, so a second merge changes nothing.
func MergeLines(existing, proposed string) string {
	have := map[string]bool{}
	for _, l := range strings.Split(existing, "\n") {
		have[strings.TrimSpace(l)] = true
	}
	out := ensureNewline(existing)
	for _, l := range strings.Split(proposed, "\n") {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "#") || have[t] {
			continue
		}
		have[t] = true
		out += l + "\n"
	}
	return out
}
