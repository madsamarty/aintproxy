package scrape

import (
	"archive/zip"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Table is a named grid of strings that can be exported as CSV or Excel.
type Table struct {
	Name   string
	Header []string
	Rows   [][]string
}

// Tabular is implemented by mode results that can be exported as tables.
type Tabular interface{ Tables() []Table }

// WriteTables writes tables to path. The extension picks the format:
// .xlsx makes one workbook with a sheet per table; .csv writes one file per
// table (<name>.csv for the first, <base>_<table>.csv for the others).
// It returns the files written.
func WriteTables(path string, tables []Table) ([]string, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	files, err := writeTables(path, tables)
	for _, f := range files {
		ChownToInvoker(f)
	}
	ChownToInvoker(filepath.Dir(path))
	return files, err
}

// ChownToInvoker hands a file created while running under sudo back to the
// user who invoked sudo, so results are not root-owned.
func ChownToInvoker(path string) {
	uid, e1 := strconv.Atoi(os.Getenv("SUDO_UID"))
	gid, e2 := strconv.Atoi(os.Getenv("SUDO_GID"))
	if e1 == nil && e2 == nil && os.Geteuid() == 0 {
		os.Chown(path, uid, gid)
	}
}

func writeTables(path string, tables []Table) ([]string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".xlsx":
		f, err := os.Create(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return []string{path}, writeXLSX(f, tables)
	case ".csv":
		base := strings.TrimSuffix(path, filepath.Ext(path))
		var files []string
		for i, t := range tables {
			name := path
			if i > 0 {
				name = fmt.Sprintf("%s_%s.csv", base, strings.ToLower(t.Name))
			}
			if err := writeCSV(name, t); err != nil {
				return files, err
			}
			files = append(files, name)
		}
		return files, nil
	}
	return nil, fmt.Errorf("unsupported output %q (use .csv, .xlsx or .json)", path)
}

func writeCSV(path string, t Table) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	f.WriteString("\xEF\xBB\xBF") // UTF-8 BOM so Excel reads Arabic/emoji correctly
	w := csv.NewWriter(f)
	w.Write(t.Header)
	w.WriteAll(t.Rows)
	w.Flush()
	return w.Error()
}

// ---- minimal .xlsx writer (inline strings, bold header, no dependencies) ----

func xmlEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '&':
			b.WriteString("&amp;")
		case r == '<':
			b.WriteString("&lt;")
		case r == '>':
			b.WriteString("&gt;")
		case r == '\t' || r == '\n' || r == '\r' || r >= 0x20 && r != 0xFFFE && r != 0xFFFF && !(r >= 0xD800 && r <= 0xDFFF):
			b.WriteRune(r)
		}
	}
	return b.String()
}

func colName(i int) string {
	n := ""
	for i++; i > 0; i = (i - 1) / 26 {
		n = string(rune('A'+(i-1)%26)) + n
	}
	return n
}

func sheetName(n string, used map[string]bool) string {
	n = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`[]:*?/\`, r) {
			return '_'
		}
		return r
	}, n)
	if n == "" {
		n = "Sheet"
	}
	if r := []rune(n); len(r) > 31 {
		n = string(r[:31])
	}
	base := n
	for i := 2; used[strings.ToLower(n)]; i++ {
		n = fmt.Sprintf("%s_%d", base, i)
	}
	used[strings.ToLower(n)] = true
	return n
}

func writeXLSX(w io.Writer, tables []Table) error {
	zw := zip.NewWriter(w)
	add := func(name, content string) error {
		f, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = io.WriteString(f, content)
		return err
	}
	const xmlHead = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"

	var ct, wbSheets, rels strings.Builder
	used := map[string]bool{}
	for i, t := range tables {
		n := i + 1
		fmt.Fprintf(&ct, `<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, n)
		fmt.Fprintf(&wbSheets, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, xmlEscape(sheetName(t.Name, used)), n, n)
		fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, n, n)
	}
	k := len(tables) + 1
	fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`, k)

	if err := add("[Content_Types].xml", xmlHead+`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>`+ct.String()+`</Types>`); err != nil {
		return err
	}
	if err := add("_rels/.rels", xmlHead+`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`); err != nil {
		return err
	}
	if err := add("xl/workbook.xml", xmlHead+`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>`+wbSheets.String()+`</sheets></workbook>`); err != nil {
		return err
	}
	if err := add("xl/_rels/workbook.xml.rels", xmlHead+`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`+rels.String()+`</Relationships>`); err != nil {
		return err
	}
	if err := add("xl/styles.xml", xmlHead+`<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts><fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills><borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders><cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs><cellXfs count="2"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/><xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/></cellXfs></styleSheet>`); err != nil {
		return err
	}

	for i, t := range tables {
		var sb strings.Builder
		sb.WriteString(xmlHead + `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews><sheetFormatPr defaultRowHeight="15"/><cols>`)
		for c := range t.Header {
			fmt.Fprintf(&sb, `<col min="%d" max="%d" width="24" customWidth="1"/>`, c+1, c+1)
		}
		sb.WriteString(`</cols><sheetData>`)
		writeRow := func(r int, cells []string, style int) {
			fmt.Fprintf(&sb, `<row r="%d">`, r)
			for c, v := range cells {
				if r := []rune(v); len(r) > 32000 {
					v = string(r[:32000])
				}
				fmt.Fprintf(&sb, `<c r="%s%d" t="inlineStr" s="%d"><is><t xml:space="preserve">%s</t></is></c>`, colName(c), r, style, xmlEscape(v))
			}
			sb.WriteString(`</row>`)
		}
		writeRow(1, t.Header, 1)
		for r, row := range t.Rows {
			writeRow(r+2, row, 0)
		}
		sb.WriteString(`</sheetData></worksheet>`)
		if err := add(fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1), sb.String()); err != nil {
			return err
		}
	}
	return zw.Close()
}
