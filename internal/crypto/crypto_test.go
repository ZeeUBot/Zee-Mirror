package crypto

import (
	"bytes"
	"crypto/rand"
	"strings"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	msg := []byte("top-secret-payload")

	enc, err := Encrypt(msg, key)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if strings.Contains(enc, "top-secret") {
		t.Fatal("ciphertext leaks plaintext")
	}

	dec, err := Decrypt(enc, key)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(dec, msg) {
		t.Fatalf("round trip mismatch: got %q", dec)
	}

	enc2, _ := Encrypt(msg, key)
	if enc2 == enc {
		t.Fatal("nonce reuse: identical ciphertexts for same plaintext")
	}

	if _, err := Decrypt(enc, make([]byte, 32)); err == nil {
		t.Error("decrypt with wrong key should fail")
	}
	if _, err := Decrypt("not-hex!!", key); err == nil {
		t.Error("decrypt of invalid hex should fail")
	}
}

func TestEncrypt_RejectsBadKeySize(t *testing.T) {
	if _, err := Encrypt([]byte("x"), make([]byte, 7)); err == nil {
		t.Error("expected error for invalid AES key size")
	}
}
