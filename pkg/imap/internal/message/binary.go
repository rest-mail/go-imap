package message

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime/quotedprintable"
	"strings"

	"github.com/rest-mail/go-imap/pkg/imap/internal/wire"
)

// ErrUnknownCTE is a section whose transfer encoding this server cannot remove:
// a FETCH of it fails with NO [UNKNOWN-CTE] (RFC 3516 §4.3).
var ErrUnknownCTE = errors.New("imap: unknown content-transfer-encoding")

// BinarySection answers a BINARY item: the response item's name and the
// decoded section, partial applied. size is set for BINARY.SIZE, which answers
// the decoded length only; peek is set for BINARY.PEEK and BINARY.SIZE, which
// leave \Seen alone. ok is false when the message cannot be loaded.
func BinarySection(item string, loadRaw func() (string, bool)) (name, data string, size, peek, ok bool, err error) {
	up := strings.ToUpper(item)
	lb, rb := strings.IndexByte(item, '['), strings.IndexByte(item, ']')
	size = strings.HasPrefix(up, "BINARY.SIZE[")
	peek = size || strings.HasPrefix(up, "BINARY.PEEK[")
	partial, _ := wire.ParseBodyPartial(item[rb+1:])
	var path []uint32
	if section := item[lb+1 : rb]; section != "" {
		path, _, _ = wire.SplitSectionPart(section)
	}
	raw, ok := loadRaw()
	if !ok {
		return "", "", size, peek, false, nil
	}
	data, err = binaryData(raw, path)
	if err != nil {
		return "", "", size, peek, true, err
	}
	label := "BINARY[" + item[lb+1:rb] + "]"
	if size {
		label = "BINARY.SIZE[" + item[lb+1:rb] + "]"
	}
	if partial != nil {
		// The partial counts octets of the decoded data (RFC 3516 §4.2).
		data = partialOctets(data, partial.Start, partial.Count)
		label = fmt.Sprintf("%s<%d>", label, partial.Start)
	}
	return label, data, size, peek, true, nil
}

// binaryData is the section of raw at path with its Content-Transfer-Encoding
// removed. The empty path is the message as stored, which has no transfer
// encoding of its own; a part that does not exist is empty, as BODY's is. 7bit,
// 8bit and binary need nothing removed, and a multipart or MESSAGE/RFC822 part
// may have no other encoding (RFC 2045 §6.4). base64 and quoted-printable are
// decoded; anything else, or encoded data that does not decode, is
// ErrUnknownCTE.
func binaryData(raw string, path []uint32) (string, error) {
	if len(path) == 0 {
		return raw, nil
	}
	node := messageNode(raw, 0)
	for _, k := range path {
		var found bool
		if node, found = node.child(k); !found {
			return "", nil
		}
	}
	cte := "7BIT"
	if v := HeaderFieldValues(node.header, "Content-Transfer-Encoding"); len(v) > 0 {
		cte = transferEncoding(strings.TrimSpace(v[0]))
	}
	switch cte {
	case "7BIT", "8BIT", "BINARY":
		return node.body, nil
	case "BASE64":
		compact := strings.Map(func(r rune) rune {
			if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
				return -1
			}
			return r
		}, node.body)
		b, err := base64.StdEncoding.DecodeString(compact)
		if err != nil {
			return "", ErrUnknownCTE
		}
		return string(b), nil
	case "QUOTED-PRINTABLE":
		b, err := io.ReadAll(quotedprintable.NewReader(strings.NewReader(node.body)))
		if err != nil {
			return "", ErrUnknownCTE
		}
		return string(b), nil
	}
	return "", ErrUnknownCTE
}
