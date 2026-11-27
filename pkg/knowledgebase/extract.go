// Package knowledgebase owns archive-to-Markdown indexing and authorized lexical
// retrieval. It has no dependency on the Agent runtime or fault diagnosis.
package knowledgebase

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"unicode/utf8"

	"github.com/ccfos/nightingale/v6/models"
)

const MaxText = 2 << 20

var ErrOCR = errors.New("document has no extractable text; OCR is required")

type Section struct {
	Heading string
	Body    string
}
type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > MaxText {
		return 0, fmt.Errorf("extracted text exceeds 2 MiB")
	}
	return b.Buffer.Write(p)
}

func Extract(ctx context.Context, kind, path string, body models.ArchiveJSON) (string, error) {
	var out strings.Builder
	switch kind {
	case "online":
		var doc map[string]interface{}
		if err := json.Unmarshal([]byte(body), &doc); err != nil {
			return "", err
		}
		data, _ := doc["data"].(map[string]interface{})
		walkCanvas(&out, data["main"], 0)
	case "docx", "word":
		z, err := zip.OpenReader(path)
		if err != nil {
			return "", err
		}
		defer z.Close()
		found := false
		for _, f := range z.File {
			if f.Name != "word/document.xml" && !strings.HasPrefix(f.Name, "word/header") && !strings.HasPrefix(f.Name, "word/footer") && f.Name != "word/footnotes.xml" && f.Name != "word/endnotes.xml" {
				continue
			}
			if f.Name == "word/document.xml" {
				found = true
			} else {
				fmt.Fprintf(&out, "\n## 附注 %s\n", f.Name)
			}
			if f.UncompressedSize64 > 16<<20 {
				return "", fmt.Errorf("DOCX content exceeds extraction limit")
			}
			r, err := f.Open()
			if err != nil {
				return "", err
			}
			dec := xml.NewDecoder(io.LimitReader(r, (16<<20)+1))
			inText := false
			for {
				t, e := dec.Token()
				if e == io.EOF {
					break
				}
				if e != nil {
					r.Close()
					return "", e
				}
				switch v := t.(type) {
				case xml.StartElement:
					if v.Name.Local == "t" {
						inText = true
					}
					if v.Name.Local == "tab" {
						out.WriteString("\t")
					}
					if v.Name.Local == "br" {
						out.WriteString("\n")
					}
					if v.Name.Local == "drawing" || v.Name.Local == "pict" {
						out.WriteString("\n[图片/图表：未提取文字，请核对原件]\n")
					}
				case xml.EndElement:
					if v.Name.Local == "t" {
						inText = false
					}
					if v.Name.Local == "p" || v.Name.Local == "tr" {
						out.WriteString("\n")
					}
					if v.Name.Local == "tc" {
						out.WriteString(" | ")
					}
				case xml.CharData:
					if inText {
						out.Write(v)
					}
				}
				if out.Len() > MaxText {
					r.Close()
					return "", fmt.Errorf("DOCX text exceeds 2 MiB")
				}
			}
			r.Close()
		}
		if !found {
			return "", fmt.Errorf("DOCX document.xml is missing")
		}
	case "pdf":
		cmd := exec.CommandContext(ctx, "pdftotext", "-layout", "-enc", "UTF-8", path, "-")
		var b limitedBuffer
		cmd.Stdout = &b
		cmd.Stderr = io.Discard
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("PDF text extraction failed (pdftotext must be installed)")
		}
		pages := strings.Split(b.String(), "\f")
		for i, p := range pages {
			if strings.TrimSpace(p) != "" {
				fmt.Fprintf(&out, "\n## 第 %d 页\n\n%s\n", i+1, p)
			}
		}
	default:
		return "", ErrOCR
	}
	text := strings.TrimSpace(out.String())
	if strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(text, "[图片：未进行 OCR]", ""), "[图片/图表：未提取文字，请核对原件]", "")) == "" {
		return "", ErrOCR
	}
	if text == "" {
		return "", ErrOCR
	}
	if len(text) > MaxText || !utf8.ValidString(text) {
		return "", fmt.Errorf("invalid or oversized document text")
	}
	return text, nil
}
func walkCanvas(b *strings.Builder, v interface{}, depth int) {
	if depth > 40 || b.Len() > MaxText {
		return
	}
	switch n := v.(type) {
	case []interface{}:
		for _, c := range n {
			walkCanvas(b, c, depth+1)
		}
	case map[string]interface{}:
		typ, _ := n["type"].(string)
		if typ == "image" {
			b.WriteString("\n[图片：未进行 OCR]\n")
			return
		}
		if typ == "title" {
			b.WriteString("\n## ")
		}
		if value, ok := n["value"].(string); ok {
			b.WriteString(value)
		} else if value, ok := n["value"].([]interface{}); ok {
			walkCanvas(b, value, depth+1)
		}
		if _, ok := n["tdList"]; ok {
			b.WriteString("\n")
		}

		// Walk content fields only, never style/options/metadata or arbitrary map order.
		for _, key := range []string{"valueList", "children", "trList", "tdList"} {
			walkCanvas(b, n[key], depth+1)
		}
		if typ == "title" || typ == "table" || typ == "list" {
			b.WriteString("\n")
		}
	}
}
func Split(text string) []Section {
	result := []Section{}
	heading := "正文"
	var b strings.Builder
	flush := func() {
		if strings.TrimSpace(b.String()) != "" {
			result = append(result, Section{Heading: heading, Body: strings.TrimSpace(b.String())})
		}
		b.Reset()
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "#") {
			flush()
			heading = strings.TrimSpace(strings.TrimLeft(line, "#"))
			if len([]rune(heading)) > 120 {
				heading = string([]rune(heading)[:120])
			}
		}
		// Bound per-tool reads without cutting UTF-8 characters.
		for len([]rune(line)) > 3000 {
			part := []rune(line)
			b.WriteString(string(part[:3000]))
			flush()
			line = string(part[3000:])
		}
		if b.Len()+len(line) > 10000 {
			flush()
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	flush()
	return result
}
