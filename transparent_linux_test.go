//go:build linux

package main

import (
	"reflect"
	"testing"
)

func TestRedirectArgsRestrictsInterfaceCIDRAndTCP(t *testing.T) {
	got := redirectArgs(tcpRedirectConfig{CIDR: "198.18.0.0/16", Interface: "wg0", Port: 15001})
	want := []string{"PREROUTING", "-i", "wg0", "-d", "198.18.0.0/16", "-p", "tcp", "-j", "REDIRECT", "--to-ports", "15001"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("redirectArgs() = %v, want %v", got, want)
	}
}

func TestTCPRedirectConfigDefaults(t *testing.T) {
	t.Setenv("WG_INTERFACE", "")
	t.Setenv("TCP_REDIRECT_PORT", "")
	got, err := tcpRedirectConfigFromEnv("198.18.0.0/16")
	if err != nil {
		t.Fatal(err)
	}
	if got.Interface != "wg0" || got.Port != 15001 || got.CIDR != "198.18.0.0/16" {
		t.Fatalf("unexpected config: %+v", got)
	}
}
