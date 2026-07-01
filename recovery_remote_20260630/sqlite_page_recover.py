#!/usr/bin/env python3
import json
import struct
import sys
from pathlib import Path


def read_varint(buf, pos):
    value = 0
    for i in range(9):
        b = buf[pos + i]
        if i == 8:
            return (value << 8) | b, pos + 9
        value = (value << 7) | (b & 0x7F)
        if not (b & 0x80):
            return value, pos + i + 1
    raise ValueError("bad varint")


def decode_serial(buf, pos, serial_type):
    if serial_type == 0:
        return None, pos
    if serial_type == 1:
        return struct.unpack(">b", buf[pos : pos + 1])[0], pos + 1
    if serial_type == 2:
        return int.from_bytes(buf[pos : pos + 2], "big", signed=True), pos + 2
    if serial_type == 3:
        return int.from_bytes(buf[pos : pos + 3], "big", signed=True), pos + 3
    if serial_type == 4:
        return int.from_bytes(buf[pos : pos + 4], "big", signed=True), pos + 4
    if serial_type == 5:
        return int.from_bytes(buf[pos : pos + 6], "big", signed=True), pos + 6
    if serial_type == 6:
        return int.from_bytes(buf[pos : pos + 8], "big", signed=True), pos + 8
    if serial_type == 7:
        return struct.unpack(">d", buf[pos : pos + 8])[0], pos + 8
    if serial_type == 8:
        return 0, pos
    if serial_type == 9:
        return 1, pos
    if serial_type in (10, 11):
        return None, pos
    if serial_type >= 12:
        n = (serial_type - 12) // 2
        raw = buf[pos : pos + n]
        pos += n
        if serial_type % 2 == 1:
            try:
                return raw.decode("utf-8"), pos
            except UnicodeDecodeError:
                return raw.decode("utf-8", "replace"), pos
        return raw.hex(), pos
    return None, pos


def parse_record(payload):
    header_len, p = read_varint(payload, 0)
    serials = []
    while p < header_len:
        st, p = read_varint(payload, p)
        serials.append(st)
    values = []
    data_pos = header_len
    for st in serials:
        v, data_pos = decode_serial(payload, data_pos, st)
        values.append(v)
    return values


class SQLitePageReader:
    def __init__(self, path):
        self.path = Path(path)
        self.data = self.path.read_bytes()
        if self.data[:16] != b"SQLite format 3\x00":
            raise ValueError(f"{path} is not a sqlite file")
        ps = int.from_bytes(self.data[16:18], "big")
        self.page_size = 65536 if ps == 1 else ps
        self.page_count = len(self.data) // self.page_size

    def page(self, pgno):
        if pgno < 1 or pgno > self.page_count:
            raise ValueError(f"page {pgno} out of range")
        start = (pgno - 1) * self.page_size
        return self.data[start : start + self.page_size]

    def parse_page_header(self, pgno):
        page = self.page(pgno)
        off = 100 if pgno == 1 else 0
        page_type = page[off]
        first_freeblock = int.from_bytes(page[off + 1 : off + 3], "big")
        cell_count = int.from_bytes(page[off + 3 : off + 5], "big")
        cell_content = int.from_bytes(page[off + 5 : off + 7], "big")
        fragmented = page[off + 7]
        right_child = None
        ptr_start = off + 8
        if page_type in (0x02, 0x05):
            right_child = int.from_bytes(page[off + 8 : off + 12], "big")
            ptr_start = off + 12
        ptrs = []
        for i in range(cell_count):
            ptr = int.from_bytes(page[ptr_start + 2 * i : ptr_start + 2 * i + 2], "big")
            ptrs.append(ptr)
        return {
            "type": page_type,
            "first_freeblock": first_freeblock,
            "cell_count": cell_count,
            "cell_content": cell_content,
            "fragmented": fragmented,
            "right_child": right_child,
            "ptrs": ptrs,
            "header_offset": off,
        }

    def local_payload_size(self, payload_size, table_leaf=True):
        usable = self.page_size
        if table_leaf:
            max_local = usable - 35
            min_local = ((usable - 12) * 32 // 255) - 23
        else:
            max_local = ((usable - 12) * 64 // 255) - 23
            min_local = ((usable - 12) * 32 // 255) - 23
        if payload_size <= max_local:
            return payload_size
        local = min_local + ((payload_size - min_local) % (usable - 4))
        if local > max_local:
            local = min_local
        return local

    def read_overflow(self, start_pgno, remaining):
        out = bytearray()
        pgno = start_pgno
        seen = set()
        while pgno and remaining > 0 and pgno not in seen and 1 <= pgno <= self.page_count:
            seen.add(pgno)
            page = self.page(pgno)
            next_pgno = int.from_bytes(page[:4], "big")
            chunk = page[4 : 4 + min(remaining, self.page_size - 4)]
            out.extend(chunk)
            remaining -= len(chunk)
            pgno = next_pgno
        return bytes(out)

    def parse_table_leaf_cell(self, pgno, ptr):
        page = self.page(pgno)
        pos = ptr
        payload_size, pos = read_varint(page, pos)
        rowid, pos = read_varint(page, pos)
        local = self.local_payload_size(payload_size, table_leaf=True)
        payload = bytes(page[pos : pos + local])
        if payload_size > local:
            ovfl_ptr_pos = pos + local
            ovfl = int.from_bytes(page[ovfl_ptr_pos : ovfl_ptr_pos + 4], "big")
            payload += self.read_overflow(ovfl, payload_size - local)
        return rowid, parse_record(payload[:payload_size])

    def traverse_table(self, root_pgno, seen=None):
        if seen is None:
            seen = set()
        if root_pgno in seen or root_pgno < 1 or root_pgno > self.page_count:
            return []
        seen.add(root_pgno)
        hdr = self.parse_page_header(root_pgno)
        rows = []
        if hdr["type"] == 0x0D:
            for ptr in hdr["ptrs"]:
                try:
                    rows.append(self.parse_table_leaf_cell(root_pgno, ptr))
                except Exception as exc:
                    rows.append(("__error__", f"page={root_pgno} ptr={ptr}: {exc}"))
        elif hdr["type"] == 0x05:
            page = self.page(root_pgno)
            for ptr in hdr["ptrs"]:
                child = int.from_bytes(page[ptr : ptr + 4], "big")
                rows.extend(self.traverse_table(child, seen))
            if hdr["right_child"]:
                rows.extend(self.traverse_table(hdr["right_child"], seen))
        else:
            rows.append(("__error__", f"page={root_pgno}: unsupported page type {hdr['type']}"))
        return rows

    def schema(self):
        rows = self.traverse_table(1)
        out = []
        for rowid, vals in rows:
            if rowid == "__error__" or len(vals) < 5:
                continue
            out.append(
                {
                    "rowid": rowid,
                    "type": vals[0],
                    "name": vals[1],
                    "tbl_name": vals[2],
                    "rootpage": vals[3],
                    "sql": vals[4],
                }
            )
        return out


def main(argv):
    if len(argv) < 2:
        print("usage: sqlite_page_recover.py DB [ROOTPAGE...]", file=sys.stderr)
        return 2
    r = SQLitePageReader(argv[1])
    if len(argv) == 2:
        print(json.dumps({"page_size": r.page_size, "page_count": r.page_count, "schema": r.schema()}, ensure_ascii=False, indent=2))
        return 0
    for root in map(int, argv[2:]):
        rows = r.traverse_table(root)
        print(json.dumps({"rootpage": root, "rows": rows}, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))
