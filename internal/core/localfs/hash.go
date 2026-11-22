package localfs

import (
	"crypto/sha256"
	"encoding/hex"
)

// sha256Hex 返回原始字节的 sha256 小写十六进制（digest.Record.BlobSHA256 的口径）。
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
