package storage

import (
	"bytes"
	"testing"
)

func TestNewCrypter(t *testing.T) {
	tests := []struct {
		name    string
		secret  []byte
		salt    []byte
		wantErr bool
	}{
		{
			name:   "generates salt when none provided",
			secret: []byte("supersecret"),
		},
		{
			name:   "uses provided salt",
			secret: []byte("supersecret"),
			salt:   bytes.Repeat([]byte{0x01}, saltLen),
		},
		{
			name:   "empty secret",
			secret: []byte{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewCrypter(tc.secret, tc.salt)
			if (err != nil) != tc.wantErr {
				t.Fatalf("NewCrypter() error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}

			if len(c.key) != keyLen {
				t.Errorf("key length = %d, want %d", len(c.key), keyLen)
			}
			if len(c.salt) != saltLen {
				t.Errorf("salt length = %d, want %d", len(c.salt), saltLen)
			}

			if len(tc.salt) > 0 && !bytes.Equal(c.salt, tc.salt) {
				t.Errorf("salt not preserved: got %x, want %x", c.salt, tc.salt)
			}
		})
	}
}

func TestNewCrypter_DeterministicKey(t *testing.T) {
	secret := []byte("my-secret")
	salt := bytes.Repeat([]byte{0xAB}, saltLen)

	c1, err := NewCrypter(secret, salt)
	if err != nil {
		t.Fatal(err)
	}
	c2, err := NewCrypter(secret, salt)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(c1.key, c2.key) {
		t.Error("same secret+salt must produce the same key")
	}
}

func TestEncryptDecrypt(t *testing.T) {
	tests := []struct {
		name      string
		plaintext []byte
	}{
		{name: "normal text", plaintext: []byte("hello, world")},
		{name: "empty plaintext", plaintext: []byte{}},
		{name: "binary data", plaintext: bytes.Repeat([]byte{0x00, 0xFF}, 100)},
		{name: "large payload", plaintext: bytes.Repeat([]byte("A"), 1<<20)},
	}

	c, err := NewCrypter([]byte("test-secret"), nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ciphertext, err := c.Encrypt(tc.plaintext)
			if err != nil {
				t.Fatalf("Encrypt() error = %v", err)
			}

			if bytes.Equal(ciphertext, tc.plaintext) && len(tc.plaintext) > 0 {
				t.Error("ciphertext must differ from plaintext")
			}

			got, err := c.Decrypt(ciphertext)
			if err != nil {
				t.Fatalf("Decrypt() error = %v", err)
			}

			if !bytes.Equal(got, tc.plaintext) {
				t.Errorf("roundtrip mismatch: got %q, want %q", got, tc.plaintext)
			}
		})
	}
}

func TestEncrypt_RandomNonce(t *testing.T) {
	c, err := NewCrypter([]byte("test-secret"), nil)
	if err != nil {
		t.Fatal(err)
	}

	plaintext := []byte("same message")
	ct1, err := c.Encrypt(plaintext)
	if err != nil {
		t.Fatal(err)
	}
	ct2, err := c.Encrypt(plaintext)
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Equal(ct1, ct2) {
		t.Error("two encryptions of the same plaintext must produce different ciphertexts")
	}
}

func TestDecrypt_Errors(t *testing.T) {
	c, err := NewCrypter([]byte("test-secret"), nil)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		data []byte
	}{
		{name: "too short", data: []byte{0x01, 0x02}},
		{name: "tampered ciphertext", data: func() []byte {
			ct, _ := c.Encrypt([]byte("hello"))
			ct[len(ct)-1] ^= 0xFF
			return ct
		}()},
		{name: "wrong key", data: func() []byte {
			ct, _ := c.Encrypt([]byte("hello"))
			return ct
		}()},
	}

	wrongKey, err := NewCrypter([]byte("different-secret"), nil)
	if err != nil {
		t.Fatal(err)
	}

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			decrypter := c
			if i == 2 {
				decrypter = wrongKey
			}
			_, err := decrypter.Decrypt(tc.data)
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestSalt_Persistence(t *testing.T) {
	secret := []byte("my-secret")

	c1, err := NewCrypter(secret, nil)
	if err != nil {
		t.Fatal(err)
	}

	ct, err := c1.Encrypt([]byte("important data"))
	if err != nil {
		t.Fatal(err)
	}

	c2, err := NewCrypter(secret, c1.Salt())
	if err != nil {
		t.Fatal(err)
	}

	got, err := c2.Decrypt(ct)
	if err != nil {
		t.Fatalf("Decrypt with reconstructed Crypter failed: %v", err)
	}

	if !bytes.Equal(got, []byte("important data")) {
		t.Errorf("got %q", got)
	}
}
