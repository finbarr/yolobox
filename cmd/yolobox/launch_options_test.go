package main

import (
	"slices"
	"strings"
	"testing"
)

func TestLaunchOptionConflicts(t *testing.T) {
	project := t.TempDir()
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"--reuse-login"}, "requires --scratch"},
		{[]string{"--scratch", "--home-volume", "already-restored"}, "cannot use --home-volume with --scratch"},
		{[]string{"--gitlab-proxy", "gitlab.com/group/project", "--no-network"}, "cannot use --gitlab-proxy with --no-network"},
		{[]string{"--gitlab-proxy", "gitlab.com/group/project", "--env-from-host", "OTHER=GITLAB_TOKEN"}, "keeps GITLAB_TOKEN out of env_from_host"},
	} {
		_, _, err := parseBaseFlagsWithConfig("run", test.args, project, defaultConfig())
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%v returned %v, want %q", test.args, err, test.want)
		}
	}
}

func TestLaunchOptionsRuntimeHandoff(t *testing.T) {
	project := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GITLAB_TOKEN", "host-token-is-private")
	cfg := Config{
		Image:       "test-image",
		HomeVolume:  "yolobox-restored-test",
		GitLabProxy: "gitlab.com/group/project",
		gitLabEnv:   []string{"GITLAB_TOKEN=container-capability"},
	}
	args, _, err := buildRunArgs(cfg, project, []string{"bash"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(args, cfg.HomeVolume+":/home/yolo") || !slices.Contains(args, "GITLAB_TOKEN=container-capability") {
		t.Fatalf("home volume or proxy capability missing from runtime args")
	}
	if strings.Contains(strings.Join(args, " "), "host-token-is-private") {
		t.Fatal("runtime args contain the host credential")
	}

	cfg = Config{Image: "test-image", Scratch: true, ReuseLogin: true, scratchLogin: "private-scratch-payload"}
	args, _, err = buildRunArgs(cfg, project, []string{"codex"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(args, scratchLoginEnv) || strings.Contains(strings.Join(args, " "), cfg.scratchLogin) {
		t.Fatal("scratch login must be passed to the runtime by environment variable name only")
	}
}

func TestGitLabProxyPreservesConfiguredNoProxy(t *testing.T) {
	t.Setenv("CUSTOM_PROXY_BYPASS", "other.internal")
	cfg := Config{Env: []string{"NO_PROXY=site.internal"}, EnvFromHost: []string{"no_proxy=CUSTOM_PROXY_BYPASS"}}
	got := gitLabProxyLaunchEnv(cfg, []string{"NO_PROXY=host.internal", "no_proxy=host.internal", "OTHER=value"})
	want := []string{"NO_PROXY=host.internal,site.internal", "no_proxy=host.internal,other.internal", "OTHER=value"}
	if !slices.Equal(got, want) {
		t.Errorf("proxy env = %v, want %v", got, want)
	}
}
