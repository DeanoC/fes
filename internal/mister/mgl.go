package mister

import (
	"bytes"
	"encoding/xml"
	"fmt"

	"github.com/clawzai2-tech/mister-remote/internal/core"
)

func RenderMGL(spec core.Spec, relativeROM string) ([]byte, error) {
	escape := func(value string) (string, error) {
		var b bytes.Buffer
		if err := xml.EscapeText(&b, []byte(value)); err != nil {
			return "", err
		}
		return b.String(), nil
	}
	rbf, err := escape(spec.RBFSelector)
	if err != nil {
		return nil, err
	}
	path, err := escape(relativeROM)
	if err != nil {
		return nil, err
	}
	result := fmt.Sprintf("<mistergamedescription>\n    <rbf>%s</rbf>\n    <file delay=\"%d\" type=\"%s\" index=\"%d\" path=\"%s\"/>\n</mistergamedescription>\n", rbf, spec.FileDelay, spec.FileType, spec.FileIndex, path)
	return []byte(result), nil
}
