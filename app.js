// BSD מחשבים – בניית כרטיסי המוצרים וסינון. קישורי וואטסאפ/מייל: contact.js

function el(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text != null) e.textContent = text;
  return e;
}

function card(p) {
  const c = el("article", "card");
  c.dataset.cat = p.cat;

  const media = el("div", "card-media");
  const img = el("img");
  img.src = p.img;
  img.alt = p.name;
  img.loading = "lazy";
  img.width = 510;
  img.height = 510;
  media.append(img, el("span", "badge-cat", CATEGORIES.find(x => x.id === p.cat).name));
  c.append(media);

  const body = el("div", "card-body");
  const meta = el("div", "card-meta");
  meta.append(el("span", "brand", p.brand), el("span", "sku", "מק\"ט: " + p.sku));
  body.append(meta, el("h3", "card-title", p.name), el("p", "tagline", p.tagline));

  const specs = el("ul", "specs");
  p.specs.forEach(s => specs.append(el("li", null, s)));
  body.append(specs);

  const incl = el("div", "included");
  incl.append(el("span", "chip chip-ok", "✓ כולל התקנות מלאות"), el("span", "chip chip-ship", "🚚 משלוח חינם"));
  body.append(incl);

  const foot = el("div", "card-foot");
  foot.append(el("div", "price", p.price ? "₪" + p.price : "מחיר לפי פנייה"));
  const msg = `שלום, אני מתעניין/ת במחשב ${p.name} (מק"ט ${p.sku}). אשמח לפרטים ומחיר.`;
  const btns = el("div", "order-btns");
  const wa = el("a", "btn btn-wa", "להזמנה בוואטסאפ");
  wa.href = waLink(msg);
  wa.target = "_blank";
  wa.rel = "noopener";
  const mail = el("a", "btn btn-mail");
  mail.href = mailLink(`הזמנת מחשב: ${p.name} (מק"ט ${p.sku})`, msg);
  mail.innerHTML = MAIL_ICON;
  mail.append(el("span", null, "להזמנה במייל"));
  btns.append(wa, mail);
  foot.append(btns);
  body.append(foot);

  c.append(body);
  return c;
}

function render() {
  const grid = document.getElementById("grid");
  PRODUCTS.forEach(p => grid.append(card(p)));

  const filters = document.getElementById("filters");
  const all = [{ id: "all", name: "הכל" }, ...CATEGORIES];
  all.forEach((cat, i) => {
    const b = el("button", "filter" + (i === 0 ? " active" : ""), cat.name);
    b.type = "button";
    b.setAttribute("role", "tab");
    b.setAttribute("aria-selected", i === 0 ? "true" : "false");
    const count = cat.id === "all" ? PRODUCTS.length : PRODUCTS.filter(p => p.cat === cat.id).length;
    b.append(el("span", "count", String(count)));
    b.addEventListener("click", () => {
      filters.querySelectorAll(".filter").forEach(f => { f.classList.remove("active"); f.setAttribute("aria-selected", "false"); });
      b.classList.add("active");
      b.setAttribute("aria-selected", "true");
      grid.querySelectorAll(".card").forEach(c => { c.hidden = cat.id !== "all" && c.dataset.cat !== cat.id; });
    });
    filters.append(b);
  });
}

render();
