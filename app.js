// BSD מחשבים – בניית כרטיסי המוצרים וסינון. קישורי וואטסאפ/מייל: contact.js

function el(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text != null) e.textContent = text;
  return e;
}

function card(p) {
  const c = el("article", "card" + (p.deal ? " card-deal" : ""));
  c.dataset.cat = [p.cat, ...(p.also || [])].join(" ");

  const media = el(p.page ? "a" : "div", "card-media");
  if (p.page) media.href = p.page;
  const img = el("img");
  img.src = p.img;
  img.alt = p.name;
  img.loading = "lazy";
  img.width = 510;
  img.height = 510;
  media.append(img, el("span", "badge-cat", CATEGORIES.find(x => x.id === p.cat).name));
  if (p.deal) media.append(el("span", "badge-deal", p.deal));
  c.append(media);

  const body = el("div", "card-body");
  const meta = el("div", "card-meta");
  meta.append(el("span", "brand", p.brand), el("span", "sku", "מק\"ט: " + p.sku));
  const title = el("h3", "card-title");
  // names are LTR (Latin model names); mixed Hebrew names read better RTL
  if (/[\u0590-\u05FF]/.test(p.name)) title.style.direction = "rtl";
  if (p.page) { const t = el("a", null, p.name); t.href = p.page; title.append(t); }
  else title.textContent = p.name;
  body.append(meta, title, el("p", "tagline", p.tagline));

  const specs = el("ul", "specs");
  p.specs.forEach(s => specs.append(el("li", null, s)));
  body.append(specs);

  const incl = el("div", "included");
  incl.append(el("span", "chip chip-ok", "✓ כולל התקנות מלאות"), el("span", "chip chip-ship", "🚚 משלוח חינם"));
  body.append(incl);

  const foot = el("div", "card-foot");
  const priceRow = el("div", "price-row");
  priceRow.append(el("div", "price" + (p.deal ? " price-deal" : ""), p.price ? "₪" + p.price : "מחיר לפי פנייה"));
  if (p.page) { const more = el("a", "more-link", "לכל הפרטים ←"); more.href = p.page; priceRow.append(more); }
  foot.append(priceRow);
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

// קישור ישיר לקטגוריה: bsd-comp.com/#arch, ‏bsd-comp.com/#recommended וכו'
const SITE_URL = "https://bsd-comp.com/";

function render() {
  const grid = document.getElementById("grid");
  PRODUCTS.forEach(p => grid.append(card(p)));

  const filters = document.getElementById("filters");
  const share = el("div", "cat-share");
  share.hidden = true;
  filters.after(share);

  const all = [{ id: "all", name: "הכל" }, ...CATEGORIES];
  const buttons = {};

  function select(cat, updateUrl) {
    filters.querySelectorAll(".filter").forEach(f => { f.classList.remove("active"); f.setAttribute("aria-selected", "false"); });
    const b = buttons[cat.id];
    b.classList.add("active");
    b.setAttribute("aria-selected", "true");
    grid.querySelectorAll(".card").forEach(c => { c.hidden = cat.id !== "all" && !c.dataset.cat.split(" ").includes(cat.id); });
    if (updateUrl) history.replaceState(null, "", cat.id === "all" ? "#laptops" : "#" + cat.id);
    renderShare(cat);
  }

  function renderShare(cat) {
    share.replaceChildren();
    share.hidden = cat.id === "all";
    if (share.hidden) return;
    const url = SITE_URL + "#" + cat.id;
    const label = el("span", "cat-share-label", "קישור ישיר לקטגוריה:");
    const link = el("a", "cat-share-url", url);
    link.href = url;
    link.dir = "ltr";
    const copy = el("button", "cat-share-btn", "📋 העתקה");
    copy.type = "button";
    copy.addEventListener("click", async () => {
      try { await navigator.clipboard.writeText(url); }
      catch { const t = el("textarea"); t.value = url; document.body.append(t); t.select(); document.execCommand("copy"); t.remove(); }
      copy.textContent = "✓ הועתק";
      setTimeout(() => { copy.textContent = "📋 העתקה"; }, 1800);
    });
    const wa = el("a", "cat-share-btn cat-share-wa", "שליחה בוואטסאפ");
    wa.href = "https://wa.me/?text=" + encodeURIComponent(`${cat.name} – BSD מחשבים\n${url}`);
    wa.target = "_blank";
    wa.rel = "noopener";
    share.append(label, link, copy, wa);
  }

  all.forEach((cat, i) => {
    const b = el("button", "filter" + (i === 0 ? " active" : ""), cat.name);
    b.type = "button";
    b.setAttribute("role", "tab");
    b.setAttribute("aria-selected", i === 0 ? "true" : "false");
    const count = cat.id === "all" ? PRODUCTS.length : PRODUCTS.filter(p => p.cat === cat.id || (p.also || []).includes(cat.id)).length;
    b.append(el("span", "count", String(count)));
    b.addEventListener("click", () => select(cat, true));
    buttons[cat.id] = b;
    filters.append(b);
  });

  // opening the site with #<category> selects it and scrolls to the catalog
  function fromHash() {
    const cat = CATEGORIES.find(c => "#" + c.id === location.hash);
    if (!cat) return false;
    select(cat, false);
    document.getElementById("laptops").scrollIntoView();
    return true;
  }
  window.addEventListener("hashchange", fromHash);
  if (fromHash()) {
    // the browser may restore the old scroll position; scroll again once everything loaded
    history.scrollRestoration = "manual";
    window.addEventListener("load", () => document.getElementById("laptops").scrollIntoView());
  }
}

render();
