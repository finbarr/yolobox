package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const yoloboxContextFile = "/run/yolobox/context.json"
const yoloboxContextPayloadEnv = "YOLOBOX_CONTEXT_JSON_B64"

type contextManifest struct {
	SchemaVersion  int                   `json:"schema_version"`
	InsideYolobox  bool                  `json:"inside_yolobox"`
	YoloboxVersion string                `json:"yolobox_version"`
	GeneratedAt    string                `json:"generated_at"`
	Runtime        contextRuntime        `json:"runtime"`
	Launch         contextLaunch         `json:"launch"`
	Paths          contextPaths          `json:"paths"`
	Fork           *contextFork          `json:"fork,omitempty"`
	Config         contextConfigManifest `json:"config"`
}

type contextRuntime struct {
	Configured     string `json:"configured"`
	Selected       string `json:"selected"`
	AppleContainer bool   `json:"apple_container"`
	RootlessPodman bool   `json:"rootless_podman"`
}

type contextLaunch struct {
	Interactive            bool     `json:"interactive"`
	Command                []string `json:"command"`
	WorkingDir             string   `json:"working_dir"`
	ContextFile            string   `json:"context_file"`
	AutoPassthroughEnvKeys []string `json:"auto_passthrough_env_keys"`
	GhTokenForwarded       bool     `json:"gh_token_forwarded"`
}

type contextPaths struct {
	Project string `json:"project"`
	Home    string `json:"home"`
	Output  string `json:"output,omitempty"`
}

type contextFork struct {
	Name           string `json:"name"`
	Source         string `json:"source"`
	Copy           string `json:"copy"`
	ComposeProject string `json:"compose_project"`
}

type contextConfigManifest struct {
	Runtime               string                         `json:"runtime"`
	Image                 string                         `json:"image"`
	Mounts                []string                       `json:"mounts"`
	EnvKeys               []string                       `json:"env_keys"`
	Exclude               []string                       `json:"exclude"`
	CopyAs                []string                       `json:"copy_as"`
	SSHAgent              bool                           `json:"ssh_agent"`
	ReadonlyProject       bool                           `json:"readonly_project"`
	NoNetwork             bool                           `json:"no_network"`
	Network               string                         `json:"network"`
	Pod                   string                         `json:"pod"`
	NoYolo                bool                           `json:"no_yolo"`
	Scratch               bool                           `json:"scratch"`
	ClaudeConfig          bool                           `json:"claude_config"`
	CodexConfig           bool                           `json:"codex_config"`
	GeminiConfig          bool                           `json:"gemini_config"`
	OpencodeConfig        bool                           `json:"opencode_config"`
	GitConfig             bool                           `json:"git_config"`
	GhToken               bool                           `json:"gh_token"`
	CopyAgentInstructions bool                           `json:"copy_agent_instructions"`
	Docker                bool                           `json:"docker"`
	Clipboard             bool                           `json:"clipboard"`
	CPUs                  string                         `json:"cpus"`
	Memory                string                         `json:"memory"`
	ShmSize               string                         `json:"shm_size"`
	GPUs                  string                         `json:"gpus"`
	Devices               []string                       `json:"devices"`
	CapAdd                []string                       `json:"cap_add"`
	CapDrop               []string                       `json:"cap_drop"`
	RuntimeArgs           []string                       `json:"runtime_args"`
	Customize             contextCustomizeConfigManifest `json:"customize"`
}

type contextCustomizeConfigManifest struct {
	Packages   []string `json:"packages"`
	Dockerfile string   `json:"dockerfile"`
}

func encodeContextManifest(cfg Config, projectDir string, command []string, interactive bool, autoPassthroughEnvKeys []string, ghTokenForwarded bool) (string, error) {
	data, err := json.MarshalIndent(buildContextManifest(cfg, projectDir, command, interactive, autoPassthroughEnvKeys, ghTokenForwarded), "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to encode context manifest: %w", err)
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

func buildContextManifest(cfg Config, projectDir string, command []string, interactive bool, autoPassthroughEnvKeys []string, ghTokenForwarded bool) contextManifest {
	selectedRuntime := resolvedRuntimeName(cfg.Runtime)
	if runtimePath, err := resolveRuntime(cfg.Runtime); err == nil {
		selectedRuntime = filepath.Base(runtimePath)
	}

	paths := contextPaths{
		Project: projectDir,
		Home:    "/home/yolo",
	}
	if cfg.ReadonlyProject {
		paths.Output = "/output"
	}

	var fork *contextFork
	if cfg.Fork.Name != "" {
		fork = &contextFork{
			Name:           cfg.Fork.Name,
			Source:         cfg.Fork.Source,
			Copy:           cfg.Fork.Copy,
			ComposeProject: cfg.Fork.ComposeProject,
		}
	}

	return contextManifest{
		SchemaVersion:  1,
		InsideYolobox:  true,
		YoloboxVersion: Version,
		GeneratedAt:    time.Now().UTC().Format(time.RFC3339),
		Runtime: contextRuntime{
			Configured:     resolvedRuntimeName(cfg.Runtime),
			Selected:       selectedRuntime,
			AppleContainer: isAppleContainer(cfg.Runtime),
			RootlessPodman: isRootlessPodman(cfg.Runtime),
		},
		Launch: contextLaunch{
			Interactive:            interactive,
			Command:                append([]string{}, command...),
			WorkingDir:             projectDir,
			ContextFile:            yoloboxContextFile,
			AutoPassthroughEnvKeys: append([]string{}, autoPassthroughEnvKeys...),
			GhTokenForwarded:       ghTokenForwarded,
		},
		Paths: paths,
		Fork:  fork,
		Config: contextConfigManifest{
			Runtime:               resolvedRuntimeName(cfg.Runtime),
			Image:                 cfg.Image,
			Mounts:                append([]string{}, cfg.Mounts...),
			EnvKeys:               envKeys(cfg.Env),
			Exclude:               append([]string{}, cfg.Exclude...),
			CopyAs:                append([]string{}, cfg.CopyAs...),
			SSHAgent:              cfg.SSHAgent,
			ReadonlyProject:       cfg.ReadonlyProject,
			NoNetwork:             cfg.NoNetwork,
			Network:               cfg.Network,
			Pod:                   cfg.Pod,
			NoYolo:                cfg.NoYolo,
			Scratch:               cfg.Scratch,
			ClaudeConfig:          cfg.ClaudeConfig,
			CodexConfig:           cfg.CodexConfig,
			GeminiConfig:          cfg.GeminiConfig,
			OpencodeConfig:        cfg.OpencodeConfig,
			GitConfig:             cfg.GitConfig,
			GhToken:               cfg.GhToken,
			CopyAgentInstructions: cfg.CopyAgentInstructions,
			Docker:                cfg.Docker,
			Clipboard:             cfg.Clipboard,
			CPUs:                  cfg.CPUs,
			Memory:                cfg.Memory,
			ShmSize:               cfg.ShmSize,
			GPUs:                  cfg.GPUs,
			Devices:               append([]string{}, cfg.Devices...),
			CapAdd:                append([]string{}, cfg.CapAdd...),
			CapDrop:               append([]string{}, cfg.CapDrop...),
			RuntimeArgs:           append([]string{}, cfg.RuntimeArgs...),
			Customize: contextCustomizeConfigManifest{
				Packages:   append([]string{}, cfg.Customize.Packages...),
				Dockerfile: cfg.Customize.Dockerfile,
			},
		},
	}
}

func envKeys(envSpecs []string) []string {
	keys := make([]string, 0, len(envSpecs))
	for _, spec := range envSpecs {
		if spec == "" {
			continue
		}
		if idx := strings.Index(spec, "="); idx > 0 {
			keys = append(keys, spec[:idx])
			continue
		}
		keys = append(keys, spec)
	}
	return keys
}
