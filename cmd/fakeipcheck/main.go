package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/miekg/dns"
)

func main() {
	server := flag.String("server", "127.0.0.1:53", "DNS server address")
	name := flag.String("name", "example.com", "domain to query")
	cidr := flag.String("cidr", "198.18.0.0/16", "expected Fake-IP CIDR")
	protocol := flag.String("protocol", "udp", "DNS protocol: udp or tcp")
	timeout := flag.Duration("timeout", 3*time.Second, "query timeout")
	flag.Parse()

	_, network, err := net.ParseCIDR(*cidr)
	if err != nil || network.IP.To4() == nil {
		fail("invalid -cidr: expected an IPv4 CIDR")
	}
	if *protocol != "udp" && *protocol != "tcp" {
		fail("invalid -protocol: use udp or tcp")
	}

	message := new(dns.Msg)
	message.SetQuestion(dns.Fqdn(*name), dns.TypeA)
	client := &dns.Client{Net: *protocol, Timeout: *timeout}
	response, _, err := client.Exchange(message, *server)
	if err != nil {
		fail(fmt.Sprintf("DNS query failed: %v", err))
	}
	if response.Rcode != dns.RcodeSuccess {
		fail(fmt.Sprintf("DNS response code: %s", dns.RcodeToString[response.Rcode]))
	}

	for _, answer := range response.Answer {
		a, ok := answer.(*dns.A)
		if ok && network.Contains(a.A) {
			fmt.Printf("Fake-IP OK: %s -> %s (server=%s protocol=%s)\n", message.Question[0].Name, a.A, *server, *protocol)
			return
		}
	}
	fail(fmt.Sprintf("no A record in %s", network.String()))
}

func fail(message string) {
	fmt.Fprintln(os.Stderr, "Fake-IP check failed:", message)
	os.Exit(1)
}
