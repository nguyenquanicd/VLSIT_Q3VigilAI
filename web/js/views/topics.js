import { h, clear, fill, api, run, toast, splitList, ext, empty } from '../lib.js';

export const title = 'Chủ đề theo dõi';

const blank = () => ({ id: 0, name: '', keywords: [], exclude_keywords: [], fields: [], watched_docs: [], ai_context: '',
  source_ids: [], kinds: ['press', 'official', 'draft'], remind_days: 7, enabled: true });

export async function render(ctx) {
  const view = ctx.view;
  const { topics } = await api('GET', '/topics');
  const editId = ctx.params[0];
  if (editId) {
    const t = editId === 'new' ? blank() : topics.find((x) => String(x.id) === editId);
    if (!t) { location.hash = '#/topics'; return; }
    return form(ctx, t);
  }
  fill(view, h('div', { class: 'toolbar' },
    h('span', { class: 'hint grow' }, 'Mỗi chủ đề là một bộ điều kiện. Tin nào khớp sẽ thành cảnh báo.'),
    h('a', { href: '#/topics/new' }, h('button', { class: 'primary' }, '+ Thêm chủ đề'))));
  if (topics.length === 0) {
    view.append(empty('Chưa có chủ đề nào', 'Ví dụ: chủ đề "Thuế" với lĩnh vực Thuế và từ khóa "hóa đơn điện tử"; hoặc theo dõi đích danh một văn bản như 13/2023/NĐ-CP.'));
    return;
  }
  view.append(h('table', null,
    h('thead', null, h('tr', null, ['Tên', 'Điều kiện', 'Loại tin', 'Bật', ''].map((c) => h('th', null, c)))),
    h('tbody', null, topics.map((t) => h('tr', null,
      h('td', null, h('a', { href: '#/topics/' + t.id }, t.name)),
      h('td', null,
        t.fields.length ? h('div', null, 'Lĩnh vực: ' + t.fields.join(', ')) : null,
        t.keywords.length ? h('div', null, 'Từ khóa: ' + t.keywords.join(', ')) : null,
        t.watched_docs.length ? h('div', null, 'Văn bản theo dõi: ' + t.watched_docs.join(', ')) : null,
        t.exclude_keywords.length ? h('div', { class: 'hint' }, 'Loại trừ: ' + t.exclude_keywords.join(', ')) : null),
      h('td', null, t.kinds.map((k) => ({ press: 'Báo chí', official: 'Văn bản', draft: 'Dự thảo' }[k])).join(', ')),
      h('td', null, h('input', { type: 'checkbox', checked: t.enabled, onchange: run(async (e) => {
        await api('PUT', '/topics/' + t.id, { ...t, enabled: e.target.checked });
        toast(e.target.checked ? 'Đã bật chủ đề.' : 'Đã tắt chủ đề.');
      }) })),
      h('td', null, h('a', { href: '#/topics/' + t.id }, 'Sửa')))))));
}

function form(ctx, t) {
  const meta = ctx.app.meta;
  const f = {
    name: h('input', { value: t.name, placeholder: 'Ví dụ: Thuế và hóa đơn' }),
    keywords: h('textarea', { rows: 2, placeholder: 'hóa đơn điện tử, thuế giá trị gia tăng' }),
    exclude: h('textarea', { rows: 1, placeholder: 'bóng đá, giá vàng' }),
    watched: h('textarea', { rows: 1, placeholder: '13/2023/NĐ-CP, 59/2020/QH14' }),
    context: h('textarea', { rows: 2, placeholder: 'Ví dụ: công ty TNHH 20 nhân viên, ngành thiết kế vi mạch, có nhập khẩu thiết bị.' }),
    remind: h('input', { type: 'number', min: 0, max: 90, value: t.remind_days }),
    enabled: h('input', { type: 'checkbox', checked: t.enabled }),
  };
  f.keywords.value = t.keywords.join(', ');
  f.exclude.value = t.exclude_keywords.join(', ');
  f.watched.value = t.watched_docs.join(', ');
  f.context.value = t.ai_context;
  const fields = meta.fields.map((name) => {
    const box = h('input', { type: 'checkbox', checked: t.fields.includes(name) });
    return { name, box, el: h('label', { class: 'inline', title: 'Gồm: ' + meta.field_keywords[name].join(', ') }, box, name) };
  });
  const kinds = [['press', 'Tin báo chí'], ['official', 'Văn bản chính thức'], ['draft', 'Dự thảo, đề xuất']].map(([key, label]) => {
    const box = h('input', { type: 'checkbox', checked: t.kinds.includes(key) });
    return { key, box, el: h('label', { class: 'inline' }, box, label) };
  });
  const read = () => ({
    ...t, name: f.name.value, keywords: splitList(f.keywords.value), exclude_keywords: splitList(f.exclude.value),
    watched_docs: splitList(f.watched.value), ai_context: f.context.value.trim(),
    fields: fields.filter((x) => x.box.checked).map((x) => x.name),
    kinds: kinds.filter((x) => x.box.checked).map((x) => x.key),
    remind_days: Number(f.remind.value) || 0, enabled: f.enabled.checked,
  });
  const preview = h('div');
  fill(ctx.view, h('div', { class: 'panel form' },
    h('h2', null, t.id ? 'Sửa chủ đề' : 'Chủ đề mới'),
    h('label', null, 'Tên chủ đề', f.name),
    h('label', null, 'Lĩnh vực (mỗi lĩnh vực là một bộ từ khóa dựng sẵn; rê chuột để xem)', h('div', { class: 'checks' }, fields.map((x) => x.el))),
    h('label', null, 'Từ khóa (cách nhau bằng dấu phẩy)', f.keywords,
      h('span', { class: 'hint' }, 'Gõ có dấu thì khớp đúng dấu ("thuế" không khớp "thuê"). Gõ không dấu thì khớp mọi dạng có dấu.')),
    h('label', null, 'Từ khóa loại trừ', f.exclude),
    h('label', null, 'Văn bản theo dõi đích danh (số hiệu)', f.watched,
      h('span', { class: 'hint' }, 'Khi có văn bản mới sửa đổi, thay thế hoặc hướng dẫn các văn bản này, bạn nhận cảnh báo mức cao nhất.')),
    h('label', null, 'Mô tả bối cảnh cho AI (không bắt buộc)', f.context,
      h('span', { class: 'hint' }, 'Giúp AI đánh giá tin có liên quan tới bạn không. Nếu dùng AI qua mạng, nội dung này được gửi tới nhà cung cấp AI.')),
    h('label', null, 'Loại tin', h('div', { class: 'checks' }, kinds.map((x) => x.el))),
    h('div', { class: 'row' },
      h('label', null, 'Nhắc trước ngày có hiệu lực (ngày, 0 = không nhắc)', f.remind),
      h('label', { class: 'inline' }, f.enabled, 'Bật chủ đề này')),
    h('div', { class: 'toolbar' },
      h('button', { class: 'primary', onclick: run(async () => {
        const body = read();
        await api(t.id ? 'PUT' : 'POST', t.id ? '/topics/' + t.id : '/topics', body);
        toast('Đã lưu chủ đề.');
        location.hash = '#/topics';
      }) }, 'Lưu'),
      h('button', { onclick: run(async () => {
        const r = await api('POST', '/topics/preview', read());
        fill(preview, h('div', { class: 'banner ' + (r.matched ? 'ok' : '') },
          r.scanned === 0 ? 'Chưa có dữ liệu để thử. Hãy quét nguồn ít nhất một lần.'
            : `Trong ${r.scanned} tin đã thu thập 30 ngày qua, có ${r.matched} tin khớp chủ đề này.`),
          r.samples.map((s) => h('div', { class: 'card' }, ext(s.url, s.title),
            h('div', { class: 'meta' }, h('span', null, s.source), h('span', null, 'Khớp: ' + s.keywords.join(', '))))));
      }) }, 'Chạy thử'),
      h('a', { href: '#/topics' }, h('button', null, 'Hủy')),
      t.id ? h('button', { class: 'danger right', onclick: run(async () => {
        if (!confirm(`Xóa chủ đề "${t.name}" cùng mọi cảnh báo của nó?`)) return;
        await api('DELETE', '/topics/' + t.id);
        location.hash = '#/topics';
      }) }, 'Xóa chủ đề') : null),
    preview));
  f.name.focus();
}
