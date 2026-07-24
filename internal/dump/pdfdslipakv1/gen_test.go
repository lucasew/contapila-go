package pdfdslipakv1

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/lucasew/contapila-go/internal/dump"
)

// regenerate golden: go test ./internal/dump/pdfdslipakv1 -run TestWriteGolden -count=1
func TestWriteGolden(t *testing.T) {
	if os.Getenv("WRITE_GOLDEN") != "1" {
		t.Skip("set WRITE_GOLDEN=1 to refresh testdata/sample.json")
	}
	path := filepath.Join("testdata", "sample.pdf")
	got, err := Extract(path, dump.Options{})
	if err != nil {
		t.Fatal(err)
	}
	got.Source = "sample.pdf"
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("testdata", "sample.json"), append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}
