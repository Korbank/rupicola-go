package main

import "crypto/md5"
import "strings"
import "fmt"
import "crypto/subtle"

// for now only md5 crypt is supported
// __`$1$`__*`{salt}`*__$__*`{checksum}`*, where:
func pwVerify(password string, hashWithSalt string) (bool, error) {
	chunks := strings.Split(hashWithSalt, "$")
	var computedHash string
	var hash string
	if len(chunks) < 2 {
		return false, fmt.Errorf("Invalid hash")
	}
	switch chunks[1] {
	case "1":
		hash = chunks[3]
		computedHash = apacheMD5Crypt(password, chunks[2])
	default:
		return false, fmt.Errorf("Unsupported '%s'", chunks[1])
	}
	return subtle.ConstantTimeCompare([]byte(computedHash), []byte(hash)) == 1, nil
}

// Algorithm taken from: http://code.activestate.com/recipes/325204/
// https://github.com/inejge/pwhash/blob/master/src/md5_crypt.rs
func apacheMD5Crypt(passwd, salt string) string {
	const (
		itoa64         = "./0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
		apacheMd5Magic = "$1$"
	)

	m := md5.New()
	m.Write([]byte(passwd + apacheMd5Magic + salt))

	m2 := md5.New()
	m2.Write([]byte(passwd + salt + passwd))
	mixin := m2.Sum(nil)

	for i := range passwd {
		m.Write([]byte{mixin[i%16]})
	}

	l := len(passwd)

	for l != 0 {
		if l&1 != 0 {
			m.Write([]byte("\x00"))
		} else {
			m.Write([]byte{passwd[0]})
		}
		l >>= 1
	}

	final := m.Sum(nil)

	for i := 0; i < 1000; i++ {
		m3 := md5.New()
		if i&1 != 0 {
			m3.Write([]byte(passwd))
		} else {
			m3.Write([]byte(final))
		}

		if i%3 != 0 {
			m3.Write([]byte(salt))
		}

		if i%7 != 0 {
			m3.Write([]byte(passwd))
		}

		if i&1 != 0 {
			m3.Write([]byte(final))
		} else {
			m3.Write([]byte(passwd))
		}

		final = m3.Sum(nil)
	}

	var rearranged string
	seq := [][3]int{{0, 6, 12}, {1, 7, 13}, {2, 8, 14}, {3, 9, 15}, {4, 10,
		5}}

	for _, p := range seq {
		a, b, c := p[0], p[1], p[2]

		v := int(final[a])<<16 | int(final[b])<<8 | int(final[c])
		for i := 0; i < 4; i++ {
			rearranged += string(itoa64[v&0x3f])
			v >>= 6
		}
	}

	v := int(final[11])
	for i := 0; i < 2; i++ {
		rearranged += string(itoa64[v&0x3f])
		v >>= 6
	}

	return rearranged
}
