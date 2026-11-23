// Package attachments implements upload/download of page attachments,
// enforcing SPEC.md Decision 17 (type allowlist + configurable max size,
// both admin-configurable instance settings) and §8 (content-type sniffing,
// not trusting the client-sent MIME type or filename extension).
package attachments

import (
	"errors"
	"fmt"

	"github.com/gabriel-vasile/mimetype"
)

var ErrTooLarge = errors.New("attachment exceeds the configured maximum size")

type ErrTypeNotAllowed struct {
	SniffedType string
}

func (e ErrTypeNotAllowed) Error() string {
	return fmt.Sprintf("content type %q is not on the allowlist", e.SniffedType)
}

// Validate sniffs the real content type from the upload's bytes — never the
// client-supplied Content-Type header or filename extension (SPEC.md §8) —
// and checks it against the allowlist, plus checks size against the
// configured maximum. It returns the sniffed content type on success, since
// that (not whatever the client sent) is what should be stored and served
// back. mimetype is used instead of stdlib http.DetectContentType because
// the latter can't tell an actual .docx/.xlsx/.pptx apart from a plain zip
// (both are zip archives; only mimetype's deeper inspection distinguishes
// them), which matters directly for Decision 17's "common Office formats"
// default allowlist.
func Validate(sniffBytes []byte, size int64, allowlist []string, maxSizeBytes int64) (contentType string, err error) {
	if size > maxSizeBytes {
		return "", ErrTooLarge
	}

	detected := mimetype.Detect(sniffBytes)

	for _, allowed := range allowlist {
		for m := detected; m != nil; m = m.Parent() {
			if m.Is(allowed) {
				return m.String(), nil
			}
		}
	}

	return "", ErrTypeNotAllowed{SniffedType: detected.String()}
}
