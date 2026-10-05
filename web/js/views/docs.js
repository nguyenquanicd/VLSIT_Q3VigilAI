import { h, clear, fill, api, run, toast, fmtDate, fmtTime, labels, ext, empty } from '../lib.js';

export const title = 'Văn bản';

let query = '';

function validity(d) {
  const label = labels.validity[d.validity] || d.validity;
  const cls = d.validity === 'in_force' ? 'ok' : d.validity === 'superseded' ? 'warning' : d.validity === 'unknown' ? 'plain' : 'notice';
  // The portal publishes dates, not status, so anything else is an inference.
  const note = d.validity_basis === 'official' || d.validity === 'unknown' ? '' : ' (suy luận)';
  return h('span', { class: 'badge ' + cls, title: note ? 'Suy ra từ ngày hiệu lực và các văn bản sửa đổi tìm được; không phải dữ liệu chính thức. Đối chiếu tại vbpl.vn trước khi trích dẫn.' : '' }, label + note);
}

export async function render(ctx) {
  if (ctx.params[0]) return detail(ctx, ctx.params[0]);
  const list = h('div');
  const number = h('input', { placeholder: 'Số hiệu, ví dụ 13/2023/NĐ-CP' });
  const fetchBtn = h('button', { class: 'primary' }, 'Tải từ Cổng Chính phủ');
  fetchBtn.addEventListener('click', run(async () => {
    fetchBtn.disabled = true;
    fetchBtn.textContent = 'Đang tra và tải…';
    try {
      const d = await api('POST', '/documents/fetch', { number: number.value });
      toast('Đã tải ' + d.doc_number);
      location.hash = '#/docs/' + d.id;
    } finally {
      fetchBtn.disabled = false;
      fetchBtn.textContent = 'Tải từ Cổng Chính phủ';
    }
  }));
  let timer;
  fill(ctx.view, 
    h('div', { class: 'toolbar' },
      h('input', { type: 'search', class: 'grow', placeholder: 'Tìm trong thư viện theo tên hoặc số hiệu…', value: query,
        oninput: (e) => { query = e.target.value; clearTimeout(timer); timer = setTimeout(() => load(list), 300); } }),
      number, fetchBtn),
    list);
  await load(list);
}

async function load(list) {
  const data = await api('GET', '/documents?limit=100&q=' + encodeURIComponent(query));
  clear(list);
  if (data.documents.length === 0) {
    list.append(query ? empty('Không tìm thấy trong thư viện', 'Nếu biết số hiệu, nhập vào ô bên phải để tải từ Cổng Chính phủ.')
      : empty('Thư viện chưa có văn bản nào', 'Văn bản chính thức được lưu ở đây khi một cảnh báo được xác nhận, hoặc khi bạn tải theo số hiệu.'));
    return;
  }
  list.append(h('table', null,
    h('thead', null, h('tr', null, ['Số hiệu', 'Tên', 'Ban hành', 'Hiệu lực từ', 'Tình trạng', 'Tệp'].map((c) => h('th', null, c)))),
    h('tbody', null, data.documents.map((d) => h('tr', { class: 'click', onclick: () => { location.hash = '#/docs/' + d.id; } },
      h('td', null, d.doc_number), h('td', null, d.title),
      h('td', null, fmtDate(d.issued_at)), h('td', null, fmtDate(d.effective_at) || '—'),
      h('td', null, validity(d)),
      h('td', null, d.files.length ? d.files.length + (d.has_text ? ' (có chữ)' : ' (bản scan)') : '—'))))));
}

async function detail(ctx, id) {
  const { document: d, alerts } = await api('GET', '/documents/' + id);
  const row = (label, value) => value ? h('tr', null, h('th', null, label), h('td', null, value)) : null;
  fill(ctx.view, 
    h('div', { class: 'toolbar' }, h('a', { href: '#/docs' }, '← Thư viện'), h('span', { class: 'grow' }),
      h('button', { onclick: run(async () => {
        const s = await api('POST', '/chat/sessions', { title: d.doc_number, scope: { kind: 'document', id: d.id } });
        location.hash = '#/chat/' + s.id;
      }) }, 'Hỏi AI về văn bản này'),
      d.files.length ? h('button', { onclick: run(() => api('POST', `/documents/${d.id}/open-folder`)) }, 'Mở thư mục chứa tệp') : null),
    h('div', { class: 'panel' },
      h('h2', null, d.doc_number),
      h('p', null, d.title),
      h('table', null, h('tbody', null,
        row('Loại văn bản', d.doc_type), row('Cơ quan ban hành', d.issuer), row('Người ký', d.signer),
        row('Ngày ban hành', fmtDate(d.issued_at)), row('Ngày có hiệu lực', fmtDate(d.effective_at) || 'Cổng không nêu'),
        row('Tình trạng', validity(d)),
        row('Nguồn', d.source_url ? ext(d.source_url, 'Cổng Thông tin điện tử Chính phủ ↗') : null),
        row('Tải về lúc', fmtTime(d.fetched_at))))),
    h('div', { class: 'panel' },
      h('h2', null, 'Tệp gốc'),
      d.files.length === 0 ? h('p', { class: 'hint' }, 'Cổng không có tệp đính kèm, hoặc tệp chưa tải được.')
        : d.files.map((f, i) => h('div', null, ext(`/api/documents/${d.id}/file/${i}`, f.split('/').pop()))),
      d.files.length && !d.has_text ? h('p', { class: 'hint mt' },
        'Tệp là bản scan, không có lớp chữ nên chưa tìm kiếm hay hỏi đáp được theo nội dung. Có thể chuyển sang chữ bằng công cụ OCR (skill han-scan-to-word).') : null),
    h('div', { class: 'panel' },
      h('h2', null, 'Quan hệ với văn bản khác'),
      (d.relations || []).length === 0 ? h('p', { class: 'hint' }, 'Chưa ghi nhận quan hệ nào trong thư viện.')
        : d.relations.map((r) => h('div', null,
          r.direction === 'in' ? [h('a', { href: '#/docs/' + r.doc_id }, r.doc_number), ` ${labels.relation[r.relation]} văn bản này`]
            : ['Văn bản này ' + labels.relation[r.relation] + ' ', h('a', { href: '#/docs/' + r.doc_id }, r.doc_number)],
          h('span', { class: 'hint' }, ` · ${fmtDate(r.issued_at)} · ${r.basis === 'stated' ? 'theo trích yếu' : 'suy luận'}`))),
      h('p', { class: 'hint mt' }, 'Quan hệ được suy ra từ câu chữ của trích yếu và chỉ gồm các văn bản đã có trong thư viện, nên có thể chưa đầy đủ.')),
    alerts.length ? h('div', { class: 'panel' }, h('h2', null, 'Cảnh báo liên quan'),
      alerts.map((a) => h('div', null, h('a', { href: '#/alerts/' + a.id }, a.title), h('span', { class: 'hint' }, ' · ' + a.topic_name)))) : null);
}
