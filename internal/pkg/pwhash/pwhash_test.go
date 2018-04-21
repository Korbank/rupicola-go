package pwhash

import "testing"

var (
	password1     = "password"
	password2     = "assword"
	hashWithSalt1 = "$1$sRN7MKRJ$XfbAXBpoOphTIhBRZuPAl."
	hashWithSalt2 = "$1$sRN7MKRJ$XfbAXBpoOphTIhBRZuAPl."
)

func Test1(t *testing.T) {
	isOk, err := Verify(password1, hashWithSalt1)
	if err != nil && isOk {
		t.Fail()
	}
	if !isOk {
		t.Fail()
	}
}

func Test2(t *testing.T) {
	isOk, err := Verify(password1, hashWithSalt2)
	if err != nil && isOk {
		t.Fail()
	}
	if isOk {
		t.Fail()
	}
}
