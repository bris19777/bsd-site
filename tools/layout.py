#!/usr/bin/env python3
"""Site-wide layout pass (run by seo.py, idempotent):
- full footer on every page
- breadcrumbs + "similar products" (related.js) on every product page
- keeps ?v= cache versions of shared files in sync with index.html
"""
import json, re
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
RELATED_V = "1"

def load_products():
    """page -> (category id, product name) from products.js and deals.js"""
    out, cats = {}, {}
    src = (ROOT / "products.js").read_text(encoding="utf-8")
    for m in re.finditer(r'\{ id: "(\w+)",\s*name: "([^"]+)"', src):
        cats[m.group(1)] = m.group(2)
    for chunk in src.split("{ sku:")[1:]:
        page = re.search(r'page: "([^"]+)"', chunk)
        if page:
            cat = re.search(r'cat: "(\w+)"', chunk).group(1)
            name = re.search(r'name: "((?:[^"\\]|\\.)*)"', chunk).group(1).replace('\\"', '"')
            out[page.group(1)] = (cat, name)
    deals = ROOT / "deals.js"
    if deals.exists():
        d = deals.read_text(encoding="utf-8")
        d = d[d.index("["): d.rindex("]") + 1]
        for p in json.loads(d):
            if p.get("page"):
                out[p["page"]] = (p["cat"], p["name"])
    return out, cats

FOOTER = '''<footer class="site-footer">
    <div class="wrap">
      <div class="foot-cols">
        <div class="foot-brand">
          <img src="bsd-title.png" alt="BSD מחשבים" width="205" height="30" loading="lazy">
          <p>מכירה, שירות ותמיכה טכנית למחשבים, לבית ולעסק.</p>
          <p>כל מחשב מגיע מותקן ומוכן לעבודה, עם משלוח חינם.</p>
        </div>
        <div>
          <h3>קטגוריות</h3>
          <ul>
            <li><a href="index.html#recommended">מומלצים</a></li>
            <li><a href="index.html#deals">מציאון</a></li>
            <li><a href="index.html#basic">בסיסי</a></li>
            <li><a href="index.html#home">ביתי ומשרדי</a></li>
            <li><a href="index.html#business">עסקי</a></li>
            <li><a href="index.html#gaming">גיימינג</a></li>
            <li><a href="index.html#mac">מחשבי מקינטוש</a></li>
            <li><a href="index.html#desktop">מחשבים נייחים</a></li>
            <li><a href="index.html#monitors">מסכים</a></li>
            <li><a href="index.html#printers">מדפסות</a></li>
          </ul>
        </div>
        <div>
          <h3>מידע ושירות</h3>
          <ul>
            <li><a href="guides.html">מדריכים</a></li>
            <li><a href="guide-business-vs-home.html">מחשב עסקי או ביתי?</a></li>
            <li><a href="guide-printers.html">איך בוחרים מדפסת</a></li>
            <li><a href="remote.html">תמיכה מרחוק</a></li>
            <li><a href="index.html#services">שירותים</a></li>
          </ul>
        </div>
        <div>
          <h3>צור קשר</h3>
          <ul>
            <li><a href="tel:+972544578946" dir="ltr">054-457-8946</a></li>
            <li><a href="mailto:hanan@bsd-comp.com">hanan@bsd-comp.com</a></li>
            <li><a data-wa="שלום, הגעתי מהאתר ואשמח לפרטים" data-nomail href="#">וואטסאפ</a></li>
            <li>אזור שירות: המרכז וירושלים</li>
          </ul>
        </div>
      </div>
      <div class="foot-bottom">
        <p>© <span id="year"></span> BSD מחשבים · מכירה, שירות ותמיכה · <span class="nowrap" dir="ltr">054-457-8946</span></p>
        <p class="small">התמונות להמחשה בלבד. המפרט עשוי להשתנות בהתאם ליצרן.</p>
      </div>
    </div>
  </footer>'''

SHARED = ["style.css", "contact.js", "products.js", "deals.js", "app.js", "guides.js"]

def run():
    pages, cats = load_products()
    index = (ROOT / "index.html").read_text(encoding="utf-8")
    versions = {f: m.group(1) for f in SHARED if (m := re.search(re.escape(f) + r'\?v=([\w.]+)', index))}

    for p in sorted(ROOT.glob("*.html")):
        src = p.read_text(encoding="utf-8")
        if 'http-equiv="refresh"' in src:
            continue
        orig = src
        src = re.sub(r'<footer class="site-footer">.*?</footer>', lambda _: FOOTER, src, count=1, flags=re.S)

        if p.name in pages:
            cat, name = pages[p.name]
            cname = re.sub(r"^\W+\s*", "", cats.get(cat, ""))   # drop leading emoji ("🔥 מציאון")
            crumbs = (f'<!-- crumbs --><nav class="crumbs" aria-label="מיקום באתר"><a href="index.html">ראשי</a>'
                      f'<span class="sep">‹</span><a href="index.html#{cat}">{cname}</a>'
                      f'<span class="sep">‹</span><span aria-current="page">{name}</span></nav><!-- crumbs -->')
            if "<!-- crumbs -->" in src:
                src = re.sub(r"<!-- crumbs -->.*?<!-- crumbs -->", lambda _: crumbs, src, flags=re.S)
            else:
                src = re.sub(r'<a href="index\.html#[^"]*" class="back back-dark">[^<]*</a>', lambda _: crumbs, src, count=1)
            if "<!-- related -->" not in src:
                src = src.replace("  </main>", '''    <!-- related -->
    <section class="section" id="related" hidden></section>
    <!-- related -->
  </main>''', 1)
                src = src.replace("</body>", f'''  <!-- related-js -->
  <script src="products.js?v=0"></script>
  <script src="deals.js?v=0"></script>
  <script src="related.js?v={RELATED_V}"></script>
  <!-- related-js -->
</body>''', 1)
            src = re.sub(r'related\.js\?v=\w+', f"related.js?v={RELATED_V}", src)

        for f, v in versions.items():
            src = re.sub(re.escape(f) + r'\?v=[\w.]+', f"{f}?v={v}", src)
        if src != orig:
            p.write_text(src, encoding="utf-8")

if __name__ == "__main__":
    run()
