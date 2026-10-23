package excelize

import (
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRowsReadErrors(t *testing.T) {
	for name, body := range map[string]string{
		"style":             `<row r="1"><c r="A1" s="invalid" t="s"><v>0</v></c></row>`,
		"negative-style":    `<row r="1"><c r="A1" s="-1"><v>1</v></c></row>`,
		"style-overflow":    `<row r="1"><c r="A1" s="18446744073709551616"><v>1</v></c></row>`,
		"duplicate-value":   `<row r="1"><c r="A1"><v>1</v><v>2</v></c></row>`,
		"duplicate-formula": `<row r="1"><c r="A1"><f>1</f><f>2</f><v>1</v></c></row>`,
		"conflicting-value": `<row r="1"><c r="A1"><v>1</v><is><t>other</t></is></c></row>`,
		"index-text":        `<row r="1"><c r="A1" t="s"><v>invalid</v></c></row>`,
		"index-range":       `<row r="1"><c r="A1" t="s"><v>99</v></c></row>`,
		"nested-value":      `<row r="1"><c r="A1" t="s"><ignored><v>0</v></ignored></c></row>`,
		"negative-row":      `<row r="-1"><c r="A1"><v>1</v></c></row>`,
		"zero-row":          `<row r="0"><c r="A1"><v>1</v></c></row>`,
		"text-row":          `<row r="invalid"><c r="A1"><v>1</v></c></row>`,
		"duplicate-row":     `<row r="1"/><row r="1"/>`,
		"reversed-row":      `<row r="2"/><row r="1"/>`,
		"wrong-cell-row":    `<row r="1"><c r="A2"><v>1</v></c></row>`,
		"reversed-cell":     `<row r="1"><c r="B1"/><c r="A1"><v>1</v></c></row>`,
		"nested-row":        `<wrapper><row r="1"><c r="A1"><v>1</v></c></row></wrapper>`,
		"nested-cell":       `<row r="1"><wrapper><c r="A1"><v>1</v></c></wrapper></row>`,
		"broken-cell":       `<row r="1"><c r="A1"><v>1</c></row>`,
	} {
		t.Run(name, func(t *testing.T) {
			testRowsFailure(t, `<worksheet><sheetData>`+body+`</sheetData></worksheet>`)
		})
	}
	for name, document := range map[string]string{
		"truncated":   `<worksheet><sheetData><row r="1"><c r="A1"><v>1</v></c></row>`,
		"bad-tail":    `<worksheet><sheetData><row r="1"/></sheetData><broken></worksheet>`,
		"second-root": `<worksheet><sheetData/></worksheet><worksheet/>`,
		"wrong-root":  `<chartsheet><sheetData><row r="1"><c r="A1"><v>1</v></c></row></sheetData></chartsheet>`,
	} {
		t.Run(name, func(t *testing.T) { testRowsFailure(t, document) })
	}
}

func testRowsFailure(t *testing.T, document string) {
	t.Helper()
	for _, disk := range []bool{false, true} {
		f := NewFile(Options{TmpDir: t.TempDir()})
		f.Sheet.Delete("xl/worksheets/sheet1.xml")
		if disk {
			path := filepath.Join(t.TempDir(), "sheet.xml")
			require.NoError(t, os.WriteFile(path, []byte(document), 0600))
			f.Pkg.Delete("xl/worksheets/sheet1.xml")
			f.tempFiles.Store("xl/worksheets/sheet1.xml", path)
		} else {
			f.Pkg.Store("xl/worksheets/sheet1.xml", []byte(document))
		}
		f.Pkg.Store(defaultXMLPathSharedStrings, []byte(`<sst xmlns="`+NameSpaceSpreadSheet.Value+`"><si><t>value</t></si></sst>`))
		rows, err := f.Rows("Sheet1")
		require.NoError(t, err)
		for rows.Next() {
			if _, err = rows.Columns(); err != nil {
				break
			}
		}
		require.Error(t, rows.Error(), "disk=%v", disk)
		require.Error(t, rows.Close(), "disk=%v", disk)
		require.Error(t, rows.Close(), "repeated close must preserve the read error")
		_, err = f.GetRows("Sheet1")
		require.Error(t, err, "GetRows disk=%v", disk)
		require.NoError(t, f.Close())
	}
}

func TestRowsReaderEarlyClose(t *testing.T) {
	f := NewFile()
	t.Cleanup(func() { require.NoError(t, f.Close()) })
	f.Sheet.Delete("xl/worksheets/sheet1.xml")
	f.Pkg.Store("xl/worksheets/sheet1.xml", []byte(`<worksheet><sheetData><row r="1"><c r="A1"><v>1</v></c></row><row r="2"><c r="A2"><v>2</v></c></row><broken`))
	rows, err := f.Rows("Sheet1")
	require.NoError(t, err)
	require.True(t, rows.Next())
	values, err := rows.Columns()
	require.NoError(t, err)
	require.Equal(t, []string{"1"}, values)
	require.NoError(t, rows.Close())
}

type rowsFailingReader struct{ err error }

func (r rowsFailingReader) Read([]byte) (int, error) { return 0, r.err }

func TestRowsReaderIOError(t *testing.T) {
	want := errors.New("read failed")
	rows := &Rows{decoder: xml.NewDecoder(io.MultiReader(strings.NewReader(`<worksheet><sheetData><row r="1"/>`), rowsFailingReader{want}))}
	require.True(t, rows.Next())
	require.False(t, rows.Next())
	require.ErrorIs(t, rows.Error(), want)
	require.ErrorIs(t, rows.Close(), want)
}

func TestSharedStringStreamErrors(t *testing.T) {
	for name, document := range map[string]string{
		"truncated":       `<sst><si><t>first</t></si><si><t>broken`,
		"typed-attribute": `<sst><si><r><rPr><sz val="invalid"/></rPr><t>value</t></r></si></sst>`,
	} {
		t.Run(name, func(t *testing.T) {
			f := NewFile(Options{TmpDir: t.TempDir()})
			t.Cleanup(func() { require.NoError(t, f.Close()) })
			f.Pkg.Store(defaultXMLPathSharedStrings, []byte(document))
			for range 2 {
				_, err := f.getFromStringItem(0)
				require.Error(t, err)
				require.Nil(t, f.sharedStringTemp, "failed cache must not be published")
			}
		})
	}
	f := NewFile(Options{TmpDir: t.TempDir()})
	t.Cleanup(func() { require.NoError(t, f.Close()) })
	f.Pkg.Store(defaultXMLPathSharedStrings, []byte(`<sst><si><t>value</t></si></sst>`))
	f.options.TmpDir = filepath.Join(t.TempDir(), "missing")
	_, err := f.getFromStringItem(0)
	require.Error(t, err)
	require.Nil(t, f.sharedStringTemp)
	f.options.TmpDir = t.TempDir()
	value, err := f.getFromStringItem(0)
	require.NoError(t, err)
	require.Equal(t, "value", value)
	f.sharedStringItem = [][]uint{{0}}
	_, err = f.getFromStringItem(0)
	require.Error(t, err)
	f.sharedStringItem = [][]uint{{0, 5}}
	require.NoError(t, f.sharedStringTemp.Truncate(0))
	_, err = f.getFromStringItem(0)
	require.Error(t, err)
}

func TestRowsReaderValidValues(t *testing.T) {
	f := NewFile()
	t.Cleanup(func() { require.NoError(t, f.Close()) })
	f.Sheet.Delete("xl/worksheets/sheet1.xml")
	f.Pkg.Store("xl/worksheets/sheet1.xml", []byte("\xef\xbb\xbf"+`<worksheet><sheetData><row><c t="s"><v> +000 </v></c><c t="inlineStr"><is><r><t>rich</t></r><r><t> text</t></r></is></c></row><row r="3"><c r="B3"><f>1+1</f><v>2</v></c></row></sheetData><extLst/></worksheet>`))
	f.Pkg.Store(defaultXMLPathSharedStrings, []byte(`<sst xmlns="`+NameSpaceSpreadSheet.Value+`" count="99" uniqueCount="99"><si><t>first</t></si></sst>`))
	values, err := f.GetRows("Sheet1")
	require.NoError(t, err)
	require.Equal(t, [][]string{{"first", "rich text"}, nil, {"", "2"}}, values)
}

func TestRowsSpilledSharedStringsConcurrency(t *testing.T) {
	f := NewFile()
	for row := 1; row <= 1000; row++ {
		require.NoError(t, f.SetCellStr("Sheet1", "A"+strconv.Itoa(row), "value-"+strconv.Itoa(row)))
	}
	data, err := f.WriteToBuffer()
	require.NoError(t, err)
	require.NoError(t, f.Close())
	f, err = OpenReader(data, Options{UnzipXMLSizeLimit: 128, TmpDir: t.TempDir()})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, f.Close()) })
	_, spilled := f.tempFiles.Load(defaultXMLPathSharedStrings)
	require.True(t, spilled)
	type result struct {
		rows [][]string
		err  error
	}
	results := make(chan result, 3)
	for range 3 {
		go func() {
			rows, err := f.GetRows("Sheet1")
			results <- result{rows, err}
		}()
	}
	for range 3 {
		actual := <-results
		require.NoError(t, actual.err)
		require.Len(t, actual.rows, 1000)
		for i, row := range actual.rows {
			require.Equal(t, []string{"value-" + strconv.Itoa(i+1)}, row)
		}
	}
}

func FuzzRowsReaderErrors(f *testing.F) {
	for _, seed := range []string{
		`<worksheet><sheetData><row r="1"><c r="A1"><v>1</v></c></row></sheetData></worksheet>`,
		`<worksheet><sheetData><row r="1"><c s="invalid"/></row></sheetData></worksheet>`,
		`<worksheet><sheetData><row r="-1"/></sheetData></worksheet>`,
		`<worksheet><sheetData><row r="1"><c t="s"><v>invalid</v></c></row></sheetData></worksheet>`,
		`<worksheet><sheetData/><broken></worksheet>`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, document string) {
		book := NewFile(Options{TmpDir: t.TempDir()})
		defer func() { require.NoError(t, book.Close()) }()
		rows := &Rows{f: book, decoder: book.xmlNewDecoder(strings.NewReader(document))}
		for rows.Next() {
			if _, err := rows.Columns(); err != nil {
				require.ErrorIs(t, rows.Error(), err)
				break
			}
		}
		if err := rows.Error(); err != nil {
			require.False(t, rows.Next())
			require.ErrorIs(t, rows.Close(), err)
		} else {
			require.NoError(t, rows.Close())
		}
	})
}
