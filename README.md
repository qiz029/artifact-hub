# Artifact Hub

Artifact Hub 是一个可自托管的 HTML / Markdown artifact 仓库。用户可以创建 collection、上传 artifact，并获得稳定访问地址。Artifact 的内容和元数据一经创建便不可修改，只能删除后重新创建。

## 功能与安全边界

- **Immutable**：API 不提供 update 路由，Postgres trigger 也会拒绝任何 `UPDATE artifacts`。
- **可删除**：删除 artifact 会同时删除内容和元数据。
- **完整元数据**：记录 title、description、tags、自定义 JSON、SHA-256、文件大小、原始文件名和 MIME type。
- **隔离展示**：HTML 在 sandboxed iframe 中展示，独立地址也带 CSP sandbox；Markdown 不执行内嵌 HTML。
- **单文件限制**：仅支持 `.html`、`.htm`、`.md`、`.markdown`，最大 10 MB。

> [!WARNING]
> 当前版本没有内建账号、登录和权限控制。任何能访问服务的人都可以浏览、创建和删除数据。不要直接把 `8080` 端口暴露到公网；生产部署应放在 VPN、Cloudflare Access、Tailscale，或带认证的反向代理后面。

## 目录结构

```text
frontend/                 React + TypeScript + Vite
cmd/server/               Go HTTP 服务入口
internal/httpapi/         API、上传和静态站点
internal/database/        Postgres migration runner 与 schema
design/v0.html            初始产品原型
Dockerfile                前后端多阶段生产镜像
docker-compose.yml        Artifact Hub + Postgres 编排
```

Go 服务使用标准库 `net/http` 和 `pgx`。Artifact 内容以 `bytea` 与元数据一起存入 Postgres，因此数据库备份就是完整备份。当前设计适合 10 MB 以内的 HTML / Markdown；未来可以在保持 API 不变的情况下把内容层迁移到 S3-compatible object storage。

## 推荐部署架构

```text
Internet / private network
          |
          v
HTTPS reverse proxy or access gateway
          |
          v
Artifact Hub :8080  --->  Postgres :5432
                              |
                              v
                     Docker named volume
```

生产环境建议：

1. Artifact Hub 只绑定到 `127.0.0.1:8080`。
2. 由 Caddy、Nginx 或访问网关终止 HTTPS 并处理认证。
3. Postgres 不映射宿主机端口，只允许 Compose 内部网络访问。
4. 定期用 `pg_dump` 备份，并把备份复制到另一台机器或对象存储。

## 方式一：Docker Compose 部署（推荐）

### 1. 准备服务器

建议配置：

- Linux x86_64 或 arm64。
- Docker Engine 24+，Docker Compose v2。
- 至少 1 CPU、1 GB 内存；数据库大小主要取决于上传内容总量。
- 一个指向服务器的域名，例如 `artifacts.example.com`。

确认环境：

```bash
docker --version
docker compose version
```

### 2. 获取代码

```bash
git clone https://github.com/qiz029/artifact-hub.git
cd artifact-hub
```

### 3. 创建生产环境配置

```bash
cp .env.production.example .env
```

编辑 `.env`：

```dotenv
COMPOSE_PROJECT_NAME=artifact-hub
BIND_ADDRESS=127.0.0.1
PORT=8080
PUBLIC_URL=https://artifacts.example.com

POSTGRES_DB=artifact_hub
POSTGRES_USER=artifact
POSTGRES_PASSWORD=生成一个足够长的随机密码
DATABASE_URL=postgres://artifact:同一个密码@postgres:5432/artifact_hub?sslmode=disable
```

注意：

- `POSTGRES_PASSWORD` 和 `DATABASE_URL` 中的密码必须一致。
- 建议密码只使用字母、数字、`_` 和 `-`。如果包含 `@`、`:`、`/` 等字符，需要在 `DATABASE_URL` 中进行 URL 编码。
- `PUBLIC_URL` 必须是用户最终访问的地址，且不要带结尾 `/`。它会用于生成 artifact 的稳定链接。
- `.env` 已被 Git 忽略，不要提交生产密码。

限制配置文件权限：

```bash
chmod 600 .env
```

可以先检查 Compose 展开后的配置：

```bash
docker compose config
```

### 4. 构建并启动

```bash
docker compose up -d --build
```

首次启动会：

1. 创建 Postgres volume。
2. 等待数据库健康检查通过。
3. 启动 Artifact Hub。
4. 自动执行尚未应用的数据库 migration。

检查状态：

```bash
docker compose ps
curl --fail http://127.0.0.1:8080/api/health
```

健康响应应为：

```json
{"status":"ok"}
```

查看启动日志：

```bash
docker compose logs -f artifact-hub
```

### 5. 配置 HTTPS 反向代理

#### Caddy

Caddy 可以自动申请和续期 TLS 证书。示例 `Caddyfile`：

```caddyfile
artifacts.example.com {
    encode zstd gzip
    reverse_proxy 127.0.0.1:8080
}
```

加载配置后访问：

```text
https://artifacts.example.com
```

如果服务面向公网，还应在 Caddy 前使用 Cloudflare Access、VPN，或配置 `basic_auth`。Caddy 的密码必须先使用 `caddy hash-password` 生成哈希，不要把明文密码写进 Caddyfile。

#### Nginx

下面的配置先提供 HTTP；可以再使用 Certbot 为该 server block 添加 HTTPS：

```nginx
server {
    listen 80;
    server_name artifacts.example.com;

    client_max_body_size 12m;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }
}
```

检查并重新加载：

```bash
sudo nginx -t
sudo systemctl reload nginx
```

使用 Certbot 配置证书：

```bash
sudo certbot --nginx -d artifacts.example.com
```

需要认证时可以为 Nginx location 增加 `auth_basic` 和 `auth_basic_user_file`，或者把整个域名放到访问网关后。

### 6. 防火墙

如果使用反向代理，只需要允许 SSH、HTTP 和 HTTPS：

```bash
sudo ufw allow OpenSSH
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
sudo ufw enable
```

不要开放 `5432`。当 `BIND_ADDRESS=127.0.0.1` 时，`8080` 也不需要在防火墙中开放。

## 方式二：仅内网部署

如果只在可信局域网或 VPN 内使用，可以让服务监听所有网卡：

```dotenv
BIND_ADDRESS=0.0.0.0
PORT=8080
PUBLIC_URL=http://192.168.1.20:8080
```

然后启动：

```bash
docker compose up -d --build
```

通过 `http://服务器地址:8080` 访问。即使在内网，也建议通过防火墙限制允许访问的网段。

## 方式三：Go 二进制 + 外部 Postgres

适合已经有托管 Postgres、systemd 和独立发布流程的环境。

### 构建

需要 Go 1.23 和 Node.js 22：

```bash
npm --prefix frontend ci
npm --prefix frontend run build
go build -trimpath -o bin/artifact-hub ./cmd/server
```

部署时必须同时复制：

```text
bin/artifact-hub
frontend/dist/
```

### 运行参数

```bash
export DATABASE_URL='postgres://user:password@db.example.com:5432/artifact_hub?sslmode=require'
export HTTP_ADDR='127.0.0.1:8080'
export FRONTEND_DIR='/opt/artifact-hub/frontend/dist'
export PUBLIC_URL='https://artifacts.example.com'

/opt/artifact-hub/artifact-hub
```

外部 Postgres 用户需要具备创建表、索引、函数、trigger 和 `pgcrypto` extension 的权限。服务启动时会自动执行 migration。

### systemd 示例

`/etc/systemd/system/artifact-hub.service`：

```ini
[Unit]
Description=Artifact Hub
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=artifact-hub
Group=artifact-hub
WorkingDirectory=/opt/artifact-hub
EnvironmentFile=/etc/artifact-hub.env
ExecStart=/opt/artifact-hub/artifact-hub
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
```

启用服务：

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now artifact-hub
sudo systemctl status artifact-hub
```

## 升级

升级前先备份数据库，然后执行：

```bash
git pull --ff-only
docker compose build --pull artifact-hub
docker compose up -d
docker compose ps
curl --fail http://127.0.0.1:8080/api/health
```

服务启动时会自动执行新 migration。当前 migration 设计为向前迁移，因此准备回滚应用版本前，应先确认旧版本是否兼容新 schema；最安全的回滚方式是恢复升级前的数据库备份和对应代码版本。

## 备份与恢复

Artifact 内容保存在 Postgres 中。只备份代码或 Docker image 不会备份用户数据。

### 创建备份

```bash
mkdir -p backups
docker compose exec -T postgres sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc' > backups/artifact-hub.dump
```

确认备份不是空文件：

```bash
ls -lh backups/artifact-hub.dump
```

建议定期生成带日期的备份，将其加密后复制到其他机器或对象存储，并实际演练恢复流程。

### 恢复备份

恢复会覆盖当前数据库中的同名对象。先停止应用，避免恢复期间产生写入：

```bash
docker compose stop artifact-hub
docker compose exec -T postgres sh -c 'pg_restore --clean --if-exists --no-owner -U "$POSTGRES_USER" -d "$POSTGRES_DB"' < backups/artifact-hub.dump
docker compose start artifact-hub
curl --fail http://127.0.0.1:8080/api/health
```

如果恢复目标是全新的空数据库，可以省略 `--clean --if-exists`。

## 日常运维

查看服务状态：

```bash
docker compose ps
```

查看最近日志：

```bash
docker compose logs --tail=200 artifact-hub
docker compose logs --tail=200 postgres
```

持续查看日志：

```bash
docker compose logs -f artifact-hub
```

查看 volume：

```bash
docker volume ls | grep artifact
```

查看数据库大小：

```bash
docker compose exec -T postgres sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c "SELECT pg_size_pretty(pg_database_size(current_database()));"'
```

正常停止服务，不删除数据：

```bash
docker compose down
```

> [!CAUTION]
> `docker compose down -v` 会删除 Postgres volume 和全部 artifact。除非已经确认备份可恢复，否则不要执行。

## 环境变量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `BIND_ADDRESS` | `0.0.0.0` | Compose 在宿主机上绑定的地址；反向代理部署建议使用 `127.0.0.1`。 |
| `PORT` | `8080` | Compose 对外端口。 |
| `PUBLIC_URL` | `http://localhost:8080` | 用户访问的公开 origin，用于生成稳定 artifact URL。 |
| `POSTGRES_DB` | `artifact_hub` | Postgres 数据库名。 |
| `POSTGRES_USER` | `artifact` | Postgres 用户名。 |
| `POSTGRES_PASSWORD` | `artifact` | Postgres 密码；生产环境必须修改。 |
| `DATABASE_URL` | 本地 Compose 连接串 | Go 服务使用的完整 Postgres DSN。 |
| `HTTP_ADDR` | `:8080` | 非 Compose 部署时 Go HTTP 服务监听地址。 |
| `FRONTEND_DIR` | `frontend/dist` | 编译后前端静态文件目录。 |

## 常见问题

### 页面能打开，但健康检查返回 503

数据库尚未就绪或 `DATABASE_URL` 与 Postgres 用户、密码不一致：

```bash
docker compose ps
docker compose logs postgres
docker compose logs artifact-hub
```

### 上传出现 413 Request Entity Too Large

反向代理的上传限制小于应用限制。将 Caddy 或 Nginx 的 request body 限制调整到至少 12 MB。应用本身仍会把 artifact 限制在 10 MB。

### 稳定链接使用了错误域名或 HTTP

把 `.env` 中的 `PUBLIC_URL` 设置为最终 HTTPS origin，然后重新创建应用容器：

```bash
docker compose up -d --force-recreate artifact-hub
```

### 修改 `.env` 后没有生效

环境变量只在容器创建时注入：

```bash
docker compose up -d --force-recreate
```

### 更换 Postgres 密码后无法连接

已有 Postgres volume 初始化完成后，修改 `POSTGRES_PASSWORD` 不会自动修改数据库中的密码。需要在数据库内执行 `ALTER USER`，并同步修改 `DATABASE_URL`；或者在无数据的首次部署阶段删除 volume 后重新初始化。

## 本地开发

需要 Go 1.23、Node.js 22 和 Docker：

```bash
cp .env.example .env
docker compose up -d postgres
go run ./cmd/server
```

另开终端：

```bash
npm --prefix frontend install
npm --prefix frontend run dev
```

前端开发地址为 <http://localhost:5173>，API 会代理到 `localhost:8080`。

## HTTP API

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/collections` | Collection 列表与 artifact 数量 |
| `POST` | `/api/collections` | 创建 collection |
| `GET` | `/api/collections/{id}/artifacts` | 浏览或搜索 artifact |
| `POST` | `/api/collections/{id}/artifacts` | Multipart 上传，文件字段为 `file` |
| `GET` | `/api/artifacts/{id}` | 获取完整元数据 |
| `GET` | `/api/artifacts/{id}/content` | 获取原始内容 |
| `DELETE` | `/api/artifacts/{id}` | 删除 artifact |
| `GET` | `/a/{id}/{slug}` | 新生成的可读稳定公开地址 |
| `GET` | `/a/{id}` | 向后兼容的稳定公开地址 |

上传接口还接受 `title`、`description`、逗号分隔的 `tags`，以及 JSON object 格式的 `metadata`。

## 验证代码

```bash
go test ./cmd/... ./internal/...
npm --prefix frontend run lint
npm --prefix frontend run build
```
