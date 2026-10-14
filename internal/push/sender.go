// Package push envía Web Push cifrado y programa avisos de Pulse.
package push

import (
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

type Subscription = webpush.Subscription

type Result struct {
	Status     int
	RetryAfter time.Duration
}
type Sender interface {
	Send(context.Context, Subscription, []byte) (Result, error)
}

type WebSender struct {
	PublicKey, PrivateKey, Subject string
	Client                         webpush.HTTPClient
}

func NewSender(publicKey, privateFile, subject string) (*WebSender, error) {
	b, err := os.ReadFile(privateFile)
	if err != nil {
		return nil, errors.New("no se pudo leer el secreto VAPID")
	}
	private := strings.TrimSpace(string(b))
	key, err := base64.RawURLEncoding.DecodeString(private)
	if err != nil {
		return nil, errors.New("clave privada VAPID inválida")
	}
	k, err := ecdh.P256().NewPrivateKey(key)
	if err != nil {
		return nil, errors.New("clave privada VAPID inválida")
	}
	if base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes()) != publicKey {
		return nil, errors.New("las claves VAPID no corresponden")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, errors.New("no se pudo resolver el servicio push")
		}
		for _, a := range ips {
			if !a.IP.IsGlobalUnicast() || a.IP.IsPrivate() || a.IP.IsLoopback() || a.IP.IsLinkLocalUnicast() {
				return nil, errors.New("destino push no público")
			}
		}
		var last error
		for _, a := range ips {
			c, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, net.JoinHostPort(a.IP.String(), port))
			if err == nil {
				return c, nil
			}
			last = err
		}
		if last == nil {
			last = errors.New("servicio push sin dirección")
		}
		return nil, last
	}
	client := &http.Client{Timeout: 15 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &WebSender{PublicKey: publicKey, PrivateKey: private, Subject: subject, Client: client}, nil
}

// Solo servicios de navegador conocidos; impide convertir una suscripción en SSRF.
func ValidateSubscription(s Subscription) error {
	u, err := url.Parse(s.Endpoint)
	if err != nil || len(s.Endpoint) > 4096 || u.Scheme != "https" || u.User != nil || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") {
		return errors.New("destino push inválido")
	}
	host := strings.ToLower(u.Hostname())
	if host != "fcm.googleapis.com" && host != "updates.push.services.mozilla.com" && host != "push.services.mozilla.com" && host != "web.push.apple.com" && !strings.HasSuffix(host, ".notify.windows.com") {
		return errors.New("servicio push no admitido")
	}
	pk, err := base64.RawURLEncoding.DecodeString(s.Keys.P256dh)
	if err != nil {
		return errors.New("clave push inválida")
	}
	if _, err := ecdh.P256().NewPublicKey(pk); err != nil {
		return errors.New("clave push inválida")
	}
	auth, err := base64.RawURLEncoding.DecodeString(s.Keys.Auth)
	if err != nil || len(auth) != 16 {
		return errors.New("secreto push inválido")
	}
	return nil
}

func (s *WebSender) Send(ctx context.Context, sub Subscription, payload []byte) (Result, error) {
	response, err := webpush.SendNotificationWithContext(ctx, payload, &sub, &webpush.Options{HTTPClient: s.Client, VAPIDPublicKey: s.PublicKey, VAPIDPrivateKey: s.PrivateKey, Subscriber: s.Subject, TTL: 3600, Urgency: webpush.UrgencyNormal})
	if err != nil {
		return Result{}, errors.New("falló el envío push")
	}
	defer response.Body.Close()
	io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	result := Result{Status: response.StatusCode}
	if retry := response.Header.Get("Retry-After"); retry != "" {
		if d, err := time.ParseDuration(retry + "s"); err == nil {
			result.RetryAfter = d
		} else if t, err := http.ParseTime(retry); err == nil {
			result.RetryAfter = time.Until(t)
		}
	}
	return result, nil
}
