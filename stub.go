package gofastemail

import (
	"fmt"
	"io"

	"github.com/AOShei/go-fast-core"
)

func (r *reader) msgFile(src io.ReaderAt, size int64) (*gofastcore.Document, error) {
	return nil, fmt.Errorf("gofastemail: msg: %w", gofastcore.ErrUnsupported)
}
