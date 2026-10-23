package excelize

import (
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

func FuzzStringItemText(f *testing.F) {
	f.Add("<t>value</t>")
	f.Add("<t>first</t><t>last</t>")
	f.Add("<r><t>first</t></r><r><t>last</t></r>")
	f.Add("<t><ignored>value</ignored></t>")
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
		}
	})
}
