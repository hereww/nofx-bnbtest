#!/usr/bin/env python3
import json
import re
import sys
from pathlib import Path

from sqlite_page_recover import parse_record, read_varint


USER_ID = "9a0bb67c-7822-4007-b6de-7fbb8819af4d"


def iter_candidate_records(data, min_size=20, max_size=6000):
    n = len(data)
    for off in range(n - 8):
        try:
            payload_size, p = read_varint(data, off)
            if payload_size < min_size or payload_size > max_size:
                continue
            rowid, p2 = read_varint(data, p)
            if rowid < 0 or rowid > 10_000_000:
                continue
            if p2 + payload_size > n:
                continue
            payload = data[p2 : p2 + payload_size]
            header_len, hp = read_varint(payload, 0)
            if header_len <= 0 or header_len > min(payload_size, 300):
                continue
            serial_count = 0
            while hp < header_len:
                _, hp = read_varint(payload, hp)
                serial_count += 1
                if serial_count > 80:
                    break
            if serial_count > 80:
                continue
            vals = parse_record(payload)
        except Exception:
            continue
        yield off, payload_size, rowid, vals


def is_uuid(s):
    return isinstance(s, str) and bool(re.fullmatch(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}", s))


def looks_time(s):
    return isinstance(s, str) and bool(re.match(r"20\d\d-\d\d-\d\d", s))


def is_trader(vals):
    return (
        len(vals) == 21
        and isinstance(vals[0], str)
        and vals[0].endswith(tuple(["_1782061494", "_1782182060", "_1782312895"]))
        and vals[1] == USER_ID
        and isinstance(vals[2], str)
        and isinstance(vals[3], str)
        and USER_ID in vals[3]
        and is_uuid(vals[4])
        and is_uuid(vals[5])
        and isinstance(vals[6], (int, float))
        and isinstance(vals[7], int)
        and looks_time(vals[11])
        and looks_time(vals[12])
    )


def is_ai_model(vals):
    return (
        len(vals) == 10
        and isinstance(vals[0], str)
        and vals[1] in (USER_ID, "default")
        and isinstance(vals[2], str)
        and isinstance(vals[3], str)
        and vals[3] in ("deepseek", "openai", "custom", "gemini", "anthropic", "grok")
        and isinstance(vals[4], int)
        and isinstance(vals[5], str)
        and isinstance(vals[6], str)
        and isinstance(vals[7], str)
    )


def is_exchange(vals):
    return (
        len(vals) == 22
        and is_uuid(vals[0])
        and isinstance(vals[1], str)
        and isinstance(vals[2], str)
        and vals[3] in (USER_ID, "default")
        and isinstance(vals[4], str)
        and vals[5] in ("cex", "dex")
        and isinstance(vals[6], int)
    )


def is_user(vals):
    return len(vals) == 5 and vals[0] == USER_ID and isinstance(vals[1], str) and "@" in vals[1] and isinstance(vals[2], str)


def is_telegram(vals):
    return len(vals) >= 9 and vals[0] == 1 and isinstance(vals[1], str) and vals[1].startswith("bot") is False and "8814279042:" in vals[1]


def add_unique(bucket, source, off, rowid, vals):
    key = json.dumps(vals, ensure_ascii=False, sort_keys=True)
    if key not in bucket:
        bucket[key] = {"source": source, "offset": off, "page": off // 4096 + 1, "rowid": rowid, "values": vals}


def scan_file(path):
    data = Path(path).read_bytes()
    out = {"traders": {}, "ai_models": {}, "exchanges": {}, "users": {}, "telegram_configs": {}}
    needles = [
        USER_ID.encode(),
        "量化交易".encode(),
        "etc交易员".encode(),
        "测试交易".encode(),
        b"deepseek",
        b"binance",
        b"8814279042:",
    ]
    windows = []
    for needle in needles:
        idx = 0
        while True:
            idx = data.find(needle, idx)
            if idx < 0:
                break
            windows.append((max(0, idx - 6000), min(len(data), idx + 6000)))
            idx += max(1, len(needle))
    merged = []
    for start, end in sorted(windows):
        if not merged or start > merged[-1][1]:
            merged.append([start, end])
        else:
            merged[-1][1] = max(merged[-1][1], end)
    for start, end in merged:
        chunk = data[start:end]
        for rel, size, rowid, vals in iter_candidate_records(chunk, max_size=1200):
            off = start + rel
            if is_trader(vals):
                add_unique(out["traders"], str(path), off, rowid, vals)
            elif is_ai_model(vals):
                add_unique(out["ai_models"], str(path), off, rowid, vals)
            elif is_exchange(vals):
                add_unique(out["exchanges"], str(path), off, rowid, vals)
            elif is_user(vals):
                add_unique(out["users"], str(path), off, rowid, vals)
            elif is_telegram(vals):
                add_unique(out["telegram_configs"], str(path), off, rowid, vals)
    return {k: list(v.values()) for k, v in out.items()}


def main(argv):
    paths = [Path(p) for p in argv[1:]] or sorted(Path(".").glob("cand-*.db"))
    merged = {"traders": [], "ai_models": [], "exchanges": [], "users": [], "telegram_configs": []}
    seen = {k: set() for k in merged}
    for p in paths:
        one = scan_file(p)
        for table, rows in one.items():
            for row in rows:
                key = json.dumps(row["values"], ensure_ascii=False, sort_keys=True)
                if key in seen[table]:
                    continue
                seen[table].add(key)
                merged[table].append(row)
    print(json.dumps(merged, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))
