package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

func runLogin(args []string) error {
	cfg, err := loadSetupDefaults()
	if err != nil {
		return err
	}

	backendURL := remoteBackendURL(cfg)
	token := ""

	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = printLoginUsage
	fs.StringVar(&backendURL, "backend-url", backendURL, "remote backend API URL")
	fs.StringVar(&token, "token", "", "remote auth token")
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
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return fmt.Errorf("login requires --token when stdin is not a terminal")
		}
		fmt.Fprint(os.Stderr, "Remote auth token: ")
		data, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return err
		}
		token = strings.TrimSpace(string(data))
	}
	if token == "" {
		return fmt.Errorf("remote auth token cannot be empty")
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
	cfg.Remote.Token = ""
	if err := saveGlobalConfig(cfg); err != nil {
		return err
	}
	success("Logged out of %s", remoteBackendURL(cfg))
	return nil
}

func printLoginUsage() {
	fmt.Fprintln(os.Stderr, "USAGE:")
	fmt.Fprintln(os.Stderr, "  yolobox login [--backend-url <url>] --token <token>")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "If --token is omitted, yolobox prompts for it on an interactive terminal.")
}
