package credential

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
)

// Cipher는 수신처 자격 증명을 인증된 암호문으로 보관한다.
type Cipher struct {
	aead cipher.AEAD
}

// NewCipher는 Base64로 인코딩한 32바이트 키로 암호화기를 만든다.
func NewCipher(encoded string) (*Cipher, error) {
	// 1. 키 길이를 확인하고 AES 암호를 생성한다.
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(key) != 32 {
		return nil, errors.New("WHALECHIME_ENCRYPTION_KEY에는 32바이트 Base64 키가 필요합니다")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	// 2. 변조 여부도 확인하는 GCM 암호화기를 생성한다.
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

// Seal은 매번 다른 nonce와 수신처 식별자를 사용해 설정을 암호화한다.
func (c *Cipher) Seal(data []byte, destinationID string) []byte {
	nonce := make([]byte, c.aead.NonceSize())
	_, _ = rand.Read(nonce)
	return c.aead.Seal(nonce, nonce, data, []byte(destinationID))
}

// Open은 수신처 식별자와 무결성을 확인한 뒤 설정을 복호화한다.
func (c *Cipher) Open(data []byte, destinationID string) ([]byte, error) {
	if len(data) < c.aead.NonceSize() {
		return nil, errors.New("수신처 암호문이 올바르지 않습니다")
	}
	return c.aead.Open(nil, data[:c.aead.NonceSize()], data[c.aead.NonceSize():], []byte(destinationID))
}

// NewToken은 추측하기 어려운 API 토큰이나 내부 식별자를 생성한다.
func NewToken(prefix string) string {
	data := make([]byte, 32)
	_, _ = rand.Read(data)
	return prefix + base64.RawURLEncoding.EncodeToString(data)
}

// Hash는 인증 토큰 원문 대신 저장할 SHA-256 값을 계산한다.
func Hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
