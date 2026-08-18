package config

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"os"
	"strconv"
)

type Config struct {
	Port          string
	JWTSecret     string
	DBPath        string
	RootPath      string
	MaxUploadSize int64
	TLSCert       string
	TLSKey        string
	AllowedTypes  string // kommagetrennte MIME-Typen, leer = alle erlaubt
}

func Load() *Config {
	maxUpload, _ := strconv.ParseInt(getEnv("MAX_UPLOAD_MB", "1024"), 10, 64)
	cfg := &Config{
		Port:          getEnv("PORT", "8080"),
		JWTSecret:     getEnv("JWT_SECRET", ""),
		DBPath:        getEnv("DB_PATH", "./fileship.db"),
		RootPath:      getEnv("ROOT_PATH", "./data"),
		MaxUploadSize: maxUpload * 1024 * 1024,
		TLSCert:       getEnv("TLS_CERT", ""),
		TLSKey:        getEnv("TLS_KEY", ""),
		AllowedTypes:  getEnv("ALLOWED_TYPES", ""),
	}
	if cfg.JWTSecret == "" {
		cfg.JWTSecret = mustGenerateSecret()
	}
	return cfg
}

func (c *Config) TLSEnabled() bool {
	return c.TLSCert != "" && c.TLSKey != ""
}

func mustGenerateSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("cannot generate JWT secret: " + err.Error())
	}
	secret := hex.EncodeToString(b)
	log.Printf("⚠️  JWT_SECRET not set — generated ephemeral secret. Set JWT_SECRET env var for persistent sessions!")
	return secret
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
