package searchindex

import "testing"

func TestExtractPlainText(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "simple paragraph",
			content: `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"hello world"}]}]}`,
			want:    "hello world",
		},
		{
			name:    "multiple text nodes across nested blocks",
			content: `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"first"}]},{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"second"}]}]}]}]}`,
			want:    "first second",
		},
		{
			name:    "empty doc",
			content: `{"type":"doc","content":[]}`,
			want:    "",
		},
		{
			name:    "malformed JSON degrades to empty rather than erroring",
			content: `not json at all`,
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractPlainText([]byte(tt.content))
			if got != tt.want {
				t.Errorf("ExtractPlainText(%s) = %q, want %q", tt.content, got, tt.want)
			}
		})
	}
}
