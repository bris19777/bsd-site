#!/usr/bin/env python3
"""Build a standard product page (and its products.js card) from a dict.

Template: the ASUS Vivobook 14 page. Used from small one-off scripts:

    import sys; sys.path.insert(0, "tools"); import productpage as PP
    PP.build(p); PP.add_card(p)

p keys: file, sku, cat, also, brand, card, img, n, price, badge, title, desc, meta, h1, h1s, alt,
lead, duo [(icon, strong, big, small)], hl [(icon, strong, small)], chips [first = warranty],
wa_q, subj, suit [(icon, h3, p)], note, specs [(caption, [(th, td)])], cardtag, cardspecs.
Optional: was (struck market price on the card), no_install (accessories/monitors: no install chips).
seo.py fills crumbs, footer, search strip and cache versions afterwards.
"""
import re
from pathlib import Path

SITE = Path(__file__).resolve().parent.parent
TPL_FILE = "laptop-asus-vivobook14-x1404va-i3-1315u.html"


def rows(items):
    return "\n".join(f"            <tr><th>{k}</th><td>{v}</td></tr>" for k, v in items)


def build(p):
    t = (SITE / TPL_FILE).read_text(encoding="utf-8")
    title = f"{p['title']} – ₪{p['price']} – BSD מחשבים"
    t = re.sub(r"<title>.*?</title>", lambda _: f"<title>{title}</title>", t, 1)
    t = re.sub(r'<meta name="description" content=".*?">',
               lambda _: f'<meta name="description" content="{p["desc"].replace(chr(34), "&quot;")}">', t, 1)
    t = re.sub(r"<!-- seo -->.*?<!-- seo -->\n\s*", "", t, 1, flags=re.S)
    t = re.sub(r"  <!-- topsearch -->.*?<!-- topsearch -->\n", "", t, 1, flags=re.S)
    t = re.sub(r"<a class=\"btn btn-wa header-cta\" data-wa='[^']*' data-subject='[^']*'",
               lambda _: f"<a class=\"btn btn-wa header-cta\" data-wa='שלום, אני מתעניין/ת ב-{p['card']} (מק\"ט {p['sku']})' data-subject='{p['subj']}'", t, 1)
    duo = "\n".join(f'''              <div class="feature-big">
                <span class="fb-ic">{i}</span>
                <strong>{a}</strong>
                <b>{b}</b>
                <small>{c}</small>
              </div>''' for i, a, b, c in p["duo"])
    hl = "\n".join(f'              <li><span class="hl-ic">{i}</span><div><strong>{a}</strong><small>{b}</small></div></li>' for i, a, b in p["hl"])
    chip_list = [f'                <span class="chip chip-war">{p["chips"][0]}</span>']
    if not p.get("no_install"):
        chip_list += ['                <span class="chip chip-ok">✓ כולל התקנות מלאות</span>',
                      '                <span class="chip chip-ship">🚚 משלוח חינם</span>']
    chip_list += [f'                <span class="chip chip-war">{c}</span>' for c in p["chips"][1:]]
    chips = "\n".join(chip_list)
    suit = "\n".join(f'          <div class="rel-item"><div class="rel-ic">{i}</div><h3>{a}</h3><p>{b}</p></div>' for i, a, b in p["suit"])
    specs = "\n".join(f'''          <table class="spec">
            <caption>{cap}</caption>
{rows(items)}
          </table>''' for cap, items in p["specs"])
    label = "המחיר שלנו · כולל מע״מ" + ("" if p.get("no_install") else " והתקנות")
    main = f'''<main>
    <section class="section product-top">
      <div class="wrap">
        <!-- crumbs --><!-- crumbs -->
        <div class="product">
          <div class="gallery">
            <div class="gallery-main">
              <img id="main-img" src="images/{p['img']}/01.jpg" alt="{p['alt']}" width="800" height="800">
              <span class="badge-deal">{p['badge']}</span>
            </div>
            <div class="thumbs" id="thumbs"></div>
          </div>

          <div class="product-info">
            <div class="card-meta"><span class="brand">{p['meta']}</span><span class="sku">מק"ט: {p['sku']}</span></div>
            <h1>{p['h1']}<br><span>{p['h1s']}</span></h1>
            <p class="product-lead">{p['lead']}</p>

            <div class="feature-duo">
{duo}
            </div>

            <ul class="highlights">
{hl}
            </ul>

            <div class="price-box price-box-plain">
              <div>
                <div class="price-label price-label-plain">{label}</div>
                <div class="price-big price-big-plain">₪{p['price']}</div>{was_line(p)}
              </div>
              <div class="included">
{chips}
              </div>
              <div class="order-btns order-btns-lg">
                <a class="btn btn-wa btn-lg" data-wa='שלום, אני רוצה להזמין {p['wa_q']} במחיר ₪{p['price']}' data-subject='הזמנה: {p['subj']}' data-mail-label="להזמנה במייל" href="#">להזמנה בוואטסאפ</a>
              </div>
            </div>
          </div>
        </div>
      </div>
    </section>

    <section class="section">
      <div class="wrap">
        <div class="section-head">
          <h2>למי זה מתאים?</h2>
        </div>
        <div class="reliability">
{suit}
        </div>
        <p class="spec-note">{p['note']}</p>
      </div>
    </section>

    <section class="section section-alt">
      <div class="wrap">
        <div class="section-head">
          <h2>מפרט</h2>
        </div>
        <div class="spec-grid">
{specs}
        </div>
        <p class="spec-note">המפרט לפי נתוני היצרן ({p['brand']}) והיבואן ועשוי להשתנות. התמונות להמחשה בלבד.</p>
      </div>
    </section>
    '''
    a, b = t.index("<main>"), t.index("<!-- related -->")
    t = t[:a] + main + t[b:]
    imgs = ", ".join(f'"{i:02d}"' for i in range(1, p["n"] + 1))
    t = re.sub(r'const IMGS = \[.*?\]\.map\(n => "images/[^"]+/" \+ n \+ "\.jpg"\);',
               lambda _: f'const IMGS = [{imgs}].map(n => "images/{p["img"]}/" + n + ".jpg");', t, 1)
    assert f'images/{p["img"]}/" + n' in t
    (SITE / p["file"]).write_text(t, encoding="utf-8")


def was_line(p):
    if not p.get("was"):
        return ""
    save = int(p["was"].replace(",", "")) - int(p["price"].replace(",", ""))
    label = p.get("was_label", "מחיר שוק")
    return f'\n                <div class="price-was">{label}: <s>₪{p["was"]}</s><b>חוסכים ₪{save:,}</b></div>'


def card(p):
    also = f', also: {p["also"]}' if p.get("also") else ""
    was = f', was: "{p["was"]}"' if p.get("was") else ""
    specs = ", ".join(f'"{s}"' for s in p["cardspecs"])
    return (f'  {{ sku: "{p["sku"]}", cat: "{p["cat"]}"{also}, brand: "{p["brand"]}", name: "{p["card"]}",\n'
            f'    img: "images/{p["img"]}/01.jpg", page: "{p["file"]}", price: "{p["price"]}"{was}, deal: "{p["badge"]}",\n'
            f'    tagline: "{p["cardtag"]}",\n'
            f'    specs: [{specs}] }},\n')


def add_card(p):
    pj = SITE / "products.js"
    s = pj.read_text(encoding="utf-8")
    if f'page: "{p["file"]}"' in s:
        return False
    i = s.index("];", s.index("const PRODUCTS"))
    pj.write_text(s[:i] + card(p) + s[i:], encoding="utf-8")
    return True
