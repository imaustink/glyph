package pmmd

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// parseMarkdown parses a Markdown document into ProseMirror block nodes.
func parseMarkdown(md string) []*node {
	md = strings.TrimPrefix(md, string(rune(0xFEFF)))
	md = strings.NewReplacer("\r\n", "\n", "\r", "\n", "\x00", string(rune(0xFFFD))).Replace(md)
	return parseBlocks(strings.Split(md, "\n"), 0)
}

func isBlank(line string) bool { return strings.TrimSpace(line) == "" }

// indentWidth returns the width in columns of the leading whitespace (tabs
// advance to the next multiple of 4).
func indentWidth(line string) int {
	col := 0
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case ' ':
			col++
		case '\t':
			col += 4 - col%4
		default:
			return col
		}
	}
	return col
}

// stripIndent removes up to n columns of leading whitespace.
func stripIndent(line string, n int) string {
	col, i := 0, 0
	for i < len(line) && col < n {
		switch line[i] {
		case ' ':
			col++
			i++
		case '\t':
			w := 4 - col%4
			if col+w > n {
				return strings.Repeat(" ", col+w-n) + line[i+1:]
			}
			col += w
			i++
		default:
			return line[i:]
		}
	}
	return line[i:]
}

// maxBlockDepth bounds container nesting (blockquotes / lists). Deeper
// content is kept as plain paragraph text so pathological input cannot cause
// quadratic work or unbounded recursion.
const maxBlockDepth = 32

func parseBlocks(lines []string, depth int) []*node {
	if depth > maxBlockDepth {
		var parts []string
		for _, l := range lines {
			if t := strings.TrimSpace(l); t != "" {
				parts = append(parts, t)
			}
		}
		if len(parts) == 0 {
			return nil
		}
		return []*node{{Type: "paragraph", Content: []*node{{Type: "text", Text: strings.Join(parts, " ")}}}}
	}
	var out []*node
	i := 0
	for i < len(lines) {
		line := lines[i]
		if isBlank(line) {
			i++
			continue
		}
		if f, ok := parseFenceOpen(line); ok {
			n, next := parseFence(lines, i, f)
			out = append(out, n)
			i = next
			continue
		}
		if level, text, ok := parseATX(line); ok {
			h := &node{Type: "heading", Attrs: map[string]any{"level": level}}
			h.Content = finishInline(parseInline(text, 0))
			out = append(out, h)
			i++
			continue
		}
		if isHR(line) {
			out = append(out, &node{Type: "horizontalRule"})
			i++
			continue
		}
		if isQuote(line) {
			var inner []string
			for i < len(lines) && isQuote(lines[i]) {
				inner = append(inner, stripQuote(lines[i]))
				i++
			}
			q := &node{Type: "blockquote", Content: parseBlocks(inner, depth+1)}
			if len(q.Content) == 0 {
				q.Content = []*node{{Type: "paragraph"}}
			}
			out = append(out, q)
			continue
		}
		if _, ok := parseItemStart(line); ok {
			l, next := parseList(lines, i, depth)
			out = append(out, l)
			i = next
			continue
		}
		if img, ok := parseImageLine(line); ok {
			out = append(out, img)
			i++
			continue
		}
		// Paragraph: consecutive lines until a blank line or another block.
		plines := []string{strings.TrimLeft(line, " \t")}
		i++
		for i < len(lines) && !isBlank(lines[i]) && !startsBlock(lines[i]) {
			plines = append(plines, strings.TrimLeft(lines[i], " \t"))
			i++
		}
		if content := finishInline(parseInline(strings.Join(plines, "\n"), 0)); len(content) > 0 {
			out = append(out, &node{Type: "paragraph", Content: content})
		}
	}
	return out
}

// startsBlock reports whether line begins a block that interrupts a
// paragraph.
func startsBlock(line string) bool {
	if indentWidth(line) > 3 {
		return false
	}
	if _, ok := parseFenceOpen(line); ok {
		return true
	}
	if _, _, ok := parseATX(line); ok {
		return true
	}
	if isHR(line) || isQuote(line) {
		return true
	}
	if st, ok := parseItemStart(line); ok && strings.TrimSpace(st.content) != "" {
		return true
	}
	_, ok := parseImageLine(line)
	return ok
}

// ---- fenced code ----

type fence struct {
	ch     byte
	n      int
	indent int
	lang   string
}

func parseFenceOpen(line string) (fence, bool) {
	ind := indentWidth(line)
	if ind > 3 {
		return fence{}, false
	}
	rest := strings.TrimLeft(line, " \t")
	if len(rest) < 3 || (rest[0] != '`' && rest[0] != '~') {
		return fence{}, false
	}
	c := rest[0]
	n := 0
	for n < len(rest) && rest[n] == c {
		n++
	}
	if n < 3 {
		return fence{}, false
	}
	info := strings.TrimSpace(rest[n:])
	if c == '`' && strings.Contains(info, "`") {
		return fence{}, false
	}
	lang := ""
	if f := strings.Fields(info); len(f) > 0 {
		lang = f[0]
	}
	return fence{ch: c, n: n, indent: ind, lang: lang}, true
}

func isFenceClose(line string, f fence) bool {
	if indentWidth(line) > 3 {
		return false
	}
	rest := strings.TrimSpace(line)
	if len(rest) < f.n {
		return false
	}
	for i := 0; i < len(rest); i++ {
		if rest[i] != f.ch {
			return false
		}
	}
	return true
}

func parseFence(lines []string, i int, f fence) (*node, int) {
	var body []string
	j := i + 1
	for j < len(lines) {
		if isFenceClose(lines[j], f) {
			j++
			break
		}
		body = append(body, stripIndent(lines[j], f.indent))
		j++
	}
	n := &node{Type: "codeBlock"}
	if f.lang != "" {
		n.Attrs = map[string]any{"language": unescapeMD(f.lang)}
	}
	if text := strings.Join(body, "\n"); text != "" {
		n.Content = []*node{{Type: "text", Text: text}}
	}
	return n, j
}

// ---- headings, rules, quotes ----

func parseATX(line string) (int, string, bool) {
	if indentWidth(line) > 3 {
		return 0, "", false
	}
	rest := strings.TrimLeft(line, " \t")
	n := 0
	for n < len(rest) && rest[n] == '#' {
		n++
	}
	if n == 0 || n > 6 {
		return 0, "", false
	}
	after := rest[n:]
	if after != "" && after[0] != ' ' && after[0] != '\t' {
		return 0, "", false
	}
	t := strings.TrimSpace(after)
	j := len(t)
	for j > 0 && t[j-1] == '#' {
		j--
	}
	if j < len(t) {
		if j == 0 {
			t = ""
		} else if t[j-1] == ' ' || t[j-1] == '\t' {
			t = strings.TrimRight(t[:j], " \t")
		}
	}
	return n, t, true
}

func isHR(line string) bool {
	if indentWidth(line) > 3 {
		return false
	}
	s := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, line)
	if len(s) < 3 || (s[0] != '-' && s[0] != '*' && s[0] != '_') {
		return false
	}
	return strings.Count(s, s[:1]) == len(s)
}

func isQuote(line string) bool {
	return indentWidth(line) <= 3 && strings.HasPrefix(strings.TrimLeft(line, " \t"), ">")
}

func stripQuote(line string) string {
	rest := strings.TrimLeft(line, " \t")[1:]
	if strings.HasPrefix(rest, " ") {
		return rest[1:]
	}
	if strings.HasPrefix(rest, "\t") {
		return "  " + rest[1:]
	}
	return rest
}

// ---- images ----

func parseImageLine(line string) (*node, bool) {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "![") {
		return nil, false
	}
	rs := []rune(t)
	lk, ok := parseLinkAt(rs, 1)
	if !ok || lk.end != len(rs) || lk.dest == "" || !isSafeURL(lk.dest) {
		return nil, false
	}
	attrs := map[string]any{"src": lk.dest}
	if alt := unescapeMD(lk.label); alt != "" {
		attrs["alt"] = alt
	}
	if lk.title != "" {
		attrs["title"] = lk.title
	}
	return &node{Type: "image", Attrs: attrs}, true
}

// ---- lists ----

type itemStart struct {
	ordered bool
	ch      byte // bullet char, or '.' / ')' for ordered lists
	num     int
	w       int // content indent
	content string
}

func parseItemStart(line string) (itemStart, bool) {
	ind := indentWidth(line)
	if ind > 3 || isHR(line) {
		return itemStart{}, false
	}
	rest := strings.TrimLeft(line, " \t")
	var st itemStart
	mlen := 0
	if rest != "" && (rest[0] == '-' || rest[0] == '*' || rest[0] == '+') {
		st.ch, mlen = rest[0], 1
	} else {
		d := 0
		for d < len(rest) && rest[d] >= '0' && rest[d] <= '9' {
			d++
		}
		if d == 0 || d > 9 || d >= len(rest) || (rest[d] != '.' && rest[d] != ')') {
			return itemStart{}, false
		}
		st.ordered, st.ch, mlen = true, rest[d], d+1
		st.num, _ = strconv.Atoi(rest[:d])
	}
	after := rest[mlen:]
	if after != "" && after[0] != ' ' && after[0] != '\t' {
		return itemStart{}, false
	}
	sp, k := 0, 0
	for k < len(after) && (after[k] == ' ' || after[k] == '\t') {
		if after[k] == '\t' {
			sp += 4 - (ind+mlen+sp)%4
		} else {
			sp++
		}
		k++
	}
	st.content = after[k:]
	if st.content == "" || sp > 4 {
		st.w = ind + mlen + 1
	} else {
		st.w = ind + mlen + sp
	}
	return st, true
}

func parseList(lines []string, i, depth int) (*node, int) {
	first, _ := parseItemStart(lines[i])
	list := &node{Type: "bulletList"}
	if first.ordered {
		list.Type = "orderedList"
	}
	for {
		st, _ := parseItemStart(lines[i])
		itemLines := []string{st.content}
		j := i + 1
		for j < len(lines) {
			l := lines[j]
			if isBlank(l) {
				k := j
				for k < len(lines) && isBlank(lines[k]) {
					k++
				}
				if k < len(lines) && indentWidth(lines[k]) >= st.w {
					for ; j < k; j++ {
						itemLines = append(itemLines, "")
					}
					continue
				}
				break
			}
			if indentWidth(l) >= st.w {
				itemLines = append(itemLines, stripIndent(l, st.w))
				j++
				continue
			}
			// Lazy paragraph continuation.
			if !isBlank(itemLines[len(itemLines)-1]) && !startsBlock(l) {
				itemLines = append(itemLines, strings.TrimLeft(l, " \t"))
				j++
				continue
			}
			break
		}
		list.Content = append(list.Content, buildItem(itemLines, depth))

		k := j
		for k < len(lines) && isBlank(lines[k]) {
			k++
		}
		if k < len(lines) {
			if nx, ok := parseItemStart(lines[k]); ok && nx.ordered == first.ordered && nx.ch == first.ch {
				i = k
				continue
			}
		}
		return list, j
	}
}

var (
	checkboxRe    = regexp.MustCompile(`^\[([ xX])\](?:[ \t]+|$)`)
	taskCommentRe = regexp.MustCompile(`[ \t]*<!--[ \t]*task:[ \t]*([^\s>]*)[ \t]*-->[ \t]*$`)
)

// escapedComment reports whether the "<!--" found at or after start is
// preceded by a backslash (i.e. it is literal text, not a comment).
func escapedComment(l string, start int) bool {
	lt := strings.Index(l[start:], "<!--") + start
	bs := 0
	for k := lt - 1; k >= 0 && l[k] == '\\'; k-- {
		bs++
	}
	return bs%2 == 1
}

func buildItem(itemLines []string, depth int) *node {
	attrs := map[string]any{"nodeId": uuid.NewString()}
	hasBox, checked := false, false
	if m := checkboxRe.FindStringSubmatch(itemLines[0]); m != nil {
		hasBox, checked = true, m[1] != " "
		itemLines[0] = itemLines[0][len(m[0]):]
	}
	taskID := ""
	for idx, l := range itemLines {
		if isBlank(l) || (idx > 0 && startsBlock(l)) {
			break
		}
		if m := taskCommentRe.FindStringSubmatchIndex(l); m != nil && !escapedComment(l, m[0]) {
			if id, err := uuid.Parse(l[m[2]:m[3]]); err == nil {
				taskID = id.String()
			}
			itemLines[idx] = l[:m[0]]
			break
		}
	}
	if hasBox || taskID != "" {
		attrs["checked"] = checked
	}
	if taskID != "" {
		attrs["taskId"] = taskID
		if checked {
			attrs["taskStatus"] = "done"
		} else {
			attrs["taskStatus"] = "todo"
		}
	}
	content := parseBlocks(itemLines, depth+1)
	if len(content) == 0 || content[0].Type != "paragraph" {
		content = append([]*node{{Type: "paragraph"}}, content...)
	}
	return &node{Type: "listItem", Attrs: attrs, Content: content}
}
