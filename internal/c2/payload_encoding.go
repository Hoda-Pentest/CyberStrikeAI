package c2

import (
	"encoding/base64"
	"encoding/binary"
)

// b64StdEncode encodes bytes with standard base64
func b64StdEncode(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

// utf16LEBase64 converts a string to UTF-16LE and then base64, for PowerShell -EncodedCommand
// (Windows PowerShell accepts this format, avoiding escaping errors from special characters on the command line)
func utf16LEBase64(s string) string {
	runes := []rune(s)
	buf := make([]byte, 0, len(runes)*2)
	for _, r := range runes {
		// Note: characters >0xFFFF need surrogate pairs, but PowerShell commands are usually within the BMP
		var enc [2]byte
		binary.LittleEndian.PutUint16(enc[:], uint16(r))
		buf = append(buf, enc[:]...)
	}
	return base64.StdEncoding.EncodeToString(buf)
}
