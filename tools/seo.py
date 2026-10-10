#!/usr/bin/env python3
"""SEO housekeeping for bsd-comp.com. Run after adding/removing pages:
    python3 tools/seo.py
- adds canonical + Open Graph tags to every page (idempotent)
- regenerates sitemap.xml and robots.txt
- wraps "2560×1600"-style numbers in LTR isolates so RTL text doesn't reverse them
"""
import datetime, html, pathlib, re, subprocess

SITE = "https://bsd-comp.com/"
ROOT = pathlib.Path(__file__).resolve().parent.parent
MARK = "<!-- seo -->"
LRI, PDI = "\u2066", "\u2069"
_num = r"\d[\d.]*(?:[–-]\d[\d.]*)?"
_dims = re.compile(rf"(?<![\d.])({_num}(?:\s*×\s*{_num})+[A-Za-z]*|\d+–\d+)(?![\d.])")

def fix_bidi(text):
    # rebuild from scratch each run (idempotent): drop old isolates, then wrap
    text = text.replace(LRI, "").replace(PDI, "")
    return _dims.sub(lambda m: LRI + m.group(1) + PDI, text)

def fix_bidi_html(src):
    head, sep, body = src.partition("<body>")
    parts = re.split(r"(<[^>]+>)", body)
    return head + sep + "".join(x if x.startswith("<") else fix_bidi(x) for x in parts)

def page_url(name):
    return SITE if name == "index.html" else SITE + name

def og_image(src):
    m = re.search(r'id="main-img" src="([^"]+)"', src)
    return SITE + (m.group(1) if m else "bsd-icon-256.png")

def lastmod(path):
    try:
        out = subprocess.run(["git", "log", "-1", "--format=%cs", "--", path.name],
                             cwd=ROOT, capture_output=True, text=True).stdout.strip()
        if out:
            return out
    except OSError:
        pass
    return datetime.date.today().isoformat()

# site-wide layout first (footer, breadcrumbs, similar products, cache versions)
import importlib.util as _ilu
_spec = _ilu.spec_from_file_location("layout", ROOT / "tools" / "layout.py"); _lay = _ilu.module_from_spec(_spec); _spec.loader.exec_module(_lay); _lay.run()
_spec2 = _ilu.spec_from_file_location("searchindex", ROOT / "tools" / "searchindex.py"); _si = _ilu.module_from_spec(_spec2); _spec2.loader.exec_module(_si); _si.run(_lay.load_products()[0].keys())

# redirect stubs (old URLs) stay out of the SEO tags and the sitemap
pages = sorted(p for p in ROOT.glob("*.html") if 'http-equiv="refresh"' not in p.read_text(encoding="utf-8"))
for p in pages:
    src = p.read_text(encoding="utf-8")
    title = html.unescape(re.search(r"<title>(.*?)</title>", src, re.S).group(1).strip())
    m = re.search(r'<meta name="description" content="(.*?)">', src, re.S)
    desc = html.unescape(m.group(1)) if m else title
    url = page_url(p.name)
    tags = "\n".join([
        f"  {MARK}",
        f'  <link rel="canonical" href="{url}">',
        '  <meta property="og:type" content="website">',
        '  <meta property="og:site_name" content="BSD מחשבים">',
        '  <meta property="og:locale" content="he_IL">',
        f'  <meta property="og:url" content="{url}">',
        f'  <meta property="og:title" content="{html.escape(title)}">',
        f'  <meta property="og:description" content="{html.escape(desc)}">',
        f'  <meta property="og:image" content="{og_image(src)}">',
        f"  {MARK}",
    ])
    # replace an earlier block, or insert before the favicon link
    src = re.sub(rf"  {re.escape(MARK)}.*?{re.escape(MARK)}", lambda _: tags, src, flags=re.S) \
        if MARK in src else src.replace('  <link rel="icon"', tags + '\n  <link rel="icon"', 1)
    p.write_text(fix_bidi_html(src), encoding="utf-8")

prod = ROOT / "products.js"
prod.write_text(fix_bidi(prod.read_text(encoding="utf-8")), encoding="utf-8")

urls = []
for p in pages:
    prio = "1.0" if p.name == "index.html" else "0.8"
    urls.append(f"  <url><loc>{page_url(p.name)}</loc><lastmod>{lastmod(p)}</lastmod><priority>{prio}</priority></url>")
(ROOT / "sitemap.xml").write_text(
    '<?xml version="1.0" encoding="UTF-8"?>\n'
    '<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n' + "\n".join(urls) + "\n</urlset>\n",
    encoding="utf-8")
(ROOT / "robots.txt").write_text(f"User-agent: *\nAllow: /\n\nSitemap: {SITE}sitemap.xml\n", encoding="utf-8")
print(f"{len(pages)} pages, sitemap.xml + robots.txt written")
