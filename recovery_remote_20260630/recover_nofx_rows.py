#!/usr/bin/env python3
import json
import re
import sys
from pathlib import Path

from sqlite_page_recover import SQLitePageReader, parse_record, read_varint


USER_ID = "9a0bb67c-7822-4007-b6de-7fbb8819af4d"
NEEDLES = [
    USER_ID,
    f"{USER_ID}_deepseek",
    "9688e0d9-724c-42e0-9497-e4c2cd6ffbe5",
    "e45c4fa2-f8ab-4bd7-a829-c6a18eee1c7e",
    "9688e0d9_9a0bb67c-7822-4007-b6de-7fbb8819af4d_deepseek",
    "e45c4fa2_9a0bb67c-7822-4007-b6de-7fbb8819af4d_deepseek",
    "6a89d9d9-2df3-4a7e-a2dd-fff92f920d48",
    "07e87e1d-c966-487e-b396-0ef0dc5ece2a",
    "0585063f-1c80-4e8b-bf54-7c4e8609817c",
    "hereww@qq.com",
    "deepseek",
    "binance",
    "测试交易",
    "etc交易员",
    "量化交易主账户以太坊",
    "8814279042:",
]

WINDOW_NEEDLES = [
    f"{USER_ID}_deepseek",
    "9688e0d9-724c-42e0-9497-e4c2cd6ffbe5",
    "e45c4fa2-f8ab-4bd7-a829-c6a18eee1c7e",
    "9688e0d9_9a0bb67c-7822-4007-b6de-7fbb8819af4d_deepseek",
    "e45c4fa2_9a0bb67c-7822-4007-b6de-7fbb8819af4d_deepseek",
    "6a89d9d9-2df3-4a7e-a2dd-fff92f920d48",
    "07e87e1d-c966-487e-b396-0ef0dc5ece2a",
    "0585063f-1c80-4e8b-bf54-7c4e8609817c",
    "hereww@qq.com",
    "测试交易",
    "etc交易员",
    "量化交易主账户以太坊",
    "8814279042:",
]

PAGE_NEEDLES = [
    USER_ID,
    *WINDOW_NEEDLES,
]


def is_uuid(v):
    return isinstance(v, str) and re.fullmatch(
        r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}", v
    )


def looks_time(v):
    return isinstance(v, str) and re.match(r"20\d\d-\d\d-\d\d", v)


def values_text(vals):
    return "\n".join(v for v in vals if isinstance(v, str))


def classify(vals):
    if len(vals) == 5 and vals[0] == USER_ID and isinstance(vals[1], str) and "@" in vals[1]:
        return "users"
    if (
        len(vals) == 10
        and isinstance(vals[0], str)
        and vals[1] in (USER_ID, "default")
        and isinstance(vals[2], str)
        and vals[3] in ("deepseek", "openai", "custom", "gemini", "anthropic", "grok")
    ):
        return "ai_models"
    if (
        len(vals) == 22
        and is_uuid(vals[0])
        and vals[3] in (USER_ID, "default")
        and vals[5] in ("cex", "dex")
    ):
        return "exchanges"
    if (
        len(vals) == 21
        and isinstance(vals[0], str)
        and vals[1] == USER_ID
        and isinstance(vals[2], str)
        and isinstance(vals[3], str)
        and is_uuid(vals[4])
        and is_uuid(vals[5])
    ):
        return "traders"
    if (
        len(vals) == 11
        and is_uuid(vals[0])
        and vals[1] in (USER_ID, "default", "")
        and isinstance(vals[2], str)
        and isinstance(vals[8], str)
        and vals[8].startswith("{")
    ):
        return "strategies"
    if len(vals) == 9 and isinstance(vals[1], str) and isinstance(vals[2], int):
        text = values_text(vals)
        if "8814279042:" in text or "telegram" in text.lower():
            return "telegram_configs"
    return None


def interesting(vals):
    text = values_text(vals)
    return any(n in text for n in NEEDLES)


def add(out, table, source, page, offset, rowid, vals):
    key = json.dumps(vals, ensure_ascii=False, sort_keys=True)
    if key in out["_seen"][table]:
        return
    out["_seen"][table].add(key)
    out[table].append(
        {
            "source": str(source),
            "page": page,
            "offset": offset,
            "rowid": rowid,
            "values": vals,
        }
    )


def target_pages(reader, needles, radius=2):
    data = reader.data
    pages = set()
    for needle in needles:
        raw = needle.encode()
        idx = 0
        while True:
            idx = data.find(raw, idx)
            if idx < 0:
                break
            pg = idx // reader.page_size + 1
            for near in range(max(1, pg - radius), min(reader.page_count, pg + radius) + 1):
                pages.add(near)
            idx += max(1, len(raw))
    return sorted(pages)


def scan_leaf_pages(path, out, pages=None):
    try:
        reader = SQLitePageReader(path)
    except Exception as exc:
        print(f"{path}: open failed: {exc}", file=sys.stderr)
        return
    if pages is None:
        pages = range(1, reader.page_count + 1)
    for pgno in pages:
        try:
            hdr = reader.parse_page_header(pgno)
        except Exception:
            continue
        if hdr["type"] != 0x0D or hdr["cell_count"] < 1 or hdr["cell_count"] > 300:
            continue
        for ptr in hdr["ptrs"]:
            if ptr <= 0 or ptr >= reader.page_size:
                continue
            try:
                rowid, vals = reader.parse_table_leaf_cell(pgno, ptr)
            except Exception:
                continue
            if not isinstance(vals, list) or not interesting(vals):
                continue
            table = classify(vals)
            if table:
                add(out, table, path, pgno, ptr, rowid, vals)


def iter_raw_records(buf, min_size=20, max_size=80000):
    n = len(buf)
    for off in range(max(0, n - 1)):
        try:
            payload_size, p = read_varint(buf, off)
            if payload_size < min_size or payload_size > max_size:
                continue
            rowid, p2 = read_varint(buf, p)
            if p2 + payload_size > n:
                continue
            vals = parse_record(buf[p2 : p2 + payload_size])
        except Exception:
            continue
        yield off, rowid, vals


def scan_raw_target_pages(path, out, pages):
    data = Path(path).read_bytes()
    for pgno in pages:
        start = (pgno - 1) * 4096
        chunk = data[start : start + 4096]
        for rel, rowid, vals in iter_raw_records(chunk):
            if not isinstance(vals, list) or not interesting(vals):
                continue
            table = classify(vals)
            if table:
                add(out, table, path, pgno, start + rel, rowid, vals)


def scan_needle_windows(path, out):
    data = Path(path).read_bytes()
    windows = []
    for needle in WINDOW_NEEDLES:
        raw = needle.encode()
        idx = 0
        while True:
            idx = data.find(raw, idx)
            if idx < 0:
                break
            windows.append((max(0, idx - 20000), min(len(data), idx + 20000)))
            idx += max(1, len(raw))
    merged = []
    for start, end in sorted(windows):
        if not merged or start > merged[-1][1]:
            merged.append([start, end])
        else:
            merged[-1][1] = max(merged[-1][1], end)
    for start, end in merged:
        chunk = data[start:end]
        for rel, rowid, vals in iter_raw_records(chunk):
            if not isinstance(vals, list) or not interesting(vals):
                continue
            table = classify(vals)
            if table:
                page = (start + rel) // 4096 + 1
                add(out, table, path, page, start + rel, rowid, vals)


def make_out():
    tables = ["users", "ai_models", "exchanges", "traders", "strategies", "telegram_configs"]
    return {**{t: [] for t in tables}, "_seen": {t: set() for t in tables}}


def strip_seen(out):
    out.pop("_seen", None)
    return out


def main(argv):
    paths = [Path(p) for p in argv[1:]] or sorted(Path(".").glob("cand-?.db"))
    out = make_out()
    for path in paths:
        print(f"scanning {path}", file=sys.stderr)
        try:
            reader = SQLitePageReader(path)
            pages = target_pages(reader, PAGE_NEEDLES)
            print(f"target pages: {len(pages)} / {reader.page_count}", file=sys.stderr)
        except Exception:
            pages = None
        scan_leaf_pages(path, out, pages)
        if pages:
            scan_raw_target_pages(path, out, pages)
        if "--with-windows" in argv:
            scan_needle_windows(path, out)
    print(json.dumps(strip_seen(out), ensure_ascii=False, indent=2))


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))
