package config

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// ErrGoogleOAuthNotConfigured is returned (wrapped) when Google OAuth env vars are unset. Use errors.Is(err, ErrGoogleOAuthNotConfigured).
var ErrGoogleOAuthNotConfigured = errors.New("google oauth not configured")

// GoogleOAuthScopes lists all Google APIs requested in a single consent screen.
var GoogleOAuthScopes = []string{
	"https://www.googleapis.com/auth/gmail.readonly",
	"https://www.googleapis.com/auth/gmail.send",
	"https://www.googleapis.com/auth/gmail.modify",
	"https://www.googleapis.com/auth/drive",
	"https://www.googleapis.com/auth/drive.file",
	"https://www.googleapis.com/auth/spreadsheets",
	"https://www.googleapis.com/auth/documents",
	"https://www.googleapis.com/auth/presentations",
	"https://www.googleapis.com/auth/calendar",
	"https://www.googleapis.com/auth/calendar.events",
	"https://www.googleapis.com/auth/forms.body",
	"https://www.googleapis.com/auth/forms.responses.readonly",
	"https://www.googleapis.com/auth/userinfo.email",
	"https://www.googleapis.com/auth/userinfo.profile",
}

// GoogleOAuthConfig holds Google OAuth2 web client settings from the environment.
type GoogleOAuthConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
}

// LoadGoogleOAuthConfig reads GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET, GOOGLE_REDIRECT_URI.
func LoadGoogleOAuthConfig() (*GoogleOAuthConfig, error) {
	clientID := os.Getenv("GOOGLE_CLIENT_ID")
	clientSecret := os.Getenv("GOOGLE_CLIENT_SECRET")
	redirectURI := os.Getenv("GOOGLE_REDIRECT_URI")
	if clientID == "" || clientSecret == "" || redirectURI == "" {
		return nil, fmt.Errorf("%w: Google OAuth2 not configured. "+
			"Please set GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET, "+
			"and GOOGLE_REDIRECT_URI environment variables.", ErrGoogleOAuthNotConfigured)
	}
	return &GoogleOAuthConfig{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURI:  redirectURI,
	}, nil
}

// GetOAuth2Config builds a golang oauth2.Config from the current environment.
func (c *GoogleOAuthConfig) GetOAuth2Config() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		RedirectURL:  c.RedirectURI,
		Scopes:       GoogleOAuthScopes,
		Endpoint:     google.Endpoint,
	}
}

// GetOAuth2Config returns a golang oauth2.Config for Google APIs.
func GetOAuth2Config() (*oauth2.Config, error) {
	cfg, err := LoadGoogleOAuthConfig()
	if err != nil {
		return nil, err
	}
	return cfg.GetOAuth2Config(), nil
}
