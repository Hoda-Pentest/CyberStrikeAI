package c2

import (
	"encoding/base64"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// NormalizeConsoleOutput converts raw console bytes from an implant/Shell into UTF-8 text.
// osTag comes from the session's os field (such as windows / Windows 10); an empty value is treated as auto.
func NormalizeConsoleOutput(raw []byte, osTag string) string {
	if len(raw) == 0 {
		return ""
	}
	osTag = strings.ToLower(strings.TrimSpace(osTag))
	isWindows := strings.Contains(osTag, "windows")

	if utf8.Valid(raw) {
		return string(raw)
	}
	if isWindows {
		if out, _, err := transform.Bytes(simplifiedchinese.GB18030.NewDecoder(), raw); err == nil {
			return string(out)
		}
	}
	// Non-Windows or decode failure: fall back to GB18030 (which covers GBK)
	if out, _, err := transform.Bytes(simplifiedchinese.GB18030.NewDecoder(), raw); err == nil {
		return string(out)
	}
	return string(raw)
}

// ResolveTaskResultText merges the Output/OutputB64 (and Error/ErrorB64) reported by the beacon, decoding per the session OS.
func ResolveTaskResultText(plain, b64, sessionOS string) string {
	if strings.TrimSpace(b64) != "" {
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
		if err == nil {
			return NormalizeConsoleOutput(raw, sessionOS)
		}
	}
	if plain == "" {
		return ""
	}
	return NormalizeConsoleOutput([]byte(plain), sessionOS)
}
