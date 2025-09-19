package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gliderlabs/ssh"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
)

// The "Device Flow" option should be enabled in the application settings on GitHub.
func handleAuthentication(s ssh.Session, cfg *Config) (string, error) {
	conf := &oauth2.Config{
		ClientID: cfg.GitHubAuth.ClientID,
		Scopes:   []string{"read:user"},
		Endpoint: github.Endpoint,
	}

	code, err := conf.DeviceAuth(context.Background())
	if err != nil {
		return "", fmt.Errorf("could not get device code: %w", err)
	}

	// Instructions to the user in the terminal
	fmt.Fprintf(s, "To log in, please visit %s in your browser\n", code.VerificationURI)
	fmt.Fprintf(s, "And enter the code: %s\n", code.UserCode)
	fmt.Fprintln(s, "Waiting for authorization...")

	// Getting the access token
	token, err := conf.DeviceAccessToken(context.Background(), code)
	if err != nil {
		return "", fmt.Errorf("failed to get access token: %w", err)
	}

	// Getting the username
	fmt.Fprintln(s, "\nAuthentication successful! Fetching user info...")
	username, err := getGitHubUsername(token.AccessToken)
	if err != nil {
		return "", fmt.Errorf("could not fetch user info: %w", err)
	}

	return username, nil
}

// Calling API by using the token to get actual username
func getGitHubUsername(token string) (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", "https://api.github.com/user", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var user struct {
		Login string `json:"login"`
	}
	if err := json.Unmarshal(body, &user); err != nil {
		return "", err
	}

	if user.Login == "" {
		return "", fmt.Errorf("could not find username in the response")
	}

	return user.Login, nil
}
