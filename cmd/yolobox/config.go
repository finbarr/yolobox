package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type CustomizeConfig struct {
	Packages   []string `toml:"packages"`
	Dockerfile string   `toml:"dockerfile"`
}

type RemoteConfig struct {
	BackendURL   string                   `toml:"backend_url"`
	BackendToken string                   `toml:"backend_token"`
	Provider     string                   `toml:"provider"`
	SSHUser      string                   `toml:"ssh_user"`
	Setup        []string                 `toml:"setup"`
	DigitalOcean RemoteDigitalOceanConfig `toml:"digitalocean"`
}

type RemoteDigitalOceanConfig struct {
	Token   string   `toml:"token"`
	Region  string   `toml:"region"`
	Size    string   `toml:"size"`
	Image   string   `toml:"image"`
	SSHKeys []string `toml:"ssh_keys"`
	Tags    []string `toml:"tags"`
	VPCUUID string   `toml:"vpc_uuid"`
}

type ForkConfig struct {
	Name           string `toml:"-"`
	Source         string `toml:"-"`
	Copy           string `toml:"-"`
	ComposeProject string `toml:"-"`
}

type Config struct {
	Mode                  string   `toml:"mode"`
	Runtime               string   `toml:"runtime"`
	Image                 string   `toml:"image"`
	ContainerName         string   `toml:"container_name"`
	DefaultHarness        string   `toml:"default_harness"`
	RemoteName            string   `toml:"remote_name"`
	RemoteWorkspace       string   `toml:"remote_workspace"`
	Mounts                []string `toml:"mounts"`
	Env                   []string `toml:"env"`
	Exclude               []string `toml:"exclude"`
	CopyAs                []string `toml:"copy_as"`
	SSHAgent              bool     `toml:"ssh_agent"`
	ReadonlyProject       bool     `toml:"readonly_project"`
	NoNetwork             bool     `toml:"no_network"`
	NoEnvPassthrough      bool     `toml:"no_env_passthrough"`
	Network               string   `toml:"network"`
	Pod                   string   `toml:"pod"`
	NoYolo                bool     `toml:"no_yolo"`
	Scratch               bool     `toml:"scratch"`
	ClaudeConfig          bool     `toml:"claude_config"`
	CodexConfig           bool     `toml:"codex_config"`
	GeminiConfig          bool     `toml:"gemini_config"`
	OpencodeConfig        bool     `toml:"opencode_config"`
	PiConfig              bool     `toml:"pi_config"`
	GitConfig             bool     `toml:"git_config"`
	GhToken               bool     `toml:"gh_token"`
	RTK                   bool     `toml:"rtk"`
	CopyAgentInstructions bool     `toml:"copy_agent_instructions"`
	NoProject             bool     `toml:"no_project"`
	Docker                bool     `toml:"docker"`
	Clipboard             bool     `toml:"clipboard"`
	OpenBridge            bool     `toml:"open_bridge"`

	CPUs        string          `toml:"cpus"`
	Memory      string          `toml:"memory"`
	ShmSize     string          `toml:"shm_size"`
	GPUs        string          `toml:"gpus"`
	Devices     []string        `toml:"devices"`
	CapAdd      []string        `toml:"cap_add"`
	CapDrop     []string        `toml:"cap_drop"`
	RuntimeArgs []string        `toml:"runtime_args"`
	Customize   CustomizeConfig `toml:"customize"`
	Remote      RemoteConfig    `toml:"remote"`

	Setup        bool       `toml:"-"`
	RebuildImage bool       `toml:"-"`
	Fork         ForkConfig `toml:"-"`

	ClipboardEndpoint  string `toml:"-"`
	ClipboardToken     string `toml:"-"`
	OpenBridgeEndpoint string `toml:"-"`
	OpenBridgeToken    string `toml:"-"`
}

func defaultConfig() Config {
	return Config{
		Image: "ghcr.io/finbarr/yolobox:latest",
		Remote: RemoteConfig{
			SSHUser: "root",
			DigitalOcean: RemoteDigitalOceanConfig{
				Region: "nyc3",
				Size:   "s-2vcpu-4gb",
				Image:  "ubuntu-24-04-x64",
				Tags:   []string{"yolobox"},
			},
		},
	}
}

func loadConfig(projectDir string) (Config, error) {
	started := time.Now()
	defer func() {
		traceDuration("host: load config", started)
	}()

	cfg := defaultConfig()

	globalPath, err := globalConfigPath()
	if err != nil {
		return Config{}, err
	}
	if err := mergeConfigFile(globalPath, &cfg); err != nil {
		return Config{}, err
	}

	projectPath := filepath.Join(projectDir, ".yolobox.toml")
	if err := mergeConfigFile(projectPath, &cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func loadSetupDefaults() (Config, error) {
	cfg := defaultConfig()

	globalPath, err := globalConfigPath()
	if err != nil {
		return Config{}, err
	}
	if err := mergeConfigFile(globalPath, &cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func globalConfigPath() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "yolobox", "config.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "yolobox", "config.toml"), nil
}

func mergeConfigFile(path string, cfg *Config) error {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var fileCfg Config
	if _, err := toml.DecodeFile(path, &fileCfg); err != nil {
		return err
	}

	mergeConfig(cfg, fileCfg)
	return nil
}

func mergeConfig(dst *Config, src Config) {
	if src.Mode != "" {
		dst.Mode = strings.ToLower(strings.TrimSpace(src.Mode))
	}
	if src.Runtime != "" {
		dst.Runtime = src.Runtime
	}
	if src.Image != "" {
		dst.Image = src.Image
	}
	if src.ContainerName != "" {
		dst.ContainerName = src.ContainerName
	}
	if src.DefaultHarness != "" {
		dst.DefaultHarness = strings.ToLower(strings.TrimSpace(src.DefaultHarness))
	}
	if src.RemoteName != "" {
		dst.RemoteName = strings.ToLower(strings.TrimSpace(src.RemoteName))
	}
	if src.RemoteWorkspace != "" {
		dst.RemoteWorkspace = strings.ToLower(strings.TrimSpace(src.RemoteWorkspace))
	}
	if len(src.Mounts) > 0 {
		dst.Mounts = append([]string{}, src.Mounts...)
	}
	if len(src.Env) > 0 {
		dst.Env = append([]string{}, src.Env...)
	}
	if len(src.Exclude) > 0 {
		dst.Exclude = append([]string{}, src.Exclude...)
	}
	if len(src.CopyAs) > 0 {
		dst.CopyAs = append([]string{}, src.CopyAs...)
	}
	if src.SSHAgent {
		dst.SSHAgent = true
	}
	if src.ReadonlyProject {
		dst.ReadonlyProject = true
	}
	if src.NoNetwork {
		dst.NoNetwork = true
	}
	if src.NoEnvPassthrough {
		dst.NoEnvPassthrough = true
	}
	if src.Network != "" {
		dst.Network = src.Network
	}
	if src.Pod != "" {
		dst.Pod = src.Pod
	}
	if src.NoYolo {
		dst.NoYolo = true
	}
	if src.Scratch {
		dst.Scratch = true
	}
	if src.ClaudeConfig {
		dst.ClaudeConfig = true
	}
	if src.CodexConfig {
		dst.CodexConfig = true
	}
	if src.GeminiConfig {
		dst.GeminiConfig = true
	}
	if src.OpencodeConfig {
		dst.OpencodeConfig = true
	}
	if src.PiConfig {
		dst.PiConfig = true
	}
	if src.GitConfig {
		dst.GitConfig = true
	}
	if src.GhToken {
		dst.GhToken = true
	}
	if src.RTK {
		dst.RTK = true
	}
	if src.CopyAgentInstructions {
		dst.CopyAgentInstructions = true
	}
	if src.NoProject {
		dst.NoProject = true
	}
	if src.Docker {
		dst.Docker = true
	}
	if src.Clipboard {
		dst.Clipboard = true
	}
	if src.OpenBridge {
		dst.OpenBridge = true
	}

	if src.CPUs != "" {
		dst.CPUs = src.CPUs
	}
	if src.Memory != "" {
		dst.Memory = src.Memory
	}
	if src.ShmSize != "" {
		dst.ShmSize = src.ShmSize
	}
	if src.GPUs != "" {
		dst.GPUs = src.GPUs
	}
	if len(src.Devices) > 0 {
		dst.Devices = append([]string{}, src.Devices...)
	}
	if len(src.CapAdd) > 0 {
		dst.CapAdd = append([]string{}, src.CapAdd...)
	}
	if len(src.CapDrop) > 0 {
		dst.CapDrop = append([]string{}, src.CapDrop...)
	}
	if len(src.RuntimeArgs) > 0 {
		dst.RuntimeArgs = append([]string{}, src.RuntimeArgs...)
	}
	if len(src.Customize.Packages) > 0 {
		dst.Customize.Packages = append([]string{}, src.Customize.Packages...)
	}
	if src.Customize.Dockerfile != "" {
		dst.Customize.Dockerfile = src.Customize.Dockerfile
	}
	mergeRemoteConfig(&dst.Remote, src.Remote)
}

func mergeRemoteConfig(dst *RemoteConfig, src RemoteConfig) {
	if src.BackendURL != "" {
		dst.BackendURL = strings.TrimRight(strings.TrimSpace(src.BackendURL), "/")
	}
	if src.BackendToken != "" {
		dst.BackendToken = strings.TrimSpace(src.BackendToken)
	}
	if src.Provider != "" {
		dst.Provider = strings.ToLower(strings.TrimSpace(src.Provider))
	}
	if src.SSHUser != "" {
		dst.SSHUser = strings.TrimSpace(src.SSHUser)
	}
	if len(src.Setup) > 0 {
		dst.Setup = append([]string{}, src.Setup...)
	}
	mergeRemoteDigitalOceanConfig(&dst.DigitalOcean, src.DigitalOcean)
}

func mergeRemoteDigitalOceanConfig(dst *RemoteDigitalOceanConfig, src RemoteDigitalOceanConfig) {
	if src.Token != "" {
		dst.Token = strings.TrimSpace(src.Token)
	}
	if src.Region != "" {
		dst.Region = strings.TrimSpace(src.Region)
	}
	if src.Size != "" {
		dst.Size = strings.TrimSpace(src.Size)
	}
	if src.Image != "" {
		dst.Image = strings.TrimSpace(src.Image)
	}
	if len(src.SSHKeys) > 0 {
		dst.SSHKeys = append([]string{}, src.SSHKeys...)
	}
	if len(src.Tags) > 0 {
		dst.Tags = append([]string{}, src.Tags...)
	}
	if src.VPCUUID != "" {
		dst.VPCUUID = strings.TrimSpace(src.VPCUUID)
	}
}

func printConfig(cfg Config) error {
	projectDir, err := os.Getwd()
	if err != nil {
		return err
	}
	fmt.Printf("%smode:%s %s\n", colorBold, colorReset, displayMode(cfg.Mode))
	fmt.Printf("%sruntime:%s %s\n", colorBold, colorReset, resolvedRuntimeName(cfg.Runtime))
	fmt.Printf("%simage:%s %s\n", colorBold, colorReset, cfg.Image)
	printStringConfigField("container_name", cfg.ContainerName)
	fmt.Printf("%sdefault_harness:%s %s\n", colorBold, colorReset, displayDefaultHarness(cfg.DefaultHarness))
	fmt.Printf("%sremote_name:%s %s\n", colorBold, colorReset, configValueOrNotSet(cfg.RemoteName))
	fmt.Printf("%sremote_workspace:%s %s\n", colorBold, colorReset, configValueOrNotSet(effectiveRemoteWorkspace(cfg.RemoteWorkspace)))
	fmt.Printf("%sproject:%s %s\n", colorBold, colorReset, projectDir)
	fmt.Printf("%sssh_agent:%s %t\n", colorBold, colorReset, cfg.SSHAgent)
	fmt.Printf("%sreadonly_project:%s %t\n", colorBold, colorReset, cfg.ReadonlyProject)
	fmt.Printf("%sno_network:%s %t\n", colorBold, colorReset, cfg.NoNetwork)
	fmt.Printf("%sno_env_passthrough:%s %t\n", colorBold, colorReset, cfg.NoEnvPassthrough)
	fmt.Printf("%snetwork:%s %s\n", colorBold, colorReset, cfg.Network)
	fmt.Printf("%spod:%s %s\n", colorBold, colorReset, cfg.Pod)
	fmt.Printf("%sno_yolo:%s %t\n", colorBold, colorReset, cfg.NoYolo)
	fmt.Printf("%sscratch:%s %t\n", colorBold, colorReset, cfg.Scratch)
	fmt.Printf("%sclaude_config:%s %t\n", colorBold, colorReset, cfg.ClaudeConfig)
	fmt.Printf("%scodex_config:%s %t\n", colorBold, colorReset, cfg.CodexConfig)
	fmt.Printf("%sgemini_config:%s %t\n", colorBold, colorReset, cfg.GeminiConfig)
	fmt.Printf("%sopencode_config:%s %t\n", colorBold, colorReset, cfg.OpencodeConfig)
	fmt.Printf("%spi_config:%s %t\n", colorBold, colorReset, cfg.PiConfig)
	fmt.Printf("%sgit_config:%s %t\n", colorBold, colorReset, cfg.GitConfig)
	fmt.Printf("%sgh_token:%s %t\n", colorBold, colorReset, cfg.GhToken)
	fmt.Printf("%srtk:%s %t\n", colorBold, colorReset, cfg.RTK)
	fmt.Printf("%scopy_agent_instructions:%s %t\n", colorBold, colorReset, cfg.CopyAgentInstructions)
	fmt.Printf("%sno_project:%s %t\n", colorBold, colorReset, cfg.NoProject)
	fmt.Printf("%sdocker:%s %t\n", colorBold, colorReset, cfg.Docker)
	fmt.Printf("%sclipboard:%s %t\n", colorBold, colorReset, cfg.Clipboard)
	fmt.Printf("%sopen_bridge:%s %t\n", colorBold, colorReset, cfg.OpenBridge)

	printStringConfigField("cpus", cfg.CPUs)
	printStringConfigField("memory", cfg.Memory)
	printStringConfigField("shm_size", cfg.ShmSize)
	printStringConfigField("gpus", cfg.GPUs)
	printSliceConfigField("devices", cfg.Devices)
	printSliceConfigField("cap_add", cfg.CapAdd)
	printSliceConfigField("cap_drop", cfg.CapDrop)
	printSliceConfigField("runtime_args", cfg.RuntimeArgs)
	printSliceConfigField("customize.packages", cfg.Customize.Packages)
	printStringConfigField("customize.dockerfile", cfg.Customize.Dockerfile)
	printStringConfigField("remote.backend_url", cfg.Remote.BackendURL)
	printStringConfigField("remote.backend_token", redactConfigSecret(cfg.Remote.BackendToken))
	printStringConfigField("remote.provider", cfg.Remote.Provider)
	printStringConfigField("remote.ssh_user", cfg.Remote.SSHUser)
	printSliceConfigField("remote.setup", cfg.Remote.Setup)
	printStringConfigField("remote.digitalocean.token", redactConfigSecret(remoteDigitalOceanToken(cfg)))
	printStringConfigField("remote.digitalocean.region", cfg.Remote.DigitalOcean.Region)
	printStringConfigField("remote.digitalocean.size", cfg.Remote.DigitalOcean.Size)
	printStringConfigField("remote.digitalocean.image", cfg.Remote.DigitalOcean.Image)
	printSliceConfigField("remote.digitalocean.ssh_keys", cfg.Remote.DigitalOcean.SSHKeys)
	printSliceConfigField("remote.digitalocean.tags", cfg.Remote.DigitalOcean.Tags)
	printStringConfigField("remote.digitalocean.vpc_uuid", cfg.Remote.DigitalOcean.VPCUUID)
	printSliceConfigField("exclude", cfg.Exclude)
	printSliceConfigField("copy_as", cfg.CopyAs)

	if len(cfg.Mounts) > 0 {
		fmt.Printf("%smounts:%s\n", colorBold, colorReset)
		for _, m := range cfg.Mounts {
			fmt.Printf("  - %s\n", m)
		}
	}
	if len(cfg.Env) > 0 {
		fmt.Printf("%senv:%s\n", colorBold, colorReset)
		for _, e := range cfg.Env {
			fmt.Printf("  - %s\n", e)
		}
	}
	return nil
}

func printStringConfigField(name, value string) {
	fmt.Printf("%s%s:%s %s\n", colorBold, name, colorReset, configValueOrNotSet(value))
}

func printSliceConfigField(name string, values []string) {
	if len(values) == 0 {
		fmt.Printf("%s%s:%s (none)\n", colorBold, name, colorReset)
		return
	}
	fmt.Printf("%s%s:%s\n", colorBold, name, colorReset)
	for _, v := range values {
		fmt.Printf("  - %s\n", v)
	}
}

func configValueOrNotSet(value string) string {
	if strings.TrimSpace(value) == "" {
		return "(not set)"
	}
	return value
}

func saveGlobalConfig(cfg Config) error {
	path, err := globalConfigPath()
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	var lines []string
	if mode := normalizeMode(cfg.Mode); mode == "remote" {
		lines = append(lines, `mode = "remote"`)
	}
	if harness := normalizeDefaultHarness(cfg.DefaultHarness); harness != "" {
		lines = append(lines, fmt.Sprintf("default_harness = %q", harness))
	}
	if cfg.ContainerName != "" {
		lines = append(lines, fmt.Sprintf("container_name = %q", cfg.ContainerName))
	}
	if name := strings.TrimSpace(cfg.RemoteName); name != "" {
		lines = append(lines, fmt.Sprintf("remote_name = %q", strings.ToLower(name)))
	}
	if workspace := strings.TrimSpace(cfg.RemoteWorkspace); workspace != "" && workspace != remoteDefaultWorkspace {
		lines = append(lines, fmt.Sprintf("remote_workspace = %q", strings.ToLower(workspace)))
	}
	if cfg.GitConfig {
		lines = append(lines, "git_config = true")
	}
	if cfg.ClaudeConfig {
		lines = append(lines, "claude_config = true")
	}
	if cfg.CodexConfig {
		lines = append(lines, "codex_config = true")
	}
	if cfg.GeminiConfig {
		lines = append(lines, "gemini_config = true")
	}
	if cfg.OpencodeConfig {
		lines = append(lines, "opencode_config = true")
	}
	if cfg.PiConfig {
		lines = append(lines, "pi_config = true")
	}
	if cfg.GhToken {
		lines = append(lines, "gh_token = true")
	}
	if cfg.RTK {
		lines = append(lines, "rtk = true")
	}
	if len(cfg.Exclude) > 0 {
		lines = append(lines, fmt.Sprintf("exclude = %s", formatTomlStringSlice(cfg.Exclude)))
	}
	if len(cfg.CopyAs) > 0 {
		lines = append(lines, fmt.Sprintf("copy_as = %s", formatTomlStringSlice(cfg.CopyAs)))
	}
	if cfg.SSHAgent {
		lines = append(lines, "ssh_agent = true")
	}
	if cfg.NoNetwork {
		lines = append(lines, "no_network = true")
	}
	if cfg.NoEnvPassthrough {
		lines = append(lines, "no_env_passthrough = true")
	}
	if cfg.Network != "" {
		lines = append(lines, fmt.Sprintf("network = %q", cfg.Network))
	}
	if cfg.NoYolo {
		lines = append(lines, "no_yolo = true")
	}
	if cfg.NoProject {
		lines = append(lines, "no_project = true")
	}
	if cfg.Docker {
		lines = append(lines, "docker = true")
	}
	if cfg.Clipboard {
		lines = append(lines, "clipboard = true")
	}
	if cfg.OpenBridge {
		lines = append(lines, "open_bridge = true")
	}
	if cfg.Pod != "" {
		lines = append(lines, fmt.Sprintf("pod = %q", cfg.Pod))
	}
	if cfg.CPUs != "" {
		lines = append(lines, fmt.Sprintf("cpus = %q", cfg.CPUs))
	}
	if cfg.Memory != "" {
		lines = append(lines, fmt.Sprintf("memory = %q", cfg.Memory))
	}
	if cfg.ShmSize != "" {
		lines = append(lines, fmt.Sprintf("shm_size = %q", cfg.ShmSize))
	}
	if cfg.GPUs != "" {
		lines = append(lines, fmt.Sprintf("gpus = %q", cfg.GPUs))
	}
	if len(cfg.Devices) > 0 {
		lines = append(lines, fmt.Sprintf("devices = %s", formatTomlStringSlice(cfg.Devices)))
	}
	if len(cfg.CapAdd) > 0 {
		lines = append(lines, fmt.Sprintf("cap_add = %s", formatTomlStringSlice(cfg.CapAdd)))
	}
	if len(cfg.CapDrop) > 0 {
		lines = append(lines, fmt.Sprintf("cap_drop = %s", formatTomlStringSlice(cfg.CapDrop)))
	}
	if len(cfg.RuntimeArgs) > 0 {
		lines = append(lines, fmt.Sprintf("runtime_args = %s", formatTomlStringSlice(cfg.RuntimeArgs)))
	}
	if len(cfg.Customize.Packages) > 0 || cfg.Customize.Dockerfile != "" {
		lines = append(lines, "", "[customize]")
		if len(cfg.Customize.Packages) > 0 {
			lines = append(lines, fmt.Sprintf("packages = %s", formatTomlStringSlice(cfg.Customize.Packages)))
		}
		if cfg.Customize.Dockerfile != "" {
			lines = append(lines, fmt.Sprintf("dockerfile = %q", cfg.Customize.Dockerfile))
		}
	}
	if hasRemoteConfig(cfg.Remote) {
		lines = append(lines, "", "[remote]")
		if cfg.Remote.BackendURL != "" {
			lines = append(lines, fmt.Sprintf("backend_url = %q", cfg.Remote.BackendURL))
		}
		if cfg.Remote.BackendToken != "" {
			lines = append(lines, fmt.Sprintf("backend_token = %q", cfg.Remote.BackendToken))
		}
		if cfg.Remote.Provider != "" {
			lines = append(lines, fmt.Sprintf("provider = %q", strings.ToLower(cfg.Remote.Provider)))
		}
		if cfg.Remote.SSHUser != "" {
			lines = append(lines, fmt.Sprintf("ssh_user = %q", cfg.Remote.SSHUser))
		}
		if len(cfg.Remote.Setup) > 0 {
			lines = append(lines, fmt.Sprintf("setup = %s", formatTomlStringSlice(cfg.Remote.Setup)))
		}
		if hasRemoteDigitalOceanConfig(cfg.Remote.DigitalOcean) {
			lines = append(lines, "", "[remote.digitalocean]")
			if cfg.Remote.DigitalOcean.Token != "" {
				lines = append(lines, fmt.Sprintf("token = %q", cfg.Remote.DigitalOcean.Token))
			}
			if cfg.Remote.DigitalOcean.Region != "" {
				lines = append(lines, fmt.Sprintf("region = %q", cfg.Remote.DigitalOcean.Region))
			}
			if cfg.Remote.DigitalOcean.Size != "" {
				lines = append(lines, fmt.Sprintf("size = %q", cfg.Remote.DigitalOcean.Size))
			}
			if cfg.Remote.DigitalOcean.Image != "" {
				lines = append(lines, fmt.Sprintf("image = %q", cfg.Remote.DigitalOcean.Image))
			}
			if len(cfg.Remote.DigitalOcean.SSHKeys) > 0 {
				lines = append(lines, fmt.Sprintf("ssh_keys = %s", formatTomlStringSlice(cfg.Remote.DigitalOcean.SSHKeys)))
			}
			if len(cfg.Remote.DigitalOcean.Tags) > 0 {
				lines = append(lines, fmt.Sprintf("tags = %s", formatTomlStringSlice(cfg.Remote.DigitalOcean.Tags)))
			}
			if cfg.Remote.DigitalOcean.VPCUUID != "" {
				lines = append(lines, fmt.Sprintf("vpc_uuid = %q", cfg.Remote.DigitalOcean.VPCUUID))
			}
		}
	}

	content := strings.Join(lines, "\n")
	if content != "" {
		content += "\n"
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

func hasRemoteConfig(remote RemoteConfig) bool {
	def := defaultConfig().Remote
	return remote.BackendURL != "" ||
		remote.BackendToken != "" ||
		remote.Provider != "" ||
		(remote.SSHUser != "" && remote.SSHUser != def.SSHUser) ||
		len(remote.Setup) > 0 ||
		hasRemoteDigitalOceanConfig(remote.DigitalOcean)
}

func hasRemoteDigitalOceanConfig(cfg RemoteDigitalOceanConfig) bool {
	def := defaultConfig().Remote.DigitalOcean
	return cfg.Token != "" ||
		(cfg.Region != "" && cfg.Region != def.Region) ||
		(cfg.Size != "" && cfg.Size != def.Size) ||
		(cfg.Image != "" && cfg.Image != def.Image) ||
		len(cfg.SSHKeys) > 0 ||
		!sameStringSlice(cfg.Tags, def.Tags) ||
		cfg.VPCUUID != ""
}

func sameStringSlice(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func redactConfigSecret(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return "(set)"
}

func loadConfigFromEnv() (Config, error) {
	projectDir, err := os.Getwd()
	if err != nil {
		return Config{}, err
	}
	return loadConfig(projectDir)
}
