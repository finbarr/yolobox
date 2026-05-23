package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/term"
)

type remoteAuthLoginResponse struct {
	Token string `json:"token"`
	User  struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	} `json:"user"`
}

func runLogin(args []string) error {
	cfg, err := loadSetupDefaults()
	if err != nil {
		return err
	}

	backendURL := remoteBackendURL(cfg)
	email := ""
	password := ""
	name := ""
	token := ""
	signUp := false

	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = printLoginUsage
	fs.StringVar(&backendURL, "backend-url", backendURL, "remote backend API URL")
	fs.StringVar(&email, "email", "", "account email")
	fs.StringVar(&password, "password", "", "account password")
	fs.StringVar(&name, "name", "", "account display name for --signup")
	fs.StringVar(&token, "token", "", "existing remote session token")
	fs.BoolVar(&signUp, "signup", false, "create an account before storing the session token")
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
	if token != "" && (email != "" || password != "" || signUp) {
		return fmt.Errorf("--token cannot be combined with --email, --password, or --signup")
	}
	if token == "" {
		email = strings.TrimSpace(email)
		if email == "" {
			if !term.IsTerminal(int(os.Stdin.Fd())) {
				return fmt.Errorf("login requires --email and --password, or --token, when stdin is not a terminal")
			}
			var err error
			email, err = promptLine("Email: ")
			if err != nil {
				return err
			}
		}
		if password == "" {
			if !term.IsTerminal(int(os.Stdin.Fd())) {
				return fmt.Errorf("login requires --password when stdin is not a terminal")
			}
			fmt.Fprint(os.Stderr, "Password: ")
			data, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(os.Stderr)
			if err != nil {
				return err
			}
			password = string(data)
		}
		login, err := remoteAuthLogin(backendURL, email, password, name, signUp)
		if err != nil {
			return err
		}
		token = login.Token
		if login.User.Email != "" {
			email = login.User.Email
		}
	}
	if token == "" {
		return fmt.Errorf("remote session token cannot be empty")
	}

	cfg.Remote.BackendURL = backendURL
	cfg.Remote.Token = token
	if err := saveGlobalConfig(cfg); err != nil {
		return err
	}
	if email != "" {
		success("Logged in to %s as %s", backendURL, email)
	} else {
		success("Logged in to %s", backendURL)
	}
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
	fmt.Fprintln(os.Stderr, "  yolobox login [--backend-url <url>] --email <email> --password <password>")
	fmt.Fprintln(os.Stderr, "  yolobox login --signup [--backend-url <url>] --email <email> --password <password> [--name <name>]")
	fmt.Fprintln(os.Stderr, "  yolobox login [--backend-url <url>] --token <token>")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "If --email or --password are omitted, yolobox prompts for them on an interactive terminal.")
	fmt.Fprintln(os.Stderr, "--token stores an existing backend session token without calling the login API.")
}

func remoteAuthLogin(backendURL string, email string, password string, name string, signUp bool) (remoteAuthLoginResponse, error) {
	email = strings.TrimSpace(email)
	password = strings.TrimSpace(password)
	name = strings.TrimSpace(name)
	if email == "" {
		return remoteAuthLoginResponse{}, fmt.Errorf("email cannot be empty")
	}
	if password == "" {
		return remoteAuthLoginResponse{}, fmt.Errorf("password cannot be empty")
	}
	endpoint := "/v1/auth/sign-in/email"
	payload := map[string]string{
		"email":    email,
		"password": password,
	}
	if signUp {
		endpoint = "/v1/auth/sign-up/email"
		if name == "" {
			name = strings.Split(email, "@")[0]
		}
		payload["name"] = name
	}
	var response remoteAuthLoginResponse
	headers, err := remoteAuthRequest(backendURL, endpoint, "", payload, &response)
	if err != nil {
		return response, err
	}
	if response.Token == "" {
		response.Token = strings.TrimSpace(headers.Get("set-auth-token"))
	}
	if response.Token == "" {
		return response, fmt.Errorf("remote backend login returned no session token")
	}
	return response, nil
}

func remoteAuthLogout(backendURL string, token string) error {
	_, err := remoteAuthRequest(backendURL, "/v1/auth/sign-out", token, map[string]string{}, nil)
	return err
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
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(resp.Body)
		detail := strings.TrimSpace(buf.String())
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

func promptLine(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	var value string
	if _, err := fmt.Fscanln(os.Stdin, &value); err != nil {
		return "", err
	}
	return strings.TrimSpace(value), nil
}
