import { h, clear, fill, api, toast, run, fmtTime, t, N, loadI18n, lang } from './lib.js';
import * as alerts from './views/alerts.js';
import * as chat from './views/chat.js';
import * as topics from './views/topics.js';
import * as sources from './views/sources.js';
import * as docs from './views/docs.js';
import * as logs from './views/logs.js';
import * as settings from './views/settings.js';
import * as about from './views/about.js';

const views = { alerts, chat, topics, sources, docs, logs, settings, about };
const menu = [
  ['alerts', N('Cảnh báo')], ['chat', N('Chat')], ['topics', N('Chủ đề theo dõi')], ['sources', N('Nguồn')],
  ['docs', N('Văn bản')], ['logs', N('Nhật ký quét')], ['settings', N('Cài đặt')], ['about', N('Trợ giúp & giới thiệu')],
];

export const app = { status: null, meta: null, current: null };

const $ = (id) => document.getElementById(id);

function renderNav(active) {
  const unread = app.status ? app.status.unread : 0;
  fill($('nav'), ...menu.map(([key, label]) =>
    h('a', { href: '#/' + key, class: key === active ? 'on' : '' }, t(label),
      key === 'alerts' && unread > 0 ? h('span', { class: 'count' }, unread) : null)));
}

// setLanguage saves the choice; the page is then reloaded so that everything,
// including the texts the server wrote, comes back in the new language.
export const setLanguage = run(async (value) => {
  await api('PUT', '/settings', { language: value });
  location.reload();
});

export function languageSelect() {
  return h('select', { 'aria-label': t('Ngôn ngữ'), onchange: (e) => setLanguage(e.target.value) },
    h('option', { value: 'vi', selected: lang() === 'vi' }, 'Tiếng Việt'),
    h('option', { value: 'en', selected: lang() === 'en' }, 'English'));
}

function renderStatus() {
  const s = app.status;
  if (!s) return;
  const ai = s.ai.error ? ['bad', t('AI lỗi: {0}', s.ai.error)]
    : s.ai.provider ? ['ok', t('AI: {0}', s.ai.provider)] : ['', t('Không có AI (chỉ từ khóa)')];
  const scan = s.scanning ? ['busy', t('Đang quét nguồn…')]
    : s.last_scan ? ['ok', t('Quét gần nhất {0}', fmtTime(s.last_scan.finished_at || s.last_scan.started_at))]
      : ['', t('Chưa quét lần nào')];
  fill($('side-status'),
    h('div', { title: s.ai.note || '' }, h('span', { class: 'dot ' + ai[0] }), ai[1]),
    h('div', null, h('span', { class: 'dot ' + scan[0] }), scan[1]),
    s.sources_failing > 0 ? h('div', null, h('span', { class: 'dot bad' }), h('a', { href: '#/sources' }, t('{0} nguồn không quét được', s.sources_failing))) : null,
    s.muted ? h('div', null, h('span', { class: 'dot' }), t('Thông báo đang tạm im')) : null,
    h('div', { class: 'lang' }, t('Ngôn ngữ') + ': ', languageSelect()),
    h('div', null, t('Phiên bản {0}', s.version)));
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
    onclick: run(async () => { await api('POST', '/scan'); toast(t('Đã bắt đầu quét.')); await refreshStatus(); route(); }),
  }, busy ? t('Đang quét…') : t('Quét ngay'));
}

async function route() {
  const parts = location.hash.replace(/^#\/?/, '').split('/').filter(Boolean);
  const key = views[parts[0]] ? parts[0] : 'alerts';
  app.current = key;
  renderNav(key);
  $('title').textContent = t(views[key].title);
  document.title = 'Q3VigilAI — ' + t(views[key].title);
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
      toast(t('Quét xong: {0} tin mới, {1} cảnh báo.', data.items_new || 0, data.alerts_new || 0) + note, (data.sources_failed || 0) > 0);
    }
  });
  ['scan.started', 'scan.progress', 'scan.finished', 'alert.created', 'provider.changed'].forEach(on);
}

async function start() {
  try {
    await loadI18n();
    [app.status, app.meta] = await Promise.all([api('GET', '/status'), api('GET', '/meta')]);
  } catch (e) {
    // Without a session the language is unknown, so this one message is bilingual.
    fill(document.body, h('div', { class: 'empty' },
      h('b', null, t('Chưa mở được Q3VigilAI') + ' / Q3VigilAI cannot open'),
      e.status === 401
        ? t('Phiên làm việc đã hết. Hãy bấm vào biểu tượng Q3VigilAI ở khay hệ thống (góc phải thanh tác vụ) để mở lại.') +
          ' / Session expired. Click the Q3VigilAI tray icon (bottom right of the taskbar) to reopen.'
        : e.message));
    return;
  }
  renderStatus();
  window.addEventListener('hashchange', route);
  listen();
  await route();
}

start();
