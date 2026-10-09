import { h, clear, fill, api, run, toast, fmtDate, fmtTime, labels, ext, empty, t, N } from '../lib.js';

export const title = N('Văn bản');

let query = '';

function validity(d) {
  const label = labels.validity[d.validity] || d.validity;
  const cls = d.validity === 'in_force' ? 'ok' : d.validity === 'superseded' ? 'warning' : d.validity === 'unknown' ? 'plain' : 'notice';
  // The portal publishes dates, not status, so anything else is an inference.
  const inferred = !(d.validity_basis === 'official' || d.validity === 'unknown');
  return h('span', { class: 'badge ' + cls, title: inferred ? t('Suy ra từ ngày hiệu lực và các văn bản sửa đổi tìm được; không phải dữ liệu chính thức. Đối chiếu tại vbpl.vn trước khi trích dẫn.') : '' },
    label + (inferred ? ' ' + t('(suy luận)') : ''));
}

export async function render(ctx) {
  if (ctx.params[0]) return detail(ctx, ctx.params[0]);
  const list = h('div');
  const number = h('input', { placeholder: t('Số hiệu, ví dụ 13/2023/NĐ-CP') });
  const fetchBtn = h('button', { class: 'primary' }, t('Tải từ Cổng Chính phủ'));
  fetchBtn.addEventListener('click', run(async () => {
    fetchBtn.disabled = true;
    fetchBtn.textContent = t('Đang tra và tải…');
    try {
      let d = await api('POST', '/documents/fetch', { number: number.value });
      if (d.confirmation_required) {
        const estimate = d.estimate;
        const knownMB = (estimate.total_bytes / 1_000_000).toFixed(1);
        const message = estimate.unknown_files
          ? t('Không xác định được dung lượng của {0} tệp đính kèm (đã biết khoảng {1} MB). Có thể vượt 50 MB; bạn có muốn tiếp tục tải?', estimate.unknown_files, knownMB)
          : t('Tổng dung lượng tệp đính kèm khoảng {0} MB, vượt quá 50 MB. Bạn có muốn tiếp tục tải?', knownMB);
        if (!window.confirm(message)) return;
        d = await api('POST', '/documents/fetch', { number: number.value, confirm_large: true });
      }
      toast(t('Đã tải {0}', d.doc_number));
      location.hash = '#/docs/' + d.id;
    } finally {
      fetchBtn.disabled = false;
      fetchBtn.textContent = t('Tải từ Cổng Chính phủ');
    }
  }));
  let timer;
  fill(ctx.view,
    h('div', { class: 'toolbar' },
      h('input', { type: 'search', class: 'grow', placeholder: t('Tìm trong thư viện theo tên hoặc số hiệu…'), value: query,
        oninput: (e) => { query = e.target.value; clearTimeout(timer); timer = setTimeout(() => load(list), 300); } }),
      number, fetchBtn),
    list);
  await load(list);
}

async function load(list) {
  const data = await api('GET', '/documents?limit=100&q=' + encodeURIComponent(query));
  clear(list);
  if (data.documents.length === 0) {
    list.append(query ? empty(t('Không tìm thấy trong thư viện'), t('Nếu biết số hiệu, nhập vào ô bên phải để tải từ Cổng Chính phủ.'))
      : empty(t('Thư viện chưa có văn bản nào'), t('Văn bản chính thức được lưu ở đây khi một cảnh báo được xác nhận, hoặc khi bạn tải theo số hiệu.')));
    return;
  }
  list.append(h('table', null,
    h('thead', null, h('tr', null, [t('Số hiệu'), t('Tên'), t('Ban hành'), t('Hiệu lực từ'), t('Tình trạng'), t('Tệp')].map((c) => h('th', null, c)))),
    h('tbody', null, data.documents.map((d) => h('tr', { class: 'click', onclick: () => { location.hash = '#/docs/' + d.id; } },
      h('td', null, d.doc_number), h('td', null, d.title),
      h('td', null, fmtDate(d.issued_at)), h('td', null, fmtDate(d.effective_at) || '—'),
      h('td', null, validity(d)),
      h('td', null, d.files.length ? d.files.length + ' ' + (d.has_text ? t('(có chữ)') : t('(bản scan)')) : '—'))))));
}

async function detail(ctx, id) {
  const { document: d, alerts } = await api('GET', '/documents/' + id);
  const row = (label, value) => value ? h('tr', null, h('th', null, label), h('td', null, value)) : null;
  fill(ctx.view,
    h('div', { class: 'toolbar' }, h('a', { href: '#/docs' }, t('← Thư viện')), h('span', { class: 'grow' }),
      h('button', { onclick: run(async () => {
        const s = await api('POST', '/chat/sessions', { title: d.doc_number, scope: { kind: 'document', id: d.id } });
        location.hash = '#/chat/' + s.id;
      }) }, t('Hỏi AI về văn bản này')),
      d.files.length ? h('button', { onclick: run(() => api('POST', `/documents/${d.id}/open-folder`)) }, t('Mở thư mục chứa tệp')) : null),
    h('div', { class: 'panel' },
      h('h2', null, d.doc_number),
      h('p', null, d.title),
      h('table', null, h('tbody', null,
        row(t('Loại văn bản'), d.doc_type), row(t('Cơ quan ban hành'), d.issuer), row(t('Người ký'), d.signer),
        row(t('Ngày ban hành'), fmtDate(d.issued_at)), row(t('Ngày có hiệu lực'), fmtDate(d.effective_at) || t('Cổng không nêu')),
        row(t('Tình trạng'), validity(d)),
        row(t('Nguồn'), d.source_url ? ext(d.source_url, t('Cổng Thông tin điện tử Chính phủ') + ' ↗') : null),
        row(t('Tải về lúc'), fmtTime(d.fetched_at))))),
    h('div', { class: 'panel' },
      h('h2', null, t('Tệp gốc')),
      d.files.length === 0 ? h('p', { class: 'hint' }, t('Chưa có tệp đã tải. Cổng có thể không có tệp đính kèm, hoặc lượt tải nền đã bỏ qua vì cần xác nhận dung lượng. Nhập số hiệu trong thư viện để tải chủ động.'))
        : [h('p', { class: 'hint' }, t('Đường dẫn dưới đây tương đối với thư mục dữ liệu.')),
          d.files.map((f, i) => h('div', null, ext(`/api/documents/${d.id}/file/${i}`, f.replaceAll('/', '\\'))))],
      d.files.length && !d.has_text ? h('p', { class: 'hint mt' },
        t('Tệp là bản scan, không có lớp chữ nên chưa tìm kiếm hay hỏi đáp được theo nội dung. Có thể chuyển sang chữ bằng công cụ OCR (skill han-scan-to-word).')) : null),
    h('div', { class: 'panel' },
      h('h2', null, t('Quan hệ với văn bản khác')),
      (d.relations || []).length === 0 ? h('p', { class: 'hint' }, t('Chưa ghi nhận quan hệ nào trong thư viện.'))
        : d.relations.map((r) => h('div', null,
          r.direction === 'in' ? [h('a', { href: '#/docs/' + r.doc_id }, r.doc_number), ' ' + t('{0} văn bản này', labels.relation[r.relation])]
            : [t('Văn bản này {0}', labels.relation[r.relation]) + ' ', h('a', { href: '#/docs/' + r.doc_id }, r.doc_number)],
          h('span', { class: 'hint' }, ` · ${fmtDate(r.issued_at)} · ${r.basis === 'stated' ? t('theo trích yếu') : t('suy luận')}`))),
      h('p', { class: 'hint mt' }, t('Quan hệ được suy ra từ câu chữ của trích yếu và chỉ gồm các văn bản đã có trong thư viện, nên có thể chưa đầy đủ.'))),
    alerts.length ? h('div', { class: 'panel' }, h('h2', null, t('Cảnh báo liên quan')),
      alerts.map((a) => h('div', null, h('a', { href: '#/alerts/' + a.id }, a.title), h('span', { class: 'hint' }, ' · ' + a.topic_name)))) : null);
}
