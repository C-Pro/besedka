package storage

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"

	"golang.org/x/crypto/argon2"
)

const (
	saltLen    = 16
	keyLen     = 32
	// 15 rounds shows around 0.15s in BenchmarkArgon on my machine.
	argonTime  = 15
	argonMem   = 64 * 1024
	argonThr   = 4
)

type Crypter struct {
	salt []byte
	aead cipher.AEAD
}

// NewCrypter creates an instance of Crypter to encode/decode byte slices.
// If salt parameter is empty, it will be generated randomly.
// Make sure to store the salt somewhere to be able to decrypt the data later.
func NewCrypter(secret []byte, salt []byte) (*Crypter, error) {
	var err error
	if len(salt) == 0 {
		if salt, err = genSalt(); err != nil {
			return nil, err
		}
	}

	key := deriveKey(secret, salt)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return nil, err
	}

	return &Crypter{
		salt: salt,
		aead: aead,
	}, nil
}

// Salt returns the salt used to derive the encryption key.
// It must be persisted alongside encrypted data so that the Crypter can be
// reconstructed with the same key on load.
func (c *Crypter) Salt() []byte {
	return c.salt
}

func genSalt() ([]byte, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	return salt, nil
}

func deriveKey(secret, salt []byte) []byte {
	return argon2.IDKey(secret, salt, argonTime, argonMem, argonThr, keyLen)
}

// Encrypt encrypts data using AES-256-GCM with a random nonce prepended to
// the ciphertext.
func (c *Crypter) Encrypt(data []byte) ([]byte, error) {
	return c.aead.Seal(nil, nil, data, nil), nil
}

// Decrypt decrypts data produced by Encrypt, extracting the prepended nonce
// and using AES-256-GCM to recover the plaintext.
func (c *Crypter) Decrypt(data []byte) ([]byte, error) {
	return c.aead.Open(nil, nil, data, nil)
}
