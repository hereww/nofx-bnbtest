# 自定义代币地址监控与策略说明

## 结论

用户提供的是链上代币合约地址，不是 NOFX 策略里可直接下单的交易对 symbol。

当前项目的自动交易链路主要使用交易所 symbol，例如 `BTCUSDT`、`ETHUSDT`。这些地址对应的是 DEX/链上代币，不能直接写成 `static_coins` 让 Binance Futures Trader 下单。

因此本次处理分成两层：

- 监控展示：按合约地址读取 DEX 行情，并在 Data 页面展示。
- 策略/Trader：可以创建监控用途的策略和未启动 Trader，但不应直接启动实盘，除非后续完成“合约地址 -> 可交易交易对”的映射和交易执行适配。

## 地址解析结果

数据来源：DexScreener token endpoint。展示时选择同一 base token 下流动性最高的交易池。

| 合约地址 | 链 | DEX | 名称 | 符号 | 主要报价 | 说明 |
| --- | --- | --- | --- | --- | --- | --- |
| `0x600e3b55d5368c32a94f9372563318adb6a3f882` | BSC | PancakeSwap | dmt-nat | dmt-nat | USDT | DEX 现货监控 |
| `0x812fc5119b772c6c7a66249a559f3614623f4444` | BSC | PancakeSwap | 火凤凰 | 火凤凰 | 我是未来 | DEX 现货监控 |
| `0x6d8d8df799279e761a4f49edc319b3bd50f14444` | BSC | PancakeSwap | CZ是历史 我是未来 | 我是未来 | WBNB | DEX 现货监控 |
| `0xa1ed61902f13e162305f59e1b2475e269e647777` | BSC | PancakeSwap | VIRUS | VIRUS | WBNB | DEX 现货监控 |
| `0x249130f5e2dd4cf278180c0df8273f3592ad1247` | Ethereum | Uniswap | dmt-nat | dmt-nat | ETH | DEX 现货监控 |
| `0xd6e14208e929a38db47bae48f3ac8a420aff0999` | BSC | PancakeSwap | BLM coin | BLM | WBNB | DEX 现货监控 |
| `0x5f28b56a2f6e396a69fc912aec8d42d8afa17777` | BSC | PancakeSwap | 疯狂的石头 | 疯狂的石头 | WBNB | DEX 现货监控 |

## 已补充的项目能力

### 后端

新增接口：

```text
GET /api/custom-tokens
GET /api/custom-tokens?addresses=0x...,0x...
```

返回字段包括：

- `address`
- `chain_id`
- `dex_id`
- `name`
- `symbol`
- `quote_symbol`
- `price_usd`
- `liquidity_usd`
- `volume_24h_usd`
- `pair_url`
- `trade_supported`
- `reason`

默认监控本文件列出的 7 个地址。`trade_supported` 当前固定为 `false`，因为这些是 DEX 现货合约地址，不是当前 Binance Futures Trader 可直接执行的交易 symbol。

### 前端

Data 页面新增“自定义代币监控”区块，展示：

- 代币名称/符号
- 合约地址
- 链和 DEX
- 美元价格
- 流动性
- 24h 成交额
- DexScreener 跳转链接
- 监控状态

页面会跟随现有 Data 页面刷新节奏自动更新。

## 策略与 Trader 使用方式

如果只是观察这些币：

1. 打开 `Data` 页面。
2. 查看“自定义代币监控”表格。
3. 根据价格、流动性、24h 成交额判断是否值得进一步接入交易执行。

如果要接入自动交易：

1. 先确认这些代币在哪个交易执行环境可交易，例如 PancakeSwap、Uniswap 或其他链上路由。
2. 增加交易执行适配器，支持按合约地址查询价格、滑点、授权、下单和持仓。
3. 增加策略候选标的类型，不再复用 `static_coins` 的 futures symbol 语义。
4. 完成风控：最大滑点、最小流动性、最大仓位、买卖税、黑名单/蜜罐检测。
5. 再创建可启动的 Trader。

## 当前限制

- 这些地址不是 Binance Futures 交易对。
- 不应把 `dmt-nat`、`VIRUS`、`BLM` 等直接拼成 `DMTNATUSDT`、`VIRUSUSDT`、`BLMUSDT` 用于 Binance Futures。
- 页面 Strategy Studio 的“自定义币种”输入框会自动追加 `USDT`，只适合交易所 symbol，不适合合约地址。
- 监控展示可以做，自动下单需要额外开发链上交易执行层。

