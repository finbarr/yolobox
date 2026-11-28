package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseGitLabProxyScope(t *testing.T) {
	scope, err := parseGitLabProxyScope("GitLab.example:8443/team/subgroup/project-2")
	if err != nil || scope.host != "gitlab.example:8443" || scope.project != "team/subgroup/project-2" {
		t.Fatalf("got scope %+v, error %v", scope, err)
	}
	for _, value := range []string{
		"gitlab.com/project", "https://gitlab.com/group/project", "gitlab.com@other.com/group/project",
		"gitlab.com:65536/group/project", "gitlab.com#other/group/project", "gitlab.com/group//project",
		"gitlab.com/group/../project", "gitlab.com/group%2Fother/project", "gitlab.com/group/project?x=y",
	} {
		t.Run(value, func(t *testing.T) {
			if _, err := parseGitLabProxyScope(value); err == nil {
				t.Fatalf("accepted invalid scope %q", value)
			}
		})
	}
}

func TestGitLabProxyIgnoresUnscopedGlabFallback(t *testing.T) {
	path := t.TempDir()
	fixture := `#!/bin/sh
if [ -n "$GITLAB_TOKEN" ]; then exit 2; fi
if [ "$6" = "configured.example" ]; then
    printf '%s\n' host-specific-fixture
else
    printf '%s\n' unscoped-fixture
fi
`
	if err := os.WriteFile(filepath.Join(path, "glab"), []byte(fixture), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", path)
	t.Setenv("GITLAB_HOST", "another.example")
	t.Setenv("GITLAB_TOKEN", "unrelated-environment-fixture")
	if token, err := gitlabProxyHostToken("configured.example"); err != nil || token != "host-specific-fixture" {
		t.Fatalf("did not read matching glab credentials: %v", err)
	}
	if token, err := gitlabProxyHostToken("unknown.example"); err == nil || token != "" {
		t.Fatal("glab's unscoped fallback escaped the configured host")
	}
}

func TestGitLabProxyHostTokenEnvIsScoped(t *testing.T) {
	for _, key := range []string{"GITLAB_HOST", "GITLAB_URI", "GL_HOST", "GITLAB_REPO"} {
		t.Setenv(key, "")
	}
	t.Setenv("GITLAB_TOKEN", "fixture-host-only-token")
	if token, err := gitlabProxyHostToken("gitlab.com"); err != nil || token != "fixture-host-only-token" {
		t.Fatalf("did not read the default host token: %v", err)
	}
	t.Setenv("GITLAB_REPO", "https://gitlab.example/team/subgroup/project")
	if gitlabProxyEnvHostMatches("gitlab.com") || !gitlabProxyEnvHostMatches("gitlab.example") {
		t.Fatal("GITLAB_REPO did not move the token scope to its host")
	}
	t.Setenv("GITLAB_HOST", "GitLab.other:8443")
	token, err := gitlabProxyHostToken("gitlab.other:8443")
	if err != nil || token != "fixture-host-only-token" || gitlabProxyEnvHostMatches("gitlab.example") {
		t.Fatalf("GITLAB_HOST did not take precedence over the repository host: %v", err)
	}
}

type gitlabRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn gitlabRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestGitLabProxyRebuildsScopedRequest(t *testing.T) {
	bridge := gitlabProxy{
		scope:      gitlabProxyScope{host: "gitlab.example:8443", project: "team/subgroup/project"},
		capability: "box-capability",
		hostToken:  "host-only-token",
		client: newGitLabProxyClient(gitlabRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			if got, want := request.URL.String(), "https://gitlab.example:8443/api/v4/projects/team%2Fsubgroup%2Fproject/issues?labels=bug&page=2"; got != want {
				t.Errorf("upstream URL = %q, want %q", got, want)
			}
			if request.Method != http.MethodGet || request.Header.Get("Authorization") != "Bearer host-only-token" || request.Header.Get("Accept") != "application/json" {
				t.Error("upstream request lacks the expected method, host authentication, or content preference")
			}
			for _, header := range []string{"Private-Token", "Sudo", "Cookie", "X-HTTP-Method-Override", "Forwarded"} {
				if request.Header.Get(header) != "" {
					t.Errorf("forwarded container header %s", header)
				}
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Link":       {`<https://other.example/secret>; rel="next"`},
					"Set-Cookie": {"auth=private"},
					"X-Page":     {"2"},
				},
				Body: io.NopCloser(strings.NewReader(`[{"title":"example"}]`)),
			}, nil
		})),
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v4/projects/team%2fsubgroup%2fproject/issues?page=2&labels=bug", nil)
	request.Header.Set("Private-Token", "box-capability")
	request.Header.Set("Sudo", "admin")
	request.Header.Set("Cookie", "client=secret")
	request.Header.Set("X-HTTP-Method-Override", "DELETE")
	request.Header.Set("Forwarded", "host=other.example")
	response := httptest.NewRecorder()
	bridge.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "example") {
		t.Fatalf("unexpected response %d %q", response.Code, response.Body.String())
	}
	if response.Header().Get("Link") != "" || response.Header().Get("Set-Cookie") != "" || response.Header().Get("X-Page") != "2" {
		t.Fatalf("unexpected response headers: %v", response.Header())
	}
}

func TestGitLabProxyRejectsRequestsOutsideScope(t *testing.T) {
	for _, test := range []struct {
		method, path, token string
		status              int
	}{
		{http.MethodGet, "/api/v4/projects/team%2Fproject/issues", "invalid", http.StatusUnauthorized},
		{http.MethodPost, "/api/v4/projects/team%2Fproject/issues", "capability", http.StatusMethodNotAllowed},
		{http.MethodGet, "/api/v4/graphql", "capability", http.StatusForbidden},
		{http.MethodGet, "/api/v4/projects/another%2Fproject/issues", "capability", http.StatusForbidden},
		{http.MethodGet, "/api/v4/projects/team%252Fproject/issues", "capability", http.StatusForbidden},
		{http.MethodGet, "/api/v4/projects/team%2Fproject/%2E%2E/issues", "capability", http.StatusForbidden},
		{http.MethodGet, "/api/v4/projects/team%2Fproject/pipelines/42/variables", "capability", http.StatusForbidden},
		{http.MethodGet, "/api/v4/projects/team%2Fproject/issues?%73udo=admin", "capability", http.StatusForbidden},
		{http.MethodGet, "/api/v4/projects/team%2Fproject/issues?state=opened&state=closed", "capability", http.StatusForbidden},
		{http.MethodGet, "http://other.example/api/v4/projects/team%2Fproject/issues", "capability", http.StatusForbidden},
	} {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			bridge := gitlabProxy{
				scope:      gitlabProxyScope{host: "gitlab.example", project: "team/project"},
				capability: "capability",
				client: newGitLabProxyClient(gitlabRoundTripFunc(func(*http.Request) (*http.Response, error) {
					t.Error("out-of-scope request reached upstream")
					return nil, fmt.Errorf("unexpected upstream request")
				})),
			}
			request := httptest.NewRequest(test.method, test.path, nil)
			request.Header.Set("Authorization", "Bearer "+test.token)
			response := httptest.NewRecorder()
			bridge.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("got status %d, want %d", response.Code, test.status)
			}
		})
	}
}

func TestGitLabProxyDoesNotFollowRedirects(t *testing.T) {
	var requests int
	bridge := gitlabProxy{
		scope:      gitlabProxyScope{host: "gitlab.example", project: "team/project"},
		capability: "box-capability",
		hostToken:  "host-only-token",
		client: newGitLabProxyClient(gitlabRoundTripFunc(func(*http.Request) (*http.Response, error) {
			requests++
			return &http.Response{
				StatusCode: http.StatusTemporaryRedirect,
				Header:     http.Header{"Location": {"https://elsewhere.example/"}},
				Body:       io.NopCloser(strings.NewReader("upstream redirect")),
			}, nil
		})),
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v4/projects/team%2Fproject/merge_requests/42/diffs", nil)
	request.Header.Set("Private-Token", "box-capability")
	response := httptest.NewRecorder()
	bridge.ServeHTTP(response, request)
	if requests != 1 || response.Code != http.StatusBadGateway || response.Header().Get("Location") != "" {
		t.Fatalf("redirect escaped proxy: %d upstream requests, HTTP %d, headers %v", requests, response.Code, response.Header())
	}
}
