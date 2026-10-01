// BSD מחשבים – בניית כרטיסי המוצרים, סינון וקישורי וואטסאפ.
const WA_NUMBER = "972544578946";

function waLink(text) {
  return "https://wa.me/" + WA_NUMBER + "?text=" + encodeURIComponent(text);
}

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
  body.append(el("div", "brand", p.brand), el("h3", "card-title", p.name), el("p", "tagline", p.tagline));

  const specs = el("ul", "specs");
  p.specs.forEach(s => specs.append(el("li", null, s)));
  body.append(specs);

  const incl = el("div", "included");
  incl.append(el("span", "chip chip-ok", "✓ כולל התקנות מלאות"), el("span", "chip chip-ship", "🚚 משלוח חינם"));
  body.append(incl);

  const foot = el("div", "card-foot");
  foot.append(el("div", "price", p.price ? "₪" + p.price : "מחיר לפי פנייה"));
  const btn = el("a", "btn btn-wa", "להזמנה בוואטסאפ");
  btn.href = waLink(`שלום, אני מתעניין/ת במחשב ${p.name} (מק"ט ${p.sku}). אשמח לפרטים ומחיר.`);
  btn.target = "_blank";
  btn.rel = "noopener";
  foot.append(btn);
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

  document.querySelectorAll("[data-wa]").forEach(a => {
    a.href = waLink(a.dataset.wa);
    a.target = "_blank";
    a.rel = "noopener";
  });
  document.getElementById("year").textContent = new Date().getFullYear();
}

render();
