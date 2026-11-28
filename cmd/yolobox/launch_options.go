package main

import (
	"fmt"
	"os"
	"strings"
)

func isGitLabProxyEnv(key string) bool {
	switch key {
	case "GITLAB_TOKEN", "GITLAB_ACCESS_TOKEN", "OAUTH_TOKEN", "CI_JOB_TOKEN",
		"GITLAB_HOST", "GITLAB_REPO", "GITLAB_API_HOST", "GLAB_API_PROTOCOL", "API_PROTOCOL",
		"GITLAB_URI", "GL_HOST", "GLAB_ENABLE_CI_AUTOLOGIN", "GLAB_PROXY":
		return true
	default:
		return false
	}
}

func validateGitLabProxyEnv(cfg Config) error {
	for _, entry := range cfg.Env {
		key, _, _ := strings.Cut(entry, "=")
		if isGitLabProxyEnv(key) {
			return fmt.Errorf("--gitlab-proxy manages %s; remove it from env", key)
		}
	}
	for _, entry := range cfg.EnvFromHost {
		key, source, _ := strings.Cut(entry, "=")
		for _, name := range []string{key, source} {
			if isGitLabProxyEnv(name) {
				return fmt.Errorf("--gitlab-proxy keeps %s out of env_from_host", name)
			}
		}
	}
	return nil
}

func gitLabProxyLaunchEnv(cfg Config, proxyEnv []string) []string {
	configured := map[string]string{}
	for _, entry := range cfg.Env {
		key, value, _ := strings.Cut(entry, "=")
		if key == "NO_PROXY" || key == "no_proxy" {
			configured[key] = value
		}
	}
	for _, entry := range cfg.EnvFromHost {
		key, source, _ := strings.Cut(entry, "=")
		if key == "NO_PROXY" || key == "no_proxy" {
			configured[key] = os.Getenv(source)
		}
	}
	result := make([]string, 0, len(proxyEnv))
	for _, entry := range proxyEnv {
		key, _, _ := strings.Cut(entry, "=")
		if value := configured[key]; value != "" {
			entry += "," + value
		}
		result = append(result, entry)
	}
	return result
}
