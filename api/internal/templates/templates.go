// Package templates holds the built-in page templates (SPEC.md Decision 22:
// built-in only for v1; custom per-space templates are deferred to v2).
// Content is ProseMirror/Tiptap JSON restricted to StarterKit nodes, so it
// loads in the editor without any extension beyond the baseline.
package templates

import "encoding/json"

type Template struct {
	Key          string          `json:"key"`
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	DefaultTitle string          `json:"default_title"`
	Content      json.RawMessage `json:"content"`
}

type node = map[string]any

func doc(content ...node) json.RawMessage {
	b, err := json.Marshal(node{"type": "doc", "content": content})
	if err != nil {
		panic(err) // static data
	}
	return b
}

func text(s string) node { return node{"type": "text", "text": s} }

func para(s string) node {
	if s == "" {
		return node{"type": "paragraph"}
	}
	return node{"type": "paragraph", "content": []node{text(s)}}
}

func h(level int, s string) node {
	return node{"type": "heading", "attrs": node{"level": level}, "content": []node{text(s)}}
}

func bullets(items ...string) node {
	lis := make([]node, len(items))
	for i, it := range items {
		lis[i] = node{"type": "listItem", "content": []node{para(it)}}
	}
	return node{"type": "bulletList", "content": lis}
}

var builtin = []Template{
	{
		Key: "blank", Name: "Blank page", Description: "An empty page.", DefaultTitle: "Untitled",
		Content: doc(para("")),
	},
	{
		Key: "meeting-notes", Name: "Meeting notes", Description: "Agenda, notes, decisions and action items.",
		DefaultTitle: "Meeting notes",
		Content: doc(
			h(2, "Details"),
			bullets("Date: ", "Attendees: ", "Facilitator: "),
			h(2, "Agenda"),
			bullets("Topic 1", "Topic 2"),
			h(2, "Notes"),
			para(""),
			h(2, "Decisions"),
			bullets("Decision"),
			h(2, "Action items"),
			bullets("Owner - action - due date"),
		),
	},
	{
		Key: "spec-rfc", Name: "Spec / RFC", Description: "Problem, goals, proposal, alternatives and open questions.",
		DefaultTitle: "RFC: ",
		Content: doc(
			h(2, "Summary"),
			para("One paragraph describing the proposal."),
			h(2, "Motivation"),
			para("What problem does this solve, and for whom?"),
			h(2, "Goals and non-goals"),
			bullets("Goal", "Non-goal"),
			h(2, "Proposal"),
			para(""),
			h(2, "Alternatives considered"),
			bullets("Alternative and why not"),
			h(2, "Open questions"),
			bullets("Question"),
		),
	},
	{
		Key: "decision-record", Name: "Decision record", Description: "Context, decision and consequences.",
		DefaultTitle: "Decision: ",
		Content: doc(
			h(2, "Status"),
			para("Proposed"),
			h(2, "Context"),
			para(""),
			h(2, "Decision"),
			para(""),
			h(2, "Consequences"),
			bullets("Positive", "Negative"),
		),
	},
	{
		Key: "how-to", Name: "How-to guide", Description: "Prerequisites and step-by-step instructions.",
		DefaultTitle: "How to ",
		Content: doc(
			h(2, "Overview"),
			para(""),
			h(2, "Prerequisites"),
			bullets("Requirement"),
			h(2, "Steps"),
			node{"type": "orderedList", "attrs": node{"start": 1}, "content": []node{
				{"type": "listItem", "content": []node{para("First step")}},
				{"type": "listItem", "content": []node{para("Second step")}},
			}},
			h(2, "Troubleshooting"),
			para(""),
		),
	},
}

// All returns the built-in templates in display order.
func All() []Template { return builtin }

// Get returns the template with key, if any.
func Get(key string) (Template, bool) {
	for _, t := range builtin {
		if t.Key == key {
			return t, true
		}
	}
	return Template{}, false
}
