// BSD מחשבים – בניית כרטיסי המוצרים וסינון. קישורי וואטסאפ/מייל: contact.js

function el(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text != null) e.textContent = text;
  return e;
}

// lower-case, drop direction marks, quotes and punctuation so "i7 14700" finds "i7-14700HX"
function norm(t) {
  return t.toLowerCase().replace(/[\u200e\u200f"'״׳`]/g, "").replace(/[-·,:()/+]/g, " ").replace(/\s+/g, " ").trim();
}

function card(p) {
  const c = el("article", "card" + (p.deal ? " card-deal" : ""));
  c.dataset.cat = [p.cat, ...(p.also || [])].join(" ");
  c.dataset.price = p.price ? parseInt(p.price.replace(/,/g, ""), 10) : "";
  const catNames = [p.cat, ...(p.also || [])].map(id => CATEGORIES.find(x => x.id === id).name);
  c.dataset.search = norm([p.sku, p.brand, p.name, p.tagline, p.deal || "", ...p.specs, ...catNames].join(" "));
  // smart search: facts from the product page (search-index.js) + the card itself
  const idx = (typeof SEARCH_INDEX !== "undefined" && p.page && SEARCH_INDEX[p.page]) || {};
  const all = [p.name, p.tagline, ...p.specs].join(" ");
  const pick = (v, re) => v != null ? v : ((all.match(re) || [])[1] ? parseFloat(all.match(re)[1]) : null);
  if (idx.t) c.dataset.search += " " + norm(idx.t);
  c._meta = { p, t: (all + " " + (idx.t || "")).toLowerCase(), cats: [p.cat, ...(p.also || [])],
    kg: pick(idx.kg, /(\d(?:\.\d+)?)\s*ק"?ג/), ram: pick(idx.ram, /(\d{1,2})GB/), inch: pick(idx.inch, /(\d{2}(?:\.\d)?)\s*אינץ/),
    wh: idx.wh != null ? idx.wh : null, price: p.price ? parseInt(p.price.replace(/,/g, ""), 10) : null };

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

  // computers come installed + free shipping; printers and monitors don't carry those chips
  const noun = { printers: "מדפסת", monitors: "מסך" }[p.cat] || "מחשב";
  if (noun === "מחשב") {
    const incl = el("div", "included");
    incl.append(el("span", "chip chip-ok", "✓ כולל התקנות מלאות"), el("span", "chip chip-ship", "🚚 משלוח חינם"));
    body.append(incl);
  }

  // מציאון: compare with the lowest new-model price on Zap
  if (p.zap) {
    const saving = parseInt(p.zap.price.replace(/,/g, ""), 10) - parseInt(p.price.replace(/,/g, ""), 10);
    const z = el("div", "zap-line");
    z.append(el("span", null, "בזאפ (חדש): ₪" + p.zap.price));
    if (saving > 0) z.append(el("b", null, "חוסכים ₪" + saving.toLocaleString("en-US")));
    z.title = p.zap.label;
    body.append(z);
  }

  // regular products on sale: strike the market price and show the saving
  if (p.was) {
    const saving = parseInt(p.was.replace(/,/g, ""), 10) - parseInt(p.price.replace(/,/g, ""), 10);
    const w = el("div", "zap-line");
    w.append(el("span", null, "מחיר שוק: ₪" + p.was));
    if (saving > 0) w.append(el("b", null, "חוסכים ₪" + saving.toLocaleString("en-US")));
    body.append(w);
  }
  if (p.stockLow) {
    const st = el("div", "stock-low stock-low-card");
    st.append(el("span", "stock-dot"), document.createTextNode("יחידות בודדות במלאי"));
    body.append(st);
  }

  const foot = el("div", "card-foot");
  const priceRow = el("div", "price-row");
  priceRow.append(el("div", "price" + (p.deal ? " price-deal" : ""), p.price ? "₪" + p.price : "מחיר לפי פנייה"));
  if (p.page) { const more = el("a", "more-link", "לכל הפרטים ←"); more.href = p.page; priceRow.append(more); }
  foot.append(priceRow);
  const msg = `שלום, אני מתעניין/ת ב${noun} ${p.name} (מק"ט ${p.sku}). אשמח לפרטים ומחיר.`;
  const btns = el("div", "order-btns");
  const wa = el("a", "btn btn-wa", "להזמנה בוואטסאפ");
  wa.href = waLink(msg);
  wa.target = "_blank";
  wa.rel = "noopener";
  const mail = el("a", "btn btn-mail");
  mail.href = mailLink(`הזמנת ${noun}: ${p.name} (מק"ט ${p.sku})`, msg);
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

// soldOut: true hides a product from the catalog (temporarily out of stock)
// DEALS (deals.js) = the מציאון category, generated by the update-deals skill
// priced products always come first in every view (stable sort keeps the list order within each group)
const SHOWN = PRODUCTS.concat(typeof DEALS !== "undefined" ? DEALS : []).filter(p => !p.soldOut)
  .sort((a, b) => (b.price ? 1 : 0) - (a.price ? 1 : 0));

// owner mode: bsd-comp.com/?owner=1 turns on the "direct link to category" box on this device (?owner=0 turns it off)
const OWNER = (() => {
  const q = new URLSearchParams(location.search).get("owner");
  try {
    if (q === "1") localStorage.setItem("bsd-owner", "1");
    if (q === "0") localStorage.removeItem("bsd-owner");
    return localStorage.getItem("bsd-owner") === "1";
  } catch { return q === "1"; }
})();

const BUDGETS = [
  { id: "any", name: "כל התקציבים" },
  { id: "b1", name: "עד ₪2,500", min: 0, max: 2500 },
  { id: "b2", name: "\u2066₪2,500–3,500\u2069", min: 2500, max: 3500 },
  { id: "b3", name: "\u2066₪3,500–5,000\u2069", min: 3500, max: 5000 },
  { id: "b4", name: "מעל ₪5,000", min: 5000, max: Infinity },
];

function render() {
  const grid = document.getElementById("grid");
  SHOWN.forEach(p => grid.append(card(p)));

  const filters = document.getElementById("filters");

  // search box: matches model, SKU, processor, specs and category (all words must match)
  const search = el("div", "search");
  const input = el("input", "search-input");
  input.type = "search";
  input.placeholder = "חיפוש חכם: למשל \"מחשב קל עם מודם סלולרי\"";
  input.setAttribute("aria-label", "חיפוש מחשב");
  input.autocomplete = "off";
  const clear = el("button", "search-clear", "✕");
  clear.type = "button";
  clear.setAttribute("aria-label", "ניקוי החיפוש");
  clear.hidden = true;
  search.append(el("span", "search-ic", "🔍"), input, clear);
  const intro = el("p", "smart-intro");
  intro.append(el("strong", null, "✨ חיפוש חכם: "), document.createTextNode("כתבו במילים שלכם מה אתם צריכים, למשל "), el("b", null, "\"מחשב קל ללימודים\""), document.createTextNode(", ונציג את המחשבים המתאימים."));
  filters.before(intro, search);
  const tips = el("div", "smart-tips");
  tips.append(el("span", "smart-tips-label", "💡 נסו:"));
  ["מחשב קל", "עם מודם סלולרי", "ללימודי תכנות", "מסך מגע", "לעריכה ואדריכלות", "מחשב עסקי עד 3000 ש\"ח"].forEach(t => {
    const b = el("button", "smart-tip", t);
    b.type = "button";
    b.addEventListener("click", () => { input.value = t; apply(); input.focus(); });
    tips.append(b);
  });
  const understood = el("div", "smart-understood");
  understood.hidden = true;
  search.after(tips, understood);
  const empty = el("p", "no-results");
  empty.hidden = true;
  grid.after(empty);
  let current = { id: "all" };
  let budget = BUDGETS[0];
  let showUnpriced = false;

  // sort + budget toolbar (under the category buttons)
  const tools = el("div", "cat-tools");
  const sortSel = el("select", "sort-select");
  sortSel.setAttribute("aria-label", "מיון");
  [["rec", "מיון: מומלץ"], ["asc", "מחיר: מהזול ליקר"], ["desc", "מחיר: מהיקר לזול"]].forEach(([v, t]) => {
    const o = el("option", null, t); o.value = v; sortSel.append(o);
  });
  const budgets = el("div", "budgets");
  const budgetBtns = {};
  BUDGETS.forEach((b, i) => {
    const btn = el("button", "budget" + (i === 0 ? " active" : ""), b.name);
    btn.type = "button";
    btn.addEventListener("click", () => {
      budget = b;
      Object.values(budgetBtns).forEach(x => x.classList.remove("active"));
      btn.classList.add("active");
      apply();
    });
    budgetBtns[b.id] = btn;
    budgets.append(btn);
  });
  tools.append(sortSel, budgets);
  filters.after(tools);

  const order = [...grid.children];   // recommended order (priced first)
  function applyOrder() {
    const priced = c => c.dataset.price !== "";
    let list = order.slice();
    if (sortSel.value !== "rec") {
      const dir = sortSel.value === "asc" ? 1 : -1;
      list.sort((a, b) => (priced(b) - priced(a)) || (priced(a) ? dir * (a.dataset.price - b.dataset.price) : 0));
    }
    grid.append(...list);
  }
  sortSel.addEventListener("change", applyOrder);

  // models without a price are folded behind a button while the view has priced ones
  const more = el("button", "show-more");
  more.type = "button";
  more.hidden = true;
  more.addEventListener("click", () => { showUnpriced = true; apply(); });
  grid.after(more);

  function apply() {
    const words = norm(input.value).split(" ").filter(Boolean);
    const cards = [...grid.querySelectorAll(".card")];
    const base = c => {
      const inCat = current.id === "all" || c.dataset.cat.split(" ").includes(current.id);
      const price = c.dataset.price === "" ? null : +c.dataset.price;
      const inBudget = budget.id === "any" || (price !== null && price > budget.min && price <= budget.max);
      return inCat && inBudget;
    };
    const smart = smartParse(input.value);
    let match, partial = false;
    if (smart.intents.length || smart.maxPrice) {
      // smart search: score each product by how many of the understood needs it meets
      const need = smart.intents.filter(i => !i.soft);
      const scored = cards.filter(base).map(c => {
        const m = c._meta;
        if (smart.maxPrice && (m.price == null || m.price > smart.maxPrice)) return null;
        if (smart.intents.some(i => i.soft) && !smart.intents.find(i => i.soft).test(m)) return null;
        const score = need.filter(i => i.test(m)).length + smart.rest.filter(w => c.dataset.search.includes(w)).length;
        return { c, m, score };
      }).filter(Boolean);
      const required = need.length + smart.rest.length;
      const best = Math.max(0, ...scored.map(s => s.score));
      const keep = scored.filter(s => required === 0 || (best > 0 && s.score === best));
      partial = required > 0 && best < required && keep.length > 0;
      const ranker = need.find(i => i.rank);
      keep.sort((a, b) => (ranker ? ranker.rank(b.m) - ranker.rank(a.m) : 0) || ((a.m.price ?? 1e9) - (b.m.price ?? 1e9)));
      match = keep.map(s => s.c);
      grid.append(...match, ...cards.filter(c => !match.includes(c)));
      understood.replaceChildren(el("span", "su-label", partial ? "לא מצאנו מוצר שעונה על הכל. הכי קרובים ל:" : "חיפוש חכם, הבנתי:"));
      smart.intents.forEach(i => understood.append(el("span", "su-chip", i.label)));
      if (smart.maxPrice) understood.append(el("span", "su-chip", "עד ₪" + smart.maxPrice.toLocaleString("en-US")));
      smart.rest.forEach(w => understood.append(el("span", "su-chip su-word", w)));
      understood.hidden = false;
    } else {
      match = cards.filter(c => base(c) && words.every(w => c.dataset.search.includes(w)));
      understood.hidden = true;
      if (grid.dataset.smart) applyOrder();
    }
    grid.dataset.smart = understood.hidden ? "" : "1";
    const priced = match.filter(c => c.dataset.price !== "");
    // fold the "price on request" models unless searching, asked for, or nothing else to show
    const fold = !words.length && !showUnpriced && priced.length > 0;
    const visible = new Set(fold ? priced : match);
    cards.forEach(c => { c.hidden = !visible.has(c); });
    const folded = match.length - visible.size;
    more.hidden = folded === 0;
    more.textContent = `הצגת עוד ${folded} דגמים במחיר לפי פנייה ↓`;
    const shown = visible.size;
    clear.hidden = !input.value;
    empty.hidden = shown > 0;
    if (!shown) {
      const what = input.value.trim() ? ` עבור "${input.value.trim()}"` : "";
      empty.replaceChildren(`לא נמצאו מוצרים${what}${current.id !== "all" ? " בקטגוריה " + current.name : ""}${budget.id !== "any" ? " בטווח " + budget.name : ""}. `);
      const ask = el("a", null, "שאלו אותנו בוואטסאפ");
      ask.href = waLink(`שלום, אני מחפש/ת מחשב: ${input.value.trim()}`);
      ask.target = "_blank";
      ask.rel = "noopener";
      empty.append(ask);
    }
  }
  input.addEventListener("input", apply);
  clear.addEventListener("click", () => { input.value = ""; apply(); input.focus(); });

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
    current = cat;
    showUnpriced = false;
    apply();
    b.scrollIntoView({ block: "nearest", inline: "center" });
    if (updateUrl) history.replaceState(null, "", cat.id === "all" ? "#laptops" : "#" + cat.id);
    renderShare(cat);
  }

  function renderShare(cat) {
    share.replaceChildren();
    share.hidden = cat.id === "all" || !OWNER;
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
    const count = cat.id === "all" ? SHOWN.length : SHOWN.filter(p => p.cat === cat.id || (p.also || []).includes(cat.id)).length;
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
  } else {
    apply();
  }
}

render();

// homepage: a random guide teaser above the catalog (picks from the guide pages in guides.js)
(function () {
  const box = document.getElementById("guide-teaser");
  if (!box || typeof GUIDES === "undefined") return;
  const pages = GUIDES.filter(g => g.section === "guides" && g.type === "page");
  if (!pages.length) return;
  const g = pages[Math.floor(Math.random() * pages.length)];
  box.href = g.href;
  document.getElementById("gt-title").textContent = g.title;
  document.getElementById("gt-desc").textContent = g.desc;
  box.hidden = false;
})();
