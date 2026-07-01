import os, struct, subprocess, tempfile
DEVICE='/dev/mapper/cs-root'
MAGIC=b'SQLite format 3\x00'
NEEDLES=[b'traders', b'exchanges', b'ai_models', b'strategies', b'trader_equity_snapshots']
MAX_CANDIDATES=4000
CHUNK=1024*1024
WINDOW=2*1024*1024
hits=[]
with open(DEVICE,'rb', buffering=0) as f:
    offset=0
    while len(hits)<MAX_CANDIDATES:
        data=f.read(CHUNK)
        if not data: break
        start=0
        while True:
            i=data.find(MAGIC,start)
            if i<0: break
            hits.append(offset+i)
            start=i+1
        # overlap
        if len(data)==CHUNK:
            f.seek(offset+CHUNK-len(MAGIC)+1)
            offset=offset+CHUNK-len(MAGIC)+1
        else:
            offset += len(data)
print('headers', len(hits))
results=[]
with open(DEVICE,'rb', buffering=0) as f:
    for off in hits:
        f.seek(off)
        data=f.read(WINDOW)
        score=sum(1 for n in NEEDLES if n in data)
        if score:
            page_size=0
            if len(data)>=18:
                page_size=struct.unpack('>H', data[16:18])[0]
                if page_size==1: page_size=65536
            page_count=None
            if len(data)>=32:
                page_count=struct.unpack('>I', data[28:32])[0]
            est=(page_size or 0)*(page_count or 0)
            results.append((score, est, page_size, page_count, off, [n.decode() for n in NEEDLES if n in data]))
for row in sorted(results, reverse=True)[:80]:
    print(row)
