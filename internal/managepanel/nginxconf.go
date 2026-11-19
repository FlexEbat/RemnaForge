package managepanel

import "strings"

// nginxBlock is a top-level `keyword { ... }` block found in an nginx.conf
// file, e.g. one `server { ... }` block.
type nginxBlock struct {
	HeaderStart int // index where the keyword itself starts
	BodyStart   int // index of the block's opening '{'
	BodyEnd     int // index just past the block's matching closing '}'
}

// Body returns the block's contents between (but not including) its { }.
func (b nginxBlock) Body(conf string) string {
	return conf[b.BodyStart+1 : b.BodyEnd-1]
}

func isWordByte(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// findTopLevelBlocks scans conf for every top-level (brace-depth 0)
// occurrence of `keyword` immediately followed (after optional whitespace)
// by `{`, and returns each one's full extent using brace-depth-aware
// matching for the closing `}`. This correctly handles nested blocks
// (like `location {}` inside `server {}`) and any indentation or
// whitespace style, unlike a literal `strings.Split(conf, "server {")`
// or a search for a bare "\n}" (which breaks the moment someone
// hand-indents the closing brace, or writes "server{" / "server  {").
//
// This isn't a full nginx config parser, but it ignores comments and a
// "#" inside quotes, which makes it a meaningful robustness improvement over
// plain literal-string matching: that breaks the moment a hand-edited
// nginx.conf uses different indentation or spacing than what this tool
// itself generates, or keeps an old block commented out.
func findTopLevelBlocks(original, keyword string) []nginxBlock {
	// Comments are blanked out, with the same length, so a commented-out
	// "server {" is not a block and a brace in a comment does not move the
	// depth. The positions found are valid in the original text.
	conf := maskComments(original)
	var blocks []nginxBlock
	depth := 0

	for i := 0; i < len(conf); {
		c := conf[i]
		switch {
		case c == '{':
			depth++
			i++
		case c == '}':
			if depth > 0 {
				depth--
			}
			i++
		case depth == 0 && strings.HasPrefix(conf[i:], keyword):
			end := i + len(keyword)
			precededOK := i == 0 || !isWordByte(conf[i-1])
			followedOK := end >= len(conf) || !isWordByte(conf[end])
			if !precededOK || !followedOK {
				i++
				continue
			}
			j := end
			for j < len(conf) && (conf[j] == ' ' || conf[j] == '\t' || conf[j] == '\n' || conf[j] == '\r') {
				j++
			}
			if j >= len(conf) || conf[j] != '{' {
				i++
				continue
			}
			bodyStart := j
			blockDepth := 1
			k := j + 1
			for k < len(conf) && blockDepth > 0 {
				switch conf[k] {
				case '{':
					blockDepth++
				case '}':
					blockDepth--
				}
				k++
			}
			blocks = append(blocks, nginxBlock{HeaderStart: i, BodyStart: bodyStart, BodyEnd: k})
			i = k
		default:
			i++
		}
	}

	return blocks
}

// maskComments returns conf with every comment replaced by spaces of the same
// length. A "#" starts a comment unless it sits inside a single or double
// quoted string on its line.
func maskComments(conf string) string {
	b := []byte(conf)
	var quote byte
	inComment := false
	for i, c := range b {
		switch {
		case c == '\n':
			inComment, quote = false, 0
		case inComment:
			b[i] = ' '
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '#':
			inComment = true
			b[i] = ' '
		}
	}
	return string(b)
}
