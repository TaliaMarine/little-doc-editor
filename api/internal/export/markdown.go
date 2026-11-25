package export

import (
	"strconv"
	"strings"
)

// Markdown renders doc as GitHub-flavoured Markdown. Inline images become
// ![alt](attachment:<id>) — the attachment ID is the stable reference.
func Markdown(doc Node) string {
	var sb strings.Builder
	mdBlocks(&sb, doc.Content, "")
	return strings.TrimRight(sb.String(), "\n") + "\n"
}

// mdBlocks renders sibling blocks separated by blank lines; prefix is
// prepended to every output line (blockquote / list indentation is handled
// by the callers re-prefixing rendered text, so prefix is only ""-used here).
func mdBlocks(sb *strings.Builder, blocks []Node, _ string) {
	for i, b := range blocks {
		s := mdBlock(b)
		if s == "" && b.Type != "paragraph" {
			continue
		}
		if i > 0 && sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(s)
		sb.WriteString("\n")
	}
}

func mdBlock(n Node) string {
	switch n.Type {
	case "paragraph":
		return mdInline(n.Content)
	case "heading":
		lvl := n.attrInt("level", 1)
		if lvl < 1 {
			lvl = 1
		}
		if lvl > 6 {
			lvl = 6
		}
		return strings.Repeat("#", lvl) + " " + mdInline(n.Content)
	case "bulletList", "orderedList", "taskList":
		return mdList(n)
	case "blockquote":
		var inner strings.Builder
		mdBlocks(&inner, n.Content, "")
		return prefixLines(strings.TrimRight(inner.String(), "\n"), "> ", "> ")
	case "codeBlock":
		return mdCode(n)
	case "horizontalRule":
		return "---"
	case "table":
		return mdTable(n)
	case "image":
		return mdInline([]Node{n})
	default:
		// Unknown container: render children as blocks (or inline).
		if len(n.Content) == 0 {
			return ""
		}
		if isInlineType(n.Content[0].Type) {
			return mdInline(n.Content)
		}
		var inner strings.Builder
		mdBlocks(&inner, n.Content, "")
		return strings.TrimRight(inner.String(), "\n")
	}
}

func mdCode(n Node) string {
	var text strings.Builder
	for _, c := range n.Content {
		text.WriteString(c.Text)
	}
	body := text.String()
	fence := "```"
	for strings.Contains(body, fence) {
		fence += "`"
	}
	lang := n.attrString("language")
	// A language is an info string: drop anything that isn't a plain token.
	lang = strings.Map(func(r rune) rune {
		if r == '`' || r == '\n' || r == '\r' || r == ' ' {
			return -1
		}
		return r
	}, lang)
	return fence + lang + "\n" + strings.TrimRight(body, "\n") + "\n" + fence
}

func mdList(n Node) string {
	var lines []string
	start := n.attrInt("start", 1)
	for i, item := range n.Content {
		marker := "- "
		switch n.Type {
		case "orderedList":
			marker = strconv.Itoa(start+i) + ". "
		case "taskList":
			marker = "- [ ] "
			if checked, _ := item.Attrs["checked"].(bool); checked {
				marker = "- [x] "
			}
		}
		var parts []string
		for j, b := range item.Content {
			s := mdBlock(b)
			if j > 0 {
				// Tight when a nested list follows; loose between other blocks.
				if t := b.Type; t == "bulletList" || t == "orderedList" || t == "taskList" {
					parts = append(parts, "\n"+s)
					continue
				}
				parts = append(parts, "\n\n"+s)
				continue
			}
			parts = append(parts, s)
		}
		body := strings.Join(parts, "")
		lines = append(lines, prefixLines(body, marker, strings.Repeat(" ", len(marker))))
	}
	return strings.Join(lines, "\n")
}

func prefixLines(s, first, rest string) string {
	if s == "" {
		return strings.TrimRight(first, " ")
	}
	parts := strings.Split(s, "\n")
	for i, p := range parts {
		pre := rest
		if i == 0 {
			pre = first
		}
		if p == "" {
			parts[i] = strings.TrimRight(pre, " ")
		} else {
			parts[i] = pre + p
		}
	}
	return strings.Join(parts, "\n")
}

func mdTable(n Node) string {
	var rows [][]string
	cols := 0
	for _, row := range n.Content {
		var cells []string
		for _, cell := range row.Content {
			var parts []string
			for _, b := range cell.Content {
				if s := strings.TrimSpace(mdBlock(b)); s != "" {
					parts = append(parts, s)
				}
			}
			c := strings.ReplaceAll(strings.Join(parts, "<br>"), "\n", "<br>")
			cells = append(cells, strings.ReplaceAll(c, "|", `\|`))
		}
		if len(cells) > cols {
			cols = len(cells)
		}
		rows = append(rows, cells)
	}
	if len(rows) == 0 || cols == 0 {
		return ""
	}
	line := func(cells []string) string {
		for len(cells) < cols {
			cells = append(cells, "")
		}
		return "| " + strings.Join(cells, " | ") + " |"
	}
	out := []string{line(rows[0])}
	sep := make([]string, cols)
	for i := range sep {
		sep[i] = "---"
	}
	out = append(out, line(sep))
	for _, r := range rows[1:] {
		out = append(out, line(r))
	}
	return strings.Join(out, "\n")
}

func mdInline(nodes []Node) string {
	var sb strings.Builder
	for _, n := range nodes {
		switch n.Type {
		case "text":
			sb.WriteString(mdText(n))
		case "hardBreak":
			sb.WriteString("  \n")
		case "image":
			alt := mdEscape(n.attrString("alt"))
			sb.WriteString("![" + alt + "](" + mdImageTarget(n) + ")")
		default:
			sb.WriteString(mdInline(n.Content))
		}
	}
	return sb.String()
}

func mdImageTarget(n Node) string {
	if id := n.AttachmentID(); id != "" {
		return "attachment:" + id
	}
	if src := n.attrString("src"); safeURL(src, false) {
		return strings.NewReplacer(" ", "%20", "(", "%28", ")", "%29").Replace(src)
	}
	return ""
}

func mdText(n Node) string {
	text := n.Text
	var bold, italic, strike, code bool
	href := ""
	for _, m := range n.Marks {
		switch m.Type {
		case "bold", "strong":
			bold = true
		case "italic", "em":
			italic = true
		case "strike", "s":
			strike = true
		case "code":
			code = true
		case "link":
			if h := m.attrString("href"); safeURL(h, true) {
				href = h
			}
		}
	}
	// Keep surrounding whitespace outside the emphasis markers, which
	// Markdown would otherwise not recognise ("** bold **").
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return text
	}
	lead := text[:len(text)-len(strings.TrimLeft(text, " \t"))]
	trail := text[len(strings.TrimRight(text, " \t")):]
	var core string
	if code {
		// No line breaks inside a code span: a blank line would end the
		// paragraph and the next line could start a raw-HTML block.
		core = "`" + strings.NewReplacer("`", "'", "\r", " ", "\n", " ").Replace(trimmed) + "`"
	} else {
		core = mdEscape(trimmed)
	}
	if bold {
		core = "**" + core + "**"
	}
	if italic {
		core = "_" + core + "_"
	}
	if strike {
		core = "~~" + core + "~~"
	}
	if href != "" {
		core = "[" + core + "](" + strings.NewReplacer(" ", "%20", "(", "%28", ")", "%29").Replace(href) + ")"
	}
	return lead + core + trail
}

var mdEscaper = strings.NewReplacer(`\`, `\\`, "`", "\\`", "*", `\*`, "_", `\_`, "[", `\[`, "]", `\]`, "<", `\<`)

func mdEscape(s string) string { return mdEscaper.Replace(s) }
