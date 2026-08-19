package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"time"
)

const (
	totpDigits   = 6
	totpPeriod   = 30 // seconds
	totpWindow   = 1  // allow ±1 window
	backupCount  = 8
	backupLength = 10
)

// GenerateTOTPSecret erstellt einen neuen zufälligen Base32-Secret (160 bit)
func GenerateTOTPSecret() (string, error) {
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw), nil
}

// TOTPNow berechnet den aktuellen TOTP-Code für ein Secret
func TOTPNow(secret string) (string, error) {
	return totpAt(secret, time.Now())
}

// VerifyTOTP prüft einen Code mit ±window Zeitfenstern
func VerifyTOTP(secret, code string) bool {
	code = strings.TrimSpace(code)
	now := time.Now()
	for delta := -totpWindow; delta <= totpWindow; delta++ {
		t := now.Add(time.Duration(delta*totpPeriod) * time.Second)
		c, err := totpAt(secret, t)
		if err == nil && c == code {
			return true
		}
	}
	return false
}

// TOTPProvisioningURI erstellt den otpauth:// URI für QR-Code-Generierung
func TOTPProvisioningURI(secret, accountName, issuer string) string {
	return fmt.Sprintf(
		"otpauth://totp/%s:%s?secret=%s&issuer=%s&algorithm=SHA1&digits=%d&period=%d",
		issuer, accountName, secret, issuer, totpDigits, totpPeriod,
	)
}

// GenerateBackupCodes erstellt N zufällige Backup-Codes
func GenerateBackupCodes() ([]string, error) {
	codes := make([]string, backupCount)
	for i := range codes {
		b := make([]byte, backupLength/2)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		codes[i] = fmt.Sprintf("%x", b)
	}
	return codes, nil
}

// ── internal ─────────────────────────────────────

func totpAt(secret string, t time.Time) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(
		strings.ToUpper(strings.TrimSpace(secret)),
	)
	if err != nil {
		return "", err
	}

	counter := uint64(math.Floor(float64(t.Unix()) / float64(totpPeriod)))
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, counter)

	mac := hmac.New(sha1.New, key)
	mac.Write(buf)
	h := mac.Sum(nil)

	offset := h[len(h)-1] & 0x0f
	code := (uint32(h[offset])&0x7f)<<24 |
		uint32(h[offset+1])<<16 |
		uint32(h[offset+2])<<8 |
		uint32(h[offset+3])

	code = code % uint32(math.Pow10(totpDigits))
	return fmt.Sprintf("%0*d", totpDigits, code), nil
}
