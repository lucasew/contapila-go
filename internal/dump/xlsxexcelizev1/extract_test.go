package xlsxexcelizev1

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/lucasew/contapila-go/internal/dump"
)

func TestExtractSample(t *testing.T) {
	path := filepath.Join("testdata", "sample.xlsx")
	got, err := Extract(path, dump.Options{})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	got.Source = "sample.xlsx"

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

func TestTypedCellsPresent(t *testing.T) {
	got, err := Extract(filepath.Join("testdata", "sample.xlsx"), dump.Options{})
	if err != nil {
		t.Fatal(err)
	}
	// Walk for date cell and formula cell
	var sawDate, sawFormula, sawLink, sawComment bool
	var walk func(n dump.Node)
	walk = func(n dump.Node) {
		if n.Type == "cell" {
			if n.Props["type"] == "date" {
				sawDate = true
			}
			if _, ok := n.Props["formula"]; ok {
				sawFormula = true
			}
			if _, ok := n.Props["hyperlink"]; ok {
				sawLink = true
			}
			if _, ok := n.Props["comment"]; ok {
				sawComment = true
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(got.Data)
	if !sawDate || !sawFormula || !sawLink || !sawComment {
		t.Fatalf("expected date/formula/link/comment cells; date=%v formula=%v link=%v comment=%v",
			sawDate, sawFormula, sawLink, sawComment)
	}
}
