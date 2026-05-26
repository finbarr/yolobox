package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const remoteAuthDeviceClientID = "yolobox-cli"

var (
	openBrowserURL  = defaultOpenBrowserURL
	remoteAuthSleep = time.Sleep
)

type remoteAuthLoginResponse struct {
	Token string `json:"token"`
	User  struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	} `json:"user"`
}

type remoteAuthDeviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

type remoteAuthDeviceTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	Scope       string `json:"scope"`
}

type remoteAuthEndpointError struct {
	Code        string `json:"error"`
	Description string `json:"error_description"`
	Message     string `json:"message"`
}

func (e remoteAuthEndpointError) Error() string {
	if e.Description != "" {
		return e.Description
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Code != "" {
		return e.Code
	}
	return "remote backend auth failed"
}

func runLogin(args []string) error {
	cfg, err := loadSetupDefaults()
	if err != nil {
		return err
	}

	backendURL := remoteBackendURL(cfg)
	token := ""
	noOpen := false

	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = printLoginUsage
	fs.StringVar(&backendURL, "backend-url", backendURL, "remote backend API URL")
	fs.StringVar(&token, "token", "", "existing remote session token")
	fs.BoolVar(&noOpen, "no-open", false, "print the browser login URL without trying to open it")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return errHelp
		}
		return err
	}
	if len(fs.Args()) != 0 {
		return fmt.Errorf("unexpected login args: %v", fs.Args())
	}

	backendURL = strings.TrimRight(strings.TrimSpace(backendURL), "/")
	if err := validateRemoteBackendURL(backendURL); err != nil {
		return fmt.Errorf("invalid --backend-url: %w", err)
	}
	token = strings.TrimSpace(token)
	if token == "" {
		login, err := remoteAuthBrowserLogin(backendURL, !noOpen)
		if err != nil {
			return err
		}
		token = login.Token
	}
	if token == "" {
		return fmt.Errorf("remote session token cannot be empty")
	}

	cfg.Remote.BackendURL = backendURL
	cfg.Remote.Token = token
	if err := saveGlobalConfig(cfg); err != nil {
		return err
	}
	success("Logged in to %s", backendURL)
	return nil
}

func runLogout(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("unexpected logout args: %v", args)
	}
	cfg, err := loadSetupDefaults()
	if err != nil {
		return err
	}
	backendURL := remoteBackendURL(cfg)
	token := remoteAuthToken(cfg)
	if token != "" {
		if err := remoteAuthLogout(backendURL, token); err != nil {
			warn("Could not revoke backend session: %v", err)
		}
	}
	cfg.Remote.Token = ""
	if err := saveGlobalConfig(cfg); err != nil {
		return err
	}
	success("Logged out of %s", backendURL)
	return nil
}

func printLoginUsage() {
	fmt.Fprintln(os.Stderr, "USAGE:")
	fmt.Fprintln(os.Stderr, "  yolobox login [--backend-url <url>] [--no-open]")
	fmt.Fprintln(os.Stderr, "  yolobox login [--backend-url <url>] --token <token>")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Without --token, yolobox opens a browser approval flow and also prints the URL.")
	fmt.Fprintln(os.Stderr, "--token stores an existing backend session token without calling the login API.")
}

func remoteAuthBrowserLogin(backendURL string, shouldOpen bool) (remoteAuthLoginResponse, error) {
	device, err := remoteAuthStartDeviceLogin(backendURL)
	if err != nil {
		return remoteAuthLoginResponse{}, err
	}
	loginURL := strings.TrimSpace(device.VerificationURIComplete)
	if loginURL == "" {
		loginURL = strings.TrimSpace(device.VerificationURI)
	}
	if loginURL == "" {
		return remoteAuthLoginResponse{}, fmt.Errorf("remote backend device login returned no verification URL")
	}

	fmt.Fprintln(os.Stderr, "Open this URL to sign in and grant CLI access:")
	fmt.Fprintf(os.Stderr, "\n  %s\n\n", loginURL)
	if device.UserCode != "" {
		fmt.Fprintf(os.Stderr, "If the browser asks for a code, enter: %s\n", formatRemoteUserCode(device.UserCode))
	}
	if shouldOpen {
		if err := openBrowserURL(loginURL); err != nil {
			warn("Could not open the browser automatically: %v", err)
		} else {
			info("Opened the browser; waiting for approval...")
		}
	} else {
		info("Waiting for browser approval...")
	}

	token, err := remoteAuthPollDeviceToken(backendURL, device)
	if err != nil {
		return remoteAuthLoginResponse{}, err
	}
	return remoteAuthLoginResponse{Token: token.AccessToken}, nil
}

func remoteAuthStartDeviceLogin(backendURL string) (remoteAuthDeviceCodeResponse, error) {
	var response remoteAuthDeviceCodeResponse
	_, err := remoteAuthRequest(backendURL, "/v1/auth/device/code", "", map[string]string{
		"client_id": remoteAuthDeviceClientID,
		"scope":     "remote",
	}, &response)
	if err != nil {
		return response, err
	}
	if strings.TrimSpace(response.DeviceCode) == "" {
		return response, fmt.Errorf("remote backend device login returned no device code")
	}
	return response, nil
}

func remoteAuthPollDeviceToken(backendURL string, device remoteAuthDeviceCodeResponse) (remoteAuthDeviceTokenResponse, error) {
	expiresIn := device.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 15 * 60
	}
	interval := time.Duration(device.Interval) * time.Second
	if interval <= 0 {
		interval = 3 * time.Second
	}
	deadline := time.Now().Add(time.Duration(expiresIn) * time.Second)

	for {
		if time.Now().After(deadline) {
			return remoteAuthDeviceTokenResponse{}, fmt.Errorf("browser login expired; run `yolobox login` again")
		}
		remoteAuthSleep(interval)

		token, authErr, err := remoteAuthExchangeDeviceToken(backendURL, device.DeviceCode)
		if err != nil {
			return token, err
		}
		if authErr == nil {
			if strings.TrimSpace(token.AccessToken) == "" {
				return token, fmt.Errorf("remote backend device login returned no access token")
			}
			return token, nil
		}
		switch authErr.Code {
		case "authorization_pending":
			continue
		case "slow_down":
			interval += 5 * time.Second
			continue
		case "expired_token":
			return token, fmt.Errorf("browser login expired; run `yolobox login` again")
		case "access_denied":
			return token, fmt.Errorf("browser login was denied")
		default:
			return token, fmt.Errorf("remote backend device login failed: %s", authErr.Error())
		}
	}
}

func remoteAuthExchangeDeviceToken(backendURL string, deviceCode string) (remoteAuthDeviceTokenResponse, *remoteAuthEndpointError, error) {
	payload := map[string]string{
		"grant_type":  "urn:ietf:params:oauth:grant-type:device_code",
		"device_code": deviceCode,
		"client_id":   remoteAuthDeviceClientID,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return remoteAuthDeviceTokenResponse{}, nil, err
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(backendURL, "/")+"/v1/auth/device/token", bytes.NewReader(data))
	if err != nil {
		return remoteAuthDeviceTokenResponse{}, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if origin := remoteAuthOrigin(backendURL); origin != "" {
		req.Header.Set("Origin", origin)
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return remoteAuthDeviceTokenResponse{}, nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(resp.Body)
		authErr := remoteAuthErrorFromBody(buf.String())
		return remoteAuthDeviceTokenResponse{}, &authErr, nil
	}
	var response remoteAuthDeviceTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return response, nil, err
	}
	return response, nil, nil
}

func remoteAuthLogout(backendURL string, token string) error {
	_, err := remoteAuthRequest(backendURL, "/v1/auth/sign-out", token, map[string]string{}, nil)
	return err
}

func defaultOpenBrowserURL(openURL string) error {
	if strings.TrimSpace(openURL) == "" {
		return fmt.Errorf("browser URL is required")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", openURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", openURL)
	default:
		cmd = exec.Command("xdg-open", openURL)
	}
	return cmd.Start()
}

func remoteAuthRequest(backendURL string, endpoint string, token string, body any, out any) (http.Header, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(backendURL, "/")+endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if origin := remoteAuthOrigin(backendURL); origin != "" {
		req.Header.Set("Origin", origin)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(resp.Body)
		detail := remoteAuthErrorDetail(buf.String())
		if detail == "" {
			detail = resp.Status
		}
		return resp.Header, fmt.Errorf("remote backend auth %s failed: %s", endpoint, detail)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.Header, err
		}
	}
	return resp.Header, nil
}

func remoteAuthErrorDetail(body string) string {
	if strings.TrimSpace(body) == "" {
		return ""
	}
	authErr := remoteAuthErrorFromBody(body)
	return authErr.Error()
}

func remoteAuthErrorFromBody(body string) remoteAuthEndpointError {
	var authErr remoteAuthEndpointError
	if err := json.Unmarshal([]byte(strings.TrimSpace(body)), &authErr); err == nil && (authErr.Code != "" || authErr.Description != "" || authErr.Message != "") {
		return authErr
	}
	authErr.Message = strings.TrimSpace(body)
	return authErr
}

func formatRemoteUserCode(code string) string {
	clean := strings.ReplaceAll(strings.TrimSpace(code), "-", "")
	if len(clean) == 8 {
		return clean[:4] + "-" + clean[4:]
	}
	return strings.TrimSpace(code)
}

func remoteAuthOrigin(backendURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(backendURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}
