package voipms

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type Client struct {
	username string
	password string
	did      string
	baseURL  string
}

func NewClient(username, password, did, baseURL string) *Client {
	if baseURL == "" {
		baseURL = "https://voip.ms/api/v1/rest.php"
	}
	return &Client{username: username, password: password, did: did, baseURL: baseURL}
}

// SendSMS sends an outbound SMS via the VoIP.ms REST API.
func (c *Client) SendSMS(ctx context.Context, to, message string) error {
	dst := strings.TrimPrefix(to, "+")

	params := url.Values{
		"api_username": {c.username},
		"api_password": {c.password},
		"method":       {"sendSMS"},
		"did":          {c.did},
		"dst":          {dst},
		"message":      {message},
	}

	reqURL := c.baseURL + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("send sms: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("voipms api: status %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), `"status":"success"`) {
		return fmt.Errorf("voipms api error: %s", body)
	}
	return nil
}
