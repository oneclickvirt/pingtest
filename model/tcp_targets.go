package model

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed snapshot/tcp-targets.json
var embeddedTCPTargets []byte

const (
	TCPTargetRegistryRawURL = "https://raw.githubusercontent.com/oneclickvirt/pingtest/main/model/snapshot/tcp-targets.json"
	TCPTargetRegistryCDNURL = "https://cdn.spiritlhl.net/" + TCPTargetRegistryRawURL
)

// TCPTarget describes an endpoint used by the TCP handshake probe.
type TCPTarget struct {
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Category string `json:"category,omitempty"`
	Source   string `json:"source,omitempty"`
}

type TCPTargetRegistrySource struct {
	Name string
	URL  string
}

type TCPTargetRegistryLoadResult struct {
	Targets  []TCPTarget
	Source   string
	Fallback bool
}

func DefaultTCPTargetRegistrySources() []TCPTargetRegistrySource {
	return []TCPTargetRegistrySource{
		{Name: "cdn", URL: TCPTargetRegistryCDNURL},
		{Name: "raw", URL: TCPTargetRegistryRawURL},
	}
}

// AllTCPTargets returns a copy of the registry formed from the existing
// website list and TCPbench endpoints. Existing website entries take
// precedence when their normalized host and port are already present.
func AllTCPTargets() []TCPTarget {
	targets, _ := decodeTCPTargetRegistry(embeddedTCPTargets, 1)
	return mergeTCPTargets(targets)
}

func LoadMergedTCPTargets(ctx context.Context, client *http.Client, sources []TCPTargetRegistrySource, minimum int) (TCPTargetRegistryLoadResult, error) {
	loaded, err := LoadTCPTargetRegistry(ctx, client, sources, minimum)
	if err != nil {
		return TCPTargetRegistryLoadResult{}, err
	}
	loaded.Targets = mergeTCPTargets(loaded.Targets)
	return loaded, nil
}

func mergeTCPTargets(additional []TCPTarget) []TCPTarget {
	result := make([]TCPTarget, 0, len(PopularWebsites)+len(additional))
	seen := make(map[string]int, cap(result))

	for _, target := range WebsiteTCPTargets() {
		key := targetKey(target)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = len(result)
		result = append(result, target)
	}
	for _, target := range additional {
		key := targetKey(target)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = len(result)
		result = append(result, target)
	}
	return result
}

func NormalizeTCPTargetRegistrySnapshot(data []byte, minimum int) ([]byte, error) {
	targets, err := decodeTCPTargetRegistry(data, minimum)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(targets, func(i, j int) bool {
		if targets[i].Category != targets[j].Category {
			return targets[i].Category < targets[j].Category
		}
		if targets[i].Name != targets[j].Name {
			return targets[i].Name < targets[j].Name
		}
		if targets[i].Host != targets[j].Host {
			return targets[i].Host < targets[j].Host
		}
		return targets[i].Port < targets[j].Port
	})
	encoded, err := json.MarshalIndent(targets, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func LoadTCPTargetRegistry(ctx context.Context, client *http.Client, sources []TCPTargetRegistrySource, minimum int) (TCPTargetRegistryLoadResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}
	if minimum < 1 {
		minimum = 1
	}
	var lastErr error
	for index, source := range sources {
		data, err := fetchTCPTargetRegistry(ctx, client, source.URL)
		if err != nil {
			lastErr = fmt.Errorf("load %s TCP target registry: %w", source.Name, err)
			continue
		}
		targets, err := decodeTCPTargetRegistry(data, minimum)
		if err != nil {
			lastErr = fmt.Errorf("validate %s TCP target registry: %w", source.Name, err)
			continue
		}
		return TCPTargetRegistryLoadResult{Targets: targets, Source: source.Name, Fallback: index > 0}, nil
	}
	targets, err := decodeTCPTargetRegistry(embeddedTCPTargets, minimum)
	if err == nil {
		return TCPTargetRegistryLoadResult{Targets: targets, Source: "embedded", Fallback: true}, nil
	}
	if lastErr == nil {
		lastErr = errors.New("no TCP target registry sources configured")
	}
	return TCPTargetRegistryLoadResult{}, fmt.Errorf("%w; embedded fallback: %v", lastErr, err)
}

func fetchTCPTargetRegistry(ctx context.Context, client *http.Client, endpoint string) ([]byte, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return nil, errors.New("invalid registry URL")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "oneclickvirt-pingtest/tcp-target-registry-v1")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	return io.ReadAll(io.LimitReader(response.Body, 2<<20))
}

func decodeTCPTargetRegistry(data []byte, minimum int) ([]TCPTarget, error) {
	var input []TCPTarget
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return nil, err
	}
	if err := ensureTCPTargetJSONEOF(decoder); err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(input))
	targets := make([]TCPTarget, 0, len(input))
	for _, target := range input {
		target.Name = strings.TrimSpace(target.Name)
		target.Host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(target.Host), "."))
		target.Category = strings.TrimSpace(target.Category)
		target.Source = strings.TrimSpace(target.Source)
		if target.Port == 0 {
			target.Port = 443
		}
		if target.Name == "" || !validTCPTargetHost(target.Host) || target.Port < 1 || target.Port > 65535 {
			continue
		}
		key := targetKey(target)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		targets = append(targets, target)
	}
	if len(targets) < minimum {
		return nil, fmt.Errorf("TCP target registry has %d valid targets; require at least %d", len(targets), minimum)
	}
	return targets, nil
}

func validTCPTargetHost(host string) bool {
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
		return true
	}
	if host == "" || len(host) > 253 || strings.ContainsAny(host, " /\\:") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if character == '-' || character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
				continue
			}
			return false
		}
	}
	return true
}

func ensureTCPTargetJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("TCP target registry contains trailing JSON")
		}
		return err
	}
	return nil
}

// WebsiteTCPTargets returns a normalized copy of the existing popular website
// registry for context-aware TCP latency probes.
func WebsiteTCPTargets() []TCPTarget {
	result := make([]TCPTarget, 0, len(PopularWebsites))
	seen := make(map[string]struct{}, len(PopularWebsites))
	for _, website := range PopularWebsites {
		target, ok := websiteTarget(website)
		if !ok {
			continue
		}
		target.Source = "popular-websites"
		key := targetKey(target)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, target)
	}
	return result
}

func websiteTarget(website Website) (TCPTarget, bool) {
	parsed, err := url.Parse(strings.TrimSpace(website.URL))
	if err != nil || parsed.Hostname() == "" {
		return TCPTarget{}, false
	}
	port := 443
	if parsed.Port() != "" {
		parsedPort, err := strconv.Atoi(parsed.Port())
		if err != nil || parsedPort < 1 || parsedPort > 65535 {
			return TCPTarget{}, false
		}
		port = parsedPort
	}
	return TCPTarget{
		Name:     website.Name,
		Host:     strings.ToLower(strings.TrimSuffix(parsed.Hostname(), ".")),
		Port:     port,
		Category: website.Category,
	}, true
}

func targetKey(target TCPTarget) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(target.Host), ".")) + ":" + strconv.Itoa(target.Port)
}
