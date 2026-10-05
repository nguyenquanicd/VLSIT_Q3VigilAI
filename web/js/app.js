import { h, clear, fill, api, toast, run, fmtTime } from './lib.js';
import * as alerts from './views/alerts.js';
import * as chat from './views/chat.js';
import * as topics from './views/topics.js';
import * as sources from './views/sources.js';
import * as docs from './views/docs.js';
import * as logs from './views/logs.js';
import * as settings from './views/settings.js';

const views = { alerts, chat, topics, sources, docs, logs, settings };
const menu = [
  ['alerts', 'Cảnh báo'], ['chat', 'Chat'], ['topics', 'Chủ đề theo dõi'], ['sources', 'Nguồn'],
  ['docs', 'Văn bản'], ['logs', 'Nhật ký quét'], ['settings', 'Cài đặt'],
];

export const app = { status: null, meta: null, current: null };

const $ = (id) => document.getElementById(id);

function renderNav(active) {
  const unread = app.status ? app.status.unread : 0;
  fill($('nav'), ...menu.map(([key, label]) =>
    h('a', { href: '#/' + key, class: key === active ? 'on' : '' }, label,
      key === 'alerts' && unread > 0 ? h('span', { class: 'count' }, unread) : null)));
}

function renderStatus() {
  const s = app.status;
  if (!s) return;
  const ai = s.ai.error ? ['bad', 'AI lỗi: ' + s.ai.error]
    : s.ai.provider ? ['ok', 'AI: ' + s.ai.provider] : ['', 'Không có AI (chỉ từ khóa)'];
  const scan = s.scanning ? ['busy', 'Đang quét nguồn…']
    : s.last_scan ? ['ok', 'Quét gần nhất ' + fmtTime(s.last_scan.finished_at || s.last_scan.started_at)]
      : ['', 'Chưa quét lần nào'];
  fill($('side-status'), 
    h('div', { title: s.ai.note || '' }, h('span', { class: 'dot ' + ai[0] }), ai[1]),
    h('div', null, h('span', { class: 'dot ' + scan[0] }), scan[1]),
    s.sources_failing > 0 ? h('div', null, h('span', { class: 'dot bad' }), h('a', { href: '#/sources' }, s.sources_failing + ' nguồn không quét được')) : null,
    s.muted ? h('div', null, h('span', { class: 'dot' }), 'Thông báo đang tạm im') : null,
    h('div', null, 'Phiên bản ' + s.version));
}

export async function refreshStatus() {
  app.status = await api('GET', '/status');
  renderStatus();
  renderNav(app.current);
}

function scanButton() {
  const busy = app.status && app.status.scanning;
  return h('button', {
    class: 'primary', disabled: busy,
    onclick: run(async () => { await api('POST', '/scan'); toast('Đã bắt đầu quét.'); await refreshStatus(); route(); }),
  }, busy ? 'Đang quét…' : 'Quét ngay');
}

async function route() {
  const parts = location.hash.replace(/^#\/?/, '').split('/').filter(Boolean);
  const key = views[parts[0]] ? parts[0] : 'alerts';
  app.current = key;
  renderNav(key);
  $('title').textContent = views[key].title;
  fill($('top-actions'), scanButton());
  const view = clear($('view'));
  try {
    await views[key].render({ view, actions: $('top-actions'), params: parts.slice(1), app });
  } catch (e) {
    view.append(h('div', { class: 'banner bad' }, e.message || String(e)));
  }
}

function listen() {
  const es = new EventSource('/api/events');
  const on = (name) => es.addEventListener(name, async (ev) => {
    let data = {};
    try { data = JSON.parse(ev.data); } catch (e) { /* ignore */ }
    try { await refreshStatus(); } catch (e) { /* shown elsewhere */ }
    const v = views[app.current];
    if (name !== 'scan.progress') fill($('top-actions'), scanButton());
    if (v && v.onEvent) v.onEvent(name, data);
    if (name === 'scan.finished') {
      const note = data.note ? ' ' + data.note : '';
      toast(`Quét xong: ${data.items_new || 0} tin mới, ${data.alerts_new || 0} cảnh báo.` + note, (data.sources_failed || 0) > 0);
    }
  });
  ['scan.started', 'scan.progress', 'scan.finished', 'alert.created', 'provider.changed'].forEach(on);
}

async function start() {
  try {
    [app.status, app.meta] = await Promise.all([api('GET', '/status'), api('GET', '/meta')]);
  } catch (e) {
    fill(document.body, h('div', { class: 'empty' },
      h('b', null, 'Chưa mở được Q3VNLaw'),
      e.status === 401 ? 'Phiên làm việc đã hết. Hãy bấm vào biểu tượng Q3VNLaw ở khay hệ thống (góc phải thanh tác vụ) để mở lại.' : e.message));
    return;
  }
  renderStatus();
  window.addEventListener('hashchange', route);
  listen();
  await route();
}

start();
