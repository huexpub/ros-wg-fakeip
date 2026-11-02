package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func main() {
	maxAge := 180 * time.Second
	if value := os.Getenv("WG_CHECK_MAX_AGE"); value != "" {
		seconds, err := strconv.Atoi(value)
		if err != nil || seconds < 1 {
			fail("WG_CHECK_MAX_AGE must be a positive number of seconds")
		}
		maxAge = time.Duration(seconds) * time.Second
	}

	args := []string{"show", "all", "dump"}
	if len(os.Args) > 1 {
		args = []string{"show", os.Args[1], "dump"}
	}
	output, err := exec.Command("wg", args...).Output()
	if err != nil {
		fail(fmt.Sprintf("wg command failed: %v", err))
	}

	interfaces, peers := 0, 0
	fresh := false
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), "\t")
		if len(fields) < 5 {
			continue
		}
		if len(fields) == 4 {
			interfaces++
			fmt.Printf("interface %s: listen-port=%s\n", shortKey(fields[1]), fields[2])
			continue
		}
		if len(fields) < 9 {
			continue
		}
		peers++
		handshake, err := strconv.ParseInt(fields[5], 10, 64)
		if err != nil {
			fail("invalid handshake timestamp returned by wg")
		}
		age := "never"
		if handshake > 0 {
			elapsed := time.Since(time.Unix(handshake, 0))
			age = elapsed.Round(time.Second).String()
			if elapsed >= 0 && elapsed <= maxAge {
				fresh = true
			}
		}
		fmt.Printf("peer %s: handshake=%s rx=%s tx=%s\n", shortKey(fields[0]), age, fields[6], fields[7])
	}
	if err := scanner.Err(); err != nil {
		fail(err.Error())
	}
	if interfaces == 0 {
		fail("no WireGuard interface found")
	}
	if peers == 0 {
		fail("no WireGuard peer found")
	}
	if !fresh {
		fail(fmt.Sprintf("no peer handshake in the last %s", maxAge))
	}
	fmt.Printf("WireGuard link OK: %d interface(s), %d peer(s)\n", interfaces, peers)
}

func shortKey(value string) string {
	if len(value) <= 12 {
		return value
	}
	return value[:12] + "..."
}

func fail(message string) {
	fmt.Fprintln(os.Stderr, "WireGuard check failed:", message)
	os.Exit(1)
}
