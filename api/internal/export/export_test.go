package export

import (
	"strings"
	"testing"
)

const sample = `{"type":"doc","content":[
 {"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Title <b>"}]},
 {"type":"paragraph","content":[
   {"type":"text","text":"plain "},
   {"type":"text","text":"bold","marks":[{"type":"bold"}]},
   {"type":"text","text":" and "},
   {"type":"text","text":"link","marks":[{"type":"link","attrs":{"href":"https://example.com/a b"}}]},
   {"type":"text","text":"evil","marks":[{"type":"link","attrs":{"href":"javascript:alert(1)"}}]}
 ]},
 {"type":"bulletList","content":[
   {"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"one"}]}]},
   {"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"two"}]},
     {"type":"orderedList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"nested"}]}]}]}]}
 ]},
 {"type":"codeBlock","attrs":{"language":"mermaid"},"content":[{"type":"text","text":"graph TD; A-->B"}]},
 {"type":"image","attrs":{"attachmentId":"11111111-1111-1111-1111-111111111111","alt":"pic"}},
 {"type":"table","content":[
   {"type":"tableRow","content":[{"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"H1"}]}]},{"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"H2"}]}]}]},
   {"type":"tableRow","content":[{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"a|b"}]}]},{"type":"tableCell","content":[{"type":"paragraph"}]}]}
 ]}
]}`

func TestMarkdown(t *testing.T) {
	md := Markdown(Parse([]byte(sample)))
	for _, want := range []string{
		"## Title \\<b>",
		"plain **bold** and [link](https://example.com/a%20b)evil",
		"- one",
		"- two\n  1. nested",
		"```mermaid\ngraph TD; A-->B\n```",
		"![pic](attachment:11111111-1111-1111-1111-111111111111)",
		"| H1 | H2 |\n| --- | --- |\n| a\\|b |  |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q in:\n%s", want, md)
		}
	}
	if strings.Contains(md, "javascript:") {
		t.Error("unsafe link leaked into markdown")
	}
}

func TestHTMLEscapesAndEmbeds(t *testing.T) {
	doc := Parse([]byte(sample))
	body := HTMLBody(doc, func(id string) string { return "data:image/png;base64,AAAA" })
	for _, want := range []string{
		"<h2>Title &lt;b&gt;</h2>",
		"<strong>bold</strong>",
		`<a href="https://example.com/a b" rel="noopener noreferrer">link</a>`,
		`<img src="data:image/png;base64,AAAA" alt="pic">`,
		"<th><p>H1</p>",
		`<code class="language-mermaid">graph TD; A--&gt;B</code>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("html missing %q in:\n%s", want, body)
		}
	}
	if strings.Contains(body, "javascript:") || strings.Contains(strings.ToLower(body), "<script") {
		t.Errorf("unsafe content in html:\n%s", body)
	}
	if got := HTMLBody(doc, nil); !strings.Contains(got, "missing-image") {
		t.Error("unresolved image should leave a placeholder")
	}
	full := HTMLDocument("T<", []PageBody{{Title: "P", HTML: body}}, true)
	if !strings.Contains(full, "Content-Security-Policy") || !strings.Contains(full, "<title>T&lt;</title>") {
		t.Error("document must carry CSP and escaped title")
	}
}

func TestMalformedDocIsEmpty(t *testing.T) {
	if got := Markdown(Parse([]byte("not json"))); strings.TrimSpace(got) != "" {
		t.Errorf("got %q", got)
	}
}

func TestMarkdownCodeSpanCannotBreakOutOfTheParagraph(t *testing.T) {
	doc := Parse([]byte(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"x\n\n<script>alert(1)</script>","marks":[{"type":"code"}]}]}]}`))
	md := Markdown(doc)
	if strings.Contains(md, "\n\n") || strings.Contains(strings.TrimSpace(md), "\n") {
		t.Fatalf("code span must stay on one line: %q", md)
	}
}
