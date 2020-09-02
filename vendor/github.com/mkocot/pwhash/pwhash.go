package pwhash

// md5crypt is Based on https://svnweb.freebsd.org/base/head/lib/libcrypt/crypt-md5.c?view=markup
import (
	"bytes"
	"crypto/md5"
	"crypto/subtle"
	"errors"
	"fmt"
)

const (
	separator = '$'
	md5Id     = "1"
	sha256id  = "5"
)

var (
	availableHashes = map[string]crypt{
		"1": &md5crypt{},
		"5": &sha256crypt{},
		"6": &sha512crypt{},
	}
)

type md5crypt struct{}

// Verify if given password match hash
// for now only md5, sha256 crypt is supported
// __`${id}$`__*`{salt}`*__$__*`{checksum}`*
func Verify(password string, hashWithSalt string) (bool, error) {
	// Just redirection to hide operations on bytes
	return verify([]byte(password), []byte(hashWithSalt))
}

func verify(password, hashWithSalt []byte) (bool, error) {
	saltID := extractID(hashWithSalt)
	crypto, ok := availableHashes[string(saltID)]
	if !ok {
		return false, fmt.Errorf("unsupported hash prefix '%s'", saltID)
	}

	given := extractHash(hashWithSalt)
	computed := extractHash(crypto.crypt(password, hashWithSalt))

	if given == nil || computed == nil {
		return false, errors.New("invalid hash")
	}
	return subtle.ConstantTimeCompare(computed, given) == 1, nil
}

// Return slice containing hashid, or nil if no hash id found
func extractID(slt []byte) []byte {
	// minimal is $x$, so at least 3 characters just for id
	if len(slt) < 3 {
		return nil
	}
	if slt[0] != separator {
		return nil
	}
	nextSeparator := bytes.IndexByte(slt[1:], separator)
	if nextSeparator < 0 {
		return nil
	}
	return slt[1 : nextSeparator+1]
}

func extractHash(slt []byte) []byte {
	lastSeparator := bytes.LastIndexByte(slt, separator)
	if lastSeparator < 0 {
		// Well nobody put anything anywhere, so just asume
		// its hash itself
		return slt
	}
	return slt[lastSeparator+1:]
}

type crypt interface {
	// we put CURRENTLY STORED HASH into SALT and calculate PWD hash FROM IT!
	crypt(pwd, slt []byte) []byte
	DetectHash(salt []byte) bool
}

func (x *md5crypt) DetectHash(slt []byte) bool {
	return bytes.HasPrefix(slt, []byte("$1$"))
}

func (x *md5crypt) crypt(pwd, slt []byte) []byte {
	return md5Crypt(pwd, slt)
}

func md5Crypt(passwd, salt []byte) []byte {
	const (
		magicString = "$1$"
	)
	// if we have prefix, drop it
	if bytes.HasPrefix(salt, []byte(magicString)) {
		salt = salt[3:]
	}
	// if we got hash after salt, drop it
	if index := bytes.Index(salt, []byte{separator}); index > 0 {
		salt = salt[:index]
	}

	// salt is atmost 8 chars
	if len(salt) > 8 {
		salt = salt[:8]
	}
	m := md5.New()
	m.Write(passwd)
	m.Write([]byte(magicString))
	m.Write(salt)

	m2 := md5.New()
	m2.Write(passwd)
	m2.Write(salt)
	m2.Write(passwd)

	mixin := m2.Sum(nil)

	for i := range passwd {
		m.Write([]byte{mixin[i%16]})
	}
	mixin = mixin[:0]

	for l := len(passwd); l != 0; l >>= 1 {
		if l&1 != 0 {
			m.Write([]byte{0})
		} else {
			m.Write([]byte{passwd[0]})
		}

	}

	final := m.Sum(nil)

	for i := 0; i < 1000; i++ {
		m3 := md5.New()
		if i&1 != 0 {
			m3.Write(passwd)
		} else {
			m3.Write(final)
		}

		if i%3 != 0 {
			m3.Write(salt)
		}

		if i%7 != 0 {
			m3.Write(passwd)
		}

		if i&1 != 0 {
			m3.Write(final)
		} else {
			m3.Write(passwd)
		}

		final = m3.Sum(nil)
	}

	var rearranged [22]byte
	rearrangedIndex := 0

	seq := [][3]int32{{0, 6, 12}, {1, 7, 13}, {2, 8, 14}, {3, 9, 15}, {4, 10,
		5}}

	for _, p := range seq {
		a, b, c := p[0], p[1], p[2]

		v := uint32(final[a])<<16 | uint32(final[b])<<8 | uint32(final[c])
		for i := 0; i < 4; i++ {
			rearranged[rearrangedIndex] = b64t[v&0x3f]
			rearrangedIndex++
			v >>= 6
		}
	}

	v := final[11]
	for i := 0; i < 2; i++ {
		rearranged[rearrangedIndex] = b64t[v&0x3f]
		rearrangedIndex++
		v >>= 6
	}
	final = final[:0]
	return rearranged[:]
}
