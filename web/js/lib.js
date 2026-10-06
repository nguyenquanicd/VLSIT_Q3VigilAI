// Shared helpers. Everything that reaches the page from a source goes
// through h(), which only ever sets text, never HTML.

// ---- translation -----------------------------------------------------------
// The source text of the UI is Vietnamese. t() looks a message up in the
// English dictionary served by /api/i18n (empty while the language is
// Vietnamese) and falls back to the message itself, so a missing translation
// shows Vietnamese rather than nothing. Placeholders are {0}, {1}, …
// N() marks a message for translation where it stands without translating it
// yet: a table is declared once and translated each time it is read.
let dict = {};
let current = 'vi';

export function setI18n(lang, d) { current = lang === 'en' ? 'en' : 'vi'; dict = d || {}; document.documentElement.lang = current; }
export function lang() { return current; }

export function t(msg, ...args) {
  let out = dict[msg] || msg;
  args.forEach((a, i) => { out = out.split('{' + i + '}').join(String(a)); });
  return out;
}

export const N = (msg) => msg;

// tc is t() for a message that needs a context: the dictionary holds it as
// "context|message" and falls back to the plain message ("Cảnh báo" is both
// the alerts list and the highest alert level).
export function tc(context, msg, ...args) {
  return dict[context + '|' + msg] ? t(context + '|' + msg, ...args) : t(msg, ...args);
}

// group wraps a table of N() messages so that reading an entry translates it.
function group(table, context) {
  return new Proxy(table, {
    get: (o, k) => (typeof o[k] === 'string' && o[k] !== '' ? (context ? tc(context, o[k]) : t(o[k])) : o[k]),
  });
}

// ---- DOM -------------------------------------------------------------------
export function h(tag, attrs, ...kids) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (v == null || v === false) continue;
    if (k === 'class') el.className = v;
    else if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
    else if (k === 'value') el.value = v;
    else if (k === 'checked' || k === 'disabled' || k === 'selected' || k === 'open') el[k] = !!v;
    else el.setAttribute(k, v === true ? '' : String(v));
  }
  add(el, kids);
  return el;
}

function add(el, kids) {
  for (const k of kids) {
    if (k == null || k === false) continue;
    if (Array.isArray(k)) add(el, k);
    else el.append(k instanceof Node ? k : document.createTextNode(String(k)));
  }
}

export function clear(el) { el.replaceChildren(); return el; }

// fill replaces the content of el. Unlike Element.append it skips null and
// false, so views can write `cond ? node : null` among the children.
export function fill(el, ...kids) {
  el.replaceChildren();
  add(el, kids);
  return el;
}

// ---- API -------------------------------------------------------------------
export class ApiError extends Error {
  constructor(status, code, message) { super(message); this.status = status; this.code = code; }
}

export async function api(method, path, body) {
  const opt = { method, headers: {}, credentials: 'same-origin' };
  if (body !== undefined) {
    opt.headers['Content-Type'] = 'application/json';
    opt.body = JSON.stringify(body);
  }
  let resp;
  try {
    resp = await fetch('/api' + path, opt);
  } catch (e) {
    throw new ApiError(0, 'offline', t('Không kết nối được tới Q3VigilAI. Ứng dụng có thể đã thoát.'));
  }
  let data = {};
  try { data = await resp.json(); } catch (e) { /* empty body */ }
  if (!resp.ok) {
    const err = data.error || {};
    throw new ApiError(resp.status, err.code || 'error', err.message || t('Lỗi {0}', resp.status));
  }
  return data;
}

export async function loadI18n() {
  const r = await api('GET', '/i18n');
  setI18n(r.lang, r.dict);
}

export function toast(message, bad) {
  const el = h('div', { class: 'toast' + (bad ? ' bad' : '') }, message);
  document.getElementById('toasts').append(el);
  setTimeout(() => el.remove(), bad ? 7000 : 3500);
}

// run wraps an async action: errors become a toast instead of vanishing.
export function run(fn) {
  return async (...args) => {
    try { return await fn(...args); } catch (e) { toast(e.message || String(e), true); }
  };
}

// ---- dates -----------------------------------------------------------------
const pad = (n) => String(n).padStart(2, '0');
const months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];

// Vietnamese: 22:25 05/10/2026. English: 5 Oct 2026, 22:25.
export function fmtTime(iso) {
  if (!iso) return '';
  const d = new Date(iso);
  if (isNaN(d)) return iso;
  if (current === 'en') return `${d.getDate()} ${months[d.getMonth()]} ${d.getFullYear()}, ${pad(d.getHours())}:${pad(d.getMinutes())}`;
  return `${pad(d.getHours())}:${pad(d.getMinutes())} ${pad(d.getDate())}/${pad(d.getMonth() + 1)}/${d.getFullYear()}`;
}

// fmtDate formats a YYYY-MM-DD date.
export function fmtDate(ymd) {
  const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(ymd || '');
  if (!m) return ymd || '';
  if (current === 'en') return `${Number(m[3])} ${months[Number(m[2]) - 1]} ${m[1]}`;
  return `${m[3]}/${m[2]}/${m[1]}`;
}

export function ago(iso) {
  if (!iso) return '';
  const s = (Date.now() - new Date(iso).getTime()) / 1000;
  if (s < 60) return t('vừa xong');
  if (s < 3600) return t('{0} phút trước', Math.floor(s / 60));
  if (s < 86400) return t('{0} giờ trước', Math.floor(s / 3600));
  if (s < 7 * 86400) return t('{0} ngày trước', Math.floor(s / 86400));
  return fmtTime(iso);
}

// ---- display names of the values the server sends as codes ------------------
export const labels = {
  severity: group({ info: N('Thông tin'), notice: N('Cần chú ý'), warning: N('Cảnh báo') }, 'mức'),
  status: group({ proposal: N('Đề xuất'), draft: N('Dự thảo'), issued: N('Đã ban hành'), effective: N('Đã có hiệu lực'), other: N('Tin liên quan'), unknown: N('Chưa phân loại') }),
  kind: group({ press: N('Báo chí'), official: N('Văn bản chính thức'), effective_soon: N('Sắp có hiệu lực'), doc_changed: N('Văn bản theo dõi thay đổi') }),
  verified: group({ confirmed: N('Đã xác nhận từ nguồn chính thức'), unconfirmed: N('Chưa xác nhận từ nguồn chính thức') }),
  tier: group({ 1: N('Nguồn chính thức'), 2: N('CSDL pháp luật'), 3: N('Báo chí') }),
  validity: group({ in_force: N('Đang có hiệu lực'), not_yet: N('Chưa có hiệu lực'), superseded: N('Đã bị thay thế'), amended: N('Đã được sửa đổi'), unknown: N('Chưa rõ') }),
  relation: group({ amends: N('sửa đổi, bổ sung'), replaces: N('thay thế'), repeals: N('bãi bỏ'), guides: N('hướng dẫn'), consolidates: N('hợp nhất'), mentions: N('nhắc tới') }),
  trigger: group({ schedule: N('Theo lịch'), manual: N('Quét tay'), catchup: N('Quét bù'), backfill: N('Áp dụng chủ đề') }),
  sourceStatus: group({ ok: N('Tốt'), unchanged: N('Không đổi'), empty: N('Không có tin'), error: N('Lỗi'), '': N('Chưa quét') }),
  relevance: group({ high: N('cao'), medium: N('trung bình'), low: N('thấp') }),
};

// ---- small helpers ---------------------------------------------------------
export function ext(url, text) {
  return h('a', { href: url, target: '_blank', rel: 'noopener noreferrer' }, text);
}

export function splitList(text) {
  return (text || '').split(/[,;\n]/).map((s) => s.trim()).filter(Boolean);
}

export function empty(title, text) {
  return h('div', { class: 'empty' }, h('b', null, title), text);
}
