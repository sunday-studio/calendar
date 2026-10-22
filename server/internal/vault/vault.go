// Package vault seals small secrets (OAuth refresh tokens etc.) with XChaCha20-Poly1305.
package vault

import (
	"crypto/rand"
	"errors"

	"golang.org/x/crypto/chacha20poly1305"
)

const KeySize = chacha20poly1305.KeySize // 32

var ErrOpen = errors.New("vault: cannot open sealed value")

type Vault struct {
	aead interface {
		Seal(dst, nonce, plaintext, additionalData []byte) []byte
		Open(dst, nonce, ciphertext, additionalData []byte) ([]byte, error)
		NonceSize() int
	}
}

func New(key []byte) (*Vault, error) {
	if len(key) != KeySize {
		return nil, errors.New("vault: master key must be exactly 32 bytes")
	}
	a, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	return &Vault{aead: a}, nil
}

// Seal returns nonce||ciphertext. aad binds the value to its row (e.g. the account id).
func (v *Vault) Seal(plaintext, aad []byte) ([]byte, error) {
	nonce := make([]byte, v.aead.NonceSize(), v.aead.NonceSize()+len(plaintext)+chacha20poly1305.Overhead)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return v.aead.Seal(nonce, nonce, plaintext, aad), nil
}

func (v *Vault) Open(sealed, aad []byte) ([]byte, error) {
	n := v.aead.NonceSize()
	if len(sealed) < n+chacha20poly1305.Overhead {
		return nil, ErrOpen
	}
	pt, err := v.aead.Open(nil, sealed[:n], sealed[n:], aad)
	if err != nil {
		return nil, ErrOpen
	}
	return pt, nil
}
