// Shared helpers. Everything that reaches the page from a source goes
// through h(), which only ever sets text, never HTML.

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
    throw new ApiError(0, 'offline', 'Không kết nối được tới Q3VNLaw. Ứng dụng có thể đã thoát.');
  }
  let data = {};
  try { data = await resp.json(); } catch (e) { /* empty body */ }
  if (!resp.ok) {
    const err = data.error || {};
    throw new ApiError(resp.status, err.code || 'error', err.message || ('Lỗi ' + resp.status));
  }
  return data;
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

const pad = (n) => String(n).padStart(2, '0');

export function fmtTime(iso) {
  if (!iso) return '';
  const d = new Date(iso);
  if (isNaN(d)) return iso;
  return `${pad(d.getHours())}:${pad(d.getMinutes())} ${pad(d.getDate())}/${pad(d.getMonth() + 1)}/${d.getFullYear()}`;
}

// fmtDate formats a YYYY-MM-DD date the Vietnamese way.
export function fmtDate(ymd) {
  const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(ymd || '');
  return m ? `${m[3]}/${m[2]}/${m[1]}` : (ymd || '');
}

export function ago(iso) {
  if (!iso) return '';
  const s = (Date.now() - new Date(iso).getTime()) / 1000;
  if (s < 60) return 'vừa xong';
  if (s < 3600) return Math.floor(s / 60) + ' phút trước';
  if (s < 86400) return Math.floor(s / 3600) + ' giờ trước';
  if (s < 7 * 86400) return Math.floor(s / 86400) + ' ngày trước';
  return fmtTime(iso);
}

export const labels = {
  severity: { info: 'Thông tin', notice: 'Cần chú ý', warning: 'Cảnh báo' },
  status: { proposal: 'Đề xuất', draft: 'Dự thảo', issued: 'Đã ban hành', effective: 'Đã có hiệu lực', other: 'Tin liên quan', unknown: 'Chưa phân loại' },
  kind: { press: 'Báo chí', official: 'Văn bản chính thức', effective_soon: 'Sắp có hiệu lực', doc_changed: 'Văn bản theo dõi thay đổi' },
  verified: { confirmed: 'Đã xác nhận từ nguồn chính thức', unconfirmed: 'Chưa xác nhận từ nguồn chính thức' },
  tier: { 1: 'Nguồn chính thức', 2: 'CSDL pháp luật', 3: 'Báo chí' },
  validity: { in_force: 'Đang có hiệu lực', not_yet: 'Chưa có hiệu lực', superseded: 'Đã bị thay thế', amended: 'Đã được sửa đổi', unknown: 'Chưa rõ' },
  relation: { amends: 'sửa đổi, bổ sung', replaces: 'thay thế', repeals: 'bãi bỏ', guides: 'hướng dẫn', consolidates: 'hợp nhất', mentions: 'nhắc tới' },
  trigger: { schedule: 'Theo lịch', manual: 'Quét tay', catchup: 'Quét bù', backfill: 'Áp dụng chủ đề' },
  sourceStatus: { ok: 'Tốt', unchanged: 'Không đổi', empty: 'Không có tin', error: 'Lỗi', '': 'Chưa quét' },
};

export function ext(url, text) {
  return h('a', { href: url, target: '_blank', rel: 'noopener noreferrer' }, text);
}

export function splitList(text) {
  return (text || '').split(/[,;\n]/).map((s) => s.trim()).filter(Boolean);
}

export function empty(title, text) {
  return h('div', { class: 'empty' }, h('b', null, title), text);
}

// fill replaces the content of el. Unlike Element.append it skips null and
// false, so views can write `cond ? node : null` among the children.
export function fill(el, ...kids) {
  el.replaceChildren();
  add(el, kids);
  return el;
}
