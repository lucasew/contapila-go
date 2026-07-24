// Package pdfdslipakv1 implements dialect pdf-dslipak-v1.
//
// # Grammar
//
//	document
//	  page { number }
//	    text { font, font_size, x, y, w, s }
//	    rect { min: {x,y}, max: {x,y} }
//
// Leaves only (content-stream order). No derived row/column views.
// Coordinates are PDF points; Y increases bottom to top (library convention).
package pdfdslipakv1

import (
	"fmt"

	"github.com/dslipak/pdf"
	"github.com/lucasew/contapila-go/internal/dump"
)

// Dialect is the CLI/JSON id for this extractor.
const Dialect = "pdf-dslipak-v1"

func init() {
	dump.Register(Dialect, Extract)
}

// Extract opens path and returns the document element tree.
func Extract(path string) (dump.ExtractedData, error) {
	r, err := pdf.Open(path)
	if err != nil {
		return dump.ExtractedData{}, fmt.Errorf("open pdf: %w", err)
	}

	doc := dump.Node{Type: "document"}
	n := r.NumPage()
	for i := 1; i <= n; i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			return dump.ExtractedData{}, fmt.Errorf("page %d: missing", i)
		}
		content := p.Content()
		page := dump.Node{
			Type:  "page",
			Props: map[string]any{"number": i},
		}
		for _, t := range content.Text {
			page.Children = append(page.Children, dump.Node{
				Type: "text",
				Props: map[string]any{
					"font":      t.Font,
					"font_size": t.FontSize,
					"x":         t.X,
					"y":         t.Y,
					"w":         t.W,
					"s":         t.S,
				},
			})
		}
		for _, rc := range content.Rect {
			page.Children = append(page.Children, dump.Node{
				Type: "rect",
				Props: map[string]any{
					"min": map[string]any{"x": rc.Min.X, "y": rc.Min.Y},
					"max": map[string]any{"x": rc.Max.X, "y": rc.Max.Y},
				},
			})
		}
		doc.Children = append(doc.Children, page)
	}

	return dump.ExtractedData{
		Dialect: Dialect,
		Source:  path,
		Data:    doc,
	}, nil
}
