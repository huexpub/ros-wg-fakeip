package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/miekg/dns"
)

type fakeDNS struct {
	pool     []net.IP
	byIP     map[string]string
	byName   map[string]net.IP
	mu       sync.RWMutex
	upstream string
}

func newFakeDNS(cidr, upstream string) (*fakeDNS, error) {
	ip, network, err := net.ParseCIDR(cidr)
	if err != nil || ip.To4() == nil {
		return nil, errors.New("FAKEIP_CIDR must be a valid IPv4 CIDR")
	}
	maskSize, bits := network.Mask.Size()
	if bits != 32 || bits-maskSize > 16 {
		return nil, errors.New("FAKEIP_CIDR must contain at most 65536 IPv4 addresses")
	}
	base := network.IP.To4()
	baseValue := uint32(base[0])<<24 | uint32(base[1])<<16 | uint32(base[2])<<8 | uint32(base[3])
	pool := make([]net.IP, 0, 1<<uint(bits-maskSize))
	for i := uint32(1); i < (1<<uint(bits-maskSize))-1; i++ {
		value := baseValue + i
		pool = append(pool, net.IPv4(byte(value>>24), byte(value>>16), byte(value>>8), byte(value)))
	}
	return &fakeDNS{pool: pool, byIP: make(map[string]string), byName: make(map[string]net.IP), upstream: upstream}, nil
}

func (f *fakeDNS) hostnameForIP(ip net.IP) (string, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	name, ok := f.byIP[ip.To4().String()]
	return name, ok
}

func (f *fakeDNS) ServeDNS(w dns.ResponseWriter, req *dns.Msg) {
	resp := new(dns.Msg)
	resp.SetReply(req)
	resp.Authoritative = true
	for _, q := range req.Question {
		if q.Qtype == dns.TypeA {
			name := strings.ToLower(dns.Fqdn(q.Name))
			f.mu.Lock()
			ip := f.byName[name]
			if ip == nil {
				for _, candidate := range f.pool {
					if f.byIP[candidate.String()] == "" {
						ip = candidate
						f.byName[name], f.byIP[ip.String()] = ip, name
						break
					}
				}
			}
			f.mu.Unlock()
			if ip == nil {
				resp.Rcode = dns.RcodeServerFailure
				break
			}
			resp.Answer = append(resp.Answer, &dns.A{Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 600}, A: ip})
		} else if q.Qtype == dns.TypeAAAA {
			continue
		} else {
			upstreamReq := new(dns.Msg)
			upstreamReq.SetQuestion(q.Name, q.Qtype)
			upstreamResp, err := dns.Exchange(upstreamReq, f.upstream)
			if err != nil {
				resp.Rcode = dns.RcodeServerFailure
				break
			}
			resp.Answer = append(resp.Answer, upstreamResp.Answer...)
		}
	}
	if err := w.WriteMsg(resp); err != nil {
		log.Printf("write DNS response: %v", err)
	}
}

func main() {
	cidr := os.Getenv("FAKEIP_CIDR")
	if cidr == "" {
		cidr = "198.18.0.0/16"
	}
	upstream := os.Getenv("UPSTREAM_DNS")
	if upstream == "" {
		upstream = "1.1.1.1:53"
	}
	addr := os.Getenv("DNS_ADDR")
	if addr == "" {
		addr = ":53"
	}
	f, err := newFakeDNS(cidr, upstream)
	if err != nil {
		log.Fatal(err)
	}

	udp := &dns.Server{Addr: addr, Net: "udp", Handler: f}
	tcp := &dns.Server{Addr: addr, Net: "tcp", Handler: f}
	go func() {
		if err := udp.ListenAndServe(); err != nil {
			log.Fatalf("DNS UDP: %v", err)
		}
	}()
	go func() {
		if err := tcp.ListenAndServe(); err != nil {
			log.Fatalf("DNS TCP: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if strings.EqualFold(os.Getenv("TCP_REDIRECT_ENABLED"), "true") {
		config, err := tcpRedirectConfigFromEnv(cidr)
		if err != nil {
			log.Fatal(err)
		}
		go func() {
			if err := serveTCPRedirect(ctx, f, config); err != nil {
				log.Fatalf("TCP redirect: %v", err)
			}
		}()
	}

	httpAddr := os.Getenv("HTTP_ADDR")
	if httpAddr == "" {
		httpAddr = "127.0.0.1:8080"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /mapping", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.RLock()
		defer f.mu.RUnlock()
		for ip, name := range f.byIP {
			_, _ = w.Write([]byte(ip + " " + name + "\n"))
		}
	})
	server := &http.Server{Addr: httpAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		_ = udp.Shutdown()
		_ = tcp.Shutdown()
	}()
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP: %v", err)
		}
	}()
	log.Printf("Fake-IP DNS listening on %s for %s", addr, cidr)
	<-ctx.Done()
}
