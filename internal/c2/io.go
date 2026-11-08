package c2

import (
	"encoding/base64"
	"os"
)

// These thin wrappers exist to:
//   - make the logic in manager.go / handler more intuitive and avoid importing os repeatedly;
//   - make it easy to abstract behind an interface later (e.g. switch to an internal/storage implementation) for unit testing.

func osMkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func osWriteFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}

func base64Decode(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}
