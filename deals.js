// מציאון: נוצר אוטומטית על ידי skill update-deals. לא לערוך ידנית.
// עודכן: 2026-10-10
const DEALS = [
  {
    "sku": "MZ-E14-GEN5",
    "cat": "deals",
    "brand": "Lenovo",
    "name": "Lenovo ThinkPad E14 Gen 5 · Ryzen 5 7530U",
    "img": "images/deals/e14-gen5/01.jpg",
    "page": "deal-e14-gen5.html",
    "price": "2,400",
    "deal": "מצב 8.5/10",
    "tagline": "ThinkPad עסקי במחיר מציאה",
    "specs": [
      "Ryzen 5 7530U",
      "16GB DDR4",
      "512GB SSD",
      "14 אינץ'",
      "מצב 8.5/10",
      "יתרת אחריות יצרן: 20 ימים"
    ],
    "zap": {
      "price": "4,706",
      "label": "ThinkPad E14 חדש (Gen 8, ‏16GB, ‏512GB)"
    }
  },
  {
    "sku": "MZ-E14-GEN6",
    "cat": "deals",
    "brand": "Lenovo",
    "name": "Lenovo ThinkPad E14 Gen 6 · Ryzen 5 7535U",
    "img": "images/deals/e14-gen6/01.jpg",
    "page": "deal-e14-gen6.html",
    "price": "2,600",
    "deal": "כמו חדש",
    "tagline": "כמו חדש, סימני שימוש קלים מאוד",
    "specs": [
      "Ryzen 5 7535U",
      "16GB DDR5",
      "512GB SSD",
      "14 אינץ'",
      "מצב 9/10",
      "יתרת אחריות יצרן: 19 חודשים"
    ],
    "zap": {
      "price": "5,090",
      "label": "ThinkPad E14 Gen 6 חדש (גרסת Intel, ‏512GB)"
    }
  },
  {
    "sku": "MZ-E14-GEN7",
    "cat": "deals",
    "brand": "Lenovo",
    "name": "Lenovo ThinkPad E14 Gen 7 · Ryzen 5 230",
    "img": "images/deals/e14-gen7/01.jpg",
    "page": "deal-e14-gen7.html",
    "price": "3,100",
    "deal": "חדש באריזה",
    "tagline": "חדש לגמרי, באריזה סגורה (ניילון)",
    "specs": [
      "Ryzen 5 230",
      "16GB DDR5",
      "512GB SSD",
      "14 אינץ'",
      "מצב 10/10",
      "יתרת אחריות יצרן: 27 חודשים"
    ],
    "zap": {
      "price": "4,449",
      "label": "ThinkPad E14 Gen 7 חדש (גרסת Intel, ‏512GB)"
    }
  },
  {
    "sku": "MZ-THINKBOOK14-G8",
    "cat": "deals",
    "brand": "Lenovo",
    "name": "Lenovo ThinkBook 14 G8 · Ultra 5 225U",
    "img": "images/deals/thinkbook14-g8/01.jpg",
    "page": "deal-thinkbook14-g8.html",
    "price": "3,100",
    "deal": "חדש באריזה",
    "tagline": "חדש לגמרי, באריזה סגורה (ניילון)",
    "specs": [
      "Ultra 5 225U",
      "16GB DDR5",
      "512GB SSD",
      "14 אינץ'",
      "מצב 10/10",
      "יתרת אחריות יצרן: 27 חודשים"
    ],
    "zap": {
      "price": "4,449",
      "label": "מחשב עסקי חדש של Lenovo עם אותו מעבד (ThinkPad E14 Gen 7, ‏Ultra 5 225U)"
    }
  },
  {
    "sku": "MZ-DELL-PRO14",
    "cat": "deals",
    "brand": "Dell",
    "name": "Dell Pro 14 · Ryzen 5 220",
    "img": "images/deals/dell-pro14/01.jpg",
    "page": "deal-dell-pro14.html",
    "price": "2,600",
    "deal": "חדש באריזה",
    "tagline": "חדש לגמרי, באריזה סגורה (ניילון)",
    "specs": [
      "Ryzen 5 220",
      "16GB DDR5",
      "512GB SSD",
      "14 אינץ' ‏FHD+ ‏(1920×1200) ‏IPS",
      "מצב 10/10",
      "אחריות יצרן עד 18/04/2029"
    ],
    "zap": {
      "price": "4,449",
      "label": "Dell Pro 14 ‏PB14250 חדש (‏Ultra 5, ‏16GB, ‏512GB)"
    }
  },
  {
    "sku": "MZ-VOSTRO5402",
    "cat": "deals",
    "brand": "Dell",
    "name": "Dell Vostro 5402 · i5-1135G7",
    "img": "images/deals/vostro5402/01.jpg",
    "page": "deal-vostro5402.html",
    "price": "1,600",
    "deal": "מצב 8/10",
    "tagline": "Dell Vostro עסקי במחיר מציאה",
    "specs": [
      "i5-1135G7",
      "32GB DDR4",
      "256GB SSD",
      "14 אינץ' Full HD",
      "מצב 8/10 · שריטה במכסה התחתון",
      "אחריות BSD לחצי שנה"
    ],
    "zap": {
      "price": "2,648",
      "label": "Dell Vostro 14 חדש (V3440, ‏i5-1334U, ‏512GB)"
    }
  },
  {
    "sku": "MZ-THINKBOOK13S-G2",
    "cat": "deals",
    "brand": "Lenovo",
    "name": "Lenovo ThinkBook 13s · i5-1135G7",
    "img": "images/deals/thinkbook13s-g2/01.jpg",
    "page": "deal-thinkbook13s-g2.html",
    "price": "1,500",
    "deal": "מצב 8/10",
    "tagline": "ThinkBook עסקי במחיר מציאה",
    "specs": [
      "i5-1135G7",
      "16GB DDR4",
      "512GB SSD",
      "13.3 אינץ'",
      "מצב 8/10",
      "אחריות BSD לחצי שנה"
    ],
    "zap": {
      "price": "3,788",
      "label": "Lenovo ThinkBook 14 חדש (G7, ‏Ultra 5 125U)"
    }
  },
  {
    "sku": "MZ-X1-CARBON-GEN9",
    "cat": "deals",
    "brand": "Lenovo",
    "name": "Lenovo ThinkPad X1 Carbon Gen 9 · 4G LTE · i7-1165G7",
    "img": "images/deals/x1-carbon-gen9/01.jpg",
    "page": "deal-x1-carbon-gen9.html",
    "price": "2,990",
    "deal": "מצב מעולה",
    "tagline": "מצב מעולה",
    "specs": [
      "i7-1165G7",
      "16GB LPDDR4X",
      "512GB SSD",
      "14 אינץ' WUXGA ‏(1920×1200) ‏IPS",
      "מצב מעולה",
      "אחריות BSD לחצי שנה"
    ],
    "zap": {
      "price": "9,149",
      "label": "ThinkPad X1 Carbon חדש (Gen 13, ‏512GB)"
    }
  },
  {
    "sku": "MZ-X1-CARBON-GEN9-WIFI",
    "cat": "deals",
    "brand": "Lenovo",
    "name": "Lenovo ThinkPad X1 Carbon Gen 9 · מסך מגע · i7-1165G7",
    "img": "images/deals/x1-carbon-gen9/01.jpg",
    "page": "deal-x1-carbon-gen9-wifi.html",
    "price": "2,790",
    "deal": "מצב מעולה",
    "tagline": "מצב מעולה",
    "specs": [
      "i7-1165G7",
      "16GB LPDDR4X",
      "512GB SSD",
      "14 אינץ' WUXGA ‏(1920×1200) ‏IPS",
      "מצב מעולה",
      "אחריות BSD לחצי שנה"
    ],
    "zap": {
      "price": "9,149",
      "label": "ThinkPad X1 Carbon חדש (Gen 13, ‏512GB)"
    }
  },
  {
    "sku": "MZ-THINKBOOK-PLUS-G3",
    "cat": "deals",
    "brand": "Lenovo",
    "name": "Lenovo ThinkBook Plus G3 · מסך כפול · i7-12700H",
    "img": "images/deals/thinkbook-plus-g3/01.jpg",
    "page": "deal-thinkbook-plus-g3.html",
    "price": "3,900",
    "deal": "מצב מעולה",
    "tagline": "מצב מעולה",
    "specs": [
      "i7-12700H",
      "32GB LPDDR5",
      "1TB SSD",
      "מסך ראשי 17.3 אינץ' 3K ‏(3072×1440) מגע",
      "מצב מעולה",
      "אחריות BSD לחצי שנה"
    ],
    "zap": {
      "price": "8,250",
      "label": "המחשב הנייד החדש הזול ביותר עם מסך שני מובנה (ASUS Zenbook Duo)"
    }
  },
  {
    "sku": "MZ-X1-NANO-GEN2",
    "cat": "deals",
    "brand": "Lenovo",
    "name": "Lenovo ThinkPad X1 Nano Gen 2 · 4G LTE · i7-1260P",
    "img": "images/deals/x1-nano-gen2/01.jpg",
    "page": "deal-x1-nano-gen2.html",
    "price": "4,490",
    "deal": "חדש",
    "tagline": "חדש, ללא אריזה מקורית",
    "specs": [
      "i7-1260P",
      "16GB LPDDR5",
      "512GB SSD",
      "13 אינץ' 2K ‏(2160×1350)",
      "חדש, ללא אריזה מקורית",
      "כולל אחריות יצרן"
    ],
    "zap": {
      "price": "8,688",
      "label": "אותו דגם בדיוק, חדש (21E80020IV)"
    },
    "also": [
      "business"
    ]
  }
];
