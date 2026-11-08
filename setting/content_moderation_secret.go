package setting

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"os"
	"strings"
)

const moderationSecretPrefix = "jev-gcm-v1:"

// Use an explicitly persisted deployment secret; the host's randomly generated
// startup fallback would make stored credentials unreadable after a restart.
func moderationCipher() (cipher.AEAD, error) {
	secret := os.Getenv("CRYPTO_SECRET")
	if secret == "" {
		secret = os.Getenv("SESSION_SECRET")
	}
	if len(secret) < 32 || secret == "random_string" {
		return nil, errors.New("a persistent CRYPTO_SECRET or SESSION_SECRET of at least 32 bytes is required for moderation credentials")
	}
	key := sha256.Sum256([]byte("new-api/content-moderation/v1\x00" + secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, errors.New("moderation encryption unavailable")
	}
	return cipher.NewGCMWithRandomNonce(block)
}

func EncryptContentModerationKey(value string) (string, error) {
	if err := ValidateContentModerationOption(ContentModerationAPIKeyKey, value); err != nil {
		return "", err
	}
	aead, err := moderationCipher()
	if err != nil {
		return "", err
	}
	sealed := aead.Seal(nil, nil, []byte(strings.TrimSpace(value)), []byte(ContentModerationAPIKeyKey))
	return moderationSecretPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

func DecryptContentModerationKey(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	encoded, ok := strings.CutPrefix(value, moderationSecretPrefix)
	if !ok {
		return "", errors.New("moderation credential is not encrypted; save it again through settings")
	}
	aead, err := moderationCipher()
	if err != nil {
		return "", err
	}
	sealed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", errors.New("invalid encrypted moderation credential")
	}
	plain, err := aead.Open(nil, nil, sealed, []byte(ContentModerationAPIKeyKey))
	if err != nil {
		return "", errors.New("cannot decrypt moderation credential")
	}
	return string(plain), nil
}
