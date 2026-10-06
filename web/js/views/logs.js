import { h, clear, fill, api, fmtTime, labels, empty, t, N } from '../lib.js';

export const title = N('Nhật ký quét');

let ctxRef = null;

export async function render(ctx) {
  ctxRef = ctx;
  const { scans } = await api('GET', '/scans?limit=200');
  clear(ctx.view);
  if (scans.length === 0) {
    ctx.view.append(empty(t('Chưa có lượt quét nào'), t('Bấm "Quét ngay" hoặc chờ tới lịch.')));
    return;
  }
  ctx.view.append(h('table', null,
    h('thead', null, h('tr', null, [t('Bắt đầu'), t('Kiểu'), t('Thời lượng'), t('Nguồn tốt'), t('Nguồn lỗi'), t('Tin mới'), t('Cảnh báo'), t('Gọi AI'), t('Ghi chú')].map((c) => h('th', null, c)))),
    h('tbody', null, scans.map((s) => {
      const secs = s.finished_at ? Math.round((new Date(s.finished_at) - new Date(s.started_at)) / 1000) : null;
      return h('tr', null,
        h('td', null, fmtTime(s.started_at)),
        h('td', null, labels.trigger[s.trigger] || s.trigger),
        h('td', { class: 'num' }, secs == null ? t('đang chạy') : t('{0} giây', secs)),
        h('td', { class: 'num' }, s.sources_ok),
        h('td', { class: 'num' }, s.sources_failed ? h('span', { class: 'badge warning' }, s.sources_failed) : 0),
        h('td', { class: 'num' }, s.items_new), h('td', { class: 'num' }, s.alerts_new), h('td', { class: 'num' }, s.ai_calls),
        h('td', null, s.note || '', s.errors.map((e) => h('div', { class: 'hint' }, t(e.source) + ': ' + e.error))));
    }))));
}

export function onEvent(name) {
  if ((name === 'scan.finished' || name === 'scan.started') && ctxRef) render(ctxRef);
}
