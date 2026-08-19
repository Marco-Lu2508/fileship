package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	accessTokenDuration  = 15 * time.Minute
	refreshTokenDuration = 7 * 24 * time.Hour
)

type Claims struct {
	UserID       int64  `json:"uid"`
	IsAdmin      bool   `json:"adm"`
	TwoFAPending bool   `json:"2fa_pending,omitempty"` // true = 2FA noch nicht abgeschlossen
	jwt.RegisteredClaims
}

func GenerateAccessToken(secret string, userID int64, isAdmin bool) (string, error) {
	return generateToken(secret, userID, isAdmin, false)
}

// GeneratePendingToken erzeugt ein kurzlebiges Token das nur 2FA-Verify erlaubt
func GeneratePendingToken(secret string, userID int64, isAdmin bool) (string, error) {
	return generateToken(secret, userID, isAdmin, true)
}

func generateToken(secret string, userID int64, isAdmin bool, pending bool) (string, error) {
	dur := accessTokenDuration
	if pending {
		dur = 5 * time.Minute // Sehr kurz — nur für 2FA-Step
	}
	claims := Claims{
		UserID:       userID,
		IsAdmin:      isAdmin,
		TwoFAPending: pending,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(dur)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

func ParseAccessToken(secret, tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

func GenerateRefreshToken() (string, time.Time, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", time.Time{}, err
	}
	return hex.EncodeToString(b), time.Now().Add(refreshTokenDuration), nil
}
