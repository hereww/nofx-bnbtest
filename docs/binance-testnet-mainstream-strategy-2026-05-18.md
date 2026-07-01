# Binance 模拟盘主流币低风险策略测试说明

测试时间：2026-05-18（Asia/Shanghai）

## 测试目标

本次测试按“先用 Binance 测试网支持的主流 USDT 永续合约跑模拟盘”的思路执行，所有候选标的必须来自交易所可交易 symbol。

核心原则：

- Binance 模拟盘使用的是 Binance USD-M Futures 测试网。
- 可交易对象必须是 Binance Futures 支持的交易对，例如 `BTCUSDT`。
- 不接入合约地址、链上监控或外部 DEX 行情源。
- 自动交易启动属于高风险动作，本次只完成配置和连接验证，未启动新交易员。

## 已确认环境

交易所账户：

- Exchange ID：`9688e0d9-724c-42e0-9497-e4c2cd6ffbe5`
- 账户名：`lianghua`
- 类型：`binance`
- 测试网：`true`
- 状态：`ok`
- 测试网余额：`5000 USDT`

模型配置：

- Model ID：`9a0bb67c-7822-4007-b6de-7fbb8819af4d_openai`
- Provider：`openai`
- Model：`gpt-5.5`
- 状态：已启用，已有 API Key

## 已创建策略

策略名称：`币安模拟盘主流币低风险策略`

策略 ID：

```text
6a89d9d9-2df3-4a7e-a2dd-fff92f920d48
```

策略说明：

```text
用于 Binance USD-M Futures 测试网的低风险模拟盘策略；固定监控 BTC/ETH/BNB/SOL/XRP USDT 永续合约。
```

固定币种池：

```text
BTCUSDT
ETHUSDT
BNBUSDT
SOLUSDT
XRPUSDT
```

这些交易对已通过 Binance Futures 测试网 `exchangeInfo` 校验，均为：

- `TRADING`
- `PERPETUAL`
- `quoteAsset = USDT`
- `marginAsset = USDT`

## 策略参数

币种来源：

- `source_type = static`
- `use_ai500 = false`
- 不使用 OI Top / OI Low 自动扩展币池

K 线与指标：

- 主周期：`5m`
- 主周期 K 线数量：`30`
- 长周期：`1h`
- 长周期 K 线数量：`24`
- 多周期：`5m`, `15m`, `1h`
- 启用：EMA、MACD、RSI、ATR、BOLL、Volume、OI、Funding Rate
- 关闭外部量化榜单数据，避免测试阶段依赖额外数据源

风控参数：

- 最大持仓数：`2`
- BTC/ETH 最大杠杆：`1x`
- 山寨币最大杠杆：`1x`
- 最大保证金使用率：`30%`
- 最小开仓金额：`12 USDT`
- 最低盈亏比：`2.5`
- 最低开仓置信度：`82`

注意：系统内置限制会把单仓价值倍数下限钳制到 `0.50`，所以本次保存后的实际值为：

- BTC/ETH 单仓价值倍数：`0.50`
- 山寨币单仓价值倍数：`0.50`

## 已创建交易员

交易员名称：`币安模拟盘主流币测试员`

交易员 ID：

```text
9688e0d9_9a0bb67c-7822-4007-b6de-7fbb8819af4d_openai_1779040531
```

绑定关系：

- 策略：`币安模拟盘主流币低风险策略`
- 策略 ID：`6a89d9d9-2df3-4a7e-a2dd-fff92f920d48`
- 交易所：`lianghua / Binance Futures 测试网`
- 模型：`openai / gpt-5.5`
- 扫描间隔：`5` 分钟
- 初始余额：从测试网账户读取为 `5000 USDT`
- 状态：`未启动`

创建结果：

- 创建成功
- `startup_warning` 为空
- 新交易员当前 `is_running = false`

## 发现的问题

1. 新交易员创建时提交了 `is_cross_margin = false`，但读取配置接口返回 `is_cross_margin = true`。

   这更像是线上接口字段映射、默认值覆盖或保存逻辑问题。由于本次没有启动交易员，所以不会产生实际下单影响；如果后续要正式跑模拟盘，建议先修复或确认保证金模式字段。

2. `show_in_competition = false` 提交后，列表接口仍显示 `show_in_competition = true`。

   这同样像是创建接口或列表接口字段保存/读取不一致。它不影响策略交易逻辑，但会影响展示。

## 下一步建议

建议先做 3 步：

1. 修复或确认 `is_cross_margin`、`show_in_competition` 的保存/读取一致性。
2. 再启动新的 `币安模拟盘主流币测试员`，观察 1 到 2 个小时的日志、持仓、下单记录和收益曲线。

如果要启动新交易员，请明确确认启动以下 ID：

```text
9688e0d9_9a0bb67c-7822-4007-b6de-7fbb8819af4d_openai_1779040531
```

启动后重点观察：

- 是否只扫描 `BTCUSDT`、`ETHUSDT`、`BNBUSDT`、`SOLUSDT`、`XRPUSDT`
- 是否遵守最大 2 个持仓
- 是否使用 1x 杠杆
- 是否避免频繁开平仓
- 是否有模型输出但不满足 `82` 置信度时不下单
- 是否能在数据页展示余额、持仓、订单和收益曲线
