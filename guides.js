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
  { section: "files", type: "page", title: "תוכנת התמיכה מרחוק של BSD",
    desc: "הורדה והסבר על חיבור לתמיכה מרחוק, רק באישור שלכם.", href: "remote.html" },
];
