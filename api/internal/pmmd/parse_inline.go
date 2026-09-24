package pmmd

import (
	"regexp"
	"strings"
	"unicode"
)

// maxInlineDepth bounds recursion into nested link labels.
const maxInlineDepth = 16

// maxLinkScan bounds how far a link label / destination / autolink is
// searched for, keeping pathological input linear-ish.
const maxLinkScan = 4096

type ipiece struct {
	delim    bool
	ch       rune
	count    int
	orig     int
	canOpen  bool
	canClose bool
	active   bool
	n        *node  // content piece (text / hardBreak)
	marks    []mark // emphasis marks applied by processEmphasis
}

type inlineParser struct {
	src   []rune
	depth int
	// commentsExhausted is set once no "-->" remains in src.
	commentsExhausted bool
	pieces            []*ipiece
	buf               strings.Builder
}

// parseInline parses inline Markdown (lines joined with "\n") into text and
// hardBreak nodes.
func parseInline(s string, depth int) []*node {
	p := &inlineParser{src: []rune(s), depth: depth}
	p.scan()
	p.processEmphasis()
	return p.flatten()
}

// finishInline trims surrounding whitespace and hard breaks of a block's
// inline content and drops empty text nodes.
func finishInline(ns []*node) []*node {
	for len(ns) > 0 {
		first := ns[0]
		if first.Type == "hardBreak" {
			ns = ns[1:]
			continue
		}
		if first.Type == "text" {
			first.Text = strings.TrimLeft(first.Text, " \t")
			if first.Text == "" {
				ns = ns[1:]
				continue
			}
		}
		break
	}
	for len(ns) > 0 {
		last := ns[len(ns)-1]
		if last.Type == "hardBreak" {
			ns = ns[:len(ns)-1]
			continue
		}
		if last.Type == "text" && !hasMarkType(last.Marks, "code") {
			last.Text = strings.TrimRight(last.Text, " \t")
			if last.Text == "" {
				ns = ns[:len(ns)-1]
				continue
			}
		}
		break
	}
	if len(ns) == 0 {
		return nil
	}
	return ns
}

func (p *inlineParser) flush() {
	if p.buf.Len() == 0 {
		return
	}
	p.pieces = append(p.pieces, &ipiece{n: &node{Type: "text", Text: p.buf.String()}})
	p.buf.Reset()
}

func (p *inlineParser) add(n *node) {
	p.flush()
	p.pieces = append(p.pieces, &ipiece{n: n})
}

func isASCIIPunct(r rune) bool {
	return r < 128 && (unicode.IsPunct(r) || unicode.IsSymbol(r))
}

func isPunctRune(r rune) bool { return unicode.IsPunct(r) || unicode.IsSymbol(r) }

func runLen(src []rune, i int, c rune) int {
	n := 0
	for i+n < len(src) && src[i+n] == c {
		n++
	}
	return n
}

func skipSpaces(src []rune, i int) int {
	for i < len(src) && (src[i] == ' ' || src[i] == '\t') {
		i++
	}
	return i
}

// findBacktickClose returns the index of a run of exactly n backticks at or
// after i, or -1.
func findBacktickClose(src []rune, i, n int) int {
	for i < len(src) {
		if src[i] == '`' {
			m := runLen(src, i, '`')
			if m == n {
				return i
			}
			i += m
			continue
		}
		i++
	}
	return -1
}

var (
	autolinkRe = regexp.MustCompile(`^<([A-Za-z][A-Za-z0-9+.\-]{1,31}:[^\s<>]*)>`)
	emailRe    = regexp.MustCompile(`^<([A-Za-z0-9.!#$%&'*+/=?^_{|}~\-]+@[A-Za-z0-9](?:[A-Za-z0-9\-.]*[A-Za-z0-9])?)>`)
)

func (p *inlineParser) scan() {
	src := p.src
	i := 0
	for i < len(src) {
		r := src[i]
		switch {
		case r == '\\':
			if i+1 < len(src) && src[i+1] == '\n' {
				p.add(&node{Type: "hardBreak"})
				i = skipSpaces(src, i+2)
				continue
			}
			if i+1 < len(src) && isASCIIPunct(src[i+1]) {
				p.buf.WriteRune(src[i+1])
				i += 2
				continue
			}
			p.buf.WriteRune(r)
			i++

		case r == '\n':
			s := p.buf.String()
			trimmed := strings.TrimRight(s, " \t")
			hard := strings.HasSuffix(s, "  ")
			p.buf.Reset()
			p.buf.WriteString(trimmed)
			if hard {
				p.add(&node{Type: "hardBreak"})
			} else {
				p.buf.WriteByte(' ')
			}
			i = skipSpaces(src, i+1)

		case r == '`':
			n := runLen(src, i, '`')
			j := findBacktickClose(src, i+n, n)
			if j < 0 {
				p.buf.WriteString(strings.Repeat("`", n))
				i += n
				continue
			}
			code := strings.ReplaceAll(string(src[i+n:j]), "\n", " ")
			if len(code) >= 2 && code[0] == ' ' && code[len(code)-1] == ' ' && strings.TrimSpace(code) != "" {
				code = code[1 : len(code)-1]
			}
			if code != "" {
				p.add(&node{Type: "text", Text: code, Marks: []mark{{Type: "code"}}})
			}
			i = j + n

		case r == '*' || r == '_' || r == '~':
			n := runLen(src, i, r)
			if r == '~' && n != 2 {
				p.buf.WriteString(strings.Repeat("~", n))
				i += n
				continue
			}
			before, after := ' ', ' '
			if i > 0 {
				before = src[i-1]
			}
			if i+n < len(src) {
				after = src[i+n]
			}
			left := !unicode.IsSpace(after) && (!isPunctRune(after) || unicode.IsSpace(before) || isPunctRune(before))
			right := !unicode.IsSpace(before) && (!isPunctRune(before) || unicode.IsSpace(after) || isPunctRune(after))
			d := &ipiece{delim: true, ch: r, count: n, orig: n, active: true}
			if r == '_' {
				d.canOpen = left && (!right || isPunctRune(before))
				d.canClose = right && (!left || isPunctRune(after))
			} else {
				d.canOpen, d.canClose = left, right
			}
			p.flush()
			p.pieces = append(p.pieces, d)
			i += n

		case r == '!' && i+1 < len(src) && src[i+1] == '[':
			lk, ok := parseLinkAt(src, i+1)
			if !ok {
				p.buf.WriteRune('!')
				i++
				continue
			}
			// Inline images have no inline node in the schema; keep the alt
			// text, linked to the image when the URL is safe.
			text := unescapeMD(lk.label)
			if text == "" {
				text = lk.dest
			}
			if text != "" {
				n := &node{Type: "text", Text: text}
				if lk.dest != "" && isSafeURL(lk.dest) {
					n.Marks = []mark{linkMark(lk.dest)}
				}
				p.add(n)
			}
			i = lk.end

		case r == '[':
			lk, ok := parseLinkAt(src, i)
			if !ok {
				p.buf.WriteRune('[')
				i++
				continue
			}
			var label []*node
			if p.depth >= maxInlineDepth {
				label = []*node{{Type: "text", Text: unescapeMD(lk.label)}}
			} else {
				label = parseInline(lk.label, p.depth+1)
			}
			linked := lk.dest != "" && isSafeURL(lk.dest)
			for _, n := range label {
				if n.Type == "text" && linked {
					var ms []mark
					for _, m := range n.Marks {
						if m.Type != "link" {
							ms = append(ms, m)
						}
					}
					n.Marks = append(ms, linkMark(lk.dest))
				}
				p.add(n)
			}
			i = lk.end

		case r == '<':
			if hasRunePrefix(src[i:], "<!--") && !p.commentsExhausted {
				if end := indexRunes(src, i+4, "-->"); end >= 0 {
					i = end + 3
					continue
				}
				p.commentsExhausted = true
			}
			j := i + 1
			for j < len(src) && j-i < maxLinkScan && src[j] != '>' && src[j] != '<' && !unicode.IsSpace(src[j]) {
				j++
			}
			rest := ""
			if j < len(src) && src[j] == '>' {
				rest = string(src[i : j+1])
			}
			if m := autolinkRe.FindStringSubmatch(rest); m != nil && isSafeURL(m[1]) {
				p.add(&node{Type: "text", Text: m[1], Marks: []mark{linkMark(m[1])}})
				i += len([]rune(m[0]))
				continue
			}
			if m := emailRe.FindStringSubmatch(rest); m != nil {
				p.add(&node{Type: "text", Text: m[1], Marks: []mark{linkMark("mailto:" + m[1])}})
				i += len([]rune(m[0]))
				continue
			}
			p.buf.WriteRune('<')
			i++

		default:
			p.buf.WriteRune(r)
			i++
		}
	}
	p.flush()
}

func linkMark(href string) mark {
	return mark{Type: "link", Attrs: map[string]any{"href": href}}
}

// processEmphasis matches delimiter runs following the CommonMark algorithm
// (simplified: no openers_bottom optimisation).
func (p *inlineParser) processEmphasis() {
	ps := p.pieces
	for c := 0; c < len(ps); c++ {
		closer := ps[c]
		if !closer.delim || !closer.active || !closer.canClose {
			continue
		}
		for o := c - 1; o >= 0 && closer.count > 0; o-- {
			op := ps[o]
			if !op.delim || !op.active || !op.canOpen || op.ch != closer.ch || op.count == 0 {
				continue
			}
			if op.ch != '~' && (op.canClose || closer.canOpen) &&
				(op.orig+closer.orig)%3 == 0 && (op.orig%3 != 0 || closer.orig%3 != 0) {
				continue
			}
			n := 1
			if op.count >= 2 && closer.count >= 2 {
				n = 2
			}
			var m mark
			switch {
			case op.ch == '~':
				m = mark{Type: "strike"}
			case n == 2:
				m = mark{Type: "bold"}
			default:
				m = mark{Type: "italic"}
			}
			for k := o + 1; k < c; k++ {
				if !hasMark(ps[k].marks, m) {
					ps[k].marks = append(ps[k].marks, m)
				}
				if ps[k].delim {
					ps[k].active = false
				}
			}
			op.count -= n
			closer.count -= n
			if op.count == 0 {
				op.active = false
			}
			o = c // restart the search from just before the closer
		}
		if closer.count == 0 {
			closer.active = false
		}
	}
}

func (p *inlineParser) flatten() []*node {
	var out []*node
	for _, pc := range p.pieces {
		var n *node
		if pc.delim {
			if pc.count == 0 {
				continue
			}
			n = &node{Type: "text", Text: strings.Repeat(string(pc.ch), pc.count)}
		} else {
			n = pc.n
		}
		if n.Type == "text" {
			if n.Text == "" {
				continue
			}
			for _, m := range pc.marks {
				if !hasMark(n.Marks, m) {
					n.Marks = append(n.Marks, m)
				}
			}
			sortMarks(n.Marks)
			if k := len(out); k > 0 && out[k-1].Type == "text" && sameMarks(out[k-1].Marks, n.Marks) {
				out[k-1].Text += n.Text
				continue
			}
		}
		out = append(out, n)
	}
	return out
}

type linkRes struct {
	label, dest, title string
	end                int
}

// parseLinkAt parses an inline link "[label](dest "title")" starting at the
// '[' at src[i].
func parseLinkAt(src []rune, i int) (linkRes, bool) {
	depth := 0
	j := i
	for j < len(src) && j-i < maxLinkScan {
		c := src[j]
		if c == '\\' && j+1 < len(src) {
			j += 2
			continue
		}
		if c == '`' {
			n := runLen(src, j, '`')
			if k := findBacktickClose(src, j+n, n); k >= 0 {
				j = k + n
			} else {
				j += n
			}
			continue
		}
		if c == '[' {
			depth++
		} else if c == ']' {
			depth--
			if depth == 0 {
				break
			}
		}
		j++
	}
	if j >= len(src) || src[j] != ']' {
		return linkRes{}, false
	}
	labelEnd := j
	k := j + 1
	if k >= len(src) || src[k] != '(' {
		return linkRes{}, false
	}
	k = skipWS(src, k+1)
	var dest []rune
	if k < len(src) && src[k] == '<' {
		k++
		for k < len(src) && src[k] != '>' {
			if src[k] == '\n' || src[k] == '<' {
				return linkRes{}, false
			}
			if src[k] == '\\' && k+1 < len(src) && isASCIIPunct(src[k+1]) {
				dest = append(dest, src[k+1])
				k += 2
				continue
			}
			dest = append(dest, src[k])
			k++
		}
		if k >= len(src) {
			return linkRes{}, false
		}
		k++
	} else {
		paren := 0
		for start := k; k < len(src) && k-start < maxLinkScan; {
			c := src[k]
			if c == '\\' && k+1 < len(src) && isASCIIPunct(src[k+1]) {
				dest = append(dest, src[k+1])
				k += 2
				continue
			}
			if unicode.IsSpace(c) || unicode.IsControl(c) {
				break
			}
			if c == '(' {
				paren++
			} else if c == ')' {
				if paren == 0 {
					break
				}
				paren--
			}
			dest = append(dest, c)
			k++
		}
	}
	beforeTitle := k
	k = skipWS(src, k)
	var title []rune
	if k > beforeTitle && k < len(src) && (src[k] == '"' || src[k] == '\'' || src[k] == '(') {
		closeCh := src[k]
		if closeCh == '(' {
			closeCh = ')'
		}
		k++
		for k < len(src) && src[k] != closeCh {
			if src[k] == '\\' && k+1 < len(src) && isASCIIPunct(src[k+1]) {
				title = append(title, src[k+1])
				k += 2
				continue
			}
			title = append(title, src[k])
			k++
		}
		if k >= len(src) {
			return linkRes{}, false
		}
		k = skipWS(src, k+1)
	}
	if k >= len(src) || src[k] != ')' {
		return linkRes{}, false
	}
	return linkRes{label: string(src[i+1 : labelEnd]), dest: string(dest), title: string(title), end: k + 1}, true
}

func hasRunePrefix(src []rune, prefix string) bool {
	for _, r := range prefix {
		if len(src) == 0 || src[0] != r {
			return false
		}
		src = src[1:]
	}
	return true
}

func indexRunes(src []rune, from int, needle string) int {
	for k := from; k < len(src); k++ {
		if hasRunePrefix(src[k:], needle) {
			return k
		}
	}
	return -1
}

func skipWS(src []rune, i int) int {
	for i < len(src) && (src[i] == ' ' || src[i] == '\t' || src[i] == '\n') {
		i++
	}
	return i
}

// unescapeMD removes backslash escapes from plain text.
func unescapeMD(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	rs := []rune(s)
	var b strings.Builder
	for i := 0; i < len(rs); i++ {
		if rs[i] == '\\' && i+1 < len(rs) && isASCIIPunct(rs[i+1]) {
			i++
		}
		b.WriteRune(rs[i])
	}
	return b.String()
}
