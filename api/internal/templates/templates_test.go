package templates

import (
	"encoding/json"
	"testing"
)

func TestBuiltinTemplatesAreValidDocs(t *testing.T) {
	if _, ok := Get("blank"); !ok {
		t.Fatal("blank template missing")
	}
	seen := map[string]bool{}
	for _, tpl := range All() {
		if seen[tpl.Key] {
			t.Errorf("duplicate key %s", tpl.Key)
		}
		seen[tpl.Key] = true
		var d struct {
			Type    string            `json:"type"`
			Content []json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(tpl.Content, &d); err != nil || d.Type != "doc" || len(d.Content) == 0 {
			t.Errorf("%s: not a non-empty ProseMirror doc: %v", tpl.Key, err)
		}
		if tpl.Name == "" || tpl.DefaultTitle == "" {
			t.Errorf("%s: missing name/title", tpl.Key)
		}
	}
}
