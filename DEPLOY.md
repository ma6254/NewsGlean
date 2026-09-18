# NewsGlean 公网部署指南

> 面向**单账户、公网部署**：VPS + 反向代理（Nginx / Caddy）做 HTTPS 与访问门。
> 鉴权策略：**HTTP Basic Auth 放在反代层**，Go 服务保持回环绑定，不上 JWT、不做登录页（理由见文末「为什么不用 JWT」）。

## 目标架构

```
公网 ──→ 443 (HTTPS + Basic Auth) ──→ 127.0.0.1:28080 (Go 服务，绑定回环)
```

三条铁律：

1. Go 服务的 `server.http_addr` **保持 `127.0.0.1:28080`**，绝不改成 `0.0.0.0`；
2. TLS 终止 + Basic Auth 全部在反代层，`/api`、`/swagger`、静态页面、SSE 一次性锁死；
3. 防火墙只放行 80 / 443，28080 永远不对公网开放。

> ⚠️ 注意：PLAN.md「安全默认值」里规划的「非回环地址且未配置认证时拒绝启动」目前**尚未实现**。在它落地前，请靠「保持回环 + 反代做门」这条纪律兜底，别直接对外绑定端口。

## 前置条件

- 一台 Linux VPS（下面以 Ubuntu + systemd 为例）；
- 域名 `newsglean.example.com` 已解析到 VPS，且防火墙放行 80 / 443；
- 一个可执行的 `news-glean` 二进制（构建方式见 [README.md](./README.md)）。

### 构建产物（Linux）

前端是 `go:embed` 打进二进制的（`internal/webui`），所以要先出前端 `dist` 再编 Go：

```bash
# 1. 前端（在 NewsGlean-web 仓库）
npm ci && npm run build

# 2. 把 dist 拷进 server 的 internal/webui/dist/（Windows 上 build.ps1 会自动做这步）
#    Linux 上手动：cp -r ../NewsGlean-web/dist internal/webui/dist

# 3. 编 Go
go build -o news-glean .
```

## 服务端 config.yml

部署目录（如 `/opt/newsglean/`）下放一份 `config.yml`，关键就两处：

```yaml
server:
  http_addr: "127.0.0.1:28080"          # 保持回环
  base_url: "https://newsglean.example.com"  # 以后 webhook 回调等场景用，现在可留空
web:
  mode: embed                            # 前端内嵌二进制
```

其余项沿用默认值即可（见 `docs/default.yml`）。

## 方案 A：Caddy（推荐，配置最省）

1. 生成口令哈希并粘贴进 `deploy/Caddyfile`：

   ```bash
   caddy hash-password     # 把输出（$2a$14$...）替换 Caddyfile 里的 <bcrypt-hash>
   ```

2. 把 `deploy/Caddyfile` 里的域名换成你的，放到 `/etc/caddy/Caddyfile`（或 `caddy fmt --overwrite` 后 reload）。

3. Caddy 会自动申请 / 续期 TLS 证书，无需手动管证书。

   ```bash
   systemctl enable --now caddy
   caddy reload --config /etc/caddy/Caddyfile
   ```

## 方案 B：Nginx

1. 生成口令文件：

   ```bash
   htpasswd -B -c /etc/nginx/.htpasswd you   # -B 用 bcrypt
   ```

2. 申请证书（certbot 示例）：

   ```bash
   apt install -y certbot python3-certbot-nginx
   certbot certonly --webroot -w /var/www/certbot -d newsglean.example.com
   ```

3. 把 `deploy/nginx.conf` 的域名换成你的，放到 `/etc/nginx/conf.d/newsglean.conf`：

   ```bash
   nginx -t && systemctl reload nginx
   ```

   > SSE 相关配置（`proxy_buffering off` / `proxy_read_timeout 1h`）**不要删**，否则采集进度流会被缓冲/超时掐断。

## NewsGlean 自启动（systemd）

新建 `/etc/systemd/system/newsglean.service`：

```ini
[Unit]
Description=NewsGlean
After=network.target

[Service]
Type=simple
User=newsglean
WorkingDirectory=/opt/newsglean
ExecStart=/opt/newsglean/news-glean -d /opt/newsglean
Restart=on-failure
RestartSec=3

# 纪律：只绑定回环，访问门交给前面的 Caddy/Nginx

[Install]
WantedBy=multi-user.target
```

```bash
useradd --system --create-home newsglean
chown -R newsglean:newsglean /opt/newsglean
systemctl daemon-reload
systemctl enable --now newsglean
systemctl status newsglean
```

> Caddy / Nginx 各自的 systemd 服务由包管理器自带，无需额外写。

## 验证

```bash
# 1. 未带凭据应 401
curl -I https://newsglean.example.com/api/source

# 2. 带凭据应 200
curl -u you:你的口令 https://newsglean.example.com/api/source

# 3. 浏览器打开 https://newsglean.example.com —— 首次弹账号密码框，之后会话期内免输
```

## 常见问题

- **SSE 不实时 / 断流**：检查 Nginx 的 `proxy_buffering off`、`proxy_read_timeout`；Caddy 检查 `flush_interval -1`。
- **日志里客户端全是 127.0.0.1**：请求经反代后对 Go 来说都来自本机。要记录真实来源需改 `internal/server/middleware.go` 读 `X-Forwarded-For`（可选，不急于做）。
- **以后上 `webhook` 入站源**：那个端点要独立凭据（per-webhook 密钥 / HMAC），别和这里的 Basic Auth 混用——它给机器推内容用，不给你登录用。届时把 `server.base_url` 填成公网域名。

## 为什么不用 JWT / 登录页

单账户场景下：没有多用户要区分、没有角色权限要隔离、单进程无横向扩展需求。JWT 解决的是「多用户 + 无状态」，这几个前提都不成立，属于过度设计；而 Basic Auth 恰好是「单账户 + 共享口令」的最小形态。等出现第二个真实账户或分权限访问时，再上账户体系 + 会话（届时 JWT / 服务端 session 才合理）。

---

参考：配置文件 `deploy/Caddyfile`、`deploy/nginx.conf`；项目设计见 [PLAN.md](./PLAN.md)。
