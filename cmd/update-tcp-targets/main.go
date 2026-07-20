package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/oneclickvirt/pingtest/model"
)

const defaultSourceURL = "https://raw.githubusercontent.com/se-tang/TCPbench/main/backend/scripts/run.sh"

var shellStringPattern = regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)

type updateConfig struct {
	Source   string
	Output   string
	Manifest string
	Minimum  int
	Timeout  time.Duration
}

func main() {
	config := updateConfig{}
	flag.StringVar(&config.Source, "source", defaultSourceURL, "upstream TCPbench target source")
	flag.StringVar(&config.Output, "output", "model/snapshot/tcp-targets.json", "snapshot output path")
	flag.StringVar(&config.Manifest, "manifest", "model/snapshot/manifest.json", "snapshot manifest output path")
	flag.IntVar(&config.Minimum, "minimum", 50, "minimum valid targets")
	flag.DurationVar(&config.Timeout, "timeout", 30*time.Second, "upstream request timeout")
	flag.Parse()
	if err := updateSnapshot(context.Background(), http.DefaultClient, config); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func updateSnapshot(ctx context.Context, client *http.Client, config updateConfig) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if client == nil {
		client = http.DefaultClient
	}
	if config.Source == "" || config.Output == "" || config.Minimum < 1 || config.Timeout <= 0 {
		return errors.New("source, output, minimum, and timeout must be valid")
	}
	requestCtx, cancel := context.WithTimeout(ctx, config.Timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, config.Source, nil)
	if err != nil {
		return fmt.Errorf("create target request: %w", err)
	}
	request.Header.Set("Accept", "text/plain")
	request.Header.Set("User-Agent", "oneclickvirt-pingtest-target-sync/1")
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("fetch targets: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch targets: HTTP %d", response.StatusCode)
	}
	source, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return fmt.Errorf("read targets: %w", err)
	}
	targets, err := parseTCPBenchTargets(source)
	if err != nil {
		return fmt.Errorf("parse targets: %w", err)
	}
	raw, err := json.Marshal(targets)
	if err != nil {
		return err
	}
	candidate, err := model.NormalizeTCPTargetRegistrySnapshot(raw, config.Minimum)
	if err != nil {
		return fmt.Errorf("validate targets: %w", err)
	}
	return replaceSnapshot(config.Output, config.Manifest, candidate)
}

func parseTCPBenchTargets(source []byte) ([]model.TCPTarget, error) {
	names, err := parseShellArray(source, "NAMES")
	if err != nil {
		return nil, err
	}
	hosts, err := parseShellArray(source, "HOSTS")
	if err != nil {
		return nil, err
	}
	if len(names) != len(hosts) || len(names) == 0 {
		return nil, fmt.Errorf("NAMES/HOSTS length mismatch: %d/%d", len(names), len(hosts))
	}
	targets := make([]model.TCPTarget, len(names))
	for index := range names {
		targets[index] = model.TCPTarget{Name: names[index], Host: hosts[index], Port: 443, Category: "global", Source: "tcpbench"}
	}
	return targets, nil
}

func parseShellArray(source []byte, name string) ([]string, error) {
	pattern := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + `=\(([^\n]*)\)$`)
	match := pattern.FindSubmatch(source)
	if len(match) != 2 {
		return nil, fmt.Errorf("%s array not found", name)
	}
	quoted := shellStringPattern.FindAll(match[1], -1)
	values := make([]string, 0, len(quoted))
	for _, value := range quoted {
		decoded, err := strconv.Unquote(string(value))
		if err != nil {
			return nil, fmt.Errorf("decode %s value: %w", name, err)
		}
		values = append(values, decoded)
	}
	return values, nil
}

type snapshotManifest struct {
	Schema      string `json:"schema"`
	File        string `json:"file"`
	Count       int    `json:"count"`
	SHA256      string `json:"sha256"`
	GeneratedAt string `json:"generated_at"`
}

func replaceSnapshot(output, manifestOutput string, candidate []byte) error {
	count, err := snapshotCount(candidate)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(candidate)
	manifest := snapshotManifest{
		Schema: model.TCPTargetRegistrySchema, File: filepath.Base(output), Count: count,
		SHA256: hex.EncodeToString(hash[:]), GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	manifestData = append(manifestData, '\n')
	current, readErr := os.ReadFile(output)
	if readErr == nil {
		normalized, normalizeErr := model.NormalizeTCPTargetRegistrySnapshot(current, 1)
		if normalizeErr == nil {
			currentCount, currentErr := snapshotCount(normalized)
			candidateCount, candidateErr := snapshotCount(candidate)
			if currentErr == nil && candidateErr == nil && currentCount > 0 && candidateCount*100 < currentCount*65 {
				return fmt.Errorf("target count dropped from %d to %d", currentCount, candidateCount)
			}
			if bytes.Equal(normalized, candidate) && manifestMatches(manifestOutput, candidate, count) {
				return nil
			}
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return fmt.Errorf("read existing snapshot: %w", readErr)
	}
	if err := writeAtomicSnapshot(output, candidate); err != nil {
		return err
	}
	if err := writeAtomicSnapshot(manifestOutput, manifestData); err != nil {
		return err
	}
	return nil
}

func manifestMatches(path string, snapshot []byte, count int) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var manifest snapshotManifest
	if json.Unmarshal(data, &manifest) != nil || manifest.Schema != model.TCPTargetRegistrySchema || manifest.File != "tcp-targets.json" || manifest.Count != count {
		return false
	}
	hash := sha256.Sum256(snapshot)
	return manifest.SHA256 == hex.EncodeToString(hash[:])
}

func writeAtomicSnapshot(output string, candidate []byte) error {
	if output == "" {
		return errors.New("snapshot path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return fmt.Errorf("create snapshot directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(output), ".tcp-targets-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(candidate); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, output)
}

func snapshotCount(data []byte) (int, error) {
	var records []json.RawMessage
	if err := json.Unmarshal(data, &records); err != nil {
		return 0, err
	}
	return len(records), nil
}
