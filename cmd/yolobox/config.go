package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type CustomizeConfig struct {
	Packages   []string `toml:"packages"`
	Dockerfile string   `toml:"dockerfile"`
}

type ForkConfig struct {
	Name           string `toml:"-"`
	Source         string `toml:"-"`
	Copy           string `toml:"-"`
	ComposeProject string `toml:"-"`
}

type Config struct {
	Runtime               string   `toml:"runtime"`
	Image                 string   `toml:"image"`
	Mounts                []string `toml:"mounts"`
	Env                   []string `toml:"env"`
	Exclude               []string `toml:"exclude"`
	CopyAs                []string `toml:"copy_as"`
	SSHAgent              bool     `toml:"ssh_agent"`
	ReadonlyProject       bool     `toml:"readonly_project"`
	NoNetwork             bool     `toml:"no_network"`
	Network               string   `toml:"network"`
	Pod                   string   `toml:"pod"`
	NoYolo                bool     `toml:"no_yolo"`
	Scratch               bool     `toml:"scratch"`
	ClaudeConfig          bool     `toml:"claude_config"`
	CodexConfig           bool     `toml:"codex_config"`
	GeminiConfig          bool     `toml:"gemini_config"`
	OpencodeConfig        bool     `toml:"opencode_config"`
	GitConfig             bool     `toml:"git_config"`
	GhToken               bool     `toml:"gh_token"`
	CopyAgentInstructions bool     `toml:"copy_agent_instructions"`
	Docker                bool     `toml:"docker"`
	Clipboard             bool     `toml:"clipboard"`

	CPUs        string          `toml:"cpus"`
	Memory      string          `toml:"memory"`
	ShmSize     string          `toml:"shm_size"`
	GPUs        string          `toml:"gpus"`
	Devices     []string        `toml:"devices"`
	CapAdd      []string        `toml:"cap_add"`
	CapDrop     []string        `toml:"cap_drop"`
	RuntimeArgs []string        `toml:"runtime_args"`
	Customize   CustomizeConfig `toml:"customize"`

	Setup        bool       `toml:"-"`
	RebuildImage bool       `toml:"-"`
	Fork         ForkConfig `toml:"-"`

	ClipboardEndpoint string `toml:"-"`
	ClipboardToken    string `toml:"-"`
}

func defaultConfig() Config {
	return Config{
		Image: "ghcr.io/finbarr/yolobox:latest",
	}
}

func loadConfig(projectDir string) (Config, error) {
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
	if src.Runtime != "" {
		dst.Runtime = src.Runtime
	}
	if src.Image != "" {
		dst.Image = src.Image
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
	if src.GitConfig {
		dst.GitConfig = true
	}
	if src.GhToken {
		dst.GhToken = true
	}
	if src.CopyAgentInstructions {
		dst.CopyAgentInstructions = true
	}
	if src.Docker {
		dst.Docker = true
	}
	if src.Clipboard {
		dst.Clipboard = true
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
}

func printConfig(cfg Config) error {
	projectDir, err := os.Getwd()
	if err != nil {
		return err
	}
	fmt.Printf("%sruntime:%s %s\n", colorBold, colorReset, resolvedRuntimeName(cfg.Runtime))
	fmt.Printf("%simage:%s %s\n", colorBold, colorReset, cfg.Image)
	fmt.Printf("%sproject:%s %s\n", colorBold, colorReset, projectDir)
	fmt.Printf("%sssh_agent:%s %t\n", colorBold, colorReset, cfg.SSHAgent)
	fmt.Printf("%sreadonly_project:%s %t\n", colorBold, colorReset, cfg.ReadonlyProject)
	fmt.Printf("%sno_network:%s %t\n", colorBold, colorReset, cfg.NoNetwork)
	fmt.Printf("%snetwork:%s %s\n", colorBold, colorReset, cfg.Network)
	fmt.Printf("%spod:%s %s\n", colorBold, colorReset, cfg.Pod)
	fmt.Printf("%sno_yolo:%s %t\n", colorBold, colorReset, cfg.NoYolo)
	fmt.Printf("%sscratch:%s %t\n", colorBold, colorReset, cfg.Scratch)
	fmt.Printf("%sclaude_config:%s %t\n", colorBold, colorReset, cfg.ClaudeConfig)
	fmt.Printf("%scodex_config:%s %t\n", colorBold, colorReset, cfg.CodexConfig)
	fmt.Printf("%sgemini_config:%s %t\n", colorBold, colorReset, cfg.GeminiConfig)
	fmt.Printf("%sopencode_config:%s %t\n", colorBold, colorReset, cfg.OpencodeConfig)
	fmt.Printf("%sgit_config:%s %t\n", colorBold, colorReset, cfg.GitConfig)
	fmt.Printf("%sgh_token:%s %t\n", colorBold, colorReset, cfg.GhToken)
	fmt.Printf("%scopy_agent_instructions:%s %t\n", colorBold, colorReset, cfg.CopyAgentInstructions)
	fmt.Printf("%sdocker:%s %t\n", colorBold, colorReset, cfg.Docker)
	fmt.Printf("%sclipboard:%s %t\n", colorBold, colorReset, cfg.Clipboard)

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
	if cfg.GhToken {
		lines = append(lines, "gh_token = true")
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
	if cfg.Network != "" {
		lines = append(lines, fmt.Sprintf("network = %q", cfg.Network))
	}
	if cfg.NoYolo {
		lines = append(lines, "no_yolo = true")
	}
	if cfg.Docker {
		lines = append(lines, "docker = true")
	}
	if cfg.Clipboard {
		lines = append(lines, "clipboard = true")
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

	content := strings.Join(lines, "\n")
	if content != "" {
		content += "\n"
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

func loadConfigFromEnv() (Config, error) {
	projectDir, err := os.Getwd()
	if err != nil {
		return Config{}, err
	}
	return loadConfig(projectDir)
}
