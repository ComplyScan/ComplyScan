package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/profiledraft"
)

func TestFilterCasesKeepsManifestOrder(t *testing.T) {
	cases := []profiledraft.BenchmarkCase{{ID: "first"}, {ID: "second"}, {ID: "third"}}
	filtered, err := filterCases(cases, []string{"third", "first"})
	if err != nil {
		t.Fatal(err)
	}
	want := []profiledraft.BenchmarkCase{{ID: "first"}, {ID: "third"}}
	if !reflect.DeepEqual(filtered, want) {
		t.Fatalf("filtered cases = %#v, want %#v", filtered, want)
	}
}

func TestFilterCasesRejectsUnknownID(t *testing.T) {
	_, err := filterCases([]profiledraft.BenchmarkCase{{ID: "known"}}, []string{"missing"})
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("expected unknown case error, got %v", err)
	}
}
