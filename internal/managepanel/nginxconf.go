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
// matching for the closing `}` - correctly handling nested blocks (like
// `location {}` inside `server {}`) and any indentation/whitespace style,
// unlike a literal `strings.Split(conf, "server {")` or a search for a
// bare "\n}" (which breaks the moment someone hand-indents the closing
// brace, or writes "server{" / "server  {").
//
// This isn't a full nginx config parser (no comment/string-literal
// awareness), but it's a meaningful robustness improvement over the
// literal-string matching used previously, for the same reason the
// original bash's sed/grep pipeline had this exact fragility.
func findTopLevelBlocks(conf, keyword string) []nginxBlock {
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
