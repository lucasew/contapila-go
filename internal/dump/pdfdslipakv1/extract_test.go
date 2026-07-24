package pdfdslipakv1

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/lucasew/contapila-go/internal/dump"
)

func TestExtractSample(t *testing.T) {
	path := filepath.Join("testdata", "sample.pdf")
	got, err := Extract(path, dump.Options{})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	got.Source = "sample.pdf"

	wantBytes, err := os.ReadFile(filepath.Join("testdata", "sample.json"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var want dump.ExtractedData
	if err := json.Unmarshal(wantBytes, &want); err != nil {
		t.Fatalf("unmarshal golden: %v", err)
	}

	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("dump mismatch\n got: %s\nwant: %s", gotJSON, wantJSON)
	}
}

func TestDialectRegistered(t *testing.T) {
	if _, ok := dump.Lookup(Dialect); !ok {
		t.Fatalf("dialect %q not registered", Dialect)
	}
}
