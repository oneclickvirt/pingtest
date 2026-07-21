package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLoadTCPTargetRegistryFallsBackAcrossSources(t *testing.T) {
	valid := `[{"name":"Fixture","host":"fixture.test","port":443,"source":"fixture"}]`
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/bad" {
			http.Error(writer, "bad", http.StatusBadGateway)
			return
		}
		_, _ = writer.Write([]byte(valid))
	}))
	defer server.Close()

	loaded, err := LoadTCPTargetRegistry(context.Background(), server.Client(), []TCPTargetRegistrySource{
		{Name: "cdn", URL: server.URL + "/bad"},
		{Name: "raw", URL: server.URL + "/good"},
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Source != "raw" || !loaded.Fallback || len(loaded.Targets) != 1 {
		t.Fatalf("unexpected load result: %+v", loaded)
	}
}

func TestLoadTCPTargetRegistryRejectsBadManifestAndUsesNextSource(t *testing.T) {
	valid := []byte(`[{"name":"Fixture","host":"fixture.test","port":443,"source":"fixture"}]`)
	hash := sha256.Sum256(valid)
	manifest := TCPTargetManifest{Schema: TCPTargetRegistrySchema, File: "tcp-targets.json", Count: 1, SHA256: hex.EncodeToString(hash[:]), GeneratedAt: time.Now().UTC().Format(time.RFC3339)}
	manifestData, _ := json.Marshal(manifest)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/cdn-manifest":
			bad := manifest
			bad.SHA256 = strings.Repeat("0", 64)
			_ = json.NewEncoder(writer).Encode(bad)
		case "/raw-manifest":
			_, _ = writer.Write(manifestData)
		default:
			_, _ = writer.Write(valid)
		}
	}))
	defer server.Close()
	loaded, err := LoadTCPTargetRegistry(context.Background(), server.Client(), []TCPTargetRegistrySource{
		{Name: "cdn", URL: server.URL + "/cdn-data", ManifestURL: server.URL + "/cdn-manifest"},
		{Name: "raw", URL: server.URL + "/raw-data", ManifestURL: server.URL + "/raw-manifest"},
	}, 1)
	if err != nil || loaded.Source != "raw" || !loaded.Fallback {
		t.Fatalf("unexpected manifest fallback: %+v, %v", loaded, err)
	}
	if loaded.Metadata.Schema != TCPTargetRegistrySchema || loaded.Metadata.Count != 1 || loaded.Metadata.SHA256 != manifest.SHA256 {
		t.Fatalf("manifest metadata missing: %+v", loaded.Metadata)
	}
}

func TestLoadTCPTargetRegistryUsesEmbeddedFallback(t *testing.T) {
	loaded, err := LoadTCPTargetRegistry(context.Background(), nil, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Source != "embedded" || !loaded.Fallback || len(loaded.Targets) < 10 {
		t.Fatalf("unexpected embedded result: %+v", loaded)
	}
	if loaded.Metadata.Schema != TCPTargetRegistrySchema || loaded.Metadata.Count < 10 || loaded.Metadata.Count > len(loaded.Targets) || loaded.Metadata.GeneratedAt == "" || len(loaded.Metadata.SHA256) != 64 {
		t.Fatalf("unexpected embedded metadata: %+v", loaded.Metadata)
	}
}

func TestNormalizeTCPTargetRegistryRejectsFieldDriftAndSmallData(t *testing.T) {
	if _, err := NormalizeTCPTargetRegistrySnapshot([]byte(`[{"name":"x","host":"x.test","unknown":true}]`), 1); err == nil {
		t.Fatal("field drift unexpectedly accepted")
	}
	if _, err := NormalizeTCPTargetRegistrySnapshot([]byte(`[]`), 1); err == nil {
		t.Fatal("empty registry unexpectedly accepted")
	}
}
