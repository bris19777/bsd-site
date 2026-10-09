// BSD מחשבים – "מוצרים דומים" בתחתית עמוד מוצר (3 מוצרים מאותה קטגוריה, קרובים במחיר)
(function () {
  const box = document.getElementById("related");
  if (!box || typeof PRODUCTS === "undefined") return;
  const file = decodeURIComponent(location.pathname.split("/").pop());
  const everything = PRODUCTS.concat(typeof DEALS !== "undefined" ? DEALS : []);
  const me = everything.find(p => p.page === file);
  if (!me) return;
  const num = p => (p.price ? parseInt(p.price.replace(/,/g, ""), 10) : null);
  const mine = num(me);
  const cats = new Set([me.cat, ...(me.also || [])]);
  const list = everything
    .filter(p => p.page && p.page !== file && !p.soldOut && [p.cat, ...(p.also || [])].some(c => cats.has(c)))
    .sort((a, b) =>
      ((num(b) !== null) - (num(a) !== null)) ||
      ((b.cat === me.cat) - (a.cat === me.cat)) ||
      (mine !== null && num(a) !== null && num(b) !== null ? Math.abs(num(a) - mine) - Math.abs(num(b) - mine) : 0))
    .slice(0, 3);
  if (!list.length) return;

  const cat = CATEGORIES.find(c => c.id === me.cat);
  const noun = me.cat === "monitors" ? "מסכים" : "מחשבים";
  const make = (tag, cls, text) => { const e = document.createElement(tag); if (cls) e.className = cls; if (text != null) e.textContent = text; return e; };

  const wrap = make("div", "wrap");
  const head = make("div", "section-head");
  head.append(make("h2", null, noun + " דומים"));
  const p = make("p");
  const all = make("a", null, "לכל המוצרים בקטגוריה " + cat.name.replace(/^\W+\s*/, "") + " ←");
  all.href = "index.html#" + me.cat;
  p.append(all);
  head.append(p);
  const grid = make("div", "related-grid");
  list.forEach(r => {
    const a = make("a", "rel-card");
    a.href = r.page;
    const img = make("img");
    img.src = r.img; img.alt = r.name; img.loading = "lazy"; img.width = 300; img.height = 225;
    const body = make("div", "rel-body");
    body.append(make("small", null, r.brand + (r.deal ? " · " + r.deal : "")), make("strong", null, r.name),
      make("b", null, r.price ? "₪" + r.price : "מחיר לפי פנייה"));
    a.append(img, body);
    grid.append(a);
  });
  wrap.append(head, grid);
  box.append(wrap);
  box.hidden = false;
})();
