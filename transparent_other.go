//go:build !linux

package main

import (
	"context"
	"errors"
)

type tcpRedirectConfig struct {
	CIDR      string
	Interface string
	Port      int
}

func tcpRedirectConfigFromEnv(string) (tcpRedirectConfig, error) {
	return tcpRedirectConfig{}, errors.New("TCP redirect is supported only on Linux")
}

func serveTCPRedirect(context.Context, *fakeDNS, tcpRedirectConfig) error {
	return errors.New("TCP redirect is supported only on Linux")
}
