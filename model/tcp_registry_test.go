package model

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
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

func TestLoadTCPTargetRegistryUsesEmbeddedFallback(t *testing.T) {
	loaded, err := LoadTCPTargetRegistry(context.Background(), nil, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Source != "embedded" || !loaded.Fallback || len(loaded.Targets) < 10 {
		t.Fatalf("unexpected embedded result: %+v", loaded)
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
