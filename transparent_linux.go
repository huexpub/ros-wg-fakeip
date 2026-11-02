//go:build linux

package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const soOriginalDst = 80

type tcpRedirectConfig struct {
	CIDR      string
	Interface string
	Port      int
}

func originalDestination(conn *net.TCPConn) (net.IP, int, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return nil, 0, err
	}
	var ip net.IP
	var port int
	var socketErr error
	err = raw.Control(func(fd uintptr) {
		var address [16]byte
		length := uint32(len(address))
		_, _, errno := syscall.Syscall6(syscall.SYS_GETSOCKOPT, fd, syscall.SOL_IP, soOriginalDst, uintptr(unsafe.Pointer(&address[0])), uintptr(unsafe.Pointer(&length)), 0)
		if errno != 0 {
			socketErr = errno
			return
		}
		if length < 8 || binary.LittleEndian.Uint16(address[0:2]) != syscall.AF_INET {
			socketErr = errors.New("original destination is not IPv4")
			return
		}
		port = int(binary.BigEndian.Uint16(address[2:4]))
		ip = net.IPv4(address[4], address[5], address[6], address[7])
	})
	if err != nil {
		return nil, 0, err
	}
	if socketErr != nil {
		return nil, 0, socketErr
	}
	return ip, port, nil
}

func installRedirectRule(config tcpRedirectConfig) (bool, error) {
	args := redirectArgs(config)
	check := exec.Command("iptables", append([]string{"-t", "nat", "-C"}, args...)...).Run()
	if check == nil {
		return false, nil
	}
	if err := exec.Command("iptables", append([]string{"-t", "nat", "-A"}, args...)...).Run(); err != nil {
		return false, fmt.Errorf("install Fake-IP TCP redirect rule: %w", err)
	}
	return true, nil
}

func removeRedirectRule(config tcpRedirectConfig) {
	args := append([]string{"-t", "nat", "-D"}, redirectArgs(config)...)
	if err := exec.Command("iptables", args...).Run(); err != nil {
		log.Printf("remove Fake-IP TCP redirect rule: %v", err)
	}
}

func redirectArgs(config tcpRedirectConfig) []string {
	return []string{"PREROUTING", "-i", config.Interface, "-d", config.CIDR, "-p", "tcp", "-j", "REDIRECT", "--to-ports", strconv.Itoa(config.Port)}
}

func serveTCPRedirect(ctx context.Context, dnsService *fakeDNS, config tcpRedirectConfig) error {
	ip, network, err := net.ParseCIDR(config.CIDR)
	if err != nil || ip.To4() == nil {
		return errors.New("redirect CIDR must be a valid IPv4 CIDR")
	}
	config.CIDR = network.String()
	if config.Interface == "" || config.Port < 1 || config.Port > 65535 {
		return errors.New("redirect interface and valid port are required")
	}
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{Port: config.Port})
	if err != nil {
		return fmt.Errorf("listen on TCP redirect port: %w", err)
	}
	defer listener.Close()
	installed, err := installRedirectRule(config)
	if err != nil {
		return err
	}
	if installed {
		defer removeRedirectRule(config)
	}
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	var active sync.WaitGroup
	defer active.Wait()
	for {
		conn, err := listener.AcceptTCP()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accept redirected TCP: %w", err)
		}
		active.Add(1)
		go func(client *net.TCPConn) {
			defer active.Done()
			defer client.Close()
			_ = client.SetDeadline(time.Now().Add(15 * time.Second))
			fakeIP, port, err := originalDestination(client)
			if err != nil {
				log.Printf("read original TCP destination: %v", err)
				return
			}
			hostname, ok := dnsService.hostnameForIP(fakeIP)
			if !ok {
				log.Printf("no DNS mapping for Fake-IP %s", fakeIP)
				return
			}
			_ = client.SetDeadline(time.Time{})
			upstream, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp4", net.JoinHostPort(stringsTrimDot(hostname), strconv.Itoa(port)))
			if err != nil {
				log.Printf("connect mapped host %s:%d: %v", hostname, port, err)
				return
			}
			defer upstream.Close()
			copyBidirectional(client, upstream)
		}(conn)
	}
}

func copyBidirectional(client *net.TCPConn, upstream net.Conn) {
	finished := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(upstream, client)
		if tcp, ok := upstream.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
		finished <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(client, upstream)
		_ = client.CloseWrite()
		finished <- struct{}{}
	}()
	<-finished
	_ = client.Close()
	_ = upstream.Close()
	<-finished
}

func stringsTrimDot(host string) string {
	for len(host) > 0 && host[len(host)-1] == '.' {
		host = host[:len(host)-1]
	}
	return host
}

func tcpRedirectConfigFromEnv(cidr string) (tcpRedirectConfig, error) {
	port := 15001
	if value := os.Getenv("TCP_REDIRECT_PORT"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return tcpRedirectConfig{}, err
		}
		port = parsed
	}
	interfaceName := os.Getenv("WG_INTERFACE")
	if interfaceName == "" {
		interfaceName = "wg0"
	}
	return tcpRedirectConfig{CIDR: cidr, Interface: interfaceName, Port: port}, nil
}
