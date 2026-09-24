package pmmd

import (
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// ---- block rendering ----

// renderBlocks renders a sequence of block nodes, separating them with blank
// lines. In item mode (inside a listItem) a list directly following a
// paragraph is attached without a blank line so the item stays tight.
func renderBlocks(nodes []*node, inItem bool) []string {
	var out []string
	prevType := ""
	prevAlt := false
	for _, n := range nodes {
		if n == nil {
			continue
		}
		alt := false
		if isListType(n.Type) && prevType == n.Type {
			// Two adjacent lists of the same kind would merge when parsed back;
			// switching the marker character keeps them separate.
			alt = !prevAlt
		}
		lines := renderBlock(n, alt)
		if lines == nil {
			continue
		}
		if len(out) > 0 && (!inItem || !isListType(n.Type) || prevType != "paragraph") {
			out = append(out, "")
		}
		out = append(out, lines...)
		prevType, prevAlt = n.Type, alt
	}
	return out
}

func isListType(t string) bool { return t == "bulletList" || t == "orderedList" }

func renderBlock(n *node, alt bool) []string {
	switch n.Type {
	case "paragraph":
		s := renderInline(n.Content, false)
		if s == "" {
			return nil
		}
		return strings.Split(s, "\n")
	case "heading":
		level := intAttr(n.Attrs, "level", 1)
		if level < 1 {
			level = 1
		}
		if level > 6 {
			level = 6
		}
		s := renderInline(n.Content, true)
		if strings.HasSuffix(s, "#") {
			s = s[:len(s)-1] + `\#`
		}
		prefix := strings.Repeat("#", level)
		if s == "" {
			return []string{prefix}
		}
		return []string{prefix + " " + s}
	case "blockquote":
		inner := renderBlocks(n.Content, false)
		if len(inner) == 0 {
			return []string{">"}
		}
		out := make([]string, len(inner))
		for i, l := range inner {
			if l == "" {
				out[i] = ">"
			} else {
				out[i] = "> " + l
			}
		}
		return out
	case "codeBlock":
		return renderCodeBlock(n)
	case "horizontalRule":
		return []string{"---"}
	case "image":
		s := renderImage(n)
		if s == "" {
			return nil
		}
		return []string{s}
	case "bulletList", "orderedList":
		return renderList(n, alt)
	case "listItem":
		return renderList(&node{Type: "bulletList", Content: []*node{n}}, alt)
	case "text":
		return renderBlock(&node{Type: "paragraph", Content: []*node{n}}, alt)
	case "hardBreak":
		return nil
	default:
		t := strings.TrimSpace(textContent(n))
		if t == "" {
			return nil
		}
		return renderBlock(&node{Type: "paragraph", Content: []*node{{Type: "text", Text: t}}}, alt)
	}
}

func intAttr(attrs map[string]any, key string, def int) int {
	switch v := attrs[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return def
}

func stringAttr(attrs map[string]any, key string) string {
	s, _ := attrs[key].(string)
	return s
}

func renderCodeBlock(n *node) []string {
	var b strings.Builder
	for _, c := range n.Content {
		b.WriteString(textContent(c))
	}
	text := b.String()
	fence := strings.Repeat("`", max(3, maxRun(text, '`')+1))
	lang := strings.Join(strings.Fields(stringAttr(n.Attrs, "language")), "-")
	out := []string{fence + lang}
	if text != "" {
		out = append(out, strings.Split(text, "\n")...)
	}
	return append(out, fence)
}

func maxRun(s string, c rune) int {
	best, cur := 0, 0
	for _, r := range s {
		if r == c {
			cur++
			best = max(best, cur)
		} else {
			cur = 0
		}
	}
	return best
}

func renderImage(n *node) string {
	src := stringAttr(n.Attrs, "src")
	if src == "" {
		return ""
	}
	alt := stringAttr(n.Attrs, "alt")
	s := "![" + escapeText(alt, false) + "](" + formatDest(src)
	if title := stringAttr(n.Attrs, "title"); title != "" {
		s += ` "` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(title) + `"`
	}
	return s + ")"
}

func formatDest(href string) string {
	if strings.ContainsAny(href, " \t\n()<>") {
		return "<" + strings.NewReplacer("<", "%3C", ">", "%3E", "\n", "%0A").Replace(href) + ">"
	}
	return strings.ReplaceAll(href, `\`, `\\`)
}

func renderList(n *node, alt bool) []string {
	ordered := n.Type == "orderedList"
	num := intAttr(n.Attrs, "start", 1)
	tight := true
	for _, it := range n.Content {
		if !itemIsTight(it) {
			tight = false
		}
	}
	var out []string
	for idx, it := range n.Content {
		var marker string
		switch {
		case ordered && alt:
			marker = strconv.Itoa(num+idx) + ")"
		case ordered:
			marker = strconv.Itoa(num+idx) + "."
		case alt:
			marker = "*"
		default:
			marker = "-"
		}
		body, box := renderItemBody(it)
		indent := strings.Repeat(" ", len(marker)+1)
		if idx > 0 && !tight {
			out = append(out, "")
		}
		out = append(out, strings.TrimRight(marker+" "+box+body[0], " "))
		for _, l := range body[1:] {
			if l == "" {
				out = append(out, "")
			} else {
				out = append(out, indent+l)
			}
		}
	}
	return out
}

func itemIsTight(it *node) bool {
	if it.Type != "listItem" {
		return true
	}
	for i, c := range it.Content {
		if i == 0 && c.Type == "paragraph" {
			continue
		}
		if !isListType(c.Type) {
			return false
		}
	}
	return true
}

// renderItemBody renders a listItem's content. It returns at least one line
// (the text following the marker) and the checkbox prefix, if any.
func renderItemBody(it *node) ([]string, string) {
	if it.Type != "listItem" {
		lines := renderBlock(it, false)
		if len(lines) == 0 {
			lines = []string{""}
		}
		return lines, ""
	}
	taskID := stringAttr(it.Attrs, "taskId")
	checked, _ := it.Attrs["checked"].(bool)
	box := ""
	if taskID != "" {
		switch stringAttr(it.Attrs, "taskStatus") {
		case "done", "cancelled":
			box = "[x] "
		case "":
			if checked {
				box = "[x] "
			} else {
				box = "[ ] "
			}
		default:
			box = "[ ] "
		}
	} else if checked {
		box = "[x] "
	}

	lines := []string{""}
	rest := it.Content
	if len(rest) > 0 && rest[0].Type == "paragraph" {
		if s := renderInline(rest[0].Content, false); s != "" {
			lines = strings.Split(s, "\n")
		}
		rest = rest[1:]
	}
	if taskID != "" {
		last := len(lines) - 1
		lines[last] = strings.TrimLeft(lines[last]+" <!-- task:"+taskID+" -->", " ")
	}
	restLines := renderBlocks(rest, true)
	if len(restLines) > 0 {
		if !isListType(rest[0].Type) {
			lines = append(lines, "")
		}
		lines = append(lines, restLines...)
	}
	return lines, box
}

// ---- inline rendering ----

type seg struct {
	text  string
	brk   bool // hard break
	raw   bool // pre-rendered markdown (inline image)
	marks []mark
}

var emphasisTypes = map[string]bool{"bold": true, "italic": true, "strike": true}

func renderInline(nodes []*node, inHeading bool) string {
	var segs []seg
	var flatten func(ns []*node)
	flatten = func(ns []*node) {
		for _, n := range ns {
			if n == nil {
				continue
			}
			switch n.Type {
			case "text":
				t := strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(n.Text)
				if t != "" {
					segs = append(segs, seg{text: t, marks: renderableMarks(n.Marks)})
				}
			case "hardBreak":
				if inHeading {
					segs = append(segs, seg{text: " "})
				} else {
					segs = append(segs, seg{brk: true})
				}
			case "image":
				if s := renderImage(n); s != "" {
					segs = append(segs, seg{text: s, raw: true})
				}
			default:
				if len(n.Content) > 0 {
					flatten(n.Content)
				} else if n.Text != "" {
					segs = append(segs, seg{text: n.Text, marks: renderableMarks(n.Marks)})
				}
			}
		}
	}
	flatten(nodes)
	for len(segs) > 0 && segs[len(segs)-1].brk {
		segs = segs[:len(segs)-1]
	}
	for len(segs) > 0 && segs[0].brk {
		segs = segs[1:]
	}
	segs = mergeSegs(expelWhitespace(segs))
	return strings.TrimRight(emitSegs(segs), " \t")
}

func renderableMarks(ms []mark) []mark {
	var out []mark
	for _, m := range ms {
		switch m.Type {
		case "bold", "italic", "strike", "code":
		case "link":
			if linkHref(m) == "" {
				continue
			}
		default:
			continue // underline, highlight, sub/superscript render as plain text
		}
		if !hasMark(out, m) {
			out = append(out, mark{Type: m.Type, Attrs: m.Attrs})
		}
	}
	sortMarks(out)
	return out
}

// expelWhitespace moves leading/trailing whitespace of emphasised text
// outside the emphasis ("** a**" is not bold in Markdown).
func expelWhitespace(segs []seg) []seg {
	marksAt := func(i int) []mark {
		if i < 0 || i >= len(segs) || segs[i].brk {
			return nil
		}
		return segs[i].marks
	}
	strip := func(ms []mark, keep func(mark) bool) []mark {
		var out []mark
		for _, m := range ms {
			if !emphasisTypes[m.Type] || keep(m) {
				out = append(out, m)
			}
		}
		return out
	}
	var out []seg
	for i, s := range segs {
		if s.brk || s.raw || hasMarkType(s.marks, "code") {
			out = append(out, s)
			continue
		}
		hasEmph := false
		for _, m := range s.marks {
			if emphasisTypes[m.Type] {
				hasEmph = true
			}
		}
		if !hasEmph {
			out = append(out, s)
			continue
		}
		prev, next := marksAt(i-1), marksAt(i+1)
		core := strings.TrimSpace(s.text)
		if core == "" {
			s.marks = strip(s.marks, func(m mark) bool { return hasMark(prev, m) && hasMark(next, m) })
			out = append(out, s)
			continue
		}
		start := strings.Index(s.text, core)
		lead, trail := s.text[:start], s.text[start+len(core):]
		if lead != "" {
			out = append(out, seg{text: lead, marks: strip(s.marks, func(m mark) bool { return hasMark(prev, m) })})
		}
		out = append(out, seg{text: core, marks: s.marks})
		if trail != "" {
			out = append(out, seg{text: trail, marks: strip(s.marks, func(m mark) bool { return hasMark(next, m) })})
		}
	}
	return out
}

func mergeSegs(segs []seg) []seg {
	var out []seg
	for _, s := range segs {
		if n := len(out); n > 0 && !s.brk && !s.raw && !out[n-1].brk && !out[n-1].raw &&
			!hasMarkType(s.marks, "code") && sameMarks(out[n-1].marks, s.marks) {
			out[n-1].text += s.text
			continue
		}
		out = append(out, s)
	}
	return out
}

type openMark struct {
	m     mark
	close string
}

func emitSegs(segs []seg) string {
	var b strings.Builder
	var stack []openMark
	lineStart := true

	marksOf := func(i int) []mark {
		if i < 0 || i >= len(segs) {
			return nil
		}
		return segs[i].marks
	}
	// runLength counts how many consecutive segments from i carry m.
	runLength := func(i int, m mark) int {
		n := 0
		for j := i; j < len(segs); j++ {
			if segs[j].brk {
				continue
			}
			if !hasMark(segs[j].marks, m) {
				break
			}
			n++
		}
		return n
	}

	for i, s := range segs {
		target := s.marks
		if s.brk {
			// A hard break keeps marks that continue on both sides, except code.
			target = nil
			for _, m := range marksOf(i - 1) {
				if m.Type != "code" && hasMark(marksOf(i+1), m) {
					target = append(target, m)
				}
			}
		}
		keep := 0
		for keep < len(stack) && hasMark(target, stack[keep].m) {
			keep++
		}
		var toOpen []mark
		for _, m := range target {
			found := false
			for _, o := range stack[:keep] {
				if markEqual(o.m, m) {
					found = true
				}
			}
			if !found {
				toOpen = append(toOpen, m)
			}
		}
		// Code must stay innermost and is re-opened per segment.
		if keep > 0 && stack[keep-1].m.Type == "code" {
			keep--
			toOpen = append(toOpen, stack[keep].m)
		}
		for j := len(stack) - 1; j >= keep; j-- {
			b.WriteString(stack[j].close)
		}
		stack = stack[:keep]

		sort.SliceStable(toOpen, func(a, c int) bool {
			ta, tc := toOpen[a].Type == "code", toOpen[c].Type == "code"
			if ta != tc {
				return tc
			}
			ra, rc := runLength(i, toOpen[a]), runLength(i, toOpen[c])
			if ra != rc {
				return ra > rc
			}
			return rankOf(toOpen[a].Type) < rankOf(toOpen[c].Type)
		})

		if lineStart && !s.brk && !s.raw && len(toOpen) == 0 {
			s.text = strings.TrimLeft(s.text, " \t")
			if s.text == "" {
				continue
			}
		}

		// Emphasis cannot open before whitespace: emit leading whitespace first.
		if len(toOpen) > 0 && !s.brk && !s.raw && !hasMarkType(s.marks, "code") {
			if t := strings.TrimLeft(s.text, " \t"); t != "" && t != s.text && hasEmphasis(toOpen) {
				b.WriteString(s.text[:len(s.text)-len(t)])
				s.text = t
			}
		}

		for _, m := range toOpen {
			var open, cls string
			// Right after a '*' closer (overlapping marks), '*' would merge into
			// one delimiter run, so switch to '_'.
			delim := "*"
			if out := b.String(); strings.HasSuffix(out, "*") && !strings.HasSuffix(out, "\\*") {
				delim = "_"
			}
			switch m.Type {
			case "bold":
				open, cls = delim+delim, delim+delim
			case "italic":
				open, cls = delim, delim
			case "strike":
				open, cls = "~~", "~~"
			case "link":
				open, cls = "[", "]("+formatDest(linkHref(m))+")"
			case "code":
				fence := strings.Repeat("`", maxRun(s.text, '`')+1)
				pad := ""
				if strings.HasPrefix(s.text, "`") || strings.HasSuffix(s.text, "`") ||
					(strings.HasPrefix(s.text, " ") && strings.HasSuffix(s.text, " ") && strings.TrimSpace(s.text) != "") {
					pad = " "
				}
				open, cls = fence+pad, pad+fence
			}
			b.WriteString(open)
			stack = append(stack, openMark{m: m, close: cls})
			lineStart = false
		}

		switch {
		case s.brk:
			b.WriteString("\\\n")
			lineStart = true
		case s.raw || hasMarkType(s.marks, "code"):
			b.WriteString(s.text)
			lineStart = false
		default:
			b.WriteString(escapeText(s.text, lineStart))
			lineStart = false
		}
	}
	for j := len(stack) - 1; j >= 0; j-- {
		b.WriteString(stack[j].close)
	}
	return b.String()
}

func hasEmphasis(ms []mark) bool {
	for _, m := range ms {
		if emphasisTypes[m.Type] {
			return true
		}
	}
	return false
}

func isAlnum(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// escapeText backslash-escapes the characters that would otherwise be read
// as Markdown syntax. lineStart enables escaping of block-level markers.
func escapeText(s string, lineStart bool) string {
	rs := []rune(s)
	var b strings.Builder
	for i, r := range rs {
		prev, next := rune(0), rune(0)
		if i > 0 {
			prev = rs[i-1]
		}
		if i+1 < len(rs) {
			next = rs[i+1]
		}
		switch r {
		case '\\', '*', '`', '[', ']':
			b.WriteByte('\\')
		case '_':
			if !isAlnum(prev) || !isAlnum(next) {
				b.WriteByte('\\')
			}
		case '~':
			if i == 0 || i == len(rs)-1 || prev == '~' || next == '~' {
				b.WriteByte('\\')
			}
		case '<':
			if unicode.IsLetter(next) || next == '/' || next == '!' || next == '?' {
				b.WriteByte('\\')
			}
		}
		b.WriteRune(r)
	}
	out := b.String()
	if lineStart {
		out = escapeLineStart(out)
	}
	return out
}

func escapeLineStart(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '#', '>':
		return `\` + s
	case '-', '+', '=':
		if len(s) == 1 || s[1] == ' ' || s[1] == '\t' || s[1] == s[0] {
			return `\` + s
		}
	}
	d := 0
	for d < len(s) && d < 10 && s[d] >= '0' && s[d] <= '9' {
		d++
	}
	if d > 0 && d < len(s) && (s[d] == '.' || s[d] == ')') && (d+1 == len(s) || s[d+1] == ' ' || s[d+1] == '\t') {
		return s[:d] + `\` + s[d:]
	}
	return s
}
