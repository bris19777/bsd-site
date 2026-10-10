#!/usr/bin/env python3
"""Builds search-index.js: facts and spec text from every product page, for the smart search on the homepage.
Run by seo.py (after layout.py). The cache version in index.html follows the content hash."""
import hashlib, html, json, re
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent

def strip(s):
    return re.sub(r"\s+", " ", html.unescape(re.sub(r"<[^>]+>", " ", s))).strip()

def num(rx, s, cast=float):
    m = re.search(rx, s)
    return cast(m.group(1)) if m else None

def facts(src):
    main = src.split("<main>", 1)[-1].split("</main>", 1)[0]
    main = re.sub(r"<!-- related -->.*?<!-- related -->", "", main, flags=re.S)
    rows = [(strip(a), strip(b)) for a, b in re.findall(r"<tr><th>(.*?)</th><td>(.*?)</td></tr>", main, re.S)]
    bits = [strip(x) for x in re.findall(r'<(?:h1|p class="product-lead"|div class="key-banner"|ul class="highlights"|div class="feature-duo")[^>]*>(.*?)</(?:h1|p|div|ul)>', main, re.S)]
    text = " ".join(bits + [f"{a} {b}" for a, b in rows])
    row = lambda key: " ".join(b for a, b in rows if key in a)
    kg = num(r"(\d+(?:\.\d+)?)\s*ק\"?ג", row("משקל") or row("גוף"))
    if kg is None:
        g = num(r"(\d{3,4})\s*גרם", text, int)
        kg = g / 1000 if g else num(r"(\d(?:\.\d+)?)\s*ק\"ג", text)
    ram = num(r"(\d{1,2})\s*GB", row("זיכרון"), int)
    inch = num(r"(\d{2}(?:\.\d)?)\s*(?:אינץ|\")", row("מסך") or text)
    wh = num(r"(\d{2,3}(?:\.\d)?)\s*Wh", row("סוללה") or text)
    out = {"t": re.sub(r"[‎‏⁦-⁩]", "", text)[:1600]}
    for k, v in (("kg", kg), ("ram", ram), ("inch", inch), ("wh", wh)):
        if v is not None and (k != "kg" or 0.5 < v < 5):
            out[k] = v
    return out

def run(pages):
    index = {}
    for page in sorted(pages):
        p = ROOT / page
        if p.exists():
            index[page] = facts(p.read_text(encoding="utf-8"))
    body = "// אינדקס לחיפוש החכם: נוצר אוטומטית על ידי tools/searchindex.py. לא לערוך ידנית.\nconst SEARCH_INDEX = " + json.dumps(index, ensure_ascii=False, separators=(",", ":")) + ";\n"
    (ROOT / "search-index.js").write_text(body, encoding="utf-8")
    ver = hashlib.md5(body.encode()).hexdigest()[:8]
    idx = ROOT / "index.html"
    s = idx.read_text(encoding="utf-8")
    s2 = re.sub(r"search-index\.js\?v=\w+", f"search-index.js?v={ver}", s)
    if s2 != s:
        idx.write_text(s2, encoding="utf-8")
    return len(index)
