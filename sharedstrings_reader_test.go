package excelize

import (
	"bytes"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStringItemReadErrors(t *testing.T) {
	for name, contents := range map[string]string{
		"bare-unicode-space":   "\u00a0",
		"duplicate-properties": `<r><rPr/><rPr/><t>value</t></r>`,
		"duplicate-phonetics":  `<t>value</t><phoneticPr fontId="0"/><phoneticPr fontId="1"/>`,
		"nested-text":          `<t><ignored>value</ignored></t>`,
		"unknown-wrapper":      `<ignored><t>value</t></ignored>`,
		"duplicate-text":       `<t>first</t><t>last</t>`,
		"text-and-run":         `<t>first</t><r><t>last</t></r>`,
		"run-and-text":         `<r><t>first</t></r><t>last</t>`,
		"run-duplicate-text":   `<r><t>first</t><t>last</t></r>`,
		"run-nested-text":      `<r><t><ignored>value</ignored></t></r>`,
		"run-unknown-wrapper":  `<r><ignored><t>value</t></ignored></r>`,
		"bare-text":            `value`,
		"run-bare-text":        `<r>value</r>`,
	} {
		t.Run(name, func(t *testing.T) {
			for _, container := range []string{"si", "is"} {
				var item xlsxSI
				err := xml.Unmarshal([]byte("<"+container+">"+contents+"</"+container+">"), &item)
				require.Error(t, err, container)
			}
			for _, disk := range []bool{false, true} {
				f := NewFile(Options{TmpDir: t.TempDir()})
				t.Cleanup(func() { require.NoError(t, f.Close()) })
				data := []byte(`<sst xmlns="` + NameSpaceSpreadSheet.Value + `"><si>` + contents + `</si></sst>`)
				if disk {
					path := filepath.Join(t.TempDir(), "strings.xml")
					require.NoError(t, os.WriteFile(path, data, 0600))
					f.tempFiles.Store(defaultXMLPathSharedStrings, path)
				} else {
					f.Pkg.Store(defaultXMLPathSharedStrings, data)
				}
				f.Sheet.Delete("xl/worksheets/sheet1.xml")
				f.Pkg.Store("xl/worksheets/sheet1.xml", []byte(`<worksheet><sheetData><row><c t="s"><v>0</v></c></row></sheetData></worksheet>`))
				for range 2 {
					_, err := f.GetRows("Sheet1")
					require.Error(t, err, "disk=%v", disk)
					require.Nil(t, f.sharedStringTemp)
				}
			}
		})
	}
}

func TestStringItemValidText(t *testing.T) {
	for _, contents := range []string{
		`<t xml:space="preserve"> first &amp; <![CDATA[last]]> </t>`,
		`<r><rPr><b/></rPr><t xml:space="preserve"> first &amp; </t></r><r><t xml:space="preserve">last </t></r>`,
		`<t xml:space="preserve"> first &amp; last </t><rPh sb="0" eb="5"><t>annotation</t></rPh><phoneticPr fontId="0"/>`,
	} {
		var item xlsxSI
		require.NoError(t, xml.Unmarshal([]byte("<si>"+contents+"</si>"), &item))
		require.Equal(t, " first & last ", item.String())
		data, err := xml.Marshal(item)
		require.NoError(t, err)
		if item.T != nil {
			require.Contains(t, string(data), `space="preserve"`)
		}
		f := NewFile(Options{TmpDir: t.TempDir()})
		f.Pkg.Store(defaultXMLPathSharedStrings, []byte(`<sst xmlns="`+NameSpaceSpreadSheet.Value+`"><si>`+contents+`</si></sst>`))
		value, err := f.getFromStringItem(0)
		require.NoError(t, err)
		require.Equal(t, item.String(), value)
		require.NoError(t, f.Close())
	}
}

func TestSharedStringValuesPreserved(t *testing.T) {
	value := " http://purl.oclc.org/ooxml/spreadsheetml/main & <tag>\t "
	var escaped bytes.Buffer
	require.NoError(t, xml.EscapeText(&escaped, []byte(value)))
	for _, namespace := range []string{NameSpaceSpreadSheet.Value, StrictNameSpaceSpreadSheet} {
		for _, disk := range []bool{false, true} {
			for form, contents := range map[string]string{
				"plain":   `<t xml:space="preserve">` + escaped.String() + `</t>`,
				"escaped": `<t xml:space="preserve">` + strings.ReplaceAll(escaped.String(), "purl", "p&#117;rl") + `</t>`,
				"rich":    `<r><t xml:space="preserve"> http://purl.oclc.org/</t></r><r><t xml:space="preserve">ooxml/spreadsheetml/main &amp; &lt;tag&gt;&#x9; </t></r>`,
			} {
				t.Run(namespace+"/"+form+map[bool]string{false: "/memory", true: "/disk"}[disk], func(t *testing.T) {
					f := NewFile(Options{TmpDir: t.TempDir()})
					t.Cleanup(func() { require.NoError(t, f.Close()) })
					data := []byte(`<sst xmlns="` + namespace + `"><si>` + contents + `</si></sst>`)
					if disk {
						path := filepath.Join(t.TempDir(), "strings.xml")
						require.NoError(t, os.WriteFile(path, data, 0600))
						f.tempFiles.Store(defaultXMLPathSharedStrings, path)
					} else {
						f.Pkg.Store(defaultXMLPathSharedStrings, data)
					}
					f.Sheet.Delete("xl/worksheets/sheet1.xml")
					f.Pkg.Store("xl/worksheets/sheet1.xml", []byte(`<worksheet xmlns="`+NameSpaceSpreadSheet.Value+`"><sheetData><row r="1"><c r="A1" t="s"><v>0</v></c></row></sheetData></worksheet>`))
					for range 2 {
						rows, err := f.GetRows("Sheet1")
						require.NoError(t, err)
						require.Equal(t, [][]string{{value}}, rows)
						got, err := f.GetCellValue("Sheet1", "A1")
						require.NoError(t, err)
						require.Equal(t, value, got)
						if !disk {
							runs, err := f.GetCellRichText("Sheet1", "A1")
							require.NoError(t, err)
							var text strings.Builder
							for _, run := range runs {
								text.WriteString(run.Text)
							}
							require.Equal(t, value, text.String())
						}
					}
				})
			}
		}
	}
}

func FuzzStringItemText(f *testing.F) {
	f.Add("<t>value</t>")
	f.Add("<t>first</t><t>last</t>")
	f.Add("<r><t>first</t></r><r><t>last</t></r>")
	f.Add("<t><ignored>value</ignored></t>")
	f.Add("<t>http://purl.oclc.org/ooxml/spreadsheetml/main</t>")
	f.Add("<t>http://p&#117;rl.oclc.org/ooxml/spreadsheetml/main</t>")
	f.Add("<r><t>http://purl.oclc.org/</t></r><r><t>ooxml/spreadsheetml/main</t></r>")
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 1<<16 {
			t.Skip()
		}
		var inline, shared xlsxSI
		errInline := xml.Unmarshal([]byte("<is>"+input+"</is>"), &inline)
		errShared := xml.Unmarshal([]byte("<si>"+input+"</si>"), &shared)
		require.Equal(t, errInline == nil, errShared == nil)
		if errInline == nil {
			require.Equal(t, inline.String(), shared.String())
			var encoded strings.Builder
			require.NoError(t, xml.NewEncoder(&encoded).Encode(shared))
			var decoded xlsxSI
			require.NoError(t, xml.Unmarshal([]byte(encoded.String()), &decoded))
			require.Equal(t, shared.String(), decoded.String())
			book := NewFile(Options{TmpDir: t.TempDir()})
			defer func() { require.NoError(t, book.Close()) }()
			book.Pkg.Store(defaultXMLPathSharedStrings, []byte(`<sst xmlns="`+NameSpaceSpreadSheet.Value+`"><si>`+input+`</si></sst>`))
			table, err := book.sharedStringsReader()
			require.NoError(t, err)
			require.Len(t, table.SI, 1)
			require.Equal(t, shared.String(), table.SI[0].String())
		}
	})
}
