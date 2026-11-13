package security

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	TokenTypeAccess   = "access"
	TokenTypeTakeover = "takeover"
)

var (
	ErrTokenExpired = errors.New("token expired")
	ErrTokenInvalid = errors.New("token invalid")
)

// Claims is the JWT payload. `typ` stops a takeover token being replayed as an
// access token (and vice versa); `sid` binds an access token to one session row.
type Claims struct {
	Type      string `json:"typ"`
	SessionID string `json:"sid,omitempty"`
	jwt.RegisteredClaims
}

type TokenManager struct {
	secret      []byte
	issuer      string
	audience    string
	accessTTL   time.Duration
	takeoverTTL time.Duration
}

func NewTokenManager(secret []byte, issuer, audience string, accessTTL, takeoverTTL time.Duration) *TokenManager {
	return &TokenManager{secret: secret, issuer: issuer, audience: audience, accessTTL: accessTTL, takeoverTTL: takeoverTTL}
}

func (m *TokenManager) IssueAccess(userID, sessionID uuid.UUID) (string, time.Time, error) {
	return m.issue(TokenTypeAccess, userID, sessionID.String(), m.accessTTL)
}

func (m *TokenManager) IssueTakeover(userID uuid.UUID) (string, time.Time, error) {
	return m.issue(TokenTypeTakeover, userID, "", m.takeoverTTL)
}

func (m *TokenManager) ParseAccess(raw string) (*Claims, error) {
	c, err := m.parse(raw, TokenTypeAccess)
	if err != nil {
		return nil, err
	}
	if c.SessionID == "" {
		return nil, ErrTokenInvalid
	}
	return c, nil
}

func (m *TokenManager) ParseTakeover(raw string) (*Claims, error) {
	return m.parse(raw, TokenTypeTakeover)
}

func (m *TokenManager) issue(typ string, sub uuid.UUID, sid string, ttl time.Duration) (string, time.Time, error) {
	now := time.Now()
	exp := now.Add(ttl)
	claims := Claims{
		Type:      typ,
		SessionID: sid,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.issuer,
			Subject:   sub.String(),
			Audience:  jwt.ClaimStrings{m.audience},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
			ID:        uuid.NewString(),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	return signed, exp, err
}

func (m *TokenManager) parse(raw, typ string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(raw, claims,
		func(*jwt.Token) (any, error) { return m.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(m.issuer),
		jwt.WithAudience(m.audience),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(5*time.Second),
	)
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, ErrTokenInvalid
	}
	if claims.Type != typ || claims.ID == "" {
		return nil, ErrTokenInvalid
	}
	return claims, nil
}

// RandomToken returns n bytes of CSPRNG output, base64url encoded.
func RandomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HashToken returns SHA-256(token). High-entropy random tokens don't need a slow KDF.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func EqualHash(a, b []byte) bool {
	return len(a) > 0 && subtle.ConstantTimeCompare(a, b) == 1
}
