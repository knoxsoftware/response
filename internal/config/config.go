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
	DatabaseURL     string
	FSSharedSecret  string
	VoIPMSUsername  string
	VoIPMSPassword  string
	VoIPMSDID       string
	SMSTreePath     string
	Port            string
	BaseURL         string
	BootstrapAdmins []BootstrapAdmin
}

func Load() (*Config, error) {
	c := &Config{
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		FSSharedSecret: os.Getenv("FS_SHARED_SECRET"),
		VoIPMSUsername: os.Getenv("VOIPMS_USERNAME"),
		VoIPMSPassword: os.Getenv("VOIPMS_PASSWORD"),
		VoIPMSDID:      os.Getenv("VOIPMS_DID"),
		SMSTreePath:    os.Getenv("SMS_TREE_PATH"),
		Port:           os.Getenv("PORT"),
		BaseURL:        os.Getenv("BASE_URL"),
	}
	if c.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if c.FSSharedSecret == "" {
		return nil, fmt.Errorf("FS_SHARED_SECRET is required")
	}
	if c.VoIPMSUsername == "" || c.VoIPMSPassword == "" || c.VoIPMSDID == "" {
		return nil, fmt.Errorf("VOIPMS_USERNAME, VOIPMS_PASSWORD, and VOIPMS_DID are required")
	}
	if c.Port == "" {
		c.Port = "8080"
	}
	if c.BaseURL == "" {
		return nil, fmt.Errorf("BASE_URL is required")
	}
	if c.SMSTreePath == "" {
		c.SMSTreePath = "/config/sms-tree.yaml"
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
