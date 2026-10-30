// Package push sends APNs notifications: a token-authenticated HTTP/2 client, a dispatcher that
// turns committed changes into pushes, and the morning summary. Payloads carry ids and counters
// only — event titles never pass through Apple (ENGINEERING 6.7).
package push

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"calendar/server/internal/telemetry"
)

// Message is one notification for one device.
type Message struct {
	Kind       string // sync | moved | cancelled | changed | reauth | summary | reminder (telemetry label only; not sent)
	Type       string // "alert" | "background"
	Priority   int    // 10 immediate, 5 power-friendly
	CollapseID string
	Body       []byte // JSON payload
}

// Target is one registered device.
type Target struct {
	Token, Env, BundleID string
}

var (
	// ErrUnregistered means APNs rejected the device token for good; the endpoint is dropped.
	ErrUnregistered = errors.New("apns: device token is no longer valid")
	// ErrTransient is a failure worth retrying later (rate limit, 5xx, network).
	ErrTransient = errors.New("apns: temporary failure")
	// ErrThrottled is the 429 flavour of ErrTransient (also matches errors.Is(err, ErrTransient)).
	ErrThrottled = errors.New("apns: throttled")
)

// Sender delivers a message. The real one is *APNs; tests use a recording fake.
type Sender interface {
	Send(ctx context.Context, t Target, m Message) error
}

// APNsConfig is the token-auth credential set (secrets from files, never from git).
type APNsConfig struct {
	KeyPEM  []byte // the .p8 file
	KeyID   string
	TeamID  string
	Topic   string // bundle id
	BaseURL map[string]string
	Client  *http.Client
	Now     func() time.Time
}

const (
	sandboxURL    = "https://api.sandbox.push.apple.com"
	productionURL = "https://api.push.apple.com"
	jwtLifetime   = 50 * time.Minute // Apple: refresh between 20 and 60 minutes
)

// APNs is a Sender over the HTTP/2 provider API.
type APNs struct {
	cfg  APNsConfig
	key  *ecdsa.PrivateKey
	mu   sync.Mutex
	jwt  string
	made time.Time
}

// LoadKey parses a .p8 (PKCS#8 PEM, EC P-256).
func LoadKey(pemBytes []byte) (*ecdsa.PrivateKey, error) {
	blk, _ := pem.Decode(pemBytes)
	if blk == nil {
		return nil, errors.New("apns key: not PEM")
	}
	k, err := x509.ParsePKCS8PrivateKey(blk.Bytes)
	if err != nil {
		return nil, fmt.Errorf("apns key: %w", err)
	}
	ek, ok := k.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("apns key: not an EC key")
	}
	return ek, nil
}

// NewAPNs builds a client. Topic, KeyID and TeamID are required.
func NewAPNs(cfg APNsConfig) (*APNs, error) {
	if cfg.KeyID == "" || cfg.TeamID == "" || cfg.Topic == "" {
		return nil, errors.New("apns: key id, team id and topic are required")
	}
	k, err := LoadKey(cfg.KeyPEM)
	if err != nil {
		return nil, err
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: 20 * time.Second}
	}
	// client spans for APNs calls: templated path (the device token never reaches a span), no query, no headers
	traced := *cfg.Client
	traced.Transport = telemetry.WrapClient(traced.Transport, "apns")
	cfg.Client = &traced
	if cfg.BaseURL == nil {
		cfg.BaseURL = map[string]string{"sandbox": sandboxURL, "production": productionURL}
	}
	return &APNs{cfg: cfg, key: k}, nil
}

// LoadAPNsFiles reads the .p8 from disk.
func LoadAPNsFiles(keyFile, keyID, teamID, topic string) (*APNs, error) {
	b, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, err
	}
	return NewAPNs(APNsConfig{KeyPEM: b, KeyID: keyID, TeamID: teamID, Topic: topic})
}

// LoadAPNsValue takes the .p8 as a value, for hosts where an env var is easier than a file: the PEM itself, the PEM on
// one line with literal \n, or base64 of the file.
func LoadAPNsValue(value, keyID, teamID, topic string) (*APNs, error) {
	v := strings.TrimSpace(value)
	if !strings.Contains(v, "BEGIN") {
		if b, err := base64.StdEncoding.DecodeString(v); err == nil {
			v = string(b)
		}
	}
	v = strings.ReplaceAll(v, `\n`, "\n")
	return NewAPNs(APNsConfig{KeyPEM: []byte(v), KeyID: keyID, TeamID: teamID, Topic: topic})
}

func (a *APNs) now() time.Time {
	if a.cfg.Now != nil {
		return a.cfg.Now()
	}
	return time.Now()
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// token returns the provider JWT (ES256), reusing it for jwtLifetime. force makes a new one.
func (a *APNs) token(force bool) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !force && a.jwt != "" && a.now().Sub(a.made) < jwtLifetime {
		return a.jwt, nil
	}
	hdr, _ := json.Marshal(map[string]string{"alg": "ES256", "kid": a.cfg.KeyID})
	cl, _ := json.Marshal(map[string]any{"iss": a.cfg.TeamID, "iat": a.now().Unix()})
	signing := b64(hdr) + "." + b64(cl)
	h := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, a.key, h[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	a.jwt, a.made = signing+"."+b64(sig), a.now()
	return a.jwt, nil
}

// VerifyJWT checks a provider token against the public key (used by tests and doctor checks).
func VerifyJWT(tok string, pub *ecdsa.PublicKey) bool {
	var parts [3]string
	n := 0
	start := 0
	for i := 0; i < len(tok) && n < 2; i++ {
		if tok[i] == '.' {
			parts[n] = tok[start:i]
			n++
			start = i + 1
		}
	}
	if n != 2 {
		return false
	}
	parts[2] = tok[start:]
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(sig) != 64 {
		return false
	}
	h := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	return ecdsa.Verify(pub, h[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:]))
}

type apnsErr struct {
	Reason string `json:"reason"`
}

// Send posts to /3/device/<token>. A 403 ExpiredProviderToken is retried once with a fresh JWT.
func (a *APNs) Send(ctx context.Context, t Target, m Message) error {
	base := a.cfg.BaseURL[t.Env]
	if base == "" {
		return fmt.Errorf("apns: unknown environment %q", t.Env)
	}
	topic := t.BundleID
	if topic == "" {
		topic = a.cfg.Topic
	}
	force := false
	for attempt := 0; ; attempt++ {
		tok, err := a.token(force)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/3/device/"+t.Token, bytes.NewReader(m.Body))
		if err != nil {
			return err
		}
		req.Header.Set("authorization", "bearer "+tok)
		req.Header.Set("apns-topic", topic)
		req.Header.Set("apns-push-type", m.Type)
		req.Header.Set("apns-priority", strconv.Itoa(m.Priority))
		if m.CollapseID != "" {
			req.Header.Set("apns-collapse-id", m.CollapseID)
		}
		resp, err := a.cfg.Client.Do(req)
		if err != nil {
			// a *url.Error embeds the request URL, which contains the device token: keep only the cause
			var ue *url.Error
			if errors.As(err, &ue) {
				err = ue.Err
			}
			return fmt.Errorf("%w: %v", ErrTransient, err)
		}
		trace.SpanFromContext(ctx).SetAttributes(attribute.Int("http.response.status_code", resp.StatusCode))
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return nil
		}
		var e apnsErr
		_ = json.Unmarshal(body, &e)
		switch {
		case resp.StatusCode == 403 && e.Reason == "ExpiredProviderToken" && attempt == 0:
			force = true
			continue
		case resp.StatusCode == 410, e.Reason == "Unregistered", e.Reason == "BadDeviceToken", e.Reason == "DeviceTokenNotForTopic":
			return fmt.Errorf("%w (%s)", ErrUnregistered, e.Reason)
		case resp.StatusCode == 429:
			return fmt.Errorf("%w: %w: HTTP %d %s", ErrTransient, ErrThrottled, resp.StatusCode, e.Reason)
		case resp.StatusCode >= 500:
			return fmt.Errorf("%w: HTTP %d %s", ErrTransient, resp.StatusCode, e.Reason)
		}
		return fmt.Errorf("apns: HTTP %d %s", resp.StatusCode, e.Reason)
	}
}
