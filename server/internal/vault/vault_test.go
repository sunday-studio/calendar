package vault

import (
	"bytes"
	"testing"
)

func key(b byte) []byte { return bytes.Repeat([]byte{b}, KeySize) }

func TestRoundTrip(t *testing.T) {
	v, _ := New(key(1))
	s, err := v.Seal([]byte("refresh-token"), []byte("acct1"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(s, []byte("refresh-token")) {
		t.Fatal("plaintext leaked")
	}
	pt, err := v.Open(s, []byte("acct1"))
	if err != nil || string(pt) != "refresh-token" {
		t.Fatalf("got %q %v", pt, err)
	}
}

func TestNonceUniqueAndTamper(t *testing.T) {
	v, _ := New(key(1))
	a, _ := v.Seal([]byte("x"), nil)
	b, _ := v.Seal([]byte("x"), nil)
	if bytes.Equal(a, b) {
		t.Fatal("nonce reused")
	}
	a[len(a)-1] ^= 1
	if _, err := v.Open(a, nil); err == nil {
		t.Fatal("tampered value opened")
	}
}

func TestWrongKeyAADAndShort(t *testing.T) {
	v1, _ := New(key(1))
	v2, _ := New(key(2))
	s, _ := v1.Seal([]byte("x"), []byte("a"))
	if _, err := v2.Open(s, []byte("a")); err == nil {
		t.Fatal("wrong key opened")
	}
	if _, err := v1.Open(s, []byte("b")); err == nil {
		t.Fatal("wrong aad opened")
	}
	if _, err := v1.Open([]byte{1, 2}, nil); err == nil {
		t.Fatal("short input opened")
	}
	if _, err := New([]byte("short")); err == nil {
		t.Fatal("short key accepted")
	}
}
