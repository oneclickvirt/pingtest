package main

import "testing"

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

func TestParseTCPBenchTargetsRejectsDrift(t *testing.T) {
	if _, err := parseTCPBenchTargets([]byte("NAMES=(\"One\")\nHOSTS=(\"one.test\" \"two.test\")\n")); err == nil {
		t.Fatal("mismatched arrays unexpectedly accepted")
	}
}
