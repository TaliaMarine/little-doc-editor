package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestWriteLookupErrorDistinguishesMissingFromFailure(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	rec := httptest.NewRecorder()
	WriteLookupError(rec, logger, pgx.ErrNoRows, "page not found")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("no rows must be 404, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	WriteLookupError(rec, logger, errors.New("connection refused"), "page not found")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("a real failure must be 500, got %d", rec.Code)
	}
	if body := rec.Body.String(); body == "" || containsAny(body, "connection refused") {
		t.Fatalf("internal error details must not leak: %s", body)
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
	}
	return false
}

func TestOptionalDistinguishesAbsentNullAndValue(t *testing.T) {
	var v struct {
		A Optional[int] `json:"a"`
		B Optional[int] `json:"b"`
		C Optional[int] `json:"c"`
	}
	if err := json.Unmarshal([]byte(`{"a":null,"b":5}`), &v); err != nil {
		t.Fatal(err)
	}
	if !v.A.Set || v.A.Value != nil {
		t.Errorf("null: %+v", v.A)
	}
	if !v.B.Set || v.B.Value == nil || *v.B.Value != 5 {
		t.Errorf("value: %+v", v.B)
	}
	if v.C.Set {
		t.Errorf("absent: %+v", v.C)
	}
	if err := json.Unmarshal([]byte(`{"a":"str"}`), &v); err == nil {
		t.Error("wrong type must error")
	}
}
