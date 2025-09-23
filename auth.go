package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
)

// The "Device Flow" option should be enabled in the application settings on GitHub.
func handleAuthentication(client *Client, cfg *Config) {
	conf := &oauth2.Config{
		ClientID: cfg.GitHubAuth.ClientID,
		Scopes:   []string{"read:user"},
		Endpoint: github.Endpoint,
	}

	code, err := conf.DeviceAuth(context.Background())
	if err != nil {
		client.send <- systemMessage(fmt.Sprintf("GitHub auth error: could not get device code: %v", err))
		return
	}

	// Instructions to the user in the TUI
	client.send <- systemMessage(fmt.Sprintf("To log in, please visit %s in your browser", code.VerificationURI))
	client.send <- systemMessage(fmt.Sprintf("And enter the code: %s", code.UserCode))
	client.send <- systemMessage("Waiting for authorization...")

	// Getting the access token
	token, err := conf.DeviceAccessToken(context.Background(), code)
	if err != nil {
		client.send <- systemMessage(fmt.Sprintf("GitHub auth error: failed to get access token: %v", err))
		return
	}

	// Getting the username
	client.send <- systemMessage("Authentication successful! Fetching user info...")
	username, err := getGitHubUsername(token.AccessToken)
	if err != nil {
		client.send <- systemMessage(fmt.Sprintf("GitHub auth error: could not fetch user info: %v", err))
		return
	}

	// Request the name change with high priority
	client.hub.requestNameChange(client, username, true)
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
