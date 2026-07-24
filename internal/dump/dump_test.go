package dump

import (
	"strings"
	"testing"
)

func TestUnknownDialect(t *testing.T) {
	_, err := Extract("no-such-v1", "x", Options{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "unknown dialect") {
		t.Fatalf("got %v", err)
	}
}
