// BSD מחשבים – קישורי יצירת קשר משותפים לכל העמודים.
// כל אלמנט עם data-wa="טקסט" מקבל קישור וואטסאפ, ולידו נוצר אוטומטית כפתור מייל תואם.
//   data-subject="..."  נושא המייל (ברירת מחדל: "פנייה מאתר BSD מחשבים")
//   data-mail-label="..." טקסט כפתור המייל (ברירת מחדל: לפי טקסט כפתור הוואטסאפ)
//   data-nomail         בלי כפתור מייל
const WA_NUMBER = "972544578946";
const MAIL_TO = "hanan@bsd-comp.com";
const MAIL_DEFAULT_SUBJECT = "פנייה מאתר BSD מחשבים";

function waLink(text) {
  return "https://wa.me/" + WA_NUMBER + "?text=" + encodeURIComponent(text);
}

function mailLink(subject, text) {
  const body = text + "\n\nשם:\nטלפון:\n";
  return "mailto:" + MAIL_TO + "?subject=" + encodeURIComponent(subject) + "&body=" + encodeURIComponent(body);
}

const MAIL_ICON = '<svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true"><path fill="currentColor" d="M20 4H4a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2V6a2 2 0 0 0-2-2zm0 4.2-8 5-8-5V6l8 5 8-5v2.2z"/></svg>';

// "ייעוץ בוואטסאפ" -> "ייעוץ במייל", "וואטסאפ" -> "מייל"
function mailLabel(waText) {
  const t = waText.trim();
  if (t.includes("בוואטסאפ")) return t.replace("בוואטסאפ", "במייל");
  if (t === "וואטסאפ") return "מייל";
  return "במייל";
}

function wireContactLinks(root) {
  (root || document).querySelectorAll("[data-wa]").forEach(a => {
    if (a.dataset.wired) return;
    a.dataset.wired = "1";
    a.href = waLink(a.dataset.wa);
    a.target = "_blank";
    a.rel = "noopener";
    if (a.hasAttribute("data-nomail")) return;

    const subject = a.dataset.subject || MAIL_DEFAULT_SUBJECT;
    const m = document.createElement("a");
    m.href = mailLink(subject, a.dataset.wa);

    if (a.classList.contains("wa-float")) {
      return;   // one floating bubble only (WhatsApp); email buttons are in the header
    } else if (a.classList.contains("btn")) {
      m.className = a.className.replace("btn-wa", "btn-mail");
      m.innerHTML = MAIL_ICON + "<span></span>";
      m.querySelector("span").textContent = a.dataset.mailLabel || mailLabel(a.textContent);
      a.after(m);
    } else {
      // inline text link: "... בוואטסאפ או במייל"
      m.textContent = "במייל";
      a.after(document.createTextNode(" או "), m);
    }
  });
}

// "back to top" button, shown after scrolling down a long page
function addBackToTop() {
  const b = document.createElement("button");
  b.type = "button";
  b.className = "to-top";
  b.setAttribute("aria-label", "לראש העמוד");
  b.textContent = "↑";
  b.hidden = true;
  b.addEventListener("click", () => window.scrollTo({ top: 0, behavior: "smooth" }));
  document.body.append(b);
  const onScroll = () => { b.hidden = window.scrollY < 900; };
  window.addEventListener("scroll", onScroll, { passive: true });
  onScroll();
}

// "save as PDF": product and guide pages get a button that opens the print dialog (Save as PDF),
// plus a print-only header with the logo, phone and page address
function addPdfButton() {
  const priceBox = document.querySelector(".product-info .price-box");
  const guideHero = document.querySelector(".hero-sm .hero-inner");
  const isGuide = guideHero && document.querySelector(".article");
  if (!priceBox && !isGuide) return;
  if (!priceBox && guideHero.querySelector('a[href$=".pdf"]')) return;   // guide already offers its own PDF
  const b = document.createElement("button");
  b.type = "button";
  b.className = "pdf-btn";
  b.textContent = "📄 שמירת העמוד כ-PDF";
  b.addEventListener("click", () => window.print());
  if (priceBox) priceBox.after(b); else guideHero.append(b);

  const head = document.createElement("div");
  head.className = "print-head";
  const logo = document.createElement("img");
  logo.src = "bsd-title.png";
  logo.alt = "BSD מחשבים";
  const info = document.createElement("span");
  info.textContent = "054-457-8946 · " + location.href.replace(/[?#].*$/, "");
  info.dir = "ltr";
  head.append(logo, info);
  document.body.prepend(head);
}

document.addEventListener("DOMContentLoaded", () => {
  wireContactLinks();
  addBackToTop();
  addPdfButton();
  const y = document.getElementById("year");
  if (y) y.textContent = new Date().getFullYear();
});
