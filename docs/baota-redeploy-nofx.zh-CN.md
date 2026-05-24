# 宝塔重新部署 NOFX

本项目推荐在宝塔服务器上使用 Docker Compose 部署。部署脚本会保留 `.env` 和 `data/`，不会主动清空交易记录、配置、密钥或 SQLite 数据库。

## 部署包

本地已生成部署包：

```text
release/nofx-baota-redeploy-20260518-211552.tar.gz
```

上传到宝塔服务器建议路径：

```text
/www/wwwroot/nofx-baota-redeploy-20260518-211552.tar.gz
```

## 宝塔终端执行

在宝塔面板打开“终端”，执行：

```bash
set -e
APP_DIR=/www/wwwroot/nofx
PKG=/www/wwwroot/nofx-baota-redeploy-20260518-211552.tar.gz

mkdir -p "$APP_DIR"
tar -xzf "$PKG" -C "$APP_DIR"
cd "$APP_DIR"

bash scripts/baota-redeploy.sh "$APP_DIR"
```

如果你上传到了其它文件名，改掉 `PKG=` 这一行即可。

## 代理配置

如果服务器访问 Binance/Bybit/OKX 等公开接口需要代理，先编辑：

```bash
cd /www/wwwroot/nofx
nano .env
```

添加或修改：

```env
HTTP_PROXY=http://127.0.0.1:7897
HTTPS_PROXY=http://127.0.0.1:7897
ALL_PROXY=socks5://127.0.0.1:7897
NO_PROXY=localhost,127.0.0.1,::1,nofx,nofx-frontend,nofx-data-gateway
```

然后重新执行：

```bash
cd /www/wwwroot/nofx
bash scripts/baota-redeploy.sh /www/wwwroot/nofx
```

## 验收命令

部署完成后在宝塔终端执行：

```bash
cd /www/wwwroot/nofx
docker compose ps
curl -s http://127.0.0.1:8090/health
curl -s http://127.0.0.1:8080/api/health
curl -s http://127.0.0.1:8080/api/data-gateway/health
curl -s 'http://127.0.0.1:8080/api/data-gateway/api/ai500/list?limit=5'
curl -s 'http://127.0.0.1:8080/api/data-gateway/api/oi/top-ranking?duration=1h&limit=5'
curl -s 'http://127.0.0.1:8080/api/data-gateway/api/oi/low-ranking?duration=1h&limit=5'
```

## 宝塔反向代理

如果宝塔网站绑定域名，例如：

```text
http://lh.odinb.fun
```

网站反向代理目标应指向前端容器端口：

```text
http://127.0.0.1:3000
```

前端容器会把 `/api/` 自动代理到后端 `nofx:8080`，后端再把 `/api/data-gateway/*` 代理到 `nofx-data-gateway:8090`。

## 常见问题

1. `docker: command not found`

   宝塔 Docker 管理器或系统 Docker 没安装。先在宝塔软件商店安装 Docker，或服务器安装 Docker Engine。

2. `data gateway is not healthy`

   查看日志：

   ```bash
   cd /www/wwwroot/nofx
   docker compose logs --tail=150 nofx-data-gateway
   ```

   如果日志中 Binance 451 或 Bybit 403，属于交易所地区限制。只要 OKX/Bitget/Gate/KuCoin 可用，网关仍能返回数据。

3. `backend data-gateway proxy failed`

   查看后端是否正确读取 `DATA_GATEWAY_URL=http://nofx-data-gateway:8090`：

   ```bash
   cd /www/wwwroot/nofx
   grep DATA_GATEWAY_URL .env
   docker compose logs --tail=150 nofx
   ```
