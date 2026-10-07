#!/usr/bin/env python3
"""SEO housekeeping for bsd-comp.com. Run after adding/removing pages:
    python3 tools/seo.py
- adds canonical + Open Graph tags to every page (idempotent)
- regenerates sitemap.xml and robots.txt
"""
import datetime, html, pathlib, re, subprocess

SITE = "https://bsd-comp.com/"
ROOT = pathlib.Path(__file__).resolve().parent.parent
MARK = "<!-- seo -->"

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

pages = sorted(ROOT.glob("*.html"))
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
    p.write_text(src, encoding="utf-8")

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
