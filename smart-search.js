// BSD מחשבים – חיפוש חכם (בלי AI): מזהה כוונות בשפה חופשית ("מחשב קל עם מודם סלולרי ללימודי תכנות")
// ומתרגם אותן לבדיקות על נתוני המוצר (משקל, זיכרון, מסך, מודם, קטגוריה, מחיר).
// meta של כל כרטיס: { p: המוצר, t: טקסט מנורמל (כרטיס + עמוד המוצר), kg, ram, inch, wh, price, cats }

const SMART_STOP = new Set(["מחשב", "מחשבים", "עם", "של", "בשביל", "עבור", "מתאים", "שמתאים", "טוב", "טובה", "הכי", "רוצה",
  "מחפש", "מחפשת", "מחפשים", "צריך", "צריכה", "אני", "לי", "יש", "משהו", "גם", "או", "ו", "את", "דגם", "לא", "יותר", "כמה", "מאוד", "ממש", "מסך", "מסכי", "מחשבון"]);

const LAPTOP_CATS = ["recommended", "deals", "refurbished", "basic", "home", "business", "premium", "gaming", "arch", "mac"];
const isLaptop = m => !["desktop", "printers", "monitors"].includes(m.p.cat) && !/נייח|desktop|מיני pc|mini pc/i.test(m.t.slice(0, 120));
const has = (m, re) => re.test(m.t);
const isComputer = m => !["printers", "monitors"].includes(m.p.cat);

const SMART_INTENTS = [
  { id: "light", label: "קל לסחיבה", words: ["קל", "קלה", "קלים", "קליל", "קלילה", "לסחוב", "לנסיעות", "נסיעות", "נוצה", "משקל"],
    test: m => m.kg != null && m.kg <= 1.45, rank: m => -(m.kg || 9) },
  { id: "lte", label: "מודם סלולרי", words: ["סלולרי", "סלולארי", "מודם", "סים", "sim", "esim", "lte", "4g", "5g", "נטסטיק"],
    test: m => has(m, /\blte\b|מודם סלולרי|\b[45]g\b/) },
  { id: "touch", label: "מסך מגע", words: ["מגע", "טאץ", "touch", "עט"],
    test: m => has(m, /מסך מגע|מגע מט|מסכי מגע|מתהפך|touchscreen/) },
  { id: "code", label: "מתאים לתכנות", words: ["תכנות", "מתכנת", "מתכנתים", "מתכנתת", "פיתוח", "קוד", "פיתון", "python", "מפתחים"],
    test: m => isLaptop(m) && (m.ram || 0) >= 16, rank: m => (m.ram || 0) },
  { id: "study", label: "ללימודים", words: ["לימודים", "לימודי", "לימוד", "סטודנט", "סטודנטים", "סטודנטית", "תלמיד", "תלמידים", "תלמידה", "אוניברסיטה", "תואר", "ישיבה"],
    test: m => isLaptop(m) && m.price != null && m.price <= 3600 },
  { id: "business", label: "עסקי", words: ["עסקי", "עסקים", "משרד", "משרדי", "עבודה", "עסק", "חברה", "הנהח", "חשבונות"],
    test: m => isComputer(m) && (m.cats.includes("business") || has(m, /thinkpad|elitebook|probook|latitude|dell pro 14|expertbook|thinkbook|thinkcentre|pro mini/)) },
  { id: "graphics", label: "גרפיקה ועריכה", words: ["גרפי", "גרפיקה", "rtx", "עריכה", "עריכת", "אדריכלות", "אדריכל", "תלת", "רנדור", "autocad", "revit", "שרטוט", "עיצוב"],
    test: m => isComputer(m) && (has(m, /rtx|oled|dci-p3|m1 max/) || m.cats.includes("arch") || m.cats.includes("gaming")) },
  { id: "oled", label: "מסך OLED", words: ["oled", "אולד"], test: m => has(m, /oled/) },
  { id: "big", label: "מסך גדול", words: ["גדול", "גדולה"], test: m => isLaptop(m) && (m.inch || 0) >= 15.3 },
  { id: "small", label: "קומפקטי", words: ["קטן", "קטנה", "קומפקטי", "קומפקטית"],
    test: m => isLaptop(m) ? (m.inch != null && m.inch <= 14.5) : (m.cats.includes("desktop") && has(m, /mini|tiny|מיני/)) },
  { id: "battery", label: "סוללה חזקה", words: ["סוללה", "סוללות", "שעות"], test: m => (m.wh || 0) >= 60 || has(m, /עד 1[5-9] שעות|עד 2\d שעות/) },
  { id: "cheap", label: "משתלם", words: ["זול", "זולה", "זולים", "משתלם", "בתקציב", "חסכוני", "בזול"],
    test: m => m.price != null && m.price <= 2500, rank: m => -(m.price || 1e6) },
  { id: "refurb", label: "מחודש / יד שנייה", words: ["מחודש", "מחודשים", "משומש", "שנייה", "מציאה", "מציאון"],
    test: m => m.cats.includes("refurbished") || m.cats.includes("deals") },
  { id: "mac", label: "Apple Mac", words: ["מק", "מקבוק", "macbook", "mac", "apple", "אפל", "מקינטוש"], test: m => m.cats.includes("mac") },
  { id: "desktop", label: "מחשב נייח", words: ["נייח", "נייחים", "שולחני", "שולחניים", "מיני"], test: m => m.cats.includes("desktop") },
  { id: "laptop", label: "נייד", words: ["נייד", "ניידים", "לפטופ", "לפטופים"], test: isLaptop, soft: true },
  { id: "printer", label: "מדפסת", words: ["מדפסת", "מדפסות", "הדפסה", "סורק"], test: m => m.cats.includes("printers") },
  { id: "monitor", label: "מסך מחשב", words: ["מוניטור", "מסכים"], test: m => m.cats.includes("monitors") },
];

// strip one-letter Hebrew prefixes (ו/ל/ב/ה/מ/ש/כ) when that reveals a known word: "ללימודים" -> "לימודים", "ומודם" -> "מודם"
const SMART_WORDS = new Map();
SMART_INTENTS.forEach(it => it.words.forEach(w => SMART_WORDS.set(w, it)));
function smartLookup(w) {
  for (let i = 0; i <= 2 && w.length - i >= 2; i++) {
    const head = w.slice(0, i), tail = w.slice(i);
    if (i && !/^[ולבהמשכ]+$/.test(head)) break;
    if (SMART_WORDS.has(tail)) return { intent: SMART_WORDS.get(tail), word: tail };
    if (SMART_STOP.has(tail)) return { stop: true };
  }
  return null;
}

// "עד 3000", "עד ₪2,500", "עד 3 אלף" -> max price
function smartParse(raw) {
  let q = raw.toLowerCase().replace(/[‎‏]/g, "");
  let maxPrice = null;
  q = q.replace(/(?:עד|מתחת ל-?|פחות מ-?)\s*₪?\s*(\d[\d,]*)\s*(אלף|k)?\s*(?:ש"?ח|שקל(?:ים)?|₪)?/g, (_, n, k) => {
    maxPrice = parseInt(n.replace(/,/g, ""), 10) * (k ? 1000 : 1);
    return " ";
  });
  const words = norm(q).split(" ").filter(Boolean);
  const intents = [], rest = [];
  words.forEach(w => {
    const hit = smartLookup(w);
    if (!hit) rest.push(w);
    else if (hit.intent && !intents.includes(hit.intent)) intents.push(hit.intent);
  });
  return { intents, rest, maxPrice };
}
