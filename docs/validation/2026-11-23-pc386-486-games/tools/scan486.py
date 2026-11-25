import zipfile, re, sys, os
G = "/mnt/emuhd/Games/PC DOS/Collections/eXoDOS 6.04 Full/eXoDOS/eXo/eXoDOS"
PAT = re.compile(rb"(requires?|need[s]?)[^\n\x00]{0,20}(486|pentium)|486 or (better|higher|faster)|486[^\n\x00]{0,12}(required|processor needed)|(386|486)[^\n\x00]{0,30}(not supported|too slow)", re.I)
SKIP = re.compile(r"setup|install|dos4gw|uninst|dosbox|patch|readme|ultramid|pmw|cwsdpmi", re.I)
for t in sys.argv[1:]:
    z = zipfile.ZipFile(os.path.join(G, t + ".zip"))
    exes = [n for n in z.namelist() if n.lower().endswith(".exe") and not SKIP.search(n)]
    hits = set()
    for n in exes:
        if z.getinfo(n).file_size > 20_000_000: continue
        for m in PAT.finditer(z.read(n)):
            s = z.read(n)[max(0, m.start()-30):m.end()+30]
            hits.add((n.split('/')[-1], re.sub(rb"[^\x20-\x7e]", b".", s).decode()))
    print(f"{t} [{len(exes)} exe]:", "; ".join(f"{a}: {b}" for a, b in sorted(hits)[:4]) or "-")
