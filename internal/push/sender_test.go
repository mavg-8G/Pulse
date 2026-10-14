package push

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"golang.org/x/crypto/hkdf"
)

func TestWebPushEncryptedRoundTrip(t *testing.T) {
	receiver, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	secret := bytes.Repeat([]byte{9}, 16)
	payload := []byte(`{"title":"Pulse","body":"Gastaste $80.00"}`)
	private, public, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []int{201, 404, 410, 429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.Header.Get("Content-Encoding") != "aes128gcm" || !strings.HasPrefix(r.Header.Get("Authorization"), "vapid ") {
					t.Error("cabeceras Web Push inválidas")
				}
				wire, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					return
				}
				if bytes.Contains(wire, payload) {
					t.Error("payload sin cifrar")
				}
				if len(wire) < 86 {
					t.Error("registro cifrado incompleto")
					return
				}
				peer, err := ecdh.P256().NewPublicKey(wire[21:86])
				if err != nil {
					t.Error(err)
					return
				}
				shared, err := receiver.ECDH(peer)
				if err != nil {
					t.Error(err)
					return
				}
				derive := func(ikm, salt, info []byte, n int) []byte {
					out := make([]byte, n)
					if _, err := io.ReadFull(hkdf.New(sha256.New, ikm, salt, info), out); err != nil {
						t.Error(err)
					}
					return out
				}
				info := append(append([]byte("WebPush: info\x00"), receiver.PublicKey().Bytes()...), peer.Bytes()...)
				ikm := derive(shared, secret, info, 32)
				key := derive(ikm, wire[:16], []byte("Content-Encoding: aes128gcm\x00"), 16)
				nonce := derive(ikm, wire[:16], []byte("Content-Encoding: nonce\x00"), 12)
				block, err := aes.NewCipher(key)
				if err != nil {
					t.Error(err)
					return
				}
				gcm, err := cipher.NewGCM(block)
				if err != nil {
					t.Error(err)
					return
				}
				plain, err := gcm.Open(nil, nonce, wire[86:], nil)
				if err != nil {
					t.Error(err)
					return
				}
				plain = bytes.TrimRight(plain, "\x00")
				if !bytes.Equal(plain, append(append([]byte{}, payload...), 2)) {
					t.Error("no recuperó el payload original")
				}
				w.Header().Set("Retry-After", "120")
				w.WriteHeader(status)
			}))
			defer server.Close()
			sender := &WebSender{PublicKey: public, PrivateKey: private, Subject: "mailto:test@example.com", Client: server.Client()}
			sub := Subscription{Endpoint: server.URL, Keys: webpush.Keys{P256dh: base64.RawURLEncoding.EncodeToString(receiver.PublicKey().Bytes()), Auth: base64.RawURLEncoding.EncodeToString(secret)}}
			result, err := sender.Send(context.Background(), sub, payload)
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != status || result.RetryAfter != 2*time.Minute {
				t.Fatalf("resultado %+v", result)
			}
		})
	}
}

func TestVAPIDKeysAndEndpointValidation(t *testing.T) {
	private, public, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "vapid_private")
	if err := os.WriteFile(file, []byte(private), 0600); err != nil {
		t.Fatal(err)
	}
	sender, err := NewSender(public, file, "mailto:test@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if sender.PublicKey != public {
		t.Fatal("clave incorrecta")
	}
	if _, err := NewSender("wrong", file, "mailto:test@example.com"); err == nil {
		t.Fatal("aceptó par incorrecto")
	}
	if _, err := NewSender(public, file+"missing", "mailto:test@example.com"); err == nil {
		t.Fatal("aceptó archivo faltante")
	}
	if err := os.WriteFile(file, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSender(public, file, "mailto:test@example.com"); err == nil {
		t.Fatal("aceptó secreto inválido")
	}
	good := validSubscription(t, "https://fcm.googleapis.com/push/test")
	if err := ValidateSubscription(good); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"https://127.0.0.1/push", "https://localhost/push", "https://fcm.googleapis.com:8080/push", "https://fcm.googleapis.com.evil.test/push", "https://u:p@web.push.apple.com/push", "https://fcm.googleapis.com/push#fragment"} {
		sub := good
		sub.Endpoint = endpoint
		if ValidateSubscription(sub) == nil {
			t.Errorf("aceptó %s", endpoint)
		}
	}
	for _, host := range []string{"updates.push.services.mozilla.com", "web.push.apple.com", "wns.notify.windows.com"} {
		sub := good
		sub.Endpoint = "https://" + host + "/push"
		if err := ValidateSubscription(sub); err != nil {
			t.Error(err)
		}
	}
	sub := good
	sub.Keys.Auth = "bad"
	if ValidateSubscription(sub) == nil {
		t.Fatal("auth inválida aceptada")
	}
	sub = good
	sub.Keys.P256dh = "bad"
	if ValidateSubscription(sub) == nil {
		t.Fatal("p256dh inválida aceptada")
	}
	// La protección de red bloquea incluso un nombre permitido que resuelva a loopback.
	transport := sender.Client.(*http.Client).Transport.(*http.Transport)
	if conn, err := transport.DialContext(context.Background(), "tcp", "127.0.0.1:443"); err == nil {
		conn.Close()
		t.Fatal("dial a loopback permitido")
	}
}
