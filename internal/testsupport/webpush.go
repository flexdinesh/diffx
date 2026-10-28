package testsupport

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"testing"
)

// PushDevice is a browser push subscription's receiving keys.
type PushDevice struct {
	Private *ecdh.PrivateKey
	Auth    []byte
}

// NewPushDevice generates receiving keys, as a browser does when subscribing.
func NewPushDevice(t testing.TB) PushDevice {
	t.Helper()
	private, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	if _, err := rand.Read(auth); err != nil {
		t.Fatal(err)
	}
	return PushDevice{Private: private, Auth: auth}
}

// P256dh and AuthSecret are the base64url keys of PushSubscription.toJSON().
func (device PushDevice) P256dh() string {
	return base64.RawURLEncoding.EncodeToString(device.Private.PublicKey().Bytes())
}
func (device PushDevice) AuthSecret() string {
	return base64.RawURLEncoding.EncodeToString(device.Auth)
}

// Decrypt opens an RFC 8291 aes128gcm message body with a single record, as
// the browser's push stack does before the service worker sees the payload.
func (device PushDevice) Decrypt(t testing.TB, body []byte) []byte {
	t.Helper()
	if len(body) < 21 {
		t.Fatalf("push body too short: %d", len(body))
	}
	salt, recordSize, keyLength := body[:16], binary.BigEndian.Uint32(body[16:20]), int(body[20])
	if len(body) < 21+keyLength || int(recordSize) < len(body)-21-keyLength {
		t.Fatalf("push body header invalid: record %d key %d body %d", recordSize, keyLength, len(body))
	}
	senderKey, ciphertext := body[21:21+keyLength], body[21+keyLength:]
	sender, err := ecdh.P256().NewPublicKey(senderKey)
	if err != nil {
		t.Fatalf("sender key: %v", err)
	}
	shared, err := device.Private.ECDH(sender)
	if err != nil {
		t.Fatal(err)
	}
	keyInfo := "WebPush: info\x00" + string(device.Private.PublicKey().Bytes()) + string(senderKey)
	ikm, err := hkdf.Key(sha256.New, shared, device.Auth, keyInfo, 32)
	if err != nil {
		t.Fatal(err)
	}
	cek, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		t.Fatal(err)
	}
	nonce, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		t.Fatal(err)
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		t.Fatalf("decrypt push payload: %v", err)
	}
	// The last record ends with the 0x02 delimiter followed by zero padding.
	plain = bytes.TrimRight(plain, "\x00")
	if len(plain) == 0 || plain[len(plain)-1] != 2 {
		t.Fatal("push payload lacks the final record delimiter")
	}
	return plain[:len(plain)-1]
}
