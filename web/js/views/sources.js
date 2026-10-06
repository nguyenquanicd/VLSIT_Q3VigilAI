import { h, clear, fill, api, run, toast, fmtTime, labels, t, N } from '../lib.js';

export const title = N('Nguồn');

let ctxRef = null;

// minutes -> label
function every(minutes) {
  if (minutes % 1440 === 0) return t('{0} ngày', minutes / 1440);
  if (minutes % 60 === 0) return t('{0} giờ', minutes / 60);
  return t('{0} phút', minutes);
}
const intervalChoices = [30, 60, 120, 180, 360, 720, 1440];

function health(s) {
  if (!s.enabled) return h('span', { class: 'badge plain' }, t('Đang tắt'));
  if (s.fail_count >= 3) return h('span', { class: 'badge warning', title: s.last_error }, t('Lỗi {0} lần liên tiếp', s.fail_count));
  if (s.empty_count >= 3) return h('span', { class: 'badge warning', title: t('Trang có thể đã đổi cấu trúc') }, t('Nghi hỏng: không còn trả tin'));
  if (s.last_status === 'error') return h('span', { class: 'badge notice', title: s.last_error }, t('Lỗi lần gần nhất'));
  if (s.last_status === 'empty') return h('span', { class: 'badge notice' }, t('Không có tin'));
  if (!s.last_status) return h('span', { class: 'badge plain' }, t('Chưa quét'));
  return h('span', { class: 'badge ok' }, labels.sourceStatus[s.last_status] || s.last_status);
}

export async function render(ctx) {
  ctxRef = ctx;
  const { sources } = await api('GET', '/sources');
  const patch = (s, body) => run(async () => { await api('PATCH', '/sources/' + s.id, body); render(ctx); });
  fill(ctx.view,
    h('div', { class: 'banner' }, t('Q3VigilAI chỉ truy cập các tên miền trong danh sách này. Mạng xã hội, diễn đàn và blog bị chặn cứng và không thể thêm vào.')),
    h('table', null,
      h('thead', null, h('tr', null, [t('Nguồn'), t('Tầng'), t('Tần suất'), t('Lần quét gần nhất'), t('Tình trạng'), t('Bật'), ''].map((c) => h('th', null, c)))),
      h('tbody', null, sources.map((s) => h('tr', null,
        // The names of the built-in sources are Vietnamese; they have translations.
        h('td', null, h('div', null, t(s.name)), h('div', { class: 'hint' }, s.domain + (s.builtin ? '' : ' · ' + t('tự thêm')))),
        h('td', null, labels.tier[s.tier]),
        h('td', null, h('select', { onchange: (e) => patch(s, { interval_minutes: Number(e.target.value) })() },
          (intervalChoices.includes(s.interval_minutes) ? intervalChoices : [s.interval_minutes, ...intervalChoices])
            .map((m) => h('option', { value: m, selected: m === s.interval_minutes }, every(m))))),
        h('td', null, s.last_scan_at ? fmtTime(s.last_scan_at) : '—',
          s.last_error && s.last_status === 'error' ? h('div', { class: 'hint' }, s.last_error) : null),
        h('td', null, health(s)),
        h('td', null, h('input', { type: 'checkbox', checked: s.enabled, onchange: (e) => patch(s, { enabled: e.target.checked })() })),
        h('td', null, s.builtin ? null : h('button', { class: 'link danger', onclick: run(async () => {
          if (!confirm(t('Xóa nguồn "{0}"?', s.name))) return;
          await api('DELETE', '/sources/' + s.id);
          render(ctx);
        }) }, t('Xóa'))))))),
    addForm(ctx));
}

function addForm(ctx) {
  const url = h('input', { type: 'url', placeholder: t('https://… (địa chỉ RSS hoặc trang chuyên mục)'), class: 'grow' });
  const name = h('input', { placeholder: t('Tên nguồn') });
  const result = h('div');
  let probed = '';
  const addBtn = h('button', { class: 'primary', disabled: true, onclick: run(async () => {
    await api('POST', '/sources', { name: name.value, url: probed, tier: 3 });
    toast(t('Đã thêm nguồn.'));
    render(ctx);
  }) }, t('Thêm nguồn'));
  return h('div', { class: 'panel mt' },
    h('h2', null, t('Thêm nguồn báo chí')),
    h('p', { class: 'hint' }, t('Chỉ thêm cơ quan báo chí có giấy phép hoặc cổng thông tin của cơ quan nhà nước. Nguồn tự thêm luôn ở tầng "Báo chí", không bao giờ được coi là nguồn chính thức.')),
    h('div', { class: 'toolbar' }, url, h('button', { onclick: run(async () => {
      addBtn.disabled = true;
      fill(result, h('div', { class: 'hint' }, t('Đang kiểm tra…')));
      try {
        const r = await api('POST', '/sources/test', { url: url.value });
        probed = url.value;
        if (!name.value) name.value = r.domain;
        fill(result, h('div', { class: 'banner ok' }, t('Đọc được {0} tin ({1}) từ {2}. Ví dụ:', r.count, r.connector === 'rss' ? 'RSS' : t('trang danh mục'), r.domain)),
          h('ul', null, r.sample.map((title) => h('li', null, title))));
        addBtn.disabled = false;
      } catch (e) {
        fill(result, h('div', { class: 'banner bad' }, e.message));
      }
    }) }, t('Kiểm tra'))),
    result,
    h('div', { class: 'toolbar' }, name, addBtn));
}

export function onEvent(name) {
  if (name === 'scan.finished' && ctxRef) render(ctxRef);
}
