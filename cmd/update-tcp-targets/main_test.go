package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestParseTCPBenchTargets(t *testing.T) {
	source := []byte("NAMES=(\"One\" \"Two/X\")\nHOSTS=(\"one.test\" \"two.test\")\n")
	targets, err := parseTCPBenchTargets(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 || targets[1].Name != "Two/X" || targets[1].Host != "two.test" || targets[1].Port != 443 {
		t.Fatalf("unexpected targets: %+v", targets)
	}
}

func TestUpdateSnapshotRedactsSourceURLFromFetchErrors(t *testing.T) {
	source := "https://private.example.invalid/targets?token=do-not-print"
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial " + source)
	})}
	err := updateSnapshot(context.Background(), client, updateConfig{
		Source: source, Output: t.TempDir() + "/targets.json", Manifest: t.TempDir() + "/manifest.json",
		Minimum: 1, Timeout: time.Second,
	})
	if err == nil {
		t.Fatal("fetch failure unexpectedly succeeded")
	}
	if strings.Contains(err.Error(), source) || strings.Contains(err.Error(), "do-not-print") {
		t.Fatalf("fetch error exposed source URL: %q", err)
	}
}

func TestParseTCPBenchTargetsRejectsDrift(t *testing.T) {
	if _, err := parseTCPBenchTargets([]byte("NAMES=(\"One\")\nHOSTS=(\"one.test\" \"two.test\")\n")); err == nil {
		t.Fatal("mismatched arrays unexpectedly accepted")
	}
}
