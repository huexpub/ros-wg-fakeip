FROM golang:1.23-alpine AS gateway-build
WORKDIR /src
COPY go.mod ./
COPY main.go ./
COPY transparent_linux.go ./
COPY transparent_other.go ./
COPY cmd ./cmd
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/gateway .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/wgcheck ./cmd/wgcheck
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/fakeipcheck ./cmd/fakeipcheck

FROM alpine:3.22
RUN apk add --no-cache ca-certificates wireguard-tools iproute2 iptables ip6tables iptables-legacy wireguard-go && \
    mkdir -p /etc/wireguard
COPY --from=gateway-build /out/gateway /usr/local/bin/gateway
COPY --from=gateway-build /out/wgcheck /usr/local/bin/wgcheck
COPY --from=gateway-build /out/fakeipcheck /usr/local/bin/fakeipcheck
COPY docker/entrypoint.sh /usr/local/bin/entrypoint
RUN chmod +x /usr/local/bin/entrypoint
ENV DNS_ADDR=:53 FAKEIP_CIDR=198.18.0.0/16 UPSTREAM_DNS=1.1.1.1:53 HTTP_ADDR=:8080 TCP_REDIRECT_ENABLED=true
EXPOSE 51820/udp 53/udp 53/tcp 8080/tcp
ENTRYPOINT ["/usr/local/bin/entrypoint"]
