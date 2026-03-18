package storage

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"

	"golang.org/x/crypto/argon2"
)

const (
	saltLen    = 16
	keyLen     = 32
	argonTime  = 1
	argonMem   = 64 * 1024
	argonThr   = 4
)

type Crypter struct {
	key  []byte
	salt []byte
}

func NewCrypter(secret []byte, salt []byte) (*Crypter, error) {
	var err error
	if len(salt) == 0 {
		if salt, err = genSalt(); err != nil {
			return nil, err
		}
	}

	key := deriveKey(secret, salt)

	return &Crypter{
		key:  key,
		salt: salt,
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
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}

	ciphertext := gcm.Seal(nonce, nonce, data, nil)
	return ciphertext, nil
}

// Decrypt decrypts data produced by Encrypt, extracting the prepended nonce
// and using AES-256-GCM to recover the plaintext.
func (c *Crypter) Decrypt(data []byte) ([]byte, error) {
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}
