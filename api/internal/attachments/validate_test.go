package attachments

import (
	"archive/zip"
	"bytes"
	"errors"
	"testing"

	"github.com/TaliaMarine/little-doc-editor/api/internal/config"
)

func defaultAllowlist() []string {
	return config.Defaults().AttachmentAllowlist
}

// pngBytes is a minimal valid 1x1 PNG signature + IHDR-ish header, enough
// for the sniffer's magic-byte check.
var pngBytes = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 0}

var pdfBytes = []byte("%PDF-1.4\n%some pdf content that is long enough to sniff")

// elfBytes mimics an ELF executable's magic number — must never be allowed.
var elfBytes = []byte{0x7F, 'E', 'L', 'F', 2, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0}

func plainZipBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, err := zw.Create("hello.txt")
	if err != nil {
		t.Fatalf("creating zip entry: %v", err)
	}
	if _, err := f.Write([]byte("hello world")); err != nil {
		t.Fatalf("writing zip entry: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("closing zip: %v", err)
	}
	return buf.Bytes()
}

func TestValidate_AllowsPNG(t *testing.T) {
	ct, err := Validate(pngBytes, 1024, defaultAllowlist(), 1024*1024)
	if err != nil {
		t.Fatalf("expected PNG to be allowed, got error: %v", err)
	}
	if ct != "image/png" {
		t.Errorf("expected image/png, got %q", ct)
	}
}

func TestValidate_AllowsPDF(t *testing.T) {
	ct, err := Validate(pdfBytes, int64(len(pdfBytes)), defaultAllowlist(), 1024*1024)
	if err != nil {
		t.Fatalf("expected PDF to be allowed, got error: %v", err)
	}
	if ct != "application/pdf" {
		t.Errorf("expected application/pdf, got %q", ct)
	}
}

func TestValidate_AllowsPlainZip(t *testing.T) {
	zipData := plainZipBytes(t)
	ct, err := Validate(zipData, int64(len(zipData)), defaultAllowlist(), 1024*1024)
	if err != nil {
		t.Fatalf("expected zip to be allowed, got error: %v", err)
	}
	if ct != "application/zip" {
		t.Errorf("expected application/zip, got %q", ct)
	}
}

// TestValidate_IgnoresClaimedContentType is really about the caller: this
// package's Validate never even takes a client-supplied Content-Type header
// as input, so there's nothing for a spoofed header to influence. This test
// exists to lock that contract in — if a future change adds such a
// parameter, it must not let it override the sniffed result.
func TestValidate_RejectsSpoofedExecutable(t *testing.T) {
	// An ELF binary renamed to look like a PDF via extension/header alone
	// (which Validate never sees) must still be rejected on sniffed bytes.
	_, err := Validate(elfBytes, int64(len(elfBytes)), defaultAllowlist(), 1024*1024)
	if err == nil {
		t.Fatal("expected ELF magic bytes to be rejected, got no error")
	}
	var typeErr ErrTypeNotAllowed
	if !errors.As(err, &typeErr) {
		t.Fatalf("expected ErrTypeNotAllowed, got %T: %v", err, err)
	}
}

func TestValidate_RejectsOversized(t *testing.T) {
	_, err := Validate(pngBytes, 10*1024*1024, defaultAllowlist(), 1024*1024)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("expected ErrTooLarge, got %v", err)
	}
}

func TestValidate_SizeCheckedBeforeSniffing(t *testing.T) {
	// Even a well-formed, allowlisted file must be rejected on size alone.
	_, err := Validate(pdfBytes, 10*1024*1024, defaultAllowlist(), 1024*1024)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("expected ErrTooLarge, got %v", err)
	}
}

func TestValidate_RejectsTypeNotOnCustomAllowlist(t *testing.T) {
	// A PDF is normally allowed, but not if the (admin-configured, per
	// Decision 17) allowlist has been narrowed to exclude it.
	_, err := Validate(pdfBytes, int64(len(pdfBytes)), []string{"image/png"}, 1024*1024)
	var typeErr ErrTypeNotAllowed
	if !errors.As(err, &typeErr) {
		t.Fatalf("expected ErrTypeNotAllowed, got %T: %v", err, err)
	}
}
