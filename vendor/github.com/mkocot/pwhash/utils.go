package pwhash

import "bytes"

const (
	b64t = "./0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	/* Maximum salt string length. */
	SALT_LEN_MAX = 16
	/* Default number of rounds if not explicitly specified. */
	ROUNDS_DEFAULT = 5000
	/* Minimum number of rounds. */
	ROUNDS_MIN = 1000
	/* Maximum number of rounds. */
	ROUNDS_MAX = 999999999
)

func b64From24bit(B2, B1, B0 byte, n int, buffer *bytes.Buffer) {
	w := (int(B2) << 16) | (int(B1) << 8) | int(B0)
	for {
		if !(n > 0) {
			break
		}
		n--
		buffer.WriteByte(byte(b64t[w&0x3f]))
		w >>= 6
	}
}
