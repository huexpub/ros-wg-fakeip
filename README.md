# ros-wg-fakeip

精简 Alpine Docker 镜像：WireGuard + Go Fake-IP DNS/TCP 转发，无 Web UI。

## 已实现

- WireGuard `wg-quick` 配置和启动。
- IPv4 A 查询分配 Fake-IP，并保存内存映射。
- DNS UDP/TCP 服务。
- IPv4 TCP 透明转发：WireGuard 入站的 Fake-IP TCP 流量通过 iptables REDIRECT，按 DNS 映射连接真实域名。
- `wgcheck`：检查 WireGuard Peer 握手。
- `fakeipcheck`：验证 DNS 是否返回指定 Fake-IP 网段。
- `/healthz`、`/mapping` 诊断接口。
- GitHub Actions 构建并发布 GHCR 镜像：`ghcr.io/huexpub/ros-wg-fakeip:latest`。

## 未实现

- TUN 用户态 TCP/IP 数据面。
- UDP/QUIC 转发。
- IPv6 Fake-IP。
- 映射持久化、TTL 回收和地址复用管理。
- Web 管理界面。

## 最小配置

复制并填写 WireGuard 配置到 VPS：

```sh
  ros-wg-fakeip:
    container_name: ros-wg-fakeip
    image: ghcr.io/huexpub/ros-wg-fakeip:latest
    restart: unless-stopped
    ports:
      - "51820:51820/udp"
      - "51821:51821/tcp"
    volumes:
      - /usr/share/zoneinfo/Asia/Shanghai:/etc/localtime:ro
      - /opt/wireguard:/etc/wireguard
      - /lib/modules:/lib/modules:ro
    cap_add:
      - NET_ADMIN
      - SYS_MODULE
    sysctls:
      - net.ipv4.ip_forward=1
      - net.ipv4.conf.all.src_valid_mark=1
      - net.ipv6.conf.all.disable_ipv6=0
      - net.ipv6.conf.all.forwarding=1
      - net.ipv6.conf.default.forwarding=1
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"
```

- - /opt/wireguard 为wireguard配置项 wg0.conf