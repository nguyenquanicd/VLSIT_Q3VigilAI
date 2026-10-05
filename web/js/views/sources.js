import { h, clear, fill, api, run, toast, fmtTime, labels } from '../lib.js';

export const title = 'Nguồn';

let ctxRef = null;

const intervals = [[30, '30 phút'], [60, '1 giờ'], [120, '2 giờ'], [180, '3 giờ'], [360, '6 giờ'], [720, '12 giờ'], [1440, '1 ngày']];

function health(s) {
  if (!s.enabled) return h('span', { class: 'badge plain' }, 'Đang tắt');
  if (s.fail_count >= 3) return h('span', { class: 'badge warning', title: s.last_error }, `Lỗi ${s.fail_count} lần liên tiếp`);
  if (s.empty_count >= 3) return h('span', { class: 'badge warning', title: 'Trang có thể đã đổi cấu trúc' }, 'Nghi hỏng: không còn trả tin');
  if (s.last_status === 'error') return h('span', { class: 'badge notice', title: s.last_error }, 'Lỗi lần gần nhất');
  if (s.last_status === 'empty') return h('span', { class: 'badge notice' }, 'Không có tin');
  if (!s.last_status) return h('span', { class: 'badge plain' }, 'Chưa quét');
  return h('span', { class: 'badge ok' }, labels.sourceStatus[s.last_status] || s.last_status);
}

export async function render(ctx) {
  ctxRef = ctx;
  const { sources } = await api('GET', '/sources');
  const patch = (s, body) => run(async () => { await api('PATCH', '/sources/' + s.id, body); render(ctx); });
  fill(ctx.view, 
    h('div', { class: 'banner' }, 'Q3VNLaw chỉ truy cập các tên miền trong danh sách này. Mạng xã hội, diễn đàn và blog bị chặn cứng và không thể thêm vào.'),
    h('table', null,
      h('thead', null, h('tr', null, ['Nguồn', 'Tầng', 'Tần suất', 'Lần quét gần nhất', 'Tình trạng', 'Bật', ''].map((c) => h('th', null, c)))),
      h('tbody', null, sources.map((s) => h('tr', null,
        h('td', null, h('div', null, s.name), h('div', { class: 'hint' }, s.domain + (s.builtin ? '' : ' · tự thêm'))),
        h('td', null, labels.tier[s.tier]),
        h('td', null, h('select', { onchange: (e) => patch(s, { interval_minutes: Number(e.target.value) })() },
          (intervals.some(([m]) => m === s.interval_minutes) ? intervals : [[s.interval_minutes, s.interval_minutes + ' phút'], ...intervals])
            .map(([m, l]) => h('option', { value: m, selected: m === s.interval_minutes }, l)))),
        h('td', null, s.last_scan_at ? fmtTime(s.last_scan_at) : '—',
          s.last_error && s.last_status === 'error' ? h('div', { class: 'hint' }, s.last_error) : null),
        h('td', null, health(s)),
        h('td', null, h('input', { type: 'checkbox', checked: s.enabled, onchange: (e) => patch(s, { enabled: e.target.checked })() })),
        h('td', null, s.builtin ? null : h('button', { class: 'link danger', onclick: run(async () => {
          if (!confirm(`Xóa nguồn "${s.name}"?`)) return;
          await api('DELETE', '/sources/' + s.id);
          render(ctx);
        }) }, 'Xóa')))))),
    addForm(ctx));
}

function addForm(ctx) {
  const url = h('input', { type: 'url', placeholder: 'https://… (địa chỉ RSS hoặc trang chuyên mục)', class: 'grow' });
  const name = h('input', { placeholder: 'Tên nguồn' });
  const result = h('div');
  let probed = '';
  const addBtn = h('button', { class: 'primary', disabled: true, onclick: run(async () => {
    await api('POST', '/sources', { name: name.value, url: probed, tier: 3 });
    toast('Đã thêm nguồn.');
    render(ctx);
  }) }, 'Thêm nguồn');
  return h('div', { class: 'panel mt' },
    h('h2', null, 'Thêm nguồn báo chí'),
    h('p', { class: 'hint' }, 'Chỉ thêm cơ quan báo chí có giấy phép hoặc cổng thông tin của cơ quan nhà nước. Nguồn tự thêm luôn ở tầng "Báo chí", không bao giờ được coi là nguồn chính thức.'),
    h('div', { class: 'toolbar' }, url, h('button', { onclick: run(async () => {
      addBtn.disabled = true;
      fill(result, h('div', { class: 'hint' }, 'Đang kiểm tra…'));
      try {
        const r = await api('POST', '/sources/test', { url: url.value });
        probed = url.value;
        if (!name.value) name.value = r.domain;
        fill(result, h('div', { class: 'banner ok' }, `Đọc được ${r.count} tin (${r.connector === 'rss' ? 'RSS' : 'trang danh mục'}) từ ${r.domain}. Ví dụ:`),
          h('ul', null, r.sample.map((t) => h('li', null, t))));
        addBtn.disabled = false;
      } catch (e) {
        fill(result, h('div', { class: 'banner bad' }, e.message));
      }
    }) }, 'Kiểm tra')),
    result,
    h('div', { class: 'toolbar' }, name, addBtn));
}

export function onEvent(name) {
  if (name === 'scan.finished' && ctxRef) render(ctxRef);
}
