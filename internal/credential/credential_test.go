package credential

import (
	"encoding/base64"
	"testing"
)

// TestCipherBinding은 암호문의 무결성과 수신처 간 복사 방지를 검증한다.
func TestCipherBinding(t *testing.T) {
	// 1. 수신처 식별자에 바인딩한 암호문을 만든다.
	cipher, err := NewCipher(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	sealed := cipher.Seal([]byte("private-webhook-token"), "destination-a")
	data, err := cipher.Open(sealed, "destination-a")
	if err != nil || string(data) != "private-webhook-token" {
		t.Fatal("정상 복호화 실패")
	}

	// 2. 다른 수신처로 복사하거나 변조한 암호문은 거부한다.
	if _, err := cipher.Open(sealed, "destination-b"); err == nil {
		t.Fatal("다른 수신처의 암호문을 복호화했습니다")
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := cipher.Open(sealed, "destination-a"); err == nil {
		t.Fatal("변조한 암호문을 복호화했습니다")
	}
}
