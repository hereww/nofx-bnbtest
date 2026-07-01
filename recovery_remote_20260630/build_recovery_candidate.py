#!/usr/bin/env python3
import json
import shutil
import sqlite3
from pathlib import Path


HERE = Path(__file__).resolve().parent
ROOT = HERE.parent
SOURCE_DB = HERE / "server-current-data-20260630.db"
RECOVERED_JSON = HERE / "recovered_rows_all.json"
OUT_DB = HERE / "data.recovered-candidate-v1.db"

OLD_USER_ID = "9a0bb67c-7822-4007-b6de-7fbb8819af4d"
EMAIL = "hereww@qq.com"
MODEL_ID = f"{OLD_USER_ID}_deepseek"
EXCHANGE_9688 = "9688e0d9-724c-42e0-9497-e4c2cd6ffbe5"
EXCHANGE_E45C = "e45c4fa2-f8ab-4bd7-a829-c6a18eee1c7e"
MISSING_STRATEGY_ETC = "07e87e1d-c966-487e-b396-0ef0dc5ece2a"
MISSING_STRATEGY_ETH = "0585063f-1c80-4e8b-bf54-7c4e8609817c"


def q(conn, sql, args=()):
    return conn.execute(sql, args).fetchall()


def load_password_hash(conn):
    row = conn.execute("select password_hash from users where email = ? limit 1", (EMAIL,)).fetchone()
    if not row or not row[0]:
        raise RuntimeError("current password hash not found")
    return row[0]


def select_traders(rows):
    by_id = {}
    for row in rows:
        vals = row["values"]
        trader_id = vals[0]
        if trader_id not in by_id:
            by_id[trader_id] = row
            continue
        # Prefer the row with the newest updated_at text; this picks the latest ETH trader edit.
        if str(vals[12]) > str(by_id[trader_id]["values"][12]):
            by_id[trader_id] = row
    return list(by_id.values())


def insert_user(conn, password_hash):
    conn.execute("delete from users")
    conn.execute(
        """
        insert into users (id,email,password_hash,created_at,updated_at)
        values (?,?,?,?,?)
        """,
        (
            OLD_USER_ID,
            EMAIL,
            password_hash,
            "2026-05-13 12:45:44.573124428+00:00",
            "2026-06-30 22:55:00+00:00",
        ),
    )


def insert_model_placeholder(conn):
    conn.execute("delete from ai_models")
    conn.execute(
        """
        insert into ai_models
        (id,user_id,name,provider,enabled,api_key,custom_api_url,custom_model_name,created_at,updated_at)
        values (?,?,?,?,?,?,?,?,?,?)
        """,
        (
            MODEL_ID,
            OLD_USER_ID,
            "DeepSeek AI (credentials need re-entry)",
            "deepseek",
            0,
            "",
            "https://api.deepseek.com",
            "deepseek-chat",
            "2026-06-21 17:04:56.579983149+00:00",
            "2026-06-30 22:55:00+00:00",
        ),
    )


def insert_exchange_placeholders(conn):
    conn.execute("delete from exchanges")
    rows = [
        (
            EXCHANGE_9688,
            "binance",
            "lianghua",
            OLD_USER_ID,
            "Binance Futures",
            "cex",
            0,
            "",
            "",
            "",
            0,
            "",
            1,
            "",
            "",
            "",
            "",
            "",
            "",
            0,
            "2026-06-21 17:04:56.579983149+00:00",
            "2026-06-30 22:55:00+00:00",
        ),
        (
            EXCHANGE_E45C,
            "binance",
            "main-eth",
            OLD_USER_ID,
            "Binance Futures",
            "cex",
            0,
            "",
            "",
            "",
            0,
            "",
            1,
            "",
            "",
            "",
            "",
            "",
            "",
            0,
            "2026-06-24 14:54:55.765434211+00:00",
            "2026-06-30 22:55:00+00:00",
        ),
    ]
    conn.executemany(
        """
        insert into exchanges
        (id,exchange_type,account_name,user_id,name,type,enabled,api_key,secret_key,passphrase,testnet,
         hyperliquid_wallet_addr,hyperliquid_unified_account,aster_user,aster_signer,aster_private_key,
         lighter_wallet_addr,lighter_private_key,lighter_api_key_private_key,lighter_api_key_index,created_at,updated_at)
        values (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
        """,
        rows,
    )


def insert_strategies(conn, rows):
    conn.execute("delete from strategies")
    configs = {}
    for row in rows:
        vals = row["values"]
        configs[vals[0]] = vals
        conn.execute(
            """
            insert or replace into strategies
            (id,user_id,name,description,is_active,is_default,is_public,config_visible,config,created_at,updated_at)
            values (?,?,?,?,?,?,?,?,?,?,?)
            """,
            vals[:11],
        )
    fallback = configs.get("1f21ab0b-4744-4b7f-802d-df7fe75d76a0") or configs.get(
        "6a89d9d9-2df3-4a7e-a2dd-fff92f920d48"
    )
    if fallback:
        for missing_id, name in [
            (MISSING_STRATEGY_ETC, "恢复占位策略-etc交易员"),
            (MISSING_STRATEGY_ETH, "恢复占位策略-量化交易主账户以太坊"),
        ]:
            vals = fallback.copy()
            vals[0] = missing_id
            vals[2] = name
            vals[3] = "原策略记录未完整恢复；为保持 trader 引用完整，暂用已恢复策略配置补齐。请在恢复后重新核对策略参数。"
            vals[4] = 0
            vals[5] = 0
            vals[6] = 0
            vals[7] = 1
            vals[9] = "2026-06-30 22:55:00+00:00"
            vals[10] = "2026-06-30 22:55:00+00:00"
            conn.execute(
                """
                insert or replace into strategies
                (id,user_id,name,description,is_active,is_default,is_public,config_visible,config,created_at,updated_at)
                values (?,?,?,?,?,?,?,?,?,?,?)
                """,
                vals[:11],
            )


def insert_traders(conn, rows):
    conn.execute("delete from traders")
    for row in select_traders(rows):
        vals = row["values"]
        vals = vals.copy()
        vals[8] = 0  # never restore into running state
        conn.execute(
            """
            insert or replace into traders
            (id,user_id,name,ai_model_id,exchange_id,strategy_id,initial_balance,scan_interval_minutes,
             is_running,is_cross_margin,show_in_competition,created_at,updated_at,btc_eth_leverage,
             altcoin_leverage,trading_symbols,use_coin_pool,use_oi_top,custom_prompt,
             override_base_prompt,system_prompt_template)
            values (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
            """,
            vals[:21],
        )


def main():
    if not SOURCE_DB.exists():
        raise SystemExit(f"missing {SOURCE_DB}")
    shutil.copy2(SOURCE_DB, OUT_DB)
    data = json.loads(RECOVERED_JSON.read_text())

    with sqlite3.connect(OUT_DB) as conn:
        password_hash = load_password_hash(conn)
        conn.execute("pragma foreign_keys=off")
        insert_user(conn, password_hash)
        insert_model_placeholder(conn)
        insert_exchange_placeholders(conn)
        insert_strategies(conn, data["strategies"])
        insert_traders(conn, data["traders"])
        conn.commit()

        checks = {}
        for table in ["users", "ai_models", "exchanges", "strategies", "traders", "telegram_configs"]:
            checks[table] = conn.execute(f"select count(*) from {table}").fetchone()[0]
        checks["integrity_check"] = conn.execute("pragma integrity_check").fetchone()[0]
        checks["trader_refs_missing_model"] = conn.execute(
            """
            select count(*) from traders t
            left join ai_models m on m.id=t.ai_model_id and m.user_id=t.user_id
            where m.id is null
            """
        ).fetchone()[0]
        checks["trader_refs_missing_exchange"] = conn.execute(
            """
            select count(*) from traders t
            left join exchanges e on e.id=t.exchange_id and e.user_id=t.user_id
            where e.id is null
            """
        ).fetchone()[0]
        checks["trader_refs_missing_strategy"] = conn.execute(
            """
            select count(*) from traders t
            left join strategies s on s.id=t.strategy_id and s.user_id=t.user_id
            where t.strategy_id != '' and s.id is null
            """
        ).fetchone()[0]
    print(json.dumps({"output": str(OUT_DB), "checks": checks}, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
