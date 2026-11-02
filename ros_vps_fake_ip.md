# ROS + VPS WireGuard Fake-IP 网关方案

本文档描述当前首版架构：RouterOS 通过 WireGuard 隧道访问 VPS，单个精简 Alpine 镜像内运行 WireGuard 工具和 Go Fake-IP DNS。当前版本完成 DNS 映射、诊断接口，以及可选的 IPv4 TCP 透明转发；UDP/QUIC 和 TUN 用户态数据面尚未实现。

---

## 1. 架构设计与流量拓扑

整个系统的核心思想是将 **网络组网**、**DNS 接管** 与 **透明转发** 进行分层处理。当前自定义镜像在一个容器中运行 WireGuard 工具和 Go Fake-IP DNS；透明捕获和代理转发仍未实现，详见 [README.md](./README.md)。

```
[ 局域网设备 ] 
      │ 1. 发起 DNS 查询
      ▼
[ RouterOS (10.0.0.2) ]
      │ 2. 劫持并跨 WG 转发 53 端口流量
      ▼
┌───── WireGuard 隧道 (10.0.0.0/24) ───────────────────────────────────┐
│                                                                      │
│    [ VPS 宿主机 (10.0.0.1) ]                                         │
│          │                                                           │
│          ▼ (单个自定义容器的网络命名空间)                           │
│    [ WireGuard 工具 + Go Fake-IP DNS ]                                            │
│       ├── 3. DNS 模块: 收到请求，响应 Fake-IP (如 198.18.x.x)           │
│       │      └── 内存维护: [Fake-IP <-> 真实域名] 映射表                 │
│       └── 4. 数据面: 当前版本尚未接管 Fake-IP 的 TCP/UDP 流量           │
└──────────────────────────────────────────────────────────────────────┘
```

### 1.1 工作流程详解

1. **DNS 阶段**：
   * 客户端发送域名请求（如 `example.com`），ROS 将 DNS 请求转发给 VPS 容器内的 Go DNS 服务。
   * VPS 的 DNS 服务拦截请求，返回一个动态分配的虚拟 IP（例如 `198.18.0.123`），同时在内存中记录映射：`198.18.0.123 <-> example.com`。
2. **流量转发阶段（尚未实现）**：
   * 客户端向 `198.18.0.123` 发起 TCP/UDP 连接。
   * ROS 可以按路由规则将数据包通过 WireGuard 隧道送达 VPS。
   * 当前 Go 服务没有捕获或转发该数据包，因此连接不会因 DNS 映射自动建立公网连接。

---

## 2. RouterOS (ROS) 端配置示例

ROS 侧仅需保持最基础的 WG 建立与静态路由转发，无需复杂的脚本。

```routeros
# 1. 创建 WireGuard 接口
/interface wireguard
add listen-port=13231 name=wg-vps

# 2. 配置 Peer (连接至 VPS)
/interface wireguard peers
add allowed-address=0.0.0.0/0 endpoint-address=YOUR_VPS_IP endpoint-port=51820 interface=wg-vps public-key="YOUR_VPS_PUBLIC_KEY" persistent-keepalive=25s

# 3. 分配隧道内网 IP
/ip address
add address=10.0.0.2/24 interface=wg-vps network=10.0.0.0

# 4. 路由设置：将 Fake-IP 网段(198.18.0.0/16)推入 WG 隧道
/ip route
add dst-address=198.18.0.0/16 gateway=wg-vps

# 5. 修改 MSS 防碎片卡死 (保底优化)
/ip firewall mangle
add chain=forward action=change-mss new-mss=clamp-to-pmtu passthrough=yes out-interface=wg-vps protocol=tcp tcp-flags=syn
```

---

## 3. Go 语言服务当前职责

当前 Go 服务只负责 Fake-IP DNS：

1. **A 查询**：在 `FAKEIP_CIDR` 地址池中分配地址，并在内存中保存地址到域名的映射。
2. **其他查询**：转发到 `UPSTREAM_DNS`；AAAA 查询不生成 Fake-IP。
3. **诊断接口**：提供 `/healthz` 和 `/mapping`，用于确认服务状态和检查映射。
4. **Fake-IP 验证工具**：镜像内的 `fakeipcheck` 会从指定 DNS 查询 A 记录，并确认结果属于 Fake-IP 地址池。

透明 TCP/UDP 转发需要另行增加 TPROXY、TUN 或用户态网络栈，以及连接回收、路由和防火墙规则；当前代码尚未包含这些模块。

---

## 4. Docker 部署

项目根目录的 Compose 以单个服务构建精简自定义镜像，容器内运行 Go DNS 和 WireGuard 工具。WireGuard 配置通过 `/etc/wireguard` 持久化目录提供；容器不包含 Web 管理界面。

配置 `docker-compose.yml` 中的持久化目录、端口和 Fake-IP DNS 参数后运行：

```sh
docker compose up -d --build
```

端口发布由唯一服务统一管理。8080 诊断端口默认仅绑定宿主机 loopback。容器映射 `/dev/net/tun` 供后续数据面使用，但仅映射设备不代表 TUN 转发已经实现。启动 WireGuard 仍需要部署环境提供 `NET_ADMIN` 等权限，并在容器内执行 `wg-quick up /etc/wireguard/wg0.conf`。

---

## 5. 关键踩坑与注意事项

1. **MTU 优化**：
   WireGuard 增加了头部开销，强烈建议 ROS 和 VPS 上的 WG 接口 MTU 统一调整为 **1360** 或 **1280**，以防 TCP 握手卡死或传输丢包。
2. **诊断接口暴露**：
   `HTTP_ADDR=:8080` 会在容器所有地址上监听，但 Compose 将宿主机映射限定为 `127.0.0.1:8080`。不要改为公网绑定，除非另有防火墙或可信访问控制。
3. **DNS 端口冲突**：
   容器所在网络命名空间中不能有其他服务占用 53 端口。启动前确认 VPS 上没有其他服务发布 DNS 端口，并在防火墙中只允许预期的 WireGuard/内网来源访问 DNS。
4. **透明转发边界**：
   当前版本没有 TPROXY、TUN、iptables/nftables 数据面，也没有 UDP/QUIC 代理逻辑；不要把 Fake-IP 路由配置误认为已经完成公网转发。