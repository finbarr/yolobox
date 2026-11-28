package main

import (
	"context"
	"crypto/subtle"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type gitlabProxyScope struct {
	host    string
	project string
}

var (
	gitlabHostLabel      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$|^[a-z0-9]$`)
	gitlabProjectSegment = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9_.-]*$`)
	gitlabBearerToken    = regexp.MustCompile(`^[a-zA-Z0-9._~+/-]+=*$`)
)

func parseGitLabProxyScope(value string) (gitlabProxyScope, error) {
	host, project, ok := strings.Cut(value, "/")
	host = normalizeGitLabProxyHost(host)
	segments := strings.Split(project, "/")
	if !ok || host == "" || len(segments) < 2 || len(project) > 1024 {
		return gitlabProxyScope{}, fmt.Errorf("--gitlab-proxy requires HOST/GROUP/PROJECT")
	}
	for _, segment := range segments {
		if !gitlabProjectSegment.MatchString(segment) {
			return gitlabProxyScope{}, fmt.Errorf("--gitlab-proxy requires a GitLab project path without URL escapes")
		}
	}
	return gitlabProxyScope{host: host, project: project}, nil
}

func normalizeGitLabProxyHost(host string) string {
	// A host is a DNS name or IPv4 address, optionally with an HTTPS port.
	// Parse the port separately to exclude URL components and ambiguous hosts.
	host = strings.ToLower(host)
	hostname, port, hasPort := strings.Cut(host, ":")
	if hasPort {
		value, err := strconv.ParseUint(port, 10, 16)
		if err != nil || value == 0 || strconv.FormatUint(value, 10) != port {
			return ""
		}
	}
	if len(hostname) > 253 {
		return ""
	}
	for _, label := range strings.Split(hostname, ".") {
		if len(label) > 63 || !gitlabHostLabel.MatchString(label) {
			return ""
		}
	}
	return host
}

type gitlabProxy struct {
	Env []string

	scope      gitlabProxyScope
	capability string
	hostToken  string
	client     *http.Client
	server     *http.Server
}

func startGitLabProxy(runtimeName string, scope gitlabProxyScope) (*gitlabProxy, error) {
	hostToken, err := gitlabProxyHostToken(scope.host)
	if err != nil {
		return nil, err
	}
	capability, err := randomToken()
	if err != nil {
		return nil, fmt.Errorf("generate GitLab proxy credential: %w", err)
	}
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		return nil, fmt.Errorf("start GitLab proxy: %w", err)
	}
	bridgeHost := hostBridgeHostName(runtimeName)
	apiHost := net.JoinHostPort(bridgeHost, strconv.Itoa(listener.Addr().(*net.TCPAddr).Port))
	bridge := &gitlabProxy{
		Env: []string{
			"GITLAB_HOST=" + scope.host,
			"GITLAB_REPO=https://" + scope.host + "/" + scope.project,
			"GITLAB_API_HOST=" + apiHost,
			"GLAB_API_PROTOCOL=http",
			"API_PROTOCOL=http",
			"GLAB_ENABLE_CI_AUTOLOGIN=false",
			"GLAB_SEND_TELEMETRY=false",
			"GLAB_CHECK_UPDATE=false",
			"CHECK_UPDATE=false",
			// glab's per-host proxy takes precedence over NO_PROXY. It falls
			// back to standard proxy selection for a whitespace-only value.
			"GLAB_PROXY= ",
			"NO_PROXY=" + gitLabNoProxy(bridgeHost, os.Getenv("NO_PROXY"), os.Getenv("no_proxy")),
			"no_proxy=" + gitLabNoProxy(bridgeHost, os.Getenv("no_proxy"), os.Getenv("NO_PROXY")),
			"GITLAB_TOKEN=" + capability,
		},
		scope:      scope,
		capability: capability,
		hostToken:  hostToken,
		client:     newGitLabProxyClient(http.DefaultTransport),
	}
	bridge.server = &http.Server{
		Handler:           bridge,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      35 * time.Second,
		IdleTimeout:       15 * time.Second,
	}
	go func() {
		if err := bridge.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			warn("GitLab proxy stopped: %s", err)
		}
	}()
	return bridge, nil
}

func gitLabNoProxy(bridgeHost, preferred, alternate string) string {
	if preferred == "" {
		preferred = alternate
	}
	if preferred == "" {
		return bridgeHost
	}
	return bridgeHost + "," + preferred
}

func newGitLabProxyClient(transport http.RoundTripper) *http.Client {
	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func (b *gitlabProxy) Close() {
	if b != nil && b.server != nil {
		_ = b.server.Close()
	}
}

var gitlabHostTokenEnvKeys = []string{"GITLAB_TOKEN", "GITLAB_ACCESS_TOKEN", "OAUTH_TOKEN"}

func gitlabProxyHostToken(host string) (string, error) {
	if gitlabProxyEnvHostMatches(host) {
		for _, key := range gitlabHostTokenEnvKeys {
			if token := strings.TrimSpace(os.Getenv(key)); gitlabProxyValidToken(token) {
				return token, nil
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	token, err := readGitLabConfigToken(ctx, host)
	if err == nil && gitlabProxyValidToken(token) {
		// glab can fall back to a global token if its host entry is absent.
		// Use only credentials that resolve specifically for this host.
		globalToken, err := readGitLabConfigToken(ctx, "")
		if err == nil && token != globalToken {
			return token, nil
		}
	}
	return "", fmt.Errorf("GitLab proxy could not read a token scoped to %s; sign in on the host with glab or set GITLAB_HOST and GITLAB_TOKEN", host)
}

func readGitLabConfigToken(ctx context.Context, host string) (string, error) {
	args := []string{"config", "get", "token", "--global"}
	if host != "" {
		args = append(args, "--host", host)
	}
	cmd := exec.CommandContext(ctx, "glab", args...)
	cmd.Dir = "/"
	cmd.Env = cmd.Environ()
	for _, key := range gitlabHostTokenEnvKeys {
		cmd.Env = append(cmd.Env, key+"=")
	}
	cmd.Env = append(cmd.Env, "GLAB_NO_PROMPT=1", "GLAB_DEBUG_HTTP=", "GLAB_ENABLE_CI_AUTOLOGIN=false",
		"GLAB_SEND_TELEMETRY=false", "GLAB_CHECK_UPDATE=false", "CHECK_UPDATE=false")
	data, err := cmd.Output()
	return strings.TrimSpace(string(data)), err
}

func gitlabProxyValidToken(token string) bool {
	return len(token) <= 4096 && gitlabBearerToken.MatchString(token)
}

func gitlabProxyEnvHostMatches(host string) bool {
	var configured string
	for _, key := range []string{"GITLAB_HOST", "GITLAB_URI", "GL_HOST"} {
		if configured = os.Getenv(key); configured != "" {
			break
		}
	}
	if configured == "" {
		configured = os.Getenv("GITLAB_REPO")
	}
	if configured == "" {
		configured = "gitlab.com"
	}
	if parsed, err := url.Parse(configured); err == nil && parsed.Host != "" && parsed.User == nil {
		configured = parsed.Host
	}
	return normalizeGitLabProxyHost(configured) == host
}

func (b *gitlabProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "GitLab proxy only supports GET", http.StatusMethodNotAllowed)
		return
	}
	if !b.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	endpoint, ok := b.scope.endpoint(r.URL)
	if !ok {
		http.Error(w, "GitLab API request is outside the proxy scope", http.StatusForbidden)
		return
	}
	upstreamURL := "https://" + b.scope.host + endpoint
	upstream, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstreamURL, nil)
	if err != nil {
		http.Error(w, "invalid GitLab API request", http.StatusBadRequest)
		return
	}
	// Build a new request instead of forwarding container headers. In
	// particular, GitLab accepts Sudo, cookies and method override headers.
	upstream.Header.Set("Authorization", "Bearer "+b.hostToken)
	upstream.Header.Set("Accept", "application/json")
	response, err := b.client.Do(upstream)
	if err != nil {
		http.Error(w, "unable to reach GitLab", http.StatusBadGateway)
		return
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		http.Error(w, "GitLab proxy does not follow redirects", http.StatusBadGateway)
		return
	}
	for _, header := range []string{"X-Page", "X-Per-Page", "X-Next-Page", "X-Prev-Page", "X-Total", "X-Total-Pages", "Retry-After"} {
		if value := response.Header.Get(header); isGitLabNumber(value) {
			w.Header().Set(header, value)
		}
	}
	if response.StatusCode >= 400 {
		http.Error(w, "GitLab returned "+http.StatusText(response.StatusCode), response.StatusCode)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
}

func (b *gitlabProxy) authorized(r *http.Request) bool {
	if b.capability == "" {
		return false
	}
	want := []byte(b.capability)
	return subtle.ConstantTimeCompare([]byte(r.Header.Get("Private-Token")), want) == 1 ||
		subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+b.capability)) == 1
}

const gitlabProjectAPI = "/api/v4/projects/"

func (s gitlabProxyScope) endpoint(requestURL *url.URL) (string, bool) {
	if requestURL.Host != "" || requestURL.Scheme != "" || len(requestURL.RawQuery) > 8192 {
		return "", false
	}
	path, ok := strings.CutPrefix(requestURL.EscapedPath(), gitlabProjectAPI)
	if !ok {
		return "", false
	}
	segment, resource, nested := strings.Cut(path, "/")
	project, err := url.PathUnescape(segment)
	if err != nil || project != s.project || nested && !allowedGitLabProxyResource(resource) {
		return "", false
	}
	query, err := url.ParseQuery(requestURL.RawQuery)
	if err != nil {
		return "", false
	}
	for key, values := range query {
		if !allowedGitLabProxyQuery(key) || len(values) != 1 || strings.ContainsRune(values[0], '\x00') {
			return "", false
		}
	}
	endpoint := gitlabProjectAPI + url.PathEscape(s.project)
	if nested {
		endpoint += "/" + resource
	}
	if len(query) != 0 {
		endpoint += "?" + query.Encode()
	}
	return endpoint, true
}

func allowedGitLabProxyResource(resource string) bool {
	kind, rest, nested := strings.Cut(resource, "/")
	switch kind {
	case "issues", "merge_requests", "pipelines":
	default:
		return false
	}
	if !nested {
		return true
	}
	id, detail, hasDetail := strings.Cut(rest, "/")
	if !isGitLabNumber(id) || id == "0" {
		return false
	}
	if !hasDetail {
		return true
	}
	switch kind {
	case "issues":
		return detail == "notes"
	case "merge_requests":
		return detail == "notes" || detail == "diffs" || detail == "changes"
	case "pipelines":
		return detail == "jobs"
	default:
		return false
	}
}

func isGitLabNumber(value string) bool {
	number, err := strconv.ParseUint(value, 10, 64)
	return err == nil && strconv.FormatUint(number, 10) == value
}

func allowedGitLabProxyQuery(key string) bool {
	switch key {
	case "page", "per_page", "state", "scope", "sort", "order_by", "search", "in",
		"assignee_id", "assignee_username", "author_id", "author_username", "labels", "milestone",
		"created_after", "created_before", "updated_after", "updated_before", "source_branch", "target_branch",
		"ref", "status", "source":
		return true
	default:
		return false
	}
}
