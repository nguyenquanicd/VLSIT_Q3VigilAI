import { h, clear, fill, api, run, toast, splitList, ext, empty, t, N } from '../lib.js';

export const title = N('Chủ đề theo dõi');

const blank = () => ({ id: 0, name: '', keywords: [], exclude_keywords: [], fields: [], watched_docs: [], ai_context: '',
  source_ids: [], kinds: ['press', 'official', 'draft'], remind_days: 7, enabled: true });

export async function render(ctx) {
  const view = ctx.view;
  const { topics } = await api('GET', '/topics');
  const editId = ctx.params[0];
  if (editId) {
    const topic = editId === 'new' ? blank() : topics.find((x) => String(x.id) === editId);
    if (!topic) { location.hash = '#/topics'; return; }
    return form(ctx, topic);
  }
  fill(view, h('div', { class: 'toolbar' },
    h('span', { class: 'hint grow' }, t('Mỗi chủ đề là một bộ điều kiện. Tin nào khớp sẽ thành cảnh báo.')),
    h('a', { href: '#/topics/new' }, h('button', { class: 'primary' }, t('+ Thêm chủ đề')))));
  if (topics.length === 0) {
    view.append(empty(t('Chưa có chủ đề nào'), t('Ví dụ: chủ đề "Thuế" với lĩnh vực Thuế và từ khóa "hóa đơn điện tử"; hoặc theo dõi đích danh một văn bản như 13/2023/NĐ-CP.')));
    return;
  }
  const kindName = { press: t('Báo chí'), official: t('Văn bản'), draft: t('Dự thảo') };
  view.append(h('table', null,
    h('thead', null, h('tr', null, [t('Tên'), t('Điều kiện'), t('Loại tin'), t('Bật'), ''].map((c) => h('th', null, c)))),
    h('tbody', null, topics.map((tp) => h('tr', null,
      h('td', null, h('a', { href: '#/topics/' + tp.id }, tp.name)),
      h('td', null,
        tp.fields.length ? h('div', null, t('Lĩnh vực: {0}', tp.fields.map((f) => t(f)).join(', '))) : null,
        tp.keywords.length ? h('div', null, t('Từ khóa: {0}', tp.keywords.join(', '))) : null,
        tp.watched_docs.length ? h('div', null, t('Văn bản theo dõi: {0}', tp.watched_docs.join(', '))) : null,
        tp.exclude_keywords.length ? h('div', { class: 'hint' }, t('Loại trừ: {0}', tp.exclude_keywords.join(', '))) : null),
      h('td', null, tp.kinds.map((k) => kindName[k]).join(', ')),
      h('td', null, h('input', { type: 'checkbox', checked: tp.enabled, onchange: run(async (e) => {
        await api('PUT', '/topics/' + tp.id, { ...tp, enabled: e.target.checked });
        toast(e.target.checked ? t('Đã bật chủ đề.') : t('Đã tắt chủ đề.'));
      }) })),
      h('td', null, h('a', { href: '#/topics/' + tp.id }, t('Sửa'))))))));
}

function form(ctx, topic) {
  const meta = ctx.app.meta;
  // Keywords are matched against Vietnamese news, so their examples stay Vietnamese.
  const f = {
    name: h('input', { value: topic.name, placeholder: t('Ví dụ: Thuế và hóa đơn') }),
    keywords: h('textarea', { rows: 2, placeholder: 'hóa đơn điện tử, thuế giá trị gia tăng' }),
    exclude: h('textarea', { rows: 1, placeholder: 'bóng đá, giá vàng' }),
    watched: h('textarea', { rows: 1, placeholder: '13/2023/NĐ-CP, 59/2020/QH14' }),
    context: h('textarea', { rows: 2, placeholder: t('Ví dụ: công ty TNHH 20 nhân viên, ngành thiết kế vi mạch, có nhập khẩu thiết bị.') }),
    remind: h('input', { type: 'number', min: 0, max: 90, value: topic.remind_days }),
    enabled: h('input', { type: 'checkbox', checked: topic.enabled }),
  };
  f.keywords.value = topic.keywords.join(', ');
  f.exclude.value = topic.exclude_keywords.join(', ');
  f.watched.value = topic.watched_docs.join(', ');
  f.context.value = topic.ai_context;
  // The value stored in a topic is the Vietnamese name; only the label is translated.
  const fields = meta.fields.map((name) => {
    const box = h('input', { type: 'checkbox', checked: topic.fields.includes(name) });
    return { name, box, el: h('label', { class: 'inline', title: t('Gồm: {0}', meta.field_keywords[name].join(', ')) }, box, t(name)) };
  });
  const kinds = [['press', t('Tin báo chí')], ['official', t('Văn bản chính thức')], ['draft', t('Dự thảo, đề xuất')]].map(([key, label]) => {
    const box = h('input', { type: 'checkbox', checked: topic.kinds.includes(key) });
    return { key, box, el: h('label', { class: 'inline' }, box, label) };
  });
  const read = () => ({
    ...topic, name: f.name.value, keywords: splitList(f.keywords.value), exclude_keywords: splitList(f.exclude.value),
    watched_docs: splitList(f.watched.value), ai_context: f.context.value.trim(),
    fields: fields.filter((x) => x.box.checked).map((x) => x.name),
    kinds: kinds.filter((x) => x.box.checked).map((x) => x.key),
    remind_days: Number(f.remind.value) || 0, enabled: f.enabled.checked,
  });
  const preview = h('div');
  fill(ctx.view, h('div', { class: 'panel form' },
    h('h2', null, topic.id ? t('Sửa chủ đề') : t('Chủ đề mới')),
    h('label', null, t('Tên chủ đề'), f.name),
    h('label', null, t('Lĩnh vực (mỗi lĩnh vực là một bộ từ khóa dựng sẵn; rê chuột để xem)'), h('div', { class: 'checks' }, fields.map((x) => x.el))),
    h('label', null, t('Từ khóa (cách nhau bằng dấu phẩy)'), f.keywords,
      h('span', { class: 'hint' }, t('Gõ có dấu thì khớp đúng dấu ("thuế" không khớp "thuê"). Gõ không dấu thì khớp mọi dạng có dấu.'))),
    h('label', null, t('Từ khóa loại trừ'), f.exclude),
    h('label', null, t('Văn bản theo dõi đích danh (số hiệu)'), f.watched,
      h('span', { class: 'hint' }, t('Khi có văn bản mới sửa đổi, thay thế hoặc hướng dẫn các văn bản này, bạn nhận cảnh báo mức cao nhất.'))),
    h('label', null, t('Mô tả bối cảnh cho AI (không bắt buộc)'), f.context,
      h('span', { class: 'hint' }, t('Giúp AI đánh giá tin có liên quan tới bạn không. Nếu dùng AI qua mạng, nội dung này được gửi tới nhà cung cấp AI.'))),
    h('label', null, t('Loại tin'), h('div', { class: 'checks' }, kinds.map((x) => x.el))),
    h('div', { class: 'row' },
      h('label', null, t('Nhắc trước ngày có hiệu lực (ngày, 0 = không nhắc)'), f.remind),
      h('label', { class: 'inline' }, f.enabled, t('Bật chủ đề này'))),
    h('div', { class: 'toolbar' },
      h('button', { class: 'primary', onclick: run(async () => {
        const body = read();
        await api(topic.id ? 'PUT' : 'POST', topic.id ? '/topics/' + topic.id : '/topics', body);
        toast(t('Đã lưu chủ đề.'));
        location.hash = '#/topics';
      }) }, t('Lưu')),
      h('button', { onclick: run(async () => {
        const r = await api('POST', '/topics/preview', read());
        fill(preview, h('div', { class: 'banner ' + (r.matched ? 'ok' : '') },
          r.scanned === 0 ? t('Chưa có dữ liệu để thử. Hãy quét nguồn ít nhất một lần.')
            : t('Trong {0} tin đã thu thập 30 ngày qua, có {1} tin khớp chủ đề này.', r.scanned, r.matched)),
          r.samples.map((s) => h('div', { class: 'card' }, ext(s.url, s.title),
            h('div', { class: 'meta' }, h('span', null, t(s.source)), h('span', null, t('Khớp: {0}', s.keywords.join(', ')))))));
      }) }, t('Chạy thử')),
      h('a', { href: '#/topics' }, h('button', null, t('Hủy'))),
      topic.id ? h('button', { class: 'danger right', onclick: run(async () => {
        if (!confirm(t('Xóa chủ đề "{0}" cùng mọi cảnh báo của nó?', topic.name))) return;
        await api('DELETE', '/topics/' + topic.id);
        location.hash = '#/topics';
      }) }, t('Xóa chủ đề')) : null),
    preview));
  f.name.focus();
}
