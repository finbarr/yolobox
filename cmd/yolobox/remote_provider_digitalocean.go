package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	remoteProviderDigitalOcean     = "digitalocean"
	digitalOceanAPIBaseURL         = "https://api.digitalocean.com"
	digitalOceanAPIURLEnv          = "YOLOBOX_DIGITALOCEAN_API_URL"
	digitalOceanAccessTokenEnv     = "DIGITALOCEAN_ACCESS_TOKEN"
	digitalOceanTokenEnv           = "DIGITALOCEAN_TOKEN"
	digitalOceanFallbackTokenEnv   = "DO_API_TOKEN"
	remoteSSHPublicKeyEnv          = "YOLOBOX_REMOTE_SSH_PUBLIC_KEY"
	digitalOceanPollInterval       = 5 * time.Second
	digitalOceanDefaultPollTimeout = 4 * time.Minute
)

type digitalOceanProvider struct {
	token       string
	apiURL      string
	region      string
	size        string
	image       string
	sshKeys     []string
	tags        []string
	vpcUUID     string
	sshUser     string
	httpClient  *http.Client
	pollTimeout time.Duration
}

type digitalOceanDroplet struct {
	ID       int64    `json:"id"`
	Name     string   `json:"name"`
	Status   string   `json:"status"`
	Tags     []string `json:"tags"`
	SizeSlug string   `json:"size_slug"`
	Region   struct {
		Slug string `json:"slug"`
	} `json:"region"`
	Image struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	} `json:"image"`
	Networks struct {
		V4 []struct {
			IPAddress string `json:"ip_address"`
			Type      string `json:"type"`
		} `json:"v4"`
	} `json:"networks"`
	CreatedAt time.Time `json:"created_at"`
}

type digitalOceanDropletsResponse struct {
	Droplets []digitalOceanDroplet `json:"droplets"`
}

type digitalOceanDropletResponse struct {
	Droplet digitalOceanDroplet `json:"droplet"`
}

type digitalOceanCreateDropletRequest struct {
	Name       string        `json:"name"`
	Region     string        `json:"region"`
	Size       string        `json:"size"`
	Image      string        `json:"image"`
	SSHKeys    []interface{} `json:"ssh_keys,omitempty"`
	Tags       []string      `json:"tags,omitempty"`
	Monitoring bool          `json:"monitoring"`
	VPCUUID    string        `json:"vpc_uuid,omitempty"`
}

type digitalOceanSSHKey struct {
	ID          int64  `json:"id"`
	Fingerprint string `json:"fingerprint"`
	Name        string `json:"name"`
	PublicKey   string `json:"public_key"`
}

type digitalOceanSSHKeysResponse struct {
	SSHKeys []digitalOceanSSHKey `json:"ssh_keys"`
}

type digitalOceanSSHKeyResponse struct {
	SSHKey digitalOceanSSHKey `json:"ssh_key"`
}

type digitalOceanErrorResponse struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

func newDigitalOceanProvider(cfg Config, opts remoteProvisionOptions) (*digitalOceanProvider, error) {
	do := cfg.Remote.DigitalOcean
	if opts.Region != "" {
		do.Region = opts.Region
	}
	if opts.Size != "" {
		do.Size = opts.Size
	}
	if opts.Image != "" {
		do.Image = opts.Image
	}
	if len(opts.SSHKeys) > 0 {
		do.SSHKeys = append([]string{}, opts.SSHKeys...)
	}
	if len(opts.Tags) > 0 {
		do.Tags = append([]string{}, opts.Tags...)
	}
	token := remoteDigitalOceanToken(cfg)
	if token == "" {
		return nil, fmt.Errorf("DigitalOcean provider requires remote.digitalocean.token, %s, %s, or %s", digitalOceanAccessTokenEnv, digitalOceanTokenEnv, digitalOceanFallbackTokenEnv)
	}
	region := firstNonEmpty(do.Region, defaultConfig().Remote.DigitalOcean.Region)
	size := firstNonEmpty(do.Size, defaultConfig().Remote.DigitalOcean.Size)
	image := firstNonEmpty(do.Image, defaultConfig().Remote.DigitalOcean.Image)
	tags := append([]string{}, do.Tags...)
	if len(tags) == 0 {
		tags = append([]string{}, defaultConfig().Remote.DigitalOcean.Tags...)
	}
	apiURL := strings.TrimRight(firstNonEmpty(os.Getenv(digitalOceanAPIURLEnv), digitalOceanAPIBaseURL), "/")
	return &digitalOceanProvider{
		token:       token,
		apiURL:      apiURL,
		region:      region,
		size:        size,
		image:       image,
		sshKeys:     append([]string{}, do.SSHKeys...),
		tags:        tags,
		vpcUUID:     strings.TrimSpace(do.VPCUUID),
		sshUser:     firstNonEmpty(opts.SSHUser, cfg.Remote.SSHUser, "root"),
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		pollTimeout: digitalOceanDefaultPollTimeout,
	}, nil
}

func remoteDigitalOceanToken(cfg Config) string {
	if token := strings.TrimSpace(cfg.Remote.DigitalOcean.Token); token != "" {
		return token
	}
	if token := strings.TrimSpace(os.Getenv(digitalOceanAccessTokenEnv)); token != "" {
		return token
	}
	if token := strings.TrimSpace(os.Getenv(digitalOceanTokenEnv)); token != "" {
		return token
	}
	return strings.TrimSpace(os.Getenv(digitalOceanFallbackTokenEnv))
}

func (p *digitalOceanProvider) Name() string {
	return remoteProviderDigitalOcean
}

func (p *digitalOceanProvider) EnsureMachine(ctx context.Context, req remoteMachineProviderRequest) (remoteMachine, string, error) {
	if existing, ok, err := p.findDroplet(ctx, req.Name); err != nil {
		return remoteMachine{}, "", err
	} else if ok {
		return p.machineFromDroplet(req.Name, existing), existing.Status, nil
	}

	sshKeys := p.sshKeys
	if len(sshKeys) == 0 {
		keyID, err := p.ensureDefaultSSHKey(ctx)
		if err != nil {
			return remoteMachine{}, "", err
		}
		sshKeys = []string{strconv.FormatInt(keyID, 10)}
	}

	body := digitalOceanCreateDropletRequest{
		Name:       digitalOceanMachineName(req.Name),
		Region:     p.region,
		Size:       p.size,
		Image:      p.image,
		SSHKeys:    digitalOceanSSHKeyValues(sshKeys),
		Tags:       p.machineTags(req.Name),
		Monitoring: true,
		VPCUUID:    p.vpcUUID,
	}
	var response digitalOceanDropletResponse
	if err := p.request(ctx, http.MethodPost, "/v2/droplets", body, &response); err != nil {
		return remoteMachine{}, "", err
	}
	droplet := response.Droplet
	if droplet.ID == 0 {
		return remoteMachine{}, "", fmt.Errorf("DigitalOcean create response did not include droplet id")
	}
	if publicIPv4FromDroplet(droplet) == "" {
		refreshed, err := p.waitForDropletAddress(ctx, droplet.ID)
		if err != nil {
			return remoteMachine{}, "", err
		}
		droplet = refreshed
	}
	return p.machineFromDroplet(req.Name, droplet), droplet.Status, nil
}

func (p *digitalOceanProvider) GetMachine(ctx context.Context, machine remoteMachine) (remoteMachine, string, error) {
	droplet, ok, err := p.findDropletForMachine(ctx, machine)
	if err != nil {
		return remoteMachine{}, "", err
	}
	if !ok {
		return remoteMachine{}, "", fmt.Errorf("DigitalOcean droplet for remote %s was not found", machine.Name)
	}
	return p.machineFromDroplet(machine.Name, droplet), droplet.Status, nil
}

func (p *digitalOceanProvider) ReleaseMachine(ctx context.Context, machine remoteMachine) error {
	dropletID := strings.TrimSpace(machine.ProviderID)
	if dropletID == "" {
		droplet, ok, err := p.findDropletForMachine(ctx, machine)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		dropletID = strconv.FormatInt(droplet.ID, 10)
	}
	return p.request(ctx, http.MethodDelete, "/v2/droplets/"+url.PathEscape(dropletID), nil, nil)
}

func (p *digitalOceanProvider) findDropletForMachine(ctx context.Context, machine remoteMachine) (digitalOceanDroplet, bool, error) {
	if machine.ProviderID != "" {
		var response digitalOceanDropletResponse
		err := p.request(ctx, http.MethodGet, "/v2/droplets/"+url.PathEscape(machine.ProviderID), nil, &response)
		if err == nil {
			return response.Droplet, response.Droplet.ID != 0, nil
		}
	}
	return p.findDroplet(ctx, machine.Name)
}

func (p *digitalOceanProvider) findDroplet(ctx context.Context, machineName string) (digitalOceanDroplet, bool, error) {
	tag := digitalOceanMachineTag(machineName)
	var response digitalOceanDropletsResponse
	endpoint := "/v2/droplets?tag_name=" + url.QueryEscape(tag) + "&per_page=200"
	if err := p.request(ctx, http.MethodGet, endpoint, nil, &response); err != nil {
		return digitalOceanDroplet{}, false, err
	}
	wantName := digitalOceanMachineName(machineName)
	for _, droplet := range response.Droplets {
		if droplet.Name == wantName && stringSliceContains(droplet.Tags, tag) {
			return droplet, true, nil
		}
	}
	return digitalOceanDroplet{}, false, nil
}

func (p *digitalOceanProvider) waitForDropletAddress(ctx context.Context, id int64) (digitalOceanDroplet, error) {
	deadline := time.Now().Add(p.pollTimeout)
	for {
		var response digitalOceanDropletResponse
		if err := p.request(ctx, http.MethodGet, "/v2/droplets/"+strconv.FormatInt(id, 10), nil, &response); err != nil {
			return digitalOceanDroplet{}, err
		}
		if publicIPv4FromDroplet(response.Droplet) != "" {
			return response.Droplet, nil
		}
		if time.Now().After(deadline) {
			return digitalOceanDroplet{}, fmt.Errorf("timed out waiting for DigitalOcean droplet %d to receive a public IPv4", id)
		}
		select {
		case <-ctx.Done():
			return digitalOceanDroplet{}, ctx.Err()
		case <-time.After(digitalOceanPollInterval):
		}
	}
}

func (p *digitalOceanProvider) ensureDefaultSSHKey(ctx context.Context) (int64, error) {
	publicKey, err := defaultRemotePublicKey()
	if err != nil {
		return 0, err
	}
	var keys digitalOceanSSHKeysResponse
	if err := p.request(ctx, http.MethodGet, "/v2/account/keys?per_page=200", nil, &keys); err != nil {
		return 0, err
	}
	publicKey = strings.TrimSpace(publicKey)
	for _, key := range keys.SSHKeys {
		if strings.TrimSpace(key.PublicKey) == publicKey {
			return key.ID, nil
		}
	}
	host, _ := os.Hostname()
	if host == "" {
		host = "host"
	}
	sum := sha256.Sum256([]byte(publicKey))
	name := fmt.Sprintf("yolobox-%s-%s", sanitizeRemoteResourceID(host), hex.EncodeToString(sum[:])[:12])
	var created digitalOceanSSHKeyResponse
	payload := map[string]string{"name": name, "public_key": publicKey}
	if err := p.request(ctx, http.MethodPost, "/v2/account/keys", payload, &created); err != nil {
		return 0, err
	}
	if created.SSHKey.ID == 0 {
		return 0, fmt.Errorf("DigitalOcean SSH key create response did not include an id")
	}
	return created.SSHKey.ID, nil
}

func (p *digitalOceanProvider) machineFromDroplet(machineName string, droplet digitalOceanDroplet) remoteMachine {
	image := droplet.Image.Slug
	if image == "" {
		image = droplet.Image.Name
	}
	return remoteMachine{
		Name:              strings.ToLower(machineName),
		Provider:          remoteProviderDigitalOcean,
		ProviderID:        strconv.FormatInt(droplet.ID, 10),
		PublicIPv4:        publicIPv4FromDroplet(droplet),
		Region:            droplet.Region.Slug,
		Size:              droplet.SizeSlug,
		Image:             image,
		SSHUser:           firstNonEmpty(p.sshUser, "root"),
		CreatedAt:         droplet.CreatedAt,
		UpdatedAt:         time.Now().UTC(),
		BootstrapComplete: false,
	}
}

func (p *digitalOceanProvider) machineTags(machineName string) []string {
	tags := append([]string{}, p.tags...)
	tags = append(tags, digitalOceanMachineTag(machineName))
	return uniqueStrings(tags)
}

func (p *digitalOceanProvider) request(ctx context.Context, method string, endpoint string, body any, out any) error {
	var requestBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		requestBody = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.apiURL+endpoint, requestBody)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		var doErr digitalOceanErrorResponse
		if err := json.Unmarshal(data, &doErr); err == nil && doErr.Message != "" {
			if doErr.ID != "" {
				return fmt.Errorf("DigitalOcean %s %s failed: %s: %s", method, endpoint, doErr.ID, doErr.Message)
			}
			return fmt.Errorf("DigitalOcean %s %s failed: %s", method, endpoint, doErr.Message)
		}
		detail := strings.TrimSpace(string(data))
		if detail == "" {
			detail = resp.Status
		}
		return fmt.Errorf("DigitalOcean %s %s failed: %s", method, endpoint, detail)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func defaultRemotePublicKey() (string, error) {
	if value := strings.TrimSpace(os.Getenv(remoteSSHPublicKeyEnv)); value != "" {
		if data, err := os.ReadFile(value); err == nil {
			return strings.TrimSpace(string(data)), nil
		}
		return value, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	for _, name := range []string{"id_ed25519.pub", "id_rsa.pub", "id_ecdsa.pub"} {
		data, err := os.ReadFile(filepath.Join(home, ".ssh", name))
		if err == nil && strings.TrimSpace(string(data)) != "" {
			return strings.TrimSpace(string(data)), nil
		}
	}
	if key, err := defaultRemotePublicKeyFromAgent(); err == nil && key != "" {
		return key, nil
	}
	return "", fmt.Errorf("DigitalOcean provider needs an SSH key; set remote.digitalocean.ssh_keys or %s, forward an SSH agent with identities, or create ~/.ssh/id_ed25519.pub", remoteSSHPublicKeyEnv)
}

func defaultRemotePublicKeyFromAgent() (string, error) {
	if strings.TrimSpace(os.Getenv("SSH_AUTH_SOCK")) == "" {
		return "", nil
	}
	out, err := exec.Command("ssh-add", "-L").Output()
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "The agent has no identities") {
			continue
		}
		if strings.HasPrefix(line, "ssh-") || strings.HasPrefix(line, "ecdsa-") || strings.HasPrefix(line, "sk-") {
			return line, nil
		}
	}
	return "", nil
}

func digitalOceanMachineName(machineName string) string {
	return "yolobox-" + sanitizeRemoteResourceID(machineName)
}

func digitalOceanMachineTag(machineName string) string {
	return "yolobox-machine-" + sanitizeRemoteResourceID(machineName)
}

func digitalOceanSSHKeyValues(keys []string) []interface{} {
	values := make([]interface{}, 0, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if id, err := strconv.Atoi(key); err == nil {
			values = append(values, id)
			continue
		}
		values = append(values, key)
	}
	return values
}

func publicIPv4FromDroplet(droplet digitalOceanDroplet) string {
	for _, network := range droplet.Networks.V4 {
		if network.Type == "public" && network.IPAddress != "" {
			return network.IPAddress
		}
	}
	return ""
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func stringSliceContains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
