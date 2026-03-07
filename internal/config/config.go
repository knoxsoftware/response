package config

import (
	"fmt"
	"os"
	"strings"
)

type BootstrapAdmin struct {
	Phone string
	PIN   string
}

type Config struct {
	DatabaseURL      string
	TwilioAuthToken  string
	Port             string
	BaseURL          string
	BootstrapAdmins  []BootstrapAdmin
}

func Load() (*Config, error) {
	c := &Config{
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		TwilioAuthToken: os.Getenv("TWILIO_AUTH_TOKEN"),
		Port:            os.Getenv("PORT"),
		BaseURL:         os.Getenv("BASE_URL"),
	}
	if c.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if c.TwilioAuthToken == "" {
		return nil, fmt.Errorf("TWILIO_AUTH_TOKEN is required")
	}
	if c.Port == "" {
		c.Port = "8080"
	}
	if c.BaseURL == "" {
		return nil, fmt.Errorf("BASE_URL is required")
	}
	if raw := os.Getenv("BOOTSTRAP_ADMINS"); raw != "" {
		for _, entry := range strings.Split(raw, ",") {
			parts := strings.SplitN(strings.TrimSpace(entry), ":", 2)
			if len(parts) != 2 {
				return nil, fmt.Errorf("BOOTSTRAP_ADMINS: invalid entry %q, expected phone:pin", entry)
			}
			c.BootstrapAdmins = append(c.BootstrapAdmins, BootstrapAdmin{
				Phone: parts[0],
				PIN:   parts[1],
			})
		}
	}
	return c, nil
}
