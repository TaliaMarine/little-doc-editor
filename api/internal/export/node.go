// Package export renders ProseMirror/Tiptap JSON (Decision 4) to Markdown
// and HTML (Decision 13, §6.9) on the server. The renderers are tolerant:
// unknown node types render their children, unknown marks are ignored, and
// malformed documents yield empty output rather than an error — export must
// never be blocked by one odd node. All text and attribute values are
// escaped; no script or event-handler attribute is ever emitted.
package export

import (
	"encoding/json"
	"strings"
)

type Mark struct {
	Type  string         `json:"type"`
	Attrs map[string]any `json:"attrs"`
}

type Node struct {
	Type    string         `json:"type"`
	Attrs   map[string]any `json:"attrs"`
	Content []Node         `json:"content"`
	Text    string         `json:"text"`
	Marks   []Mark         `json:"marks"`
}

// Parse decodes a ProseMirror document; malformed input yields an empty doc.
func Parse(content []byte) Node {
	var n Node
	if err := json.Unmarshal(content, &n); err != nil {
		return Node{Type: "doc"}
	}
	return n
}

func (n Node) attrString(key string) string {
	if s, ok := n.Attrs[key].(string); ok {
		return s
	}
	return ""
}

func (n Node) attrInt(key string, def int) int {
	switch v := n.Attrs[key].(type) {
	case float64:
		return int(v)
	}
	return def
}

func (m Mark) attrString(key string) string {
	if s, ok := m.Attrs[key].(string); ok {
		return s
	}
	return ""
}

// AttachmentID returns the attachment an image node refers to (Decision 34:
// inline images store the attachment ID, never a URL). Accepts the attribute
// spellings the editor might use, or an "attachment:<id>" src.
func (n Node) AttachmentID() string {
	for _, k := range []string{"attachmentId", "attachmentID", "attachment_id"} {
		if s := n.attrString(k); s != "" {
			return s
		}
	}
	if src := n.attrString("src"); strings.HasPrefix(src, "attachment:") {
		return strings.TrimPrefix(src, "attachment:")
	}
	return ""
}

// AttachmentIDs lists every attachment referenced by image nodes in doc.
func AttachmentIDs(doc Node) []string {
	var out []string
	var walk func(Node)
	walk = func(n Node) {
		if n.Type == "image" {
			if id := n.AttachmentID(); id != "" {
				out = append(out, id)
			}
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	walk(doc)
	return out
}

func isInlineType(t string) bool {
	switch t {
	case "text", "hardBreak", "image":
		return true
	}
	return false
}
