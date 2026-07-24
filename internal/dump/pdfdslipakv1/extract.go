// Package pdfdslipakv1 implements dialect pdf-dslipak-v1.
//
// # Grammar (open-ended depth)
//
// The tree follows the PDF page object and content-stream structure, not
// type-bucketed leaves. Nodes are nested as the stream nests (q/Q, BT/ET,
// BMC/BDC/EMC). Children stay in stream order.
//
//	document
//	  page { number, mediabox?, cropbox? }
//	    content { index? }          # one per Contents stream
//	      gsave                     # q … Q
//	      text                      # BT … ET
//	        show { s }              # Tj / ' / "  (full string, not per-glyph)
//	        show                    # TJ
//	          str { s } | kern { n }
//	        op { name, args? }      # Tf, Td, Tm, …
//	      marked { tag, props? }    # BMC/BDC … EMC
//	      rect { x, y, w, h }       # re
//	      op { name, args? }        # other operators
//	      xobject { name }          # Do
//
// Depth is unbounded: groups nest arbitrarily. There is no separate “collect
// all text then all rects” pass.
package pdfdslipakv1

import (
	"fmt"
	"os"

	"github.com/dslipak/pdf"
	"github.com/lucasew/contapila-go/internal/dump"
)

// Dialect is the CLI/JSON id for this extractor.
const Dialect = "pdf-dslipak-v1"

func init() {
	dump.Register(Dialect, Extract)
}

// Extract opens path and returns the document element tree.
func Extract(path string, opts dump.Options) (dump.ExtractedData, error) {
	r, err := openPDF(path, opts.Password)
	if err != nil {
		return dump.ExtractedData{}, fmt.Errorf("open pdf: %w", err)
	}

	doc := &tnode{Type: "document"}
	n := r.NumPage()
	for i := 1; i <= n; i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			return dump.ExtractedData{}, fmt.Errorf("page %d: missing", i)
		}
		page, err := extractPage(p, i)
		if err != nil {
			return dump.ExtractedData{}, err
		}
		doc.Kids = append(doc.Kids, page)
	}

	return dump.ExtractedData{
		Dialect: Dialect,
		Source:  path,
		Data:    doc.toDump(),
	}, nil
}

func extractPage(p pdf.Page, num int) (*tnode, error) {
	page := &tnode{
		Type:  "page",
		Props: map[string]any{"number": num},
	}
	if box := rectProps(p.V.Key("MediaBox")); box != nil {
		page.Props["mediabox"] = box
	}
	if box := rectProps(p.V.Key("CropBox")); box != nil {
		page.Props["cropbox"] = box
	}

	strm := p.V.Key("Contents")
	if strm.IsNull() {
		return page, nil
	}

	// Single stream: Len()==0 for non-array in this library; array has Len()>0.
	if strm.Kind() == pdf.Array || strm.Len() > 0 {
		for i := 0; i < strm.Len(); i++ {
			cnode := &tnode{
				Type:  "content",
				Props: map[string]any{"index": i},
			}
			if err := walkContent(strm.Index(i), cnode); err != nil {
				return nil, fmt.Errorf("page %d content %d: %w", num, i, err)
			}
			page.Kids = append(page.Kids, cnode)
		}
		return page, nil
	}

	cnode := &tnode{Type: "content"}
	if err := walkContent(strm, cnode); err != nil {
		return nil, fmt.Errorf("page %d content: %w", num, err)
	}
	page.Kids = append(page.Kids, cnode)
	return page, nil
}

func walkContent(strm pdf.Value, root *tnode) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("content interpret: %v", rec)
		}
	}()

	b := &builder{stack: []*tnode{root}}

	pdf.Interpret(strm, func(stk *pdf.Stack, op string) {
		args := popArgs(stk)
		switch op {
		case "q":
			b.push(&tnode{Type: "gsave"})
		case "Q":
			b.popUntil("gsave")
		case "BT":
			b.push(&tnode{Type: "text"})
		case "ET":
			b.popUntil("text")
		case "BMC":
			tag := ""
			if len(args) >= 1 {
				tag = nameOrString(args[0])
			}
			b.push(&tnode{Type: "marked", Props: map[string]any{"tag": tag}})
		case "BDC":
			tag := ""
			var props any
			if len(args) >= 1 {
				tag = nameOrString(args[0])
			}
			if len(args) >= 2 {
				props = valueJSON(args[1])
			}
			p := map[string]any{"tag": tag}
			if props != nil {
				p["props"] = props
			}
			b.push(&tnode{Type: "marked", Props: p})
		case "EMC":
			b.popUntil("marked")
		case "Tj", "'", "\"":
			s := ""
			if len(args) >= 1 {
				s = pdfString(args[len(args)-1])
			}
			// ' and " also take spacing numbers; keep full arg list on the node.
			n := &tnode{Type: "show", Props: map[string]any{"s": s}}
			if op != "Tj" {
				n.Props["op"] = op
				if len(args) > 1 {
					n.Props["args"] = valuesJSON(args[:len(args)-1])
				}
			}
			b.leaf(n)
		case "TJ":
			show := &tnode{Type: "show", Props: map[string]any{"op": "TJ"}}
			if len(args) >= 1 && args[0].Kind() == pdf.Array {
				arr := args[0]
				for i := 0; i < arr.Len(); i++ {
					el := arr.Index(i)
					switch el.Kind() {
					case pdf.String:
						show.Kids = append(show.Kids, &tnode{
							Type:  "str",
							Props: map[string]any{"s": pdfString(el)},
						})
					case pdf.Integer, pdf.Real:
						show.Kids = append(show.Kids, &tnode{
							Type:  "kern",
							Props: map[string]any{"n": numValue(el)},
						})
					default:
						show.Kids = append(show.Kids, &tnode{
							Type:  "item",
							Props: map[string]any{"value": valueJSON(el)},
						})
					}
				}
			}
			b.leaf(show)
		case "re":
			if len(args) >= 4 {
				b.leaf(&tnode{
					Type: "rect",
					Props: map[string]any{
						"x": numValue(args[0]),
						"y": numValue(args[1]),
						"w": numValue(args[2]),
						"h": numValue(args[3]),
					},
				})
			} else {
				b.leaf(opNode(op, args))
			}
		case "Do":
			name := ""
			if len(args) >= 1 {
				name = nameOrString(args[0])
			}
			b.leaf(&tnode{Type: "xobject", Props: map[string]any{"name": name}})
		default:
			b.leaf(opNode(op, args))
		}
	})
	return err
}

func opNode(op string, args []pdf.Value) *tnode {
	n := &tnode{Type: "op", Props: map[string]any{"name": op}}
	if len(args) > 0 {
		n.Props["args"] = valuesJSON(args)
	}
	return n
}

// --- tree builder ---

type tnode struct {
	Type  string
	Props map[string]any
	Kids  []*tnode
}

func (t *tnode) toDump() dump.Node {
	n := dump.Node{Type: t.Type, Props: t.Props}
	for _, k := range t.Kids {
		n.Children = append(n.Children, k.toDump())
	}
	return n
}

type builder struct {
	stack []*tnode
}

func (b *builder) cur() *tnode {
	return b.stack[len(b.stack)-1]
}

func (b *builder) push(n *tnode) {
	b.cur().Kids = append(b.cur().Kids, n)
	b.stack = append(b.stack, n)
}

func (b *builder) leaf(n *tnode) {
	b.cur().Kids = append(b.cur().Kids, n)
}

func (b *builder) popUntil(typ string) {
	for len(b.stack) > 1 {
		top := b.stack[len(b.stack)-1]
		b.stack = b.stack[:len(b.stack)-1]
		if top.Type == typ {
			return
		}
	}
}

// --- value helpers ---

func popArgs(stk *pdf.Stack) []pdf.Value {
	n := stk.Len()
	args := make([]pdf.Value, n)
	for i := n - 1; i >= 0; i-- {
		args[i] = stk.Pop()
	}
	return args
}

func pdfString(v pdf.Value) string {
	if v.Kind() != pdf.String {
		return nameOrString(v)
	}
	if t := v.Text(); t != "" {
		return t
	}
	return v.RawString()
}

func nameOrString(v pdf.Value) string {
	switch v.Kind() {
	case pdf.Name:
		return v.Name()
	case pdf.String:
		return pdfString(v)
	default:
		return v.String()
	}
}

func numValue(v pdf.Value) any {
	switch v.Kind() {
	case pdf.Integer:
		return v.Int64()
	case pdf.Real:
		return v.Float64()
	default:
		return valueJSON(v)
	}
}

func valuesJSON(args []pdf.Value) []any {
	out := make([]any, len(args))
	for i, a := range args {
		out[i] = valueJSON(a)
	}
	return out
}

func valueJSON(v pdf.Value) any {
	switch v.Kind() {
	case pdf.Null:
		return nil
	case pdf.Bool:
		return v.Bool()
	case pdf.Integer:
		return v.Int64()
	case pdf.Real:
		return v.Float64()
	case pdf.String:
		return pdfString(v)
	case pdf.Name:
		return v.Name()
	case pdf.Array:
		out := make([]any, v.Len())
		for i := 0; i < v.Len(); i++ {
			out[i] = valueJSON(v.Index(i))
		}
		return out
	case pdf.Dict:
		keys := v.Keys()
		out := make(map[string]any, len(keys))
		for _, k := range keys {
			out[k] = valueJSON(v.Key(k))
		}
		return out
	default:
		return v.String()
	}
}

func rectProps(v pdf.Value) []any {
	if v.IsNull() || v.Kind() != pdf.Array || v.Len() < 4 {
		return nil
	}
	out := make([]any, 4)
	for i := 0; i < 4; i++ {
		out[i] = numValue(v.Index(i))
	}
	return out
}

func openPDF(path, password string) (*pdf.Reader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	// NewReaderEncrypted tries empty owner/user first, then calls pw until "".
	triedUser := false
	pw := func() string {
		if password == "" || triedUser {
			return ""
		}
		triedUser = true
		return password
	}
	r, err := pdf.NewReaderEncrypted(f, fi.Size(), pw)
	if err != nil {
		f.Close()
		return nil, err
	}
	// Reader keeps f alive (same as pdf.Open); no explicit close API.
	return r, nil
}
