# 自研 NOFX 数据网关

`nofx-data-gateway` 是仓库内自研的 `nofxos.ai` 兼容数据服务。它只使用公开交易所行情接口，不抓取或复制 `nofxos.ai` 的私有数据。

## 作用

- 为现有策略引擎提供 `AI500`、`OI Top/OI Low`、`NetFlow`、`Price Ranking`、`Coin Detail` 数据。
- 后端通过 `DATA_GATEWAY_URL` 读取它，不需要改策略主流程。
- 浏览器通过后端同源代理 `/api/data-gateway/*` 访问，避免前端直连内部服务。

## 数据来源

首版采集这些公开数据源：

- Binance 现货、USDT 永续
- Bybit 现货、USDT 永续
- OKX 现货、USDT 永续，并补充 OI / Funding
- Bitget 现货、USDT 永续
- Gate 现货、USDT 永续
- KuCoin 现货、USDT 永续

单个交易所失败不会让接口整体失败；只要其他交易所或 SQLite 缓存有可用快照，接口仍返回 `success: true`。全部数据源失败且无缓存时返回 `success: false`。

## 运行方式

本地运行：

```bash
DATA_GATEWAY_ADDR=:8090 \
DATA_GATEWAY_DB_PATH=data/data-gateway.db \
DATA_GATEWAY_REFRESH_INTERVAL=1m \
go run ./cmd/data-gateway
```

如果本机需要代理访问交易所公开接口：

```bash
HTTP_PROXY=http://127.0.0.1:7897 \
HTTPS_PROXY=http://127.0.0.1:7897 \
ALL_PROXY=socks5://127.0.0.1:7897 \
NO_PROXY=127.0.0.1,localhost \
DATA_GATEWAY_ADDR=:8090 \
DATA_GATEWAY_DB_PATH=data/data-gateway.db \
DATA_GATEWAY_REFRESH_INTERVAL=1m \
go run ./cmd/data-gateway
```

后端读取本地网关：

```bash
DATA_GATEWAY_URL=http://127.0.0.1:8090 go run .
```

Docker Compose 中后端默认使用：

```bash
DATA_GATEWAY_URL=http://nofx-data-gateway:8090
```

## 鉴权

`DATA_GATEWAY_TOKEN` 为空时，本地开放访问，便于开发调试。

设置 token 后，兼容两种认证方式：

```bash
curl http://127.0.0.1:8090/api/ai500/list -H 'X-Gateway-Token: your-token'
curl 'http://127.0.0.1:8090/api/ai500/list?auth=your-token'
```

## 兼容接口

- `GET /health`
- `GET /api/ai500/list?limit=`
- `GET /api/ai500/:symbol`
- `GET /api/ai500/stats`
- `GET /api/oi/top-ranking?duration=&limit=`
- `GET /api/oi/low-ranking?duration=&limit=`
- `GET /api/oi/top`
- `GET /api/netflow/top-ranking?duration=&limit=&type=&trade=`
- `GET /api/netflow/low-ranking?duration=&limit=&type=&trade=`
- `GET /api/netflow/top`
- `GET /api/price/ranking?duration=1h,4h,24h&limit=`
- `GET /api/coin/:symbol?include=netflow,oi,price,ai500`

## 参数规则

- `limit` 必须是正整数，AI500 最大 500，其余排行最大 100。
- `duration` 支持 `1h`、`4h`、`24h`。
- `type` 支持 `institution`、`personal`。
- `trade` 支持 `future`、`spot`，同时兼容 `futures` 输入。

无效参数返回 HTTP 400 和 `{"success":false,"error":"..."}`。

## AI500 评分说明

AI500 首版评分为 0-100 分，使用公开行情聚合生成：

- 趋势强度：1h / 4h / 24h 价格动量。
- 成交活跃：成交额和交易所覆盖数量。
- OI 信号：OI 当前值、变化率和变化金额。
- Funding 健康度：极端 Funding 扣分。
- 风险过滤：低流动性、无有效价格、无合约数据、稳定币和杠杆代币默认不上榜。

该评分是自研规则，不保证与 `nofxos.ai` 原站分数一致。

## NetFlow 说明

首版 NetFlow 是“近似资金流”，不是交易所真实机构订单流：

- `institution` 使用 OI 变化、成交额和价格方向估算。
- `personal` 使用短周期成交额异动、价格追涨杀跌和反向 OI 信号估算。
- 响应字段保持 `amount/price/rank/symbol` 兼容，`amount` 为 USDT 估算值。

## 验收命令

直接访问数据网关：

```bash
curl http://127.0.0.1:8090/health
curl 'http://127.0.0.1:8090/api/ai500/list?limit=5'
curl 'http://127.0.0.1:8090/api/oi/top-ranking?duration=1h&limit=5'
curl 'http://127.0.0.1:8090/api/netflow/top-ranking?duration=1h&limit=5&type=institution&trade=future'
curl 'http://127.0.0.1:8090/api/price/ranking?duration=1h,4h,24h&limit=5'
curl 'http://127.0.0.1:8090/api/coin/BTC?include=netflow,oi,price,ai500'
```

通过后端代理访问：

```bash
curl 'http://127.0.0.1:18080/api/data-gateway/api/ai500/list?limit=3'
curl 'http://127.0.0.1:18080/api/data-gateway/api/oi/top-ranking?duration=1h&limit=3'
```

测试：

```bash
go test ./datagateway ./cmd/data-gateway ./provider/nofxos ./api
go build ./cmd/data-gateway
```
