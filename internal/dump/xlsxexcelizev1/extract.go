// Package xlsxexcelizev1 implements dialect xlsx-excelize-v1.
//
// # Grammar
//
//	workbook
//	  sheet { name, merges?, dimension? }
//	    row { r }
//	      cell {
//	        ref, type, value?, formula?, numfmt?,
//	        hyperlink?: { url }, comment?: { author?, text }
//	      }
//
// Rows and cells are sparse: only non-blank cells, or cells with formula /
// hyperlink / comment. Cell type is the result kind (blank|bool|number|string|date|error).
// Date values are RFC3339 via time.Time; date-only cells use UTC midnight.
// Formula text is separate from the resolved value.
package xlsxexcelizev1

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lucasew/contapila-go/internal/dump"
	"github.com/xuri/excelize/v2"
)

// Dialect is the CLI/JSON id for this extractor.
const Dialect = "xlsx-excelize-v1"

func init() {
	dump.Register(Dialect, Extract)
}

// Extract opens path and returns the workbook element tree.
func Extract(path string, opts dump.Options) (dump.ExtractedData, error) {
	var openOpts excelize.Options
	if opts.Password != "" {
		openOpts.Password = opts.Password
	}
	f, err := excelize.OpenFile(path, openOpts)
	if err != nil {
		return dump.ExtractedData{}, fmt.Errorf("open xlsx: %w", err)
	}
	defer f.Close()

	wb := dump.Node{Type: "workbook"}
	for _, sheet := range f.GetSheetList() {
		sheetNode, err := extractSheet(f, sheet)
		if err != nil {
			return dump.ExtractedData{}, err
		}
		wb.Children = append(wb.Children, sheetNode)
	}

	return dump.ExtractedData{
		Dialect: Dialect,
		Source:  path,
		Data:    wb,
	}, nil
}

func extractSheet(f *excelize.File, sheet string) (dump.Node, error) {
	props := map[string]any{"name": sheet}

	if dim, err := f.GetSheetDimension(sheet); err == nil && dim != "" {
		props["dimension"] = dim
	}

	if merges, err := f.GetMergeCells(sheet); err == nil && len(merges) > 0 {
		ranges := make([]string, 0, len(merges))
		for _, m := range merges {
			ranges = append(ranges, m.GetStartAxis()+":"+m.GetEndAxis())
		}
		props["merges"] = ranges
	}

	commentsByCell := map[string]excelize.Comment{}
	if comments, err := f.GetComments(sheet); err == nil {
		for _, c := range comments {
			commentsByCell[c.Cell] = c
		}
	}

	rows, err := f.GetRows(sheet)
	if err != nil {
		return dump.Node{}, fmt.Errorf("sheet %q rows: %w", sheet, err)
	}

	// Also surface cells that only have comments (outside GetRows span).
	commentOnly := map[string]struct{}{}
	for ref := range commentsByCell {
		commentOnly[ref] = struct{}{}
	}

	sheetNode := dump.Node{Type: "sheet", Props: props}
	maxRow := len(rows)
	for ref := range commentsByCell {
		_, r, err := excelize.CellNameToCoordinates(ref)
		if err == nil && r > maxRow {
			maxRow = r
		}
	}

	for r := 1; r <= maxRow; r++ {
		var rowCells []dump.Node
		var rowData []string
		if r-1 < len(rows) {
			rowData = rows[r-1]
		}
		// columns in this row from GetRows, plus any comment cells on this row
		colSeen := map[int]struct{}{}
		for c := 1; c <= len(rowData); c++ {
			colSeen[c] = struct{}{}
		}
		for ref := range commentsByCell {
			c, rr, err := excelize.CellNameToCoordinates(ref)
			if err == nil && rr == r {
				colSeen[c] = struct{}{}
			}
		}
		cols := make([]int, 0, len(colSeen))
		for c := range colSeen {
			cols = append(cols, c)
		}
		sort.Ints(cols)

		for _, c := range cols {
			ref, err := excelize.CoordinatesToCellName(c, r)
			if err != nil {
				return dump.Node{}, err
			}
			cell, ok, err := extractCell(f, sheet, ref, commentsByCell)
			if err != nil {
				return dump.Node{}, err
			}
			if ok {
				rowCells = append(rowCells, cell)
				delete(commentOnly, ref)
			}
		}
		if len(rowCells) > 0 {
			sheetNode.Children = append(sheetNode.Children, dump.Node{
				Type:     "row",
				Props:    map[string]any{"r": r},
				Children: rowCells,
			})
		}
	}

	// Comments on rows beyond processed set (should be rare)
	for ref := range commentOnly {
		cell, ok, err := extractCell(f, sheet, ref, commentsByCell)
		if err != nil {
			return dump.Node{}, err
		}
		if !ok {
			continue
		}
		_, r, err := excelize.CellNameToCoordinates(ref)
		if err != nil {
			return dump.Node{}, err
		}
		// append or merge into existing row
		sheetNode.Children = append(sheetNode.Children, dump.Node{
			Type:     "row",
			Props:    map[string]any{"r": r},
			Children: []dump.Node{cell},
		})
	}

	return sheetNode, nil
}

func extractCell(f *excelize.File, sheet, ref string, comments map[string]excelize.Comment) (dump.Node, bool, error) {
	formula, err := f.GetCellFormula(sheet, ref)
	if err != nil {
		return dump.Node{}, false, fmt.Errorf("cell %s formula: %w", ref, err)
	}
	hasLink, linkTarget, err := f.GetCellHyperLink(sheet, ref)
	if err != nil {
		return dump.Node{}, false, fmt.Errorf("cell %s hyperlink: %w", ref, err)
	}
	comment, hasComment := comments[ref]

	cellType, err := f.GetCellType(sheet, ref)
	if err != nil {
		return dump.Node{}, false, fmt.Errorf("cell %s type: %w", ref, err)
	}

	raw, err := f.GetCellValue(sheet, ref, excelize.Options{RawCellValue: true})
	if err != nil {
		return dump.Node{}, false, fmt.Errorf("cell %s value: %w", ref, err)
	}
	display, err := f.GetCellValue(sheet, ref)
	if err != nil {
		return dump.Node{}, false, fmt.Errorf("cell %s display: %w", ref, err)
	}

	hasExtra := formula != "" || hasLink || hasComment
	emptyVal := raw == "" && display == ""
	if emptyVal && !hasExtra {
		return dump.Node{}, false, nil
	}

	props := map[string]any{"ref": ref}

	kind, value, err := classifyValue(f, sheet, ref, cellType, raw, display)
	if err != nil {
		return dump.Node{}, false, err
	}
	props["type"] = kind
	if kind != "blank" {
		props["value"] = value
	}

	if formula != "" {
		// excelize may return with or without leading =
		if !strings.HasPrefix(formula, "=") {
			formula = "=" + formula
		}
		props["formula"] = formula
	}

	if numfmt := cellNumFmt(f, sheet, ref); numfmt != "" {
		props["numfmt"] = numfmt
	}

	if hasLink && linkTarget != "" {
		props["hyperlink"] = map[string]any{"url": linkTarget}
	}
	if hasComment {
		cprops := map[string]any{}
		if comment.Author != "" {
			cprops["author"] = comment.Author
		}
		text := comment.Text
		if text == "" && len(comment.Paragraph) > 0 {
			var b strings.Builder
			for _, p := range comment.Paragraph {
				b.WriteString(p.Text)
			}
			text = b.String()
		}
		if text != "" {
			cprops["text"] = text
		}
		if len(cprops) > 0 {
			props["comment"] = cprops
		}
	}

	return dump.Node{Type: "cell", Props: props}, true, nil
}

func classifyValue(f *excelize.File, sheet, ref string, cellType excelize.CellType, raw, display string) (kind string, value any, err error) {
	// Formula cells: type is the result kind when a value exists.
	switch cellType {
	case excelize.CellTypeBool:
		v := strings.EqualFold(raw, "TRUE") || raw == "1"
		if display != "" {
			v = strings.EqualFold(display, "TRUE") || display == "1"
		}
		return "bool", v, nil
	case excelize.CellTypeError:
		if display != "" {
			return "error", display, nil
		}
		return "error", raw, nil
	case excelize.CellTypeDate:
		return dateValue(raw, display)
	case excelize.CellTypeNumber, excelize.CellTypeFormula:
		if looksLikeDate(f, sheet, ref) {
			return dateValue(raw, display)
		}
		if raw == "" && display == "" {
			return "blank", nil, nil
		}
		s := raw
		if s == "" {
			s = display
		}
		n, perr := strconv.ParseFloat(s, 64)
		if perr != nil {
			// formula cached as string
			if display != "" {
				return "string", display, nil
			}
			return "string", s, nil
		}
		return "number", n, nil
	case excelize.CellTypeSharedString, excelize.CellTypeInlineString:
		if display != "" {
			return "string", display, nil
		}
		return "string", raw, nil
	case excelize.CellTypeUnset:
		if raw == "" && display == "" {
			return "blank", nil, nil
		}
		// try number then string
		s := raw
		if s == "" {
			s = display
		}
		if _, perr := strconv.ParseFloat(s, 64); perr == nil && looksLikeDate(f, sheet, ref) {
			return dateValue(s, display)
		}
		if n, perr := strconv.ParseFloat(s, 64); perr == nil && raw != "" {
			return "number", n, nil
		}
		return "string", s, nil
	default:
		if display != "" {
			return "string", display, nil
		}
		if raw != "" {
			return "string", raw, nil
		}
		return "blank", nil, nil
	}
}

func looksLikeDate(f *excelize.File, sheet, ref string) bool {
	styleID, err := f.GetCellStyle(sheet, ref)
	if err != nil {
		return false
	}
	style, err := f.GetStyle(styleID)
	if err != nil || style == nil {
		return false
	}
	// Built-in Excel date/time format ids (common set).
	switch style.NumFmt {
	case 14, 15, 16, 17, 18, 19, 20, 21, 22, 27, 30, 36, 45, 46, 47, 50, 57:
		return true
	}
	if style.CustomNumFmt != nil {
		cf := strings.ToLower(*style.CustomNumFmt)
		if strings.Contains(cf, "y") || strings.Contains(cf, "d") || strings.Contains(cf, "m") {
			// exclude pure number formats that use m for minutes only — keep simple
			if strings.Contains(cf, "y") || strings.Contains(cf, "d") || strings.Contains(cf, "yy") {
				return true
			}
		}
	}
	return false
}

func cellNumFmt(f *excelize.File, sheet, ref string) string {
	styleID, err := f.GetCellStyle(sheet, ref)
	if err != nil {
		return ""
	}
	style, err := f.GetStyle(styleID)
	if err != nil || style == nil {
		return ""
	}
	if style.CustomNumFmt != nil && *style.CustomNumFmt != "" {
		return *style.CustomNumFmt
	}
	if style.NumFmt == 0 {
		return ""
	}
	// expose built-in id as "builtin:N" so scripts can branch without full table
	return fmt.Sprintf("builtin:%d", style.NumFmt)
}

func dateValue(raw, display string) (string, any, error) {
	s := raw
	if s == "" {
		s = display
	}
	if s == "" {
		return "blank", nil, nil
	}
	if n, err := strconv.ParseFloat(s, 64); err == nil {
		t := excelSerialToTime(n)
		// date-only → UTC midnight when no fractional time
		if _, frac := math.Modf(n); frac == 0 {
			t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		} else {
			t = t.UTC()
		}
		return "date", t, nil
	}
	// try parse display as date
	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02",
		"01-02-06",
		"1/2/06",
		"1/2/2006",
		"01/02/2006",
	} {
		if t, err := time.ParseInLocation(layout, display, time.UTC); err == nil {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)
			return "date", t, nil
		}
	}
	return "string", display, nil
}

// excelSerialToTime converts an Excel 1900-date-system serial to UTC time.
// Uses the 1899-12-30 epoch (Excel's Lotus 1-2-3 compatibility base).
func excelSerialToTime(serial float64) time.Time {
	whole, frac := math.Modf(serial)
	base := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)
	t := base.Add(time.Duration(whole) * 24 * time.Hour)
	if frac != 0 {
		t = t.Add(time.Duration(frac * float64(24*time.Hour)))
	}
	return t
}
