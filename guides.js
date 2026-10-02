// מדריכים וקבצים שימושיים של BSD מחשבים.
// להוספת פריט: לשים את הקובץ בתיקייה files/ ולהוסיף שורה לרשימה GUIDES.
//   section: "guides" (מדריכים) / "files" (קבצים להורדה) / "links" (קישורים שימושיים)
//   type:    "pdf" / "doc" / "zip" / "exe" / "image" / "video" / "link" / "page"
//   href:    "files/שם-הקובץ.pdf" או כתובת מלאה לקישור חיצוני
//   size:    (לא חובה) גודל להצגה, למשל "2.4MB"
const GUIDE_SECTIONS = [
  { id: "guides", name: "מדריכים",            desc: "הסברים שלב אחר שלב לפעולות נפוצות" },
  { id: "files",  name: "קבצים להורדה",       desc: "תוכנות וכלים שימושיים" },
  { id: "links",  name: "קישורים שימושיים",   desc: "אתרים וכלים מומלצים" },
];

const GUIDES = [
  { section: "guides", type: "page", title: "פתרון בעיות גרפיקה במחשבים ניידים עם שני כרטיסי מסך",
    desc: "קפיאות, ריצוד וקריסות ב-AutoCAD, Revit ותוכנות גרפיות. 5 שלבים לייצוב (Lenovo LOQ ודומיו).", href: "guide-dual-gpu.html" },
  { section: "guides", type: "pdf", title: "פתרון בעיות גרפיקה עם שני כרטיסי מסך (PDF)",
    desc: "אותו מדריך, בגרסה להורדה ולהדפסה.", href: "files/bsd-guide-dual-gpu-graphics.pdf", size: "1.3MB" },
  { section: "files", type: "page", title: "תוכנת התמיכה מרחוק של BSD",
    desc: "הורדה והסבר על חיבור לתמיכה מרחוק, רק באישור שלכם.", href: "remote.html" },
  { section: "files", type: "exe", title: "AnyDesk",
    desc: "תוכנת שליטה מרחוק פופולרית. הקישור מוריד תמיד את הגרסה האחרונה מהאתר הרשמי.",
    href: "https://download.anydesk.com/AnyDesk.exe", size: "8.6MB" },
  { section: "files", type: "exe", title: "דרייבר NVIDIA Studio למחשב נייח (Desktop)",
    desc: "גרסה 616.92 (ספטמבר 2026), לכרטיסי GeForce במחשבים נייחים. מומלץ לתוכנות Autodesk ו-Adobe. בהתקנה: Custom ← Clean Installation.",
    href: "https://us.download.nvidia.com/Windows/616.92/616.92-desktop-win10-win11-64bit-international-nsd-dch-whql.exe", size: "991MB" },
  { section: "files", type: "exe", title: "דרייבר NVIDIA Studio למחשב נייד (Laptop)",
    desc: "גרסה 616.92 (ספטמבר 2026), לכרטיסי GeForce במחשבים ניידים (Lenovo LOQ ודומיו). בהתקנה: Custom ← Clean Installation.",
    href: "https://us.download.nvidia.com/Windows/616.92/616.92-notebook-win10-win11-64bit-international-nsd-dch-whql.exe", size: "991MB" },
  { section: "links", type: "link", title: "דף הדרייברים הרשמי של NVIDIA",
    desc: "לבדיקת הגרסה העדכנית ביותר לכל כרטיס מסך (לבחור Studio Driver).", href: "https://www.nvidia.com/en-us/drivers/" },
];
