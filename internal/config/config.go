package config

import (
	"os"
	"strings"
	"time"
)

// Config holds runtime configuration loaded from the environment.
type Config struct {
	MongoURL    string
	DBName      string
	JWTSecret   string
	HTTPAddr    string
	CORSOrigins []string
}

// JWTExpiration matches the Python backend (7 days).
func (c Config) JWTExpiration() time.Duration {
	return 7 * 24 * time.Hour
}

// Load reads configuration from environment variables.
func Load() Config {
	mongo := os.Getenv("MONGO_URL")
	db := os.Getenv("DB_NAME")
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "your-secret-key-change-in-production"
	}
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	cors := os.Getenv("CORS_ORIGINS")
	if cors == "" {
		cors = "*"
	}
	origins := strings.Split(cors, ",")
	for i := range origins {
		origins[i] = strings.TrimSpace(origins[i])
	}
	return Config{
		MongoURL:    mongo,
		DBName:      db,
		JWTSecret:   secret,
		HTTPAddr:    addr,
		CORSOrigins: origins,
	}
}
