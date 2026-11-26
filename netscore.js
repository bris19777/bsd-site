/* BSD NetScore - shared scoring + report rendering (used by the EXE report and by the web page) */
var NS = (function () {
  'use strict';

  var WEIGHTS = { real: 30, il: 20, world: 15, bw: 10, stab: 15, eff: 10 };
  var LABELS = {
    real: 'הורדות אמיתיות',
    il: 'אתרים ישראליים',
    world: 'אתרים בינלאומיים',
    bw: 'רוחב פס גולמי',
    stab: 'תגובתיות ויציבות',
    eff: 'ניצול הקו בפועל'
  };
  var HINTS = {
    real: 'מהירות הורדה של קבצים אמיתיים (דרייברים, התקנות) משרתים אמיתיים',
    il: 'כמה מהר נפתחים אתרים נפוצים בארץ',
    world: 'כמה מהר נפתחים אתרים נפוצים בחו"ל',
    bw: 'בדיקת מהירות קלאסית מול שרת בדיקה (הורדה והעלאה)',
    stab: 'זמן תגובה, תנודות בזמן התגובה ושיעור בקשות שנכשלו',
    eff: 'כמה מהמהירות הגולמית באמת מגיעה בהורדה רגילה'
  };

  function clamp(x) { return Math.max(0, Math.min(100, x)); }
  function hi(x, lo, top) { return x > 0 ? clamp(100 * Math.log(x / lo) / Math.log(top / lo)) : 0; }
  function lo(t, best, worst) { return clamp(100 * Math.log(worst / Math.max(t, 0.001)) / Math.log(worst / best)); }
  function mean(a) { var s = 0; for (var i = 0; i < a.length; i++) s += a[i]; return a.length ? s / a.length : 0; }
  function median(a) { if (!a.length) return 0; var b = a.slice().sort(function (x, y) { return x - y; }); var m = b.length >> 1; return b.length % 2 ? b[m] : (b[m - 1] + b[m]) / 2; }

  // 65% plain average + 35% average of the worst 1/part of the results
  function tailWeighted(a, part) {
    if (!a.length) return 0;
    var b = a.slice().sort(function (x, y) { return x - y; });
    return 0.65 * mean(b) + 0.35 * mean(b.slice(0, Math.max(1, Math.ceil(b.length / part))));
  }
  function siteGroup(r, g) {
    var list = (r.sites || []).filter(function (s) { return s.group === g && !s.blocked && !s.skipped; });
    if (!list.length) return null;
    var sc = list.map(function (s) { return s.ok ? lo(s.total_ms, 300, 4000) : 0; }).sort(function (a, b) { return a - b; });
    // a slow site here and there is usually the site's own server, not the line - drop the worst 1 in 12
    if (sc.length >= 6) sc = sc.slice(Math.max(1, Math.floor(sc.length / 12)));
    var okT = list.filter(function (s) { return s.ok; }).map(function (s) { return s.total_ms; });
    // the slow third counts extra: a line that opens some sites fast and others slowly does not feel fast
    return { score: tailWeighted(sc, 3), median_ms: median(okT), n: list.length, failed: list.length - okT.length };
  }

  function compute(r) {
    var c = {}, info = {};
    var dls = (r.downloads || []).filter(function (d) { return !d.blocked; });
    var okD = dls.filter(function (d) { return d.ok; });
    if (dls.length) {
      c.real = tailWeighted(dls.map(function (d) { return d.ok ? hi(d.mbps, 1, 150) : 0; }), 2);
      info.realMedian = median(okD.map(function (d) { return d.mbps; }));
    }
    var il = siteGroup(r, 'il'), wo = siteGroup(r, 'world');
    if (il) { c.il = il.score; info.il = il; }
    if (wo) { c.world = wo.score; info.world = wo; }

    var bd = r.bandwidth && r.bandwidth.down, bu = r.bandwidth && r.bandwidth.up;
    if (bd && bd.ok && bu && bu.ok) c.bw = 0.7 * hi(bd.mbps, 1, 200) + 0.3 * hi(bu.mbps, 0.3, 50);
    else if (bd && bd.ok) c.bw = 0.85 * hi(bd.mbps, 1, 200);

    var all = (r.sites || []).filter(function (s) { return !s.blocked && !s.skipped; }).concat(dls);
    var okN = all.filter(function (x) { return x.ok; }).length;
    var rate = all.length ? okN / all.length : 1;
    info.successRate = rate;
    var relScore = clamp((rate - 0.8) / 0.2 * 100);
    var L = r.latency;
    if (L && L.ok) c.stab = 0.5 * lo(L.median_ms, 20, 400) + 0.25 * lo(L.jitter_ms, 3, 100) + 0.25 * relScore;
    else if (all.length) c.stab = relScore * 0.6; // could not even measure latency

    if (bd && bd.ok && okD.length) {
      // anything above 100 Mbps in a real download is "all you can use"
      var ratio = Math.min(1, info.realMedian / Math.min(bd.mbps, 100));
      info.ratio = ratio;
      c.eff = hi(ratio, 0.1, 0.8);
    }

    var wsum = 0, tot = 0, comps = [];
    Object.keys(WEIGHTS).forEach(function (k) {
      if (c[k] == null) return;
      wsum += WEIGHTS[k]; tot += WEIGHTS[k] * c[k];
    });
    Object.keys(WEIGHTS).forEach(function (k) {
      comps.push({ key: k, label: LABELS[k], hint: HINTS[k], weight: WEIGHTS[k], score: c[k] == null ? null : Math.round(c[k]) });
    });
    var total = wsum ? tot / wsum : 0;
    // the weakest of the three "real use" parts pulls the total down: a fast speed test cannot hide slow real use
    var weak = [c.real, c.il, c.world].filter(function (x) { return x != null; });
    if (weak.length) total = 0.7 * total + 0.3 * Math.min.apply(null, weak);
    total = Math.round(total);
    return { total: total, grade: grade(total), comps: comps, info: info, insights: insights(r, c, info) };
  }

  function grade(s) {
    if (s >= 90) return { text: 'מצוין', color: '#12805c' };
    if (s >= 78) return { text: 'טוב מאוד', color: '#2f8f3a' };
    if (s >= 65) return { text: 'טוב', color: '#7a9a1c' };
    if (s >= 45) return { text: 'בינוני', color: '#c98a0a' };
    if (s >= 25) return { text: 'חלש', color: '#d2601a' };
    return { text: 'גרוע', color: '#c0392b' };
  }
  function colorOf(s) { return grade(s).color; }

  function f1(x) { return x >= 100 ? Math.round(x).toString() : (Math.round(x * 10) / 10).toString(); }
  function secs(msv) { return (Math.round(msv / 10) / 100).toFixed(2); }

  function insights(r, c, info) {
    var out = [];
    var web = r.mode === 'web';
    var f = r.env && r.env.filter;
    if (f && f.detected) out.push({ t: 'info', s: 'זוהה סינון מפענח בקו' + (f.name ? ': ' + f.name : '') + '. כל התעבורה עוברת דרך שרתי הסינון, והציון משקף את החוויה דרכם.' });
    else if (!web) out.push({ t: 'info', s: 'לא זוהה סינון מפענח (ייתכן קו פתוח, או סינון ברמת הספק / DNS שאינו מחליף תעודות).' });

    var bd = r.bandwidth && r.bandwidth.down;
    if (bd && bd.ok && info.realMedian != null && info.ratio != null) {
      if (info.ratio < 0.5) out.push({ t: 'bad', s: 'בדיקת המהירות מראה ' + f1(bd.mbps) + ' Mbps, אבל בהורדות אמיתיות מתקבלים בפועל רק כ-' + f1(info.realMedian) + ' Mbps. זה הפער שבדיקת מהירות רגילה לא מראה.' });
      else if (bd.mbps > 120) out.push({ t: 'good', s: 'ההורדות האמיתיות מהירות (' + f1(info.realMedian) + ' Mbps) – הקו מנוצל היטב גם מול שרתים אמיתיים.' });
      else out.push({ t: 'good', s: 'ההורדות האמיתיות (' + f1(info.realMedian) + ' Mbps) קרובות למה שבדיקת המהירות מראה (' + f1(bd.mbps) + ' Mbps).' });
    }
    if (info.il && info.world && info.il.median_ms && info.world.median_ms) {
      var a = info.il.median_ms, b = info.world.median_ms;
      if (b > a * 1.6 && b - a > 300) out.push({ t: 'warn', s: 'אתרים בחו"ל נפתחים לאט משמעותית מאתרים בארץ (' + secs(b) + ' מול ' + secs(a) + ' שניות).' });
      else if (a > b * 1.6 && a - b > 300) out.push({ t: 'warn', s: 'אתרים בארץ נפתחים לאט מאתרים בחו"ל (' + secs(a) + ' מול ' + secs(b) + ' שניות).' });
    }
    var sites = (r.sites || []);
    var waits = sites.filter(function (s) { return s.ok && !s.blocked && s.ttfb_ms; }).map(function (s) { return s.ttfb_ms - (s.dns_ms || 0) - (s.tcp_ms || 0) - (s.tls_ms || 0); }).filter(function (x) { return x > 0; });
    if (waits.length >= 5) {
      var w = median(waits);
      if (w > 800) out.push({ t: 'bad', s: 'זמן ההמתנה לתשובה מהשרת (אחרי שהחיבור כבר נוצר) גבוה: כ-' + secs(w) + ' שניות. זה אופייני לעומס על שרתי סינון.' });
    }
    var L = r.latency;
    if (L && L.ok) {
      if (L.median_ms > 150) out.push({ t: 'warn', s: 'זמן התגובה גבוה (' + Math.round(L.median_ms) + ' אלפיות שנייה) – הגלישה תרגיש "כבדה" גם כשהמהירות גבוהה.' });
      if (L.jitter_ms > 40) out.push({ t: 'warn', s: 'זמן התגובה לא יציב (תנודות של ' + Math.round(L.jitter_ms) + ' אלפיות שנייה) – עלול להפריע בשיחות וידאו ושליטה מרחוק.' });
    }
    var slow = sites.filter(function (s) { return s.ok && !s.blocked && s.total_ms > 3000; }).map(function (s) { return s.name; });
    if (slow.length) out.push({ t: 'warn', s: 'אתרים איטיים במיוחד: ' + slow.join(', ') + '.' });
    var failed = sites.filter(function (s) { return !s.ok && !s.blocked && !s.skipped; }).map(function (s) { return s.name; });
    if (failed.length) out.push({ t: 'bad', s: 'לא נפתחו בזמן: ' + failed.join(', ') + '.' });
    var blocked = sites.concat(r.downloads || []).filter(function (s) { return s.blocked; }).map(function (s) { return s.name; });
    if (blocked.length) out.push({ t: 'info', s: 'נחסמו ע"י הסינון (לא נכללו בציון): ' + blocked.join(', ') + '.' });
    var badD = (r.downloads || []).filter(function (d) { return !d.ok && !d.blocked; }).map(function (d) { return d.name; });
    if (badD.length) out.push({ t: 'bad', s: 'הורדות שנכשלו: ' + badD.join(', ') + '.' });
    if (bd && !bd.ok) out.push({ t: 'warn', s: 'בדיקת רוחב הפס הגולמי נכשלה (ייתכן ששרת הבדיקה חסום) – הציון חושב בלעדיה.' });
    return out;
  }


  // ---------- line identity: provider / link type / filter (a best guess from the public address) ----------
  // Israeli networks by AS number. kind: cell / wired / fiber / mixed (sells both) / '' (unknown).
  var ISPS = {
    8551: ['בזק בינלאומי', 'wired'], 8203: ['בזק בינלאומי', 'wired'], 51606: ['בזק בינלאומי', 'wired'], 6810: ['בזק', 'wired'],
    12400: ['פרטנר', 'mixed'], 9116: ['פרטנר (012 סמייל)', 'wired'], 5486: ['פרטנר (012 סמייל)', 'wired'],
    1680: ['סלקום / נטוויז\'ן', 'mixed'], 12849: ['HOT נט', 'wired'], 21450: ['HOT מובייל', 'cell'], 16116: ['פלאפון', 'cell'],
    47956: ['018 אקספון', 'mixed'], 51825: ['019 טלזר', 'mixed'], 210625: ['ווי-קום (Wecom)', 'cell'], 213783: ['Live Mobile', 'cell'],
    42925: ['אינטרנט רימון', ''], 39159: ['R.L אינטרנט', ''], 219504: ['יוסי תקשורת', ''], 200495: ['פרי טלקום', ''],
    21145: ['IBC (אנלימיטד)', 'fiber'], 213487: ['GFiber', 'fiber'], 201397: ['XFiber', 'fiber'], 206815: ['United Fiber', 'fiber'],
    218875: ['Quantum Fiber', 'fiber'], 212698: ['N Fiber', 'fiber'], 204160: ['Bee Fiber', 'fiber'], 215423: ['Flash Fiber', 'fiber'],
    199270: ['015 השקמה', ''], 8948: ['099 פרימו', ''], 60636: ['016 קולנט', ''], 12491: ['גילת טלקום', ''],
    204814: ['Annatel', ''], 209472: ['Annatel', ''], 50463: ['טריפל C', 'wired'], 201073: ['yes', '']
  };
  var FILTER_WORDS = [['netfree', 'נטפרי'], ['rimon', 'רימון'], ['etrog', 'אתרוג'], ['enativ', 'נתיב'], ['nativ', 'נתיב'], ['hadran', 'הדרן']];

  function lineInfo(r) {
    var n = r.net || {}, web = r.mode === 'web';
    var out = { ip: n.ip4 || n.ip || '', ip6: n.ip4 && n.ip !== n.ip4 ? n.ip : '', isp: '', ispSub: '', conn: '', connSub: '', filter: '', filterSub: '' };
    var known = n.asn ? ISPS[n.asn] : null;
    var hay = ((n.org || '') + ' ' + (n.ptr || '')).toLowerCase();

    // filter
    var f = r.env && r.env.filter, fname = '';
    if (f && f.detected) { fname = f.name || 'סינון מפענח (לא מזוהה)'; out.filterSub = 'לפי תעודות האבטחה בקו'; }
    if (n.netfree && (!fname || !(f && f.name))) { fname = 'נטפרי'; out.filterSub = 'לפי כתובת פנימית של נטפרי'; }
    if (!fname || !(f && f.name || n.netfree)) {
      for (var i = 0; i < FILTER_WORDS.length; i++) if (hay.indexOf(FILTER_WORDS[i][0]) >= 0) { fname = FILTER_WORDS[i][1]; out.filterSub = 'לפי כתובת ה-IP'; break; }
    }
    out.filtered = !!fname;
    out.filter = fname || 'לא זוהה';
    if (!fname) out.filterSub = web ? 'מהדפדפן מזהים רק חלק מהסינונים' : 'ייתכן קו פתוח או סינון שלא מחליף תעודות';

    // provider
    var raw = (n.org || '') + (n.asn ? ' · AS' + n.asn : '');
    if (known) { out.isp = known[0]; out.ispSub = raw; }
    else if (!n.ip) { out.isp = 'לא זוהה'; out.ispSub = 'לא התקבלה כתובת IP'; }
    else if (n.country && n.country !== 'IL') { out.isp = n.org || 'לא מזוהה'; out.ispSub = 'כתובת מחוץ לישראל (' + n.country + '): ייתכן VPN או שרת סינון'; }
    else if (fname) { out.isp = 'ספק לא מזוהה'; out.ispSub = raw; }
    else { out.isp = n.org || 'לא מזוהה'; out.ispSub = n.asn ? 'AS' + n.asn : ''; }
    if (n.carrier && n.local === 'cellular') out.ispSub = 'רשת סלולרית: ' + n.carrier + (out.ispSub ? ' · ' + out.ispSub : '');
    if (n.netfree_isp) out.ispSub = 'לפי נטפרי: ' + n.netfree_isp + (out.ispSub ? ' · ' + out.ispSub : '');

    // wired or cellular
    var kind = known ? known[1] : '';
    var up = r.bandwidth && r.bandwidth.up && r.bandwidth.up.ok ? r.bandwidth.up.mbps : 0;
    var localTxt = n.local === 'wifi' ? 'המכשיר מחובר ב-Wi-Fi' : n.local === 'ethernet' ? 'המחשב מחובר בכבל רשת' : '';
    // words in the registry record of the range (strongest hint), then in the reverse name / owner name
    function words(t) { var o = {}; String(t || '').toLowerCase().split(/[^a-z0-9]+/).forEach(function (w) { if (w) o[w] = 1; }); return o; }
    function has(o, list) { for (var i = 0; i < list.length; i++) if (o[list[i]]) return true; return false; }
    var CELL = ['cellular', 'cellucar', 'wireless', 'mobile', 'gprs', 'ggsn', 'lte', 'umts', '3g', '4g', '5g'];
    var FIBER = ['fiber', 'fibre', 'ftth', 'fttx', 'fttb', 'gpon'], DSL = ['adsl', 'vdsl', 'xdsl', 'dsl'], CABLE = ['cable', 'cables', 'docsis', 'hfc'], WIRED = ['broadband', 'bb'];
    var ww = words(n.whois), wp = words(n.ptr), wo = words(n.org), src = 'לפי רישום טווח הכתובות';
    function pick(o) { return has(o, CELL) ? 'cell' : has(o, FIBER) ? 'fiber' : has(o, DSL) ? 'dsl' : has(o, CABLE) ? 'cable' : has(o, WIRED) ? 'wired' : ''; }
    var k = pick(ww);
    if (!k) { k = pick(wp); src = 'לפי רישום כתובת ה-IP'; }
    if (!k && has(wo, CELL)) { k = 'cell'; src = 'הספק הוא ספק סלולרי'; }
    if (!k && kind && kind !== 'mixed') { k = kind; src = kind === 'cell' ? 'הספק הוא ספק סלולרי' : kind === 'fiber' ? 'הספק הוא ספק סיבים' : ''; }
    if (n.local === 'cellular') { out.conn = 'סלולרי'; out.connSub = 'המכשיר מחובר ישירות לרשת סלולרית'; }
    else if (k === 'cell') { out.conn = 'סלולרי'; out.connSub = src; }
    else if (k === 'fiber') { out.conn = 'קווי: סיבים אופטיים'; out.connSub = src; }
    else if (k === 'dsl') { out.conn = 'קווי: DSL (קו טלפון)'; out.connSub = src; }
    else if (k === 'cable') { out.conn = 'קווי: כבלים'; out.connSub = src; }
    else if (k === 'wired') { out.conn = up >= 50 ? 'קווי: כנראה סיבים' : 'קווי'; out.connSub = up >= 50 ? 'לפי מהירות ההעלאה' : 'סיבים, כבלים או DSL: לא ניתן לדעת מהכתובת'; }
    else if (kind === 'mixed') { out.conn = 'לא ניתן לקבוע'; out.connSub = 'הספק נותן גם קווי וגם סלולרי'; }
    else { out.conn = 'לא ניתן לקבוע'; out.connSub = ''; }
    out.kind = k;
    if (localTxt && n.local !== 'cellular') out.connSub = out.connSub ? out.connSub + ' · ' + localTxt : localTxt;
    return out;
  }

  function lineCard(r) {
    if (!r.net) return '';
    var L = lineInfo(r);
    function cell(label, v, sub, ltr) { return '<div><span>' + label + '</span><b' + (ltr ? ' dir="ltr"' : '') + '>' + esc(v || '–') + '</b>' + (sub ? '<small>' + esc(sub) + '</small>' : '') + '</div>'; }
    return '<section class="ns-card"><h2>החיבור שלך</h2><div class="ns-line">' +
      cell('ספק תקשורת', L.isp, L.ispSub) + cell('סוג חיבור', L.conn, L.connSub) + cell('סינון', L.filter, L.filterSub) + cell('כתובת IP', L.ip || 'לא זוהתה', L.ip6 ? 'IPv6: ' + L.ip6 : '', true) +
      '</div><p class="ns-note">הזיהוי הוא הערכה לפי כתובת ה-IP ומאגרים ציבוריים. ספקי משנה וסינונים עובדים לעיתים על תשתית של ספק אחר, ואז יופיע שם ספק התשתית.</p></section>';
  }

  function esc(s) { return String(s == null ? '' : s).replace(/[&<>"]/g, function (ch) { return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[ch]; }); }

  function gauge(score, color) {
    var R = 86, C = Math.PI * R; // half circle
    var off = C * (1 - score / 100);
    return '<svg class="ns-gauge" viewBox="0 0 200 118" role="img" aria-label="ציון ' + score + ' מתוך 100">' +
      '<path d="M14 104 A86 86 0 0 1 186 104" fill="none" stroke="#e6e9ef" stroke-width="16" stroke-linecap="round"/>' +
      '<path d="M14 104 A86 86 0 0 1 186 104" fill="none" stroke="' + color + '" stroke-width="16" stroke-linecap="round" stroke-dasharray="' + C.toFixed(1) + '" stroke-dashoffset="' + off.toFixed(1) + '"/>' +
      '<text x="100" y="92" text-anchor="middle" class="ns-gnum" fill="' + color + '">' + score + '</text>' +
      '<text x="100" y="112" text-anchor="middle" class="ns-gsub">מתוך 100</text></svg>';
  }

  function statusCell(x) {
    if (x.blocked) return '<span class="ns-tag ns-info">נחסם בסינון</span>';
    if (x.skipped) return '<span class="ns-tag ns-info">לא נמדד בדפדפן</span>';
    if (!x.ok) return '<span class="ns-tag ns-bad">נכשל' + (x.err ? ' (' + esc(x.err) + ')' : '') + '</span>';
    return '<span class="ns-tag ns-good">תקין</span>';
  }
  function num(v) { return v == null ? '–' : Math.round(v); }

  function sitesTable(r, g, web) {
    var list = (r.sites || []).filter(function (s) { return s.group === g; });
    if (!list.length) return '';
    var h = '<div class="ns-scroll"><table class="ns-table"><thead><tr><th>אתר</th><th>מצב</th>';
    if (!web) h += '<th>DNS</th><th>חיבור</th><th>הצפנה</th><th>בייט ראשון</th>';
    h += '<th>' + (web ? 'זמן תגובה' : 'הדף הגיע') + '</th></tr></thead><tbody>';
    list.forEach(function (s) {
      h += '<tr><td>' + esc(s.name) + '</td><td>' + statusCell(s) + '</td>';
      if (!web) h += '<td>' + num(s.dns_ms) + '</td><td>' + num(s.tcp_ms) + '</td><td>' + num(s.tls_ms) + '</td><td>' + num(s.ttfb_ms) + '</td>';
      h += '<td><b style="color:' + (s.ok && !s.blocked ? colorOf(lo(s.total_ms, 300, 4000)) : '#888') + '">' + (s.ok ? num(s.total_ms) : '–') + '</b></td></tr>';
    });
    return h + '</tbody></table></div><p class="ns-note">כל הזמנים באלפיות שנייה.</p>';
  }

  function summaryText(r, sc) {
    var p = ['ציון אינטרנט BSD: ' + sc.total + '/100 (' + sc.grade.text + ')'];
    var b = r.bandwidth || {};
    if (b.down && b.down.ok) p.push('הורדה גולמית ' + f1(b.down.mbps) + ' Mbps');
    if (b.up && b.up.ok) p.push('העלאה ' + f1(b.up.mbps) + ' Mbps');
    if (sc.info.realMedian) p.push('הורדות אמיתיות ' + f1(sc.info.realMedian) + ' Mbps');
    if (sc.info.il && sc.info.il.median_ms) p.push('אתרים בארץ ' + secs(sc.info.il.median_ms) + ' שנ\'');
    if (sc.info.world && sc.info.world.median_ms) p.push('אתרים בחו"ל ' + secs(sc.info.world.median_ms) + ' שנ\'');
    if (r.latency && r.latency.ok) p.push('תגובה ' + Math.round(r.latency.median_ms) + 'ms');
    var extra = '';
    if (r.net) {
      var li = lineInfo(r);
      extra = '\nספק: ' + li.isp + ' | חיבור: ' + li.conn + ' | סינון: ' + li.filter + (li.ip ? ' | IP: ' + li.ip : '');
    } else {
      var f = r.env && r.env.filter;
      if (f && f.detected) p.push('סינון: ' + (f.name || 'זוהה'));
    }
    var sub = sc.comps.filter(function (c) { return c.score != null; }).map(function (c) { return c.label + ' ' + c.score; });
    return p.join(' | ') + extra + '\nציוני משנה: ' + sub.join(', ');
  }

  function render(root, r) {
    var sc = compute(r), web = r.mode === 'web', g = sc.grade;
    var d = new Date(r.started);
    var when = isNaN(d) ? '' : d.toLocaleDateString('he-IL') + ' ' + d.toLocaleTimeString('he-IL', { hour: '2-digit', minute: '2-digit' });
    var b = r.bandwidth || {}, L = r.latency || {};
    var h = '';
    h += '<section class="ns-hero"><div class="ns-hero-g">' + gauge(sc.total, g.color) + '<div class="ns-grade" style="color:' + g.color + '">' + g.text + '</div></div>';
    h += '<div class="ns-hero-k">';
    h += kpi('הורדה גולמית', b.down && b.down.ok ? f1(b.down.mbps) : '–', 'Mbps');
    h += kpi('העלאה', b.up && b.up.ok ? f1(b.up.mbps) : '–', 'Mbps');
    h += kpi('הורדה אמיתית', sc.info.realMedian ? f1(sc.info.realMedian) : '–', 'Mbps');
    h += kpi('אתר בארץ', sc.info.il && sc.info.il.median_ms ? secs(sc.info.il.median_ms) : '–', 'שניות');
    h += kpi('אתר בחו"ל', sc.info.world && sc.info.world.median_ms ? secs(sc.info.world.median_ms) : '–', 'שניות');
    h += kpi('זמן תגובה', L.ok ? Math.round(L.median_ms) : '–', 'ms');
    h += '</div></section>';

    h += lineCard(r);

    h += '<section class="ns-card"><h2>ממה מורכב הציון</h2>';
    sc.comps.forEach(function (c) {
      var v = c.score == null ? 0 : c.score;
      h += '<div class="ns-comp" title="' + esc(c.hint) + '"><div class="ns-comp-h"><span>' + c.label + ' <small>' + c.weight + '%</small></span><b>' + (c.score == null ? 'לא נמדד' : c.score) + '</b></div>' +
        '<div class="ns-bar"><i style="width:' + v + '%;background:' + colorOf(v) + '"></i></div><div class="ns-comp-d">' + esc(c.hint) + '</div></div>';
    });
    h += '</section>';

    if (sc.insights.length) {
      h += '<section class="ns-card"><h2>מה זה אומר</h2><ul class="ns-ins">';
      sc.insights.forEach(function (i) { h += '<li class="ns-' + i.t + '">' + esc(i.s) + '</li>'; });
      h += '</ul></section>';
    }

    h += '<details class="ns-card ns-details"><summary>דוח מפורט</summary>';
    h += '<h3>הורדות אמיתיות</h3><div class="ns-scroll"><table class="ns-table"><thead><tr><th>קובץ</th><th>מצב</th><th>Mbps</th><th>בייט ראשון (ms)</th><th>הורד</th></tr></thead><tbody>';
    (r.downloads || []).forEach(function (x) {
      h += '<tr><td>' + esc(x.name) + '</td><td>' + statusCell(x) + '</td><td><b style="color:' + (x.ok ? colorOf(hi(x.mbps, 1, 150)) : '#888') + '">' + (x.ok ? f1(x.mbps) : '–') + '</b></td><td>' + num(x.ttfb_ms) + '</td><td>' + (x.bytes ? f1(x.bytes / 1048576) + ' MB' : '–') + '</td></tr>';
    });
    h += '</tbody></table></div>';
    h += '<h3>אתרים ישראליים</h3>' + sitesTable(r, 'il', web);
    h += '<h3>אתרים בינלאומיים</h3>' + sitesTable(r, 'world', web);
    h += '<h3>רוחב פס ותגובתיות</h3><table class="ns-table ns-kv"><tbody>';
    h += kv('הורדה גולמית', b.down && b.down.ok ? f1(b.down.mbps) + ' Mbps (' + b.down.streams + ' ערוצים במקביל)' : 'נכשל');
    h += kv('העלאה גולמית', b.up && b.up.ok ? f1(b.up.mbps) + ' Mbps' : 'נכשל');
    h += kv('זמן תגובה (חציון)', L.ok ? L.median_ms + ' ms' : 'נכשל');
    h += kv('תנודות (Jitter)', L.ok ? L.jitter_ms + ' ms' : '–');
    h += kv('בקשות שאבדו', L.ok ? L.lost : '–');
    h += kv('שיעור הצלחה כולל', Math.round(sc.info.successRate * 100) + '%');
    if (sc.info.ratio != null) h += kv('ניצול הקו בפועל', Math.round(sc.info.ratio * 100) + '%');
    h += '</tbody></table>';
    h += '<h3>סביבת הבדיקה</h3><table class="ns-table ns-kv"><tbody>';
    h += kv('מועד', esc(when)) + kv('משך הבדיקה', r.duration_s + ' שניות');
    h += kv('סוג בדיקה', web ? 'דף אינטרנט (מדידה מקורבת)' : r.mode === 'android' ? 'אפליקציית אנדרואיד (מדידה מלאה)' : 'תוכנה (מדידה מלאה)');
    var f = r.env && r.env.filter;
    if (!web) {
      h += kv('סינון מפענח', f && f.detected ? 'זוהה' + (f.name ? ' – ' + esc(f.name) : '') : 'לא זוהה');
      if (f && f.issuer) h += kv('מנפיק התעודות', '<span dir="ltr">' + esc(f.issuer) + '</span>');
      if (r.env && r.env.proxy) h += kv('פרוקסי', '<span dir="ltr">' + esc(r.env.proxy) + '</span>');
      if (r.env && r.env.host && r.mode !== 'android') h += kv('מחשב', '<span dir="ltr">' + esc(r.env.host) + '</span>');
    }
    if (r.net && r.net.ip) {
      h += kv('כתובת IP', '<span dir="ltr">' + esc(r.net.ip4 || r.net.ip) + '</span>');
      if (r.net.ip4 && r.net.ip4 !== r.net.ip) h += kv('כתובת IPv6', '<span dir="ltr">' + esc(r.net.ip) + '</span>');
      if (r.net.org || r.net.asn) h += kv('רשת (ASN)', '<span dir="ltr">' + esc((r.net.asn ? 'AS' + r.net.asn + ' ' : '') + (r.net.org || '')) + '</span>');
      if (r.net.whois) h += kv('רישום טווח הכתובות', '<span dir="ltr">' + esc(r.net.whois) + '</span>');
      if (r.net.ptr) h += kv('רישום הכתובת (PTR)', '<span dir="ltr">' + esc(r.net.ptr) + '</span>');
    }
    h += kv('גרסה', esc(r.version)) + '</tbody></table>';
    h += '</details>';

    var app = typeof NSNative !== 'undefined'; // inside the Android app
    h += '<div class="ns-actions">' + (app ? '<button type="button" class="ns-btn" id="ns-share">שיתוף התוצאה</button>' : '') +
      '<button type="button" class="ns-btn' + (app ? ' ns-btn2' : '') + '" id="ns-copy">העתקת סיכום</button>' +
      (app ? '<button type="button" class="ns-btn ns-btn2" id="ns-again">בדיקה חוזרת</button>' : '<button type="button" class="ns-btn ns-btn2" id="ns-print">הדפסה / PDF</button>') + '</div>';
    h += '<p class="ns-foot">הציון משוקלל מבדיקות מול שרתים אמיתיים, ולכן ייתכנו הבדלים קלים בין בדיקה לבדיקה. BSD מחשבים · 054-457-8946</p>';
    root.innerHTML = h;

    var cp = root.querySelector('#ns-copy');
    cp.onclick = function () {
      var t = summaryText(r, sc);
      function done() { cp.textContent = 'הועתק ✓'; setTimeout(function () { cp.textContent = 'העתקת סיכום'; }, 1800); }
      if (app) { NSNative.copy(t); done(); return; }
      if (navigator.clipboard && navigator.clipboard.writeText) navigator.clipboard.writeText(t).then(done, fallback); else fallback();
      function fallback() { var ta = document.createElement('textarea'); ta.value = t; document.body.appendChild(ta); ta.select(); try { document.execCommand('copy'); done(); } catch (e) { } ta.remove(); }
    };
    if (app) {
      root.querySelector('#ns-share').onclick = function () { NSNative.share(summaryText(r, sc)); };
      root.querySelector('#ns-again').onclick = function () { NSNative.restart(); };
      return sc;
    }
    root.querySelector('#ns-print').onclick = function () { var dt = root.querySelector('details'); if (dt) dt.open = true; window.print(); };
    return sc;
  }
  function kpi(label, v, unit) { return '<div class="ns-kpi"><span>' + label + '</span><b>' + v + '</b><small>' + unit + '</small></div>'; }
  function kv(k, v) { return '<tr><th>' + k + '</th><td>' + v + '</td></tr>'; }

  return { compute: compute, render: render, summaryText: summaryText, lineInfo: lineInfo };
})();
if (typeof module !== 'undefined') module.exports = NS;
/* BSD NetScore - browser engine. Produces the same result structure as the EXE (approximate measurements). */
var NSEngine = (function () {
  'use strict';
  var now = function () { return performance.now(); };
  var sleep = function (ms) { return new Promise(function (r) { setTimeout(r, ms); }); };
  var r1 = function (x) { return Math.round(x * 10) / 10; };
  var r2 = function (x) { return Math.round(x * 100) / 100; };
  function bust(u) { return u + (u.indexOf('?') < 0 ? '?' : '&') + 'ns=' + Date.now().toString(36) + Math.random().toString(36).slice(2, 7); }

  function probeSite(t, phaseSignal) {
    var res = { id: t.id, name: t.name, url: t.url, group: t.group, ok: false, blocked: false, status: 0, total_ms: 0 };
    var ac = new AbortController();
    var timer = setTimeout(function () { ac.abort(); }, 6000);
    var onAbort = function () { ac.abort(); };
    phaseSignal.addEventListener('abort', onAbort);
    var t0 = now();
    return fetch(t.url, { mode: 'no-cors', cache: 'no-store', credentials: 'omit', redirect: 'follow', referrerPolicy: 'no-referrer', signal: ac.signal })
      .then(function () { res.ok = true; }, function (e) {
        // A timeout is a real result. Any other failure in a browser is usually the site refusing to be
        // embedded (CORP / redirect loop), which says nothing about the line - mark it as not measurable.
        if (e && e.name === 'AbortError') res.err = 'timeout'; else { res.err = 'network'; res.skipped = true; }
      })
      .then(function () { res.total_ms = r1(now() - t0); clearTimeout(timer); phaseSignal.removeEventListener('abort', onAbort); return res; });
  }

  function runSites(sites, onProgress) {
    var out = new Array(sites.length), next = 0, done = 0;
    var phase = new AbortController();
    var cap = setTimeout(function () { phase.abort(); }, 25000);
    function worker() {
      if (next >= sites.length) return Promise.resolve();
      var i = next++;
      var p = phase.signal.aborted
        ? Promise.resolve({ id: sites[i].id, name: sites[i].name, url: sites[i].url, group: sites[i].group, ok: false, blocked: false, status: 0, total_ms: 0, err: 'timeout' })
        : probeSite(sites[i], phase.signal);
      return p.then(function (r) { out[i] = r; onProgress(++done, sites.length); return worker(); });
    }
    var ws = []; for (var k = 0; k < 8; k++) ws.push(worker());
    return Promise.all(ws).then(function () { clearTimeout(cap); return out; });
  }

  async function latency(urls) {
    var best = { ok: false, url: urls[0] || '', samples: [], median_ms: 0, jitter_ms: 0, lost: 0 };
    var deadline = now() + 4500;
    for (var u = 0; u < urls.length; u++) {
      var r = { ok: false, url: urls[u], samples: [], median_ms: 0, jitter_ms: 0, lost: 0 };
      for (var i = 0; i < 14 && now() < deadline; i++) {
        var ac = new AbortController(); var tm = setTimeout(function () { ac.abort(); }, 2000);
        var t0 = now(), ok = true;
        try { var resp = await fetch(bust(urls[u]), { mode: 'no-cors', cache: 'no-store', credentials: 'omit', signal: ac.signal }); try { await resp.arrayBuffer(); } catch (e) { } } catch (e) { ok = false; }
        clearTimeout(tm);
        var d = now() - t0;
        if (i === 0) { if (!ok) break; continue; }
        if (i === 1) continue;
        if (ok) r.samples.push(r1(d)); else r.lost++;
        await sleep(40);
      }
      if (r.samples.length >= 3) {
        var s = r.samples.slice().sort(function (a, b) { return a - b; });
        r.median_ms = s[s.length >> 1];
        var seq = r.samples.slice();
        if (seq.length >= 6) seq.splice(seq.indexOf(Math.max.apply(null, seq)), 1); // ignore one stray spike
        var j = 0; for (var k = 1; k < seq.length; k++) j += Math.abs(seq[k] - seq[k - 1]);
        r.jitter_ms = r1(j / (seq.length - 1)); r.ok = true; return r;
      }
      best = r;
    }
    return best;
  }

  async function downAttempt(url, streams, durMs) {
    var res = { ok: false, mbps: 0, bytes: 0, streams: streams, url: url };
    var ac = new AbortController(), total = 0, stop = false;
    async function stream() {
      while (!stop) {
        try {
          var resp = await fetch(bust(url), { cache: 'no-store', credentials: 'omit', signal: ac.signal });
          if (!resp.ok || !resp.body) { await sleep(200); continue; }
          var rd = resp.body.getReader();
          for (;;) { var c = await rd.read(); if (c.done) break; total += c.value.length; }
        } catch (e) { if (!stop) await sleep(100); }
      }
    }
    var ps = []; for (var i = 0; i < streams; i++) ps.push(stream());
    var start = now(), rampAt = 0, rampBytes = 0, failed = false;
    while (now() - start < durMs) {
      await sleep(50);
      var el = now() - start;
      if (!rampAt && el >= 1200) { rampAt = el; rampBytes = total; }
      if (el >= 2000 && total === 0) { failed = true; break; }
    }
    var end = now() - start, endBytes = total;
    stop = true; ac.abort();
    res.bytes = endBytes;
    if (failed || !rampAt || endBytes === 0) { res.err = 'no data'; return res; }
    res.mbps = r2((endBytes - rampBytes) * 8 / ((end - rampAt) / 1000) / 1e6);
    res.ok = res.mbps > 0;
    return res;
  }
  async function down(urls) {
    var last = null, budget = 6000;
    for (var i = 0; i < Math.min(2, urls.length); i++) {
      last = await downAttempt(urls[i], 4, budget);
      if (last.ok) return last;
      budget = 4000;
    }
    return last || { ok: false, mbps: 0, bytes: 0, streams: 4, url: '', err: 'no endpoint' };
  }

  function randomBytes(n) {
    var a = new Uint8Array(n);
    for (var o = 0; o < n; o += 65536) crypto.getRandomValues(a.subarray(o, Math.min(n, o + 65536)));
    return a;
  }
  async function up(urls) {
    var url = urls[0], streams = 3, dur = 4500;
    var res = { ok: false, mbps: 0, bytes: 0, streams: streams, url: url || '' };
    if (!url) { res.err = 'no endpoint'; return res; }
    var pool = randomBytes(8 * 1024 * 1024);
    var ac = new AbortController(), events = [], stop = false, start = now();
    async function worker() {
      var size = 128 * 1024;
      while (!stop) {
        var t0 = now();
        try {
          var resp = await fetch(url, { method: 'POST', body: new Blob([pool.subarray(0, size)]), cache: 'no-store', credentials: 'omit', signal: ac.signal });
          if (!resp.ok) { await sleep(200); continue; }
          try { await resp.arrayBuffer(); } catch (e) { }
        } catch (e) { if (!stop) await sleep(150); continue; }
        var dt = now() - t0;
        events.push({ t: now() - start, b: size });
        if (dt < 250 && size < pool.length) size *= 2; else if (dt > 1000 && size > 65536) size /= 2;
      }
    }
    for (var i = 0; i < streams; i++) worker();
    await sleep(dur);
    stop = true; ac.abort();
    var total = 0, after = 0, last = 0, ramp = 1000;
    events.forEach(function (e) { total += e.b; if (e.t > ramp) after += e.b; if (e.t > last) last = e.t; });
    res.bytes = total;
    if (!events.length) { res.err = 'upload failed'; return res; }
    res.mbps = last > ramp + 500 && after > 0 ? r2(after * 8 / ((last - ramp) / 1000) / 1e6) : r2(total * 8 / (last / 1000) / 1e6);
    res.ok = res.mbps > 0;
    return res;
  }

  async function realDownload(t) {
    var res = { id: t.id, name: t.name, url: t.url, group: t.group, ok: false, blocked: false, status: 0, ttfb_ms: 0, mbps: 0, bytes: 0, secs: 0, completed: false };
    var ac = new AbortController();
    var hard = setTimeout(function () { ac.abort(); }, 5500), win = null;
    var start = now();
    try {
      var resp = await fetch(t.url, { cache: 'no-store', credentials: 'omit', signal: ac.signal });
      res.status = resp.status; res.ttfb_ms = r1(now() - start);
      if (!resp.ok || !resp.body) { res.err = 'http ' + resp.status; clearTimeout(hard); ac.abort(); return res; }
      win = setTimeout(function () { ac.abort(); }, 3500);
      var rd = resp.body.getReader(), t0 = now(), total = 0, skipAt = 0, skipBytes = 0;
      try {
        for (;;) {
          var c = await rd.read();
          if (c.done) { res.completed = true; break; }
          total += c.value.length;
          var el = now() - t0;
          if (!skipAt && el >= 400) { skipAt = el; skipBytes = total; }
        }
      } catch (e) { /* window closed */ }
      var el2 = now() - t0;
      res.bytes = total; res.secs = r2(el2 / 1000);
      if (total < 20000) { res.err = 'no data'; }
      else {
        res.mbps = skipAt && el2 - skipAt >= 1000 ? r2((total - skipBytes) * 8 / ((el2 - skipAt) / 1000) / 1e6) : r2(total * 8 / (el2 / 1000) / 1e6);
        res.ok = true;
      }
    } catch (e) { res.err = e && e.name === 'AbortError' ? 'timeout' : 'network'; }
    clearTimeout(hard); if (win) clearTimeout(win);
    return res;
  }

  function getJSON(u, ms, headers) {
    var ac = new AbortController(), tm = setTimeout(function () { ac.abort(); }, ms);
    return fetch(u, { cache: 'no-store', credentials: 'omit', referrerPolicy: 'no-referrer', headers: headers || {}, signal: ac.signal })
      .then(function (x) { return x.ok ? x.json() : null; }).catch(function () { return null; })
      .then(function (j) { clearTimeout(tm); return j; });
  }
  function asnNum(v) { var m = /(\d+)/.exec(String(v == null ? '' : v)); return m ? parseInt(m[1], 10) : 0; }

  // public address + network owner + hints about the line (no measurements)
  async function identifyLine() {
    var n = { ip: '', ip4: '', asn: 0, org: '', country: '', ptr: '', whois: '', local: '', netfree: false, netfree_isp: '', source: '' };
    var nf = (function () { // NetFree's internal address answers only from inside NetFree
      var ac = new AbortController(), tm = setTimeout(function () { ac.abort(); }, 3000);
      return fetch('https://api.internal.netfree.link/user/0', { mode: 'no-cors', cache: 'no-store', credentials: 'omit', signal: ac.signal })
        .then(function () { return true; }, function () { return false; }).then(function (v) { clearTimeout(tm); return v; });
    })();
    var g = await getJSON('https://get.geojs.io/v1/ip/geo.json', 5000);
    if (g && g.ip) { n.ip = g.ip; n.asn = asnNum(g.asn); n.org = g.organization_name || ''; n.country = g.country_code || ''; n.source = 'geojs'; }
    if (!n.asn) {
      var w = await getJSON('https://ipwho.is/', 5000);
      if (w && w.ip && w.connection) { n.ip = w.ip; n.asn = asnNum(w.connection.asn); n.org = w.connection.isp || w.connection.org || ''; n.country = w.country_code || ''; n.source = 'ipwho.is'; }
    }
    if (!n.ip) {
      try {
        var ac = new AbortController(), tm = setTimeout(function () { ac.abort(); }, 4000);
        var t = await (await fetch('https://www.cloudflare.com/cdn-cgi/trace', { cache: 'no-store', credentials: 'omit', signal: ac.signal })).text();
        clearTimeout(tm);
        var m = /^ip=(.+)$/m.exec(t), c = /^loc=(.+)$/m.exec(t);
        if (m) { n.ip = m[1].trim(); n.source = 'cloudflare'; }
        if (c && !n.country) n.country = c[1].trim();
      } catch (e) { }
    }
    if (n.ip.indexOf(':') >= 0) { // also learn the IPv4 address (this host has no IPv6)
      try {
        var ac4 = new AbortController(), tm4 = setTimeout(function () { ac4.abort(); }, 2500);
        var t4 = await (await fetch('https://1.1.1.1/cdn-cgi/trace', { cache: 'no-store', credentials: 'omit', signal: ac4.signal })).text();
        clearTimeout(tm4);
        var m4 = /^ip=(\d+\.\d+\.\d+\.\d+)\s*$/m.exec(t4);
        if (m4) n.ip4 = m4[1];
      } catch (e) { }
    }
    // registry (RIPE) name / description of the address range, e.g. "PARTNERCOM-CELLULAR-NETS"
    var ws = [], ips = [n.ip4, n.ip].filter(function (x) { return x; });
    for (var k = 0; k < ips.length; k++) {
      var wj = await getJSON('https://stat.ripe.net/data/whois/data.json?resource=' + encodeURIComponent(ips[k]), 6000);
      var rec = wj && wj.data && wj.data.records && wj.data.records[0];
      if (rec) { var parts = rec.filter(function (x) { return x.key === 'netname' || x.key === 'descr'; }).map(function (x) { return String(x.value).trim(); }); if (parts.length) ws.push(parts.join(' | ')); }
    }
    n.whois = ws.join(' || ');
    var v4 = n.ip4 || n.ip;
    if (/^\d+\.\d+\.\d+\.\d+$/.test(v4)) { // reverse name of the address (IPv4 only)
      var d = await getJSON('https://cloudflare-dns.com/dns-query?name=' + v4.split('.').reverse().join('.') + '.in-addr.arpa&type=PTR', 3000, { accept: 'application/dns-json' });
      if (d && d.Answer && d.Answer.length && d.Answer[0].data) n.ptr = String(d.Answer[0].data).replace(/\.$/, '');
    }
    var c2 = navigator.connection || navigator.mozConnection || navigator.webkitConnection;
    if (c2 && (c2.type === 'cellular' || c2.type === 'wifi' || c2.type === 'ethernet')) n.local = c2.type;
    n.netfree = await nf;
    return n;
  }

  async function run(cfg, onPhase) {
    var res = { version: cfg.version || '1.2.0', mode: 'web', started: new Date().toISOString(), duration_s: 0,
      env: { os: navigator.platform || '', host: '', proxy: '', filter: { detected: false, name: '', issuer: '' } },
      sites: [], latency: {}, bandwidth: { down: {}, up: {} }, downloads: [] };
    var t0 = now();
    onPhase('מזהה את החיבור…', 1);
    res.net = await identifyLine();
    onPhase('בודק אתרים אמיתיים בארץ ובחו"ל…', 2);
    res.sites = await runSites(cfg.sites, function (d, n) { onPhase('בודק אתרים אמיתיים (' + d + '/' + n + ')…', 2 + 20 * d / n); });
    onPhase('מודד זמן תגובה ויציבות…', 24);
    res.latency = await latency(cfg.bandwidth.ping);
    onPhase('מודד מהירות הורדה גולמית…', 32);
    res.bandwidth.down = await down(cfg.bandwidth.down);
    onPhase('מודד מהירות העלאה…', 48);
    res.bandwidth.up = await up(cfg.bandwidth.up);
    var dl = cfg.web_downloads || [];
    for (var i = 0; i < dl.length; i++) {
      onPhase('מוריד קבצים אמיתיים (' + (i + 1) + '/' + dl.length + ')…', 60 + 38 * i / dl.length);
      res.downloads.push(await realDownload(dl[i]));
    }
    res.duration_s = r1((now() - t0) / 1000);
    onPhase('סיום', 100);
    return res;
  }
  return { run: run };
})();
var NS_CONFIG = {"sites": [{"id": "gov", "name": "gov.il", "url": "https://www.gov.il/", "group": "il"}, {"id": "btl", "name": "ביטוח לאומי", "url": "https://www.btl.gov.il/", "group": "il"}, {"id": "leumi", "name": "בנק לאומי", "url": "https://www.leumi.co.il/", "group": "il"}, {"id": "mercantile", "name": "בנק מרכנתיל", "url": "https://www.mercantile.co.il/", "group": "il"}, {"id": "pagi", "name": "בנק פאגי", "url": "https://www.pagi.co.il/", "group": "il"}, {"id": "post", "name": "דואר ישראל", "url": "https://www.israelpost.co.il/", "group": "il"}, {"id": "zap", "name": "זאפ", "url": "https://www.zap.co.il/", "group": "il"}, {"id": "ksp", "name": "KSP", "url": "https://ksp.co.il/", "group": "il"}, {"id": "kikar", "name": "כיכר השבת", "url": "https://www.kikar.co.il/", "group": "il"}, {"id": "kore", "name": "כל רגע", "url": "https://www.kore.co.il/", "group": "il"}, {"id": "jdn", "name": "JDN", "url": "https://www.jdn.co.il/", "group": "il"}, {"id": "hidabroot", "name": "הידברות", "url": "https://www.hidabroot.org/", "group": "il"}, {"id": "kolhalashon", "name": "קול הלשון", "url": "https://www.kolhalashon.com/", "group": "il"}, {"id": "yemot", "name": "ימות המשיח", "url": "https://www.yemot.co.il/", "group": "il"}, {"id": "magicode", "name": "Send מג'יקוד", "url": "https://send.magicode.me/", "group": "il"}, {"id": "clalit", "name": "כללית", "url": "https://www.clalit.co.il/", "group": "il"}, {"id": "maccabi", "name": "מכבי", "url": "https://www.maccabi4u.co.il/", "group": "il"}, {"id": "meuhedet", "name": "מאוחדת", "url": "https://www.meuhedet.co.il/", "group": "il"}, {"id": "leumit", "name": "לאומית", "url": "https://www.leumit.co.il/", "group": "il"}, {"id": "iec", "name": "חברת החשמל", "url": "https://www.iec.co.il/", "group": "il"}, {"id": "bezeq", "name": "בזק", "url": "https://www.bezeq.co.il/", "group": "il"}, {"id": "egged", "name": "אגד", "url": "https://www.egged.co.il/", "group": "il"}, {"id": "ravkav", "name": "רב-קו אונליין", "url": "https://ravkavonline.co.il/", "group": "il"}, {"id": "mizrahi", "name": "מזרחי טפחות", "url": "https://www.mizrahi-tefahot.co.il/", "group": "il"}, {"id": "poalim", "name": "בנק הפועלים", "url": "https://www.bankhapoalim.co.il/", "group": "il"}, {"id": "discount", "name": "בנק דיסקונט", "url": "https://www.discountbank.co.il/", "group": "il"}, {"id": "fibi", "name": "הבינלאומי", "url": "https://www.fibi.co.il/", "group": "il"}, {"id": "max", "name": "max", "url": "https://www.max.co.il/", "group": "il"}, {"id": "isracard", "name": "ישראכרט", "url": "https://www.isracard.co.il/", "group": "il"}, {"id": "shufersal", "name": "שופרסל", "url": "https://www.shufersal.co.il/", "group": "il"}, {"id": "ramilevy", "name": "רמי לוי", "url": "https://www.rami-levy.co.il/", "group": "il"}, {"id": "superpharm", "name": "סופר-פארם", "url": "https://www.super-pharm.co.il/", "group": "il"}, {"id": "ivory", "name": "Ivory", "url": "https://www.ivory.co.il/", "group": "il"}, {"id": "bug", "name": "Bug", "url": "https://www.bug.co.il/", "group": "il"}, {"id": "yad2", "name": "יד2", "url": "https://www.yad2.co.il/", "group": "il"}, {"id": "govmap", "name": "GovMap", "url": "https://www.govmap.gov.il/", "group": "il"}, {"id": "ims", "name": "השירות המטאורולוגי", "url": "https://www.ims.gov.il/", "group": "il"}, {"id": "otzar", "name": "אוצר החכמה", "url": "https://www.otzar.org/", "group": "il"}, {"id": "hamichlol", "name": "המכלול", "url": "https://www.hamichlol.org.il/", "group": "il"}, {"id": "emess", "name": "אמס", "url": "https://www.emess.co.il/", "group": "il"}, {"id": "google", "name": "Google", "url": "https://www.google.com/", "group": "world"}, {"id": "gmail", "name": "Gmail", "url": "https://mail.google.com/", "group": "world"}, {"id": "drive", "name": "Google Drive", "url": "https://drive.google.com/", "group": "world"}, {"id": "microsoft", "name": "Microsoft", "url": "https://www.microsoft.com/he-il", "group": "world"}, {"id": "office", "name": "Office 365", "url": "https://www.office.com/", "group": "world"}, {"id": "amazon", "name": "Amazon", "url": "https://www.amazon.com/", "group": "world"}, {"id": "aliexpress", "name": "AliExpress", "url": "https://www.aliexpress.com/", "group": "world"}, {"id": "github", "name": "GitHub", "url": "https://github.com/", "group": "world"}, {"id": "wikipedia", "name": "ויקיפדיה", "url": "https://he.wikipedia.org/wiki/", "group": "world"}, {"id": "hebrewbooks", "name": "HebrewBooks", "url": "https://hebrewbooks.org/", "group": "world"}, {"id": "sefaria", "name": "ספריא", "url": "https://www.sefaria.org.il/", "group": "world"}, {"id": "apple", "name": "Apple", "url": "https://www.apple.com/", "group": "world"}, {"id": "bing", "name": "Bing", "url": "https://www.bing.com/", "group": "world"}, {"id": "cloudflare", "name": "Cloudflare", "url": "https://www.cloudflare.com/", "group": "world"}, {"id": "dropbox", "name": "Dropbox", "url": "https://www.dropbox.com/", "group": "world"}, {"id": "zoom", "name": "Zoom", "url": "https://zoom.us/", "group": "world"}, {"id": "mozilla", "name": "Mozilla", "url": "https://www.mozilla.org/", "group": "world"}, {"id": "python", "name": "Python.org", "url": "https://www.python.org/", "group": "world"}, {"id": "lenovo", "name": "Lenovo", "url": "https://www.lenovo.com/", "group": "world"}, {"id": "dell", "name": "Dell", "url": "https://www.dell.com/", "group": "world"}, {"id": "hp", "name": "HP Support", "url": "https://support.hp.com/", "group": "world"}, {"id": "nvidia", "name": "NVIDIA", "url": "https://www.nvidia.com/", "group": "world"}, {"id": "epson", "name": "Epson", "url": "https://www.epson.com/", "group": "world"}, {"id": "brother", "name": "Brother", "url": "https://www.brother.com/", "group": "world"}, {"id": "teamviewer", "name": "TeamViewer", "url": "https://www.teamviewer.com/", "group": "world"}, {"id": "waze", "name": "Waze", "url": "https://www.waze.com/", "group": "world"}, {"id": "translate", "name": "Google Translate", "url": "https://translate.google.com/", "group": "world"}], "bandwidth": {"ping": ["https://speed.cloudflare.com/__down?bytes=0", "https://www.gstatic.com/generate_204"], "down": ["https://speed.cloudflare.com/__down?bytes=99999999", "https://proof.ovh.net/files/1Gb.dat"], "up": ["https://speed.cloudflare.com/__up"]}, "web_downloads": [{"id": "google", "name": "Google Cloud (קובץ 29MB)", "url": "https://storage.googleapis.com/mediapipe-models/pose_landmarker/pose_landmarker_heavy/float16/1/pose_landmarker_heavy.task", "group": "world"}, {"id": "jsdelivr", "name": "jsDelivr CDN (קובץ 21MB)", "url": "https://cdn.jsdelivr.net/npm/onnxruntime-web@1.19.2/dist/ort-wasm-simd-threaded.jsep.wasm", "group": "world"}, {"id": "github", "name": "GitHub (קובץ 23MB)", "url": "https://raw.githubusercontent.com/tesseract-ocr/tessdata/main/eng.traineddata", "group": "world"}, {"id": "huggingface", "name": "Hugging Face (קובץ 90MB)", "url": "https://huggingface.co/Xenova/all-MiniLM-L6-v2/resolve/main/onnx/model.onnx", "group": "world"}], "version": "1.2.0"};
(function () {
  var go = document.getElementById('ns-go'), prog = document.getElementById('ns-prog'), bar = document.getElementById('ns-prog-i'),
      phase = document.getElementById('ns-phase'), root = document.getElementById('ns-root');
  go.onclick = function () {
    go.disabled = true; go.textContent = 'בודק…'; prog.hidden = false; root.innerHTML = '';
    NSEngine.run(NS_CONFIG, function (text, pct) { phase.textContent = text; bar.style.width = pct + '%'; })
      .then(function (res) {
        NS.render(root, res);
        phase.textContent = 'הבדיקה הסתיימה (' + res.duration_s + ' שניות).';
        root.scrollIntoView({ behavior: 'smooth', block: 'start' });
      })
      .catch(function (e) { phase.textContent = 'הבדיקה נכשלה: ' + (e && e.message || e); })
      .then(function () { go.disabled = false; go.textContent = 'בדיקה חוזרת'; });
  };
})();
