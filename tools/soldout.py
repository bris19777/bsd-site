#!/usr/bin/env python3
"""Mark product pages "חסר זמנית במלאי" (temporarily out of stock), without a price.

    python3 tools/soldout.py <category-id> <page.html> [<page.html> ...]

For each page: title/description/badge say out of stock, the price box shows "חסר זמנית במלאי",
the struck market price and cash/installment boxes are removed, a notice links to the category,
and the order button becomes "עדכנו אותי כשיחזור". In products.js the card gets soldOut: true
(hidden from the catalog) and the old price is kept in a comment for restoring.
Run tools/seo.py afterwards and bump products.js?v= in index.html.
"""
import re
import sys
from pathlib import Path

SITE = Path(__file__).resolve().parent.parent
CAT_NAMES = {"gaming": "שאר מחשבי הגיימינג", "business": "שאר המחשבים העסקיים", "home": "שאר המחשבים הביתיים",
             "basic": "שאר המחשבים הבסיסיים", "premium": "שאר מחשבי הפרימיום", "desktop": "שאר המחשבים הנייחים",
             "refurbished": "שאר המחשבים המחודשים", "deals": "שאר מחשבי המציאון"}


def mark_page(f, cat):
    s = (SITE / f).read_text(encoding="utf-8")
    m = re.search(r'<div class="price-big[^"]*">₪([\d,]+)</div>', s)
    if not m:
        print(f, ": no price found (already out of stock?)")
        return None
    price = m.group(1)
    s = s.replace(f"– ₪{price} – BSD מחשבים", "– חסר זמנית במלאי – BSD מחשבים")
    s = re.sub(r'(<meta name="description" content="[^"]*?)₪' + re.escape(price) + r'[^".]*', r"\1חסר זמנית במלאי", s, 1)
    s = re.sub(r'<span class="badge-deal">[^<]*</span>', '<span class="badge-deal">חסר זמנית במלאי</span>', s, 1)
    note = (f'<div class="soldout-note"><strong>חסר זמנית במלאי.</strong> אפשר להשאיר פרטים בוואטסאפ ונעדכן כשיחזור, '
            f'או לראות את <a href="index.html#{cat}">{CAT_NAMES.get(cat, "שאר המחשבים")}</a>.</div>')
    pm = re.search(r"\n(\s*)<div class=\"price-box", s)
    s = s[:pm.start()] + "\n" + pm.group(1) + note + s[pm.start():]
    s = re.sub(r'<div class="price-label( price-label-plain)?">[^<]*</div>',
               lambda x: f'<div class="price-label{x.group(1) or ""}">זמינות</div>', s, 1)
    s = re.sub(r'<div class="price-big( price-big-plain)?"[^>]*>₪' + re.escape(price) + "</div>",
               lambda x: f'<div class="price-big{x.group(1) or ""}" style="font-size:28px">חסר זמנית במלאי</div>', s, 1)
    s = re.sub(r"\n\s*<div class=\"price-was\">.*?</div>", "", s)
    s = re.sub(r"\n\s*<div class=\"price-extras\"[^>]*>.*?\n\s*</div>", "", s, 1, flags=re.S)
    s = re.sub(r"\n\s*<div class=\"stock-low\">.*?</div>", "", s)
    name = re.sub(r"<[^>]+>", "", re.search(r"<h1>(.*?)<br>", s).group(1)).strip()
    s = re.sub(r"<a class=\"btn btn-wa btn-lg\" data-wa='[^']*' data-subject='[^']*' data-mail-label=\"[^\"]*\" href=\"#\">[^<]*</a>",
               f"<a class=\"btn btn-wa btn-lg\" data-wa='שלום, אשמח שתעדכנו אותי כש-{name} יחזור למלאי' data-subject='עדכון מלאי: {name}' "
               f"data-mail-label=\"לעדכון במייל\" href=\"#\">עדכנו אותי כשיחזור</a>", s, 1)
    (SITE / f).write_text(s, encoding="utf-8")
    return price


def mark_card(f, price):
    pj = SITE / "products.js"
    t = pj.read_text(encoding="utf-8")
    new, n = re.subn(r'page: "' + re.escape(f) + r'",(\s*)price: "' + re.escape(price) + r'",',
                     lambda x: f'page: "{f}",{x.group(1)}soldOut: true, // חסר זמנית במלאי: להחזיר price: "{price}" ולמחוק את soldOut\n   ', t, 1)
    if not n:
        print(f, ": card pattern not found in products.js")
        return
    pj.write_text(new, encoding="utf-8")


if __name__ == "__main__":
    cat, pages = sys.argv[1], sys.argv[2:]
    for f in pages:
        price = mark_page(f, cat)
        if price:
            mark_card(f, price)
            print(f, ": out of stock (was ₪" + price + ")")
