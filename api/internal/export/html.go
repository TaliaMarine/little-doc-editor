package export

import (
	"html"
	"net/url"
	"strings"
)

// ImageResolver maps an attachment ID to a data: URI to embed, or "" to
// leave a placeholder. Implementations enforce authorization/scoping.
type ImageResolver func(attachmentID string) string

// HTMLBody renders doc as an HTML fragment.
func HTMLBody(doc Node, images ImageResolver) string {
	var sb strings.Builder
	for _, b := range doc.Content {
		htmlNode(&sb, b, images)
	}
	return sb.String()
}

func esc(s string) string { return html.EscapeString(s) }

// safeURL allows http(s) (and mailto/tel when allowMail) plus relative and
// fragment references; everything else (javascript:, data:, vbscript: ...)
// is dropped.
func safeURL(raw string, allowMail bool) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return true
	case "mailto", "tel":
		return allowMail
	case "":
		return !strings.ContainsAny(raw, "\\\r\n\t") && !strings.HasPrefix(raw, "//")
	}
	return false
}

func htmlNode(sb *strings.Builder, n Node, images ImageResolver) {
	children := func() {
		for _, c := range n.Content {
			htmlNode(sb, c, images)
		}
	}
	wrap := func(tag string) {
		sb.WriteString("<" + tag + ">")
		children()
		sb.WriteString("</" + tag + ">\n")
	}
	switch n.Type {
	case "text":
		htmlText(sb, n)
	case "hardBreak":
		sb.WriteString("<br>")
	case "paragraph":
		wrap("p")
	case "heading":
		lvl := n.attrInt("level", 1)
		if lvl < 1 {
			lvl = 1
		}
		if lvl > 6 {
			lvl = 6
		}
		wrap("h" + string(rune('0'+lvl)))
	case "bulletList", "taskList":
		wrap("ul")
	case "orderedList":
		if start := n.attrInt("start", 1); start != 1 {
			sb.WriteString("<ol start=\"" + itoa(start) + "\">")
			children()
			sb.WriteString("</ol>\n")
		} else {
			wrap("ol")
		}
	case "listItem":
		wrap("li")
	case "taskItem":
		box := "&#9744; "
		if checked, _ := n.Attrs["checked"].(bool); checked {
			box = "&#9745; "
		}
		sb.WriteString("<li>" + box)
		children()
		sb.WriteString("</li>\n")
	case "blockquote":
		wrap("blockquote")
	case "horizontalRule":
		sb.WriteString("<hr>\n")
	case "codeBlock":
		var text strings.Builder
		for _, c := range n.Content {
			text.WriteString(c.Text)
		}
		cls := ""
		if lang := n.attrString("language"); lang != "" {
			cls = ` class="language-` + esc(strings.Map(func(r rune) rune {
				if r == ' ' || r == '"' || r == '<' || r == '>' || r == '&' || r == '\'' {
					return -1
				}
				return r
			}, lang)) + `"`
		}
		sb.WriteString("<pre><code" + cls + ">" + esc(text.String()) + "</code></pre>\n")
	case "table":
		sb.WriteString("<table>\n")
		children()
		sb.WriteString("</table>\n")
	case "tableRow":
		wrap("tr")
	case "tableHeader":
		wrap("th")
	case "tableCell":
		wrap("td")
	case "image":
		alt := esc(n.attrString("alt"))
		if id := n.AttachmentID(); id != "" {
			if images != nil {
				if uri := images(id); uri != "" {
					sb.WriteString(`<img src="` + esc(uri) + `" alt="` + alt + `">`)
					return
				}
			}
			sb.WriteString(`<span class="missing-image">[image` + func() string {
				if alt != "" {
					return ": " + alt
				}
				return ""
			}() + `]</span>`)
			return
		}
		if src := n.attrString("src"); safeURL(src, false) {
			sb.WriteString(`<img src="` + esc(src) + `" alt="` + alt + `">`)
		}
	default:
		children()
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

func htmlText(sb *strings.Builder, n Node) {
	var open, closeTags []string
	for _, m := range n.Marks {
		switch m.Type {
		case "bold", "strong":
			open, closeTags = append(open, "<strong>"), append([]string{"</strong>"}, closeTags...)
		case "italic", "em":
			open, closeTags = append(open, "<em>"), append([]string{"</em>"}, closeTags...)
		case "strike", "s":
			open, closeTags = append(open, "<s>"), append([]string{"</s>"}, closeTags...)
		case "underline":
			open, closeTags = append(open, "<u>"), append([]string{"</u>"}, closeTags...)
		case "code":
			open, closeTags = append(open, "<code>"), append([]string{"</code>"}, closeTags...)
		case "link":
			if href := m.attrString("href"); safeURL(href, true) {
				open = append(open, `<a href="`+esc(href)+`" rel="noopener noreferrer">`)
				closeTags = append([]string{"</a>"}, closeTags...)
			}
		}
	}
	sb.WriteString(strings.Join(open, ""))
	sb.WriteString(esc(n.Text))
	sb.WriteString(strings.Join(closeTags, ""))
}

const baseCSS = `body{font-family:-apple-system,"Segoe UI",Roboto,Helvetica,Arial,sans-serif;line-height:1.55;color:#1d1d1f;max-width:48rem;margin:2rem auto;padding:0 1rem}
h1,h2,h3,h4{line-height:1.25}pre{background:#f5f5f7;padding:.75rem;overflow:auto;border-radius:4px}code{font-family:ui-monospace,Menlo,Consolas,monospace;font-size:.92em}
blockquote{margin:1em 0;padding-left:1em;border-left:3px solid #ccc;color:#555}table{border-collapse:collapse;width:100%}th,td{border:1px solid #ccc;padding:.35rem .5rem;text-align:left;vertical-align:top}
img{max-width:100%}.missing-image{color:#888}`

const printCSS = `@page{margin:18mm}
@media print{body{max-width:none;margin:0;padding:0}.page{break-before:page}.page:first-child{break-before:auto}pre,table,img,blockquote{break-inside:avoid}pre{white-space:pre-wrap;word-break:break-word}a{color:inherit;text-decoration:none}}`

// csp blocks scripts and network access other than images, as a second line
// of defense for the exported document (the meta tag survives blob: URLs,
// which a response header would not).
const csp = `default-src 'none'; img-src data: http: https:; style-src 'unsafe-inline'`

// HTMLDocument wraps rendered bodies into a complete, self-contained page.
// Each body is one exported page, starting on a new printed page.
func HTMLDocument(title string, bodies []PageBody, printable bool) string {
	var sb strings.Builder
	sb.WriteString("<!doctype html>\n<html lang=\"en\"><head><meta charset=\"utf-8\">")
	sb.WriteString(`<meta http-equiv="Content-Security-Policy" content="` + esc(csp) + `">`)
	sb.WriteString(`<meta name="viewport" content="width=device-width,initial-scale=1">`)
	sb.WriteString("<title>" + esc(title) + "</title><style>" + baseCSS)
	if printable {
		sb.WriteString(printCSS)
	}
	sb.WriteString("</style></head><body>\n")
	for _, b := range bodies {
		sb.WriteString(`<section class="page"><h1>` + esc(b.Title) + "</h1>\n" + b.HTML + "</section>\n")
	}
	sb.WriteString("</body></html>\n")
	return sb.String()
}

type PageBody struct {
	Title string
	HTML  string
}
