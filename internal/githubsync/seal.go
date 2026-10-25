package githubsync

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
)

// sealer encrypts user tokens at rest. Associated data binds each ciphertext
// to its account and field, so rows cannot be swapped between accounts.
type sealer struct{ aead cipher.AEAD }

func newSealer(key []byte) (sealer, error) {
	if len(key) != 32 {
		return sealer{}, errors.New("GitHub token key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return sealer{}, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return sealer{}, err
	}
	return sealer{aead}, nil
}

func (s sealer) seal(ownerID, field, value string) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return s.aead.Seal(nonce, nonce, []byte(value), []byte(ownerID+"\x00"+field)), nil
}

func (s sealer) open(ownerID, field string, sealed []byte) (string, error) {
	size := s.aead.NonceSize()
	if len(sealed) < size {
		return "", errors.New("sealed GitHub token is malformed")
	}
	plain, err := s.aead.Open(nil, sealed[:size], sealed[size:], []byte(ownerID+"\x00"+field))
	if err != nil {
		return "", errors.New("sealed GitHub token cannot be opened; was the token key changed?")
	}
	return string(plain), nil
}
