package imap

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime/quotedprintable"
	"strings"
)

// BINARY (RFC 3516): FETCH answers a section with its Content-Transfer-Encoding
// removed.

// errUnknownCTE is a section whose transfer encoding this server cannot remove:
// a FETCH of it fails with NO [UNKNOWN-CTE] (RFC 3516 §4.3).
var errUnknownCTE = errors.New("imap: unknown content-transfer-encoding")

// binaryItem reports whether up, an upper-case FETCH item, is BINARY,
// BINARY.PEEK or BINARY.SIZE.
func binaryItem(up string) bool {
	return strings.HasPrefix(up, "BINARY[") || strings.HasPrefix(up, "BINARY.PEEK[") || strings.HasPrefix(up, "BINARY.SIZE[")
}

// validBinaryItem checks a BINARY item's section-binary, "[" [section-part]
// "]" (RFC 3516 §6): part numbers only, no HEADER, TEXT or MIME. BINARY and
// BINARY.PEEK may carry a partial; BINARY.SIZE may not.
func validBinaryItem(item string) bool {
	lb, rb := strings.IndexByte(item, '['), strings.IndexByte(item, ']')
	if lb < 0 || rb < lb {
		return false
	}
	if section := item[lb+1 : rb]; section != "" {
		if path, text, ok := splitSectionPart(section); !ok || len(path) == 0 || text != "" {
			return false
		}
	}
	if strings.HasPrefix(strings.ToUpper(item), "BINARY.SIZE[") {
		return item[rb+1:] == ""
	}
	_, ok := parseBodyPartial(item[rb+1:])
	return ok
}

// binarySection answers a BINARY item: the response item's name and the
// decoded section, partial applied. size is set for BINARY.SIZE, which answers
// the decoded length only; peek is set for BINARY.PEEK and BINARY.SIZE, which
// leave \Seen alone. ok is false when the message cannot be loaded.
func binarySection(item string, loadRaw func() (string, bool)) (name, data string, size, peek, ok bool, err error) {
	up := strings.ToUpper(item)
	lb, rb := strings.IndexByte(item, '['), strings.IndexByte(item, ']')
	size = strings.HasPrefix(up, "BINARY.SIZE[")
	peek = size || strings.HasPrefix(up, "BINARY.PEEK[")
	partial, _ := parseBodyPartial(item[rb+1:])
	var path []uint32
	if section := item[lb+1 : rb]; section != "" {
		path, _, _ = splitSectionPart(section)
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
		data = partialOctets(data, partial.start, partial.count)
		label = fmt.Sprintf("%s<%d>", label, partial.start)
	}
	return label, data, size, peek, true, nil
}

// binaryData is the section of raw at path with its Content-Transfer-Encoding
// removed. The empty path is the message as stored, which has no transfer
// encoding of its own; a part that does not exist is empty, as BODY's is. 7bit,
// 8bit and binary need nothing removed, and a multipart or MESSAGE/RFC822 part
// may have no other encoding (RFC 2045 §6.4). base64 and quoted-printable are
// decoded; anything else, or encoded data that does not decode, is
// errUnknownCTE.
func binaryData(raw string, path []uint32) (string, error) {
	if len(path) == 0 {
		return raw, nil
	}
	node, found := messageNode(raw, 0), true
	for _, k := range path {
		if node, found = node.child(k); !found {
			return "", nil
		}
	}
	cte := "7BIT"
	if v := headerFieldValues(node.header, "Content-Transfer-Encoding"); len(v) > 0 {
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
			return "", errUnknownCTE
		}
		return string(b), nil
	case "QUOTED-PRINTABLE":
		b, err := io.ReadAll(quotedprintable.NewReader(strings.NewReader(node.body)))
		if err != nil {
			return "", errUnknownCTE
		}
		return string(b), nil
	}
	return "", errUnknownCTE
}

// fetchFailure is the text of FETCH's NO for err: a section whose encoding
// cannot be removed carries UNKNOWN-CTE (RFC 3516 §4.3).
func fetchFailure(err error, text string) string {
	if errors.Is(err, errUnknownCTE) {
		return "[UNKNOWN-CTE] Cannot remove the section's Content-Transfer-Encoding"
	}
	return text
}
