package dockerconn

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moby/moby/client"
)

// TestHTTPAndReadOnly는 HTTP 조회와 SDK 변경 요청의 전송 전 차단을 검증한다.
func TestHTTPAndReadOnly(t *testing.T) {
	// 1. Docker 버전 협상과 컨테이너 목록만 제공하는 서버를 준비한다.
	var mutations atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("API-Version", "1.56")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			mutations.Add(1)
		}
		if strings.HasSuffix(r.URL.Path, "/containers/json") {
			io.WriteString(w, `[{"Id":"abc","Names":["/테스트"],"State":"running"}]`)
		}
	}))
	defer server.Close()
	docker, err := New(Server{Name: "HTTP", Address: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer docker.Close()

	// 2. 실제 SDK 목록 조회가 통과하고 삭제 요청은 서버에 도달하지 않아야 한다.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	list, err := docker.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil || len(list.Items) != 1 {
		t.Fatalf("HTTP 목록 조회 실패: %v", err)
	}
	_, err = docker.ContainerRemove(ctx, "abc", client.ContainerRemoveOptions{})
	if err == nil || mutations.Load() != 0 {
		t.Fatalf("변경 요청을 차단하지 않았습니다: %v", err)
	}
}

// certificates는 테스트 전용 CA와 서버·클라이언트 인증서를 만든다.
func certificates(t *testing.T) (string, string, string, tls.Certificate, *x509.CertPool) {
	t.Helper()
	// 1. 개인키와 테스트 CA를 생성한다.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "테스트 CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caPEM)

	// 2. 서버와 클라이언트 양쪽 용도의 테스트 인증서를 파일에 저장한다.
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), NotBefore: ca.NotBefore, NotAfter: ca.NotAfter, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}, KeyUsage: x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	dir := t.TempDir()
	caPath, certPath, keyPath := filepath.Join(dir, "ca.pem"), filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	for path, data := range map[string][]byte{caPath: caPEM, certPath: certPEM, keyPath: keyPEM} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	return caPath, certPath, keyPath, pair, pool
}

// TestTLSAndMTLS는 서버 검증·사설 CA·선택적 클라이언트 인증을 실제 TLS로 검증한다.
func TestTLSAndMTLS(t *testing.T) {
	ca, cert, key, pair, pool := certificates(t)
	for _, mtls := range []bool{false, true} {
		t.Run(map[bool]string{false: "HTTPS", true: "mTLS"}[mtls], func(t *testing.T) {
			// 1. 선택한 경우에만 클라이언트 인증서를 요구한다.
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("API-Version", "1.56")
			}))
			server.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}
			if mtls {
				server.TLS.ClientAuth = tls.RequireAndVerifyClientCert
				server.TLS.ClientCAs = pool
			}
			server.StartTLS()
			defer server.Close()
			config := Server{Name: "TLS", Address: server.URL, CAFile: ca, MTLS: mtls}
			if mtls {
				config.CertFile, config.KeyFile = cert, key
			}
			docker, err := New(config)
			if err != nil {
				t.Fatal(err)
			}
			defer docker.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if _, err := docker.Ping(ctx, client.PingOptions{}); err != nil {
				t.Fatal("TLS 응답 실패:", err)
			}

			// 2. 사설 CA를 빼면 mTLS 사용 여부와 관계없이 서버 검증에 실패한다.
			config.CAFile = ""
			untrusted, err := New(config)
			if err != nil {
				t.Fatal(err)
			}
			defer untrusted.Close()
			if _, err := untrusted.Ping(ctx, client.PingOptions{}); err == nil {
				t.Fatal("신뢰하지 않는 서버 인증서를 허용했습니다")
			}
			if mtls {
				config.CAFile = ca
				config.MTLS = false
				config.CertFile, config.KeyFile = "", ""
				anonymous, err := New(config)
				if err != nil {
					t.Fatal(err)
				}
				defer anonymous.Close()
				if _, err := anonymous.Ping(ctx, client.PingOptions{}); err == nil {
					t.Fatal("mTLS 서버에 클라이언트 인증서 없이 접속했습니다")
				}
			}
		})
	}
}

// TestInvalidConnections는 프로토콜 혼합·인증정보 내장·셸 인자 주입을 거부한다.
func TestInvalidConnections(t *testing.T) {
	for _, s := range []Server{{Name: "x", Address: "http://user:secret@host"}, {Name: "x", Address: "http://host", MTLS: true}, {Name: "x", Address: "https://host", MTLS: true}, {Name: "x", Address: "http://host/api"}, {Name: "x", Address: "http://host?token=secret"}, {Name: "x", Address: "ssh://-oProxyCommand=evil"}, {Name: "x", Address: "ssh://user:password@host"}} {
		if s.Validate() == nil {
			data, _ := json.Marshal(s)
			t.Fatalf("잘못된 연결을 허용했습니다: %s", data)
		}
	}
	args := strings.Join(sshArgs(Server{Address: "ssh://user@alias:2222", IdentityFile: "/tmp/my key"}), "|")
	if !strings.Contains(args, "StrictHostKeyChecking=yes") || !strings.Contains(args, "BatchMode=yes") || !strings.HasSuffix(args, "--|alias|docker|system|dial-stdio") || !strings.Contains(args, "-p|2222|-l|user") {
		t.Fatal("SSH 키·호스트 확인·고정 명령 계약이 잘못되었습니다")
	}
}
