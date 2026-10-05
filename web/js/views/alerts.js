import { h, clear, fill, api, run, toast, ago, fmtDate, labels, ext, empty } from '../lib.js';
import { refreshStatus } from '../app.js';

export const title = 'Cảnh báo';

const filter = { state: '', severity: '', kind: '', topic: 0, q: '' };
let host = null;
let openId = 0;

export async function render(ctx) {
  host = ctx.view;
  if (ctx.params[0]) openId = Number(ctx.params[0]) || 0;
  const { topics } = await api('GET', '/topics');

  const tab = (state, label) => h('button', { class: filter.state === state ? 'on' : '', onclick: () => { filter.state = state; render(ctx); } }, label);
  const select = (key, options) => h('select', { onchange: (e) => { filter[key] = e.target.value; load(); } },
    options.map(([v, l]) => h('option', { value: v, selected: String(filter[key]) === String(v) }, l)));
  let timer;
  const list = h('div', { id: 'alert-list' });
  fill(host, 
    h('div', { class: 'toolbar' },
      h('div', { class: 'tabs' }, tab('', 'Hộp thư'), tab('filtered', 'Đã lọc bỏ'), tab('dismissed', 'Đã bỏ qua')),
      select('severity', [['', 'Mọi mức'], ['warning', 'Cảnh báo'], ['notice', 'Cần chú ý'], ['info', 'Thông tin']]),
      select('kind', [['', 'Mọi loại'], ['press', 'Báo chí'], ['official', 'Văn bản chính thức'], ['doc_changed', 'Văn bản theo dõi'], ['effective_soon', 'Sắp có hiệu lực']]),
      select('topic', [[0, 'Mọi chủ đề'], ...topics.map((t) => [t.id, t.name])]),
      h('input', { type: 'search', placeholder: 'Tìm tiêu đề, số hiệu…', value: filter.q, class: 'grow',
        oninput: (e) => { filter.q = e.target.value; clearTimeout(timer); timer = setTimeout(load, 300); } }),
      h('button', { onclick: run(async () => { await api('POST', '/alerts/mark-read'); await refreshStatus(); load(); }) }, 'Đánh dấu đã đọc tất cả')),
    filter.state === 'filtered' ? h('div', { class: 'banner' },
      'Các tin dưới đây khớp từ khóa nhưng đã bị loại (AI đánh giá không liên quan, hoặc là dự thảo trong khi chủ đề không theo dõi dự thảo). Xem qua để chắc rằng không lọc sót.') : null,
    list);
  await load(topics.length);
}

async function load(topicCount) {
  const list = document.getElementById('alert-list');
  if (!list) return;
  const p = new URLSearchParams({ limit: '100' });
  for (const [k, v] of Object.entries(filter)) if (v && v !== '0') p.set(k, v);
  const data = await api('GET', '/alerts?' + p);
  clear(list);
  if (data.alerts.length === 0) {
    const filtering = filter.severity || filter.kind || filter.topic || filter.q;
    list.append(topicCount === 0
      ? empty('Chưa có chủ đề theo dõi', h('span', null, 'Hãy ', h('a', { href: '#/topics' }, 'tạo chủ đề đầu tiên'), ' để Q3VNLaw biết cần báo cho bạn điều gì.'))
      : filtering ? empty('Không có cảnh báo nào khớp bộ lọc', 'Thử bỏ bớt điều kiện lọc.')
        : filter.state ? empty('Danh sách trống', '')
          : empty('Chưa có cảnh báo', 'Khi có tin pháp luật khớp chủ đề của bạn, nó sẽ hiện ở đây. Bấm "Quét ngay" để kiểm tra các nguồn.'));
    return;
  }
  list.append(...data.alerts.map(card));
  if (data.total > data.alerts.length) list.append(h('div', { class: 'hint' }, `Đang hiện ${data.alerts.length} trên ${data.total} cảnh báo. Dùng bộ lọc để thu hẹp.`));
  if (openId) {
    const el = document.getElementById('alert-' + openId);
    if (el) { el.scrollIntoView({ block: 'center' }); el.querySelector('h3').click(); }
    openId = 0;
  }
}

function badges(a) {
  const out = [h('span', { class: 'badge ' + a.severity }, labels.severity[a.severity] || a.severity)];
  if (a.kind !== 'press') out.push(h('span', { class: 'badge plain' }, labels.kind[a.kind] || a.kind));
  if (a.legal_status) out.push(h('span', { class: 'badge plain' }, labels.status[a.legal_status] || a.legal_status));
  if (a.verified === 'confirmed') out.push(h('span', { class: 'badge ok' }, 'Đã xác nhận'));
  if (a.verified === 'unconfirmed') out.push(h('span', { class: 'badge notice' }, 'Chưa xác nhận'));
  if (a.state === 'pinned') out.push(h('span', { class: 'badge' }, 'Đã ghim'));
  return out;
}

function card(a) {
  const detail = h('div', { class: 'detail hidden' });
  const el = h('article', { id: 'alert-' + a.id, class: `card sev-${a.severity} ${a.state === 'unread' ? 'unread' : ''}` });
  const setState = (state) => run(async () => {
    await api('PATCH', '/alerts/' + a.id, { state });
    await refreshStatus();
    load();
  });
  const toggle = run(async () => {
    if (!detail.classList.toggle('hidden')) {
      fillDetail(detail, await api('GET', '/alerts/' + a.id));
      if (a.state === 'unread') {
        await api('PATCH', '/alerts/' + a.id, { state: 'read' });
        a.state = 'read';
        el.classList.remove('unread');
        refreshStatus();
      }
    }
  });
  el.append(
    h('div', { class: 'badges' }, badges(a)),
    h('h3', { onclick: toggle, title: 'Bấm để xem chi tiết' }, a.title),
    a.summary ? h('div', { class: 'summary' }, a.summary) : null,
    h('div', { class: 'meta' },
      h('span', null, (a.source_name || 'Không rõ nguồn') + (a.source_count > 1 ? ` và ${a.source_count - 1} nguồn khác` : '')),
      a.source_tier ? h('span', { class: 'chip' }, labels.tier[a.source_tier]) : null,
      h('span', null, ago(a.created_at)),
      h('span', null, 'Chủ đề: ' + a.topic_name),
      a.effective_at ? h('span', null, 'Hiệu lực từ ' + fmtDate(a.effective_at)) : null,
      (a.doc_numbers || []).map((n) => h('span', { class: 'chip' }, n))),
    h('div', { class: 'actions' },
      a.url ? ext(a.url, a.kind === 'press' ? 'Mở bài gốc ↗' : 'Mở trên cổng chính thức ↗') : null,
      a.state === 'unread' ? h('button', { class: 'link', onclick: setState('read') }, 'Đã đọc') : null,
      a.state === 'read' ? h('button', { class: 'link', onclick: setState('unread') }, 'Chưa đọc') : null,
      a.state !== 'pinned' ? h('button', { class: 'link', onclick: setState('pinned') }, 'Ghim') : h('button', { class: 'link', onclick: setState('read') }, 'Bỏ ghim'),
      a.state === 'filtered' || a.state === 'dismissed'
        ? h('button', { class: 'link', onclick: setState('unread') }, 'Đưa lại vào hộp thư')
        : h('button', { class: 'link', onclick: setState('dismissed') }, 'Bỏ qua'),
      a.feedback !== 'irrelevant' && a.state !== 'filtered' ? h('button', {
        class: 'link', onclick: run(async () => {
          await api('PATCH', '/alerts/' + a.id, { feedback: 'irrelevant', state: 'dismissed' });
          toast('Đã ghi nhận là không liên quan.');
          await refreshStatus();
          load();
        }),
      }, 'Không liên quan') : null,
      h('button', { class: 'link', onclick: run(async () => {
        const s = await api('POST', '/chat/sessions', { title: a.title.slice(0, 60), scope: { kind: 'alert', id: a.id } });
        location.hash = '#/chat/' + s.id;
      }) }, 'Hỏi AI về tin này')),
    detail);
  return el;
}

function fillDetail(el, a) {
  const row = (label, value) => value ? [h('dt', null, label), h('dd', null, value)] : null;
  fill(el, h('dl', null,
    row('Ảnh hưởng tới', a.affected),
    row('Ngày có hiệu lực', fmtDate(a.effective_at)),
    row('Tình trạng xác minh', labels.verified[a.verified]),
    row('Vì sao có cảnh báo này', a.reason),
    row('Từ khóa khớp', (a.matched_keywords || []).join(', ')),
    row('Đánh giá bởi', a.ai_provider ? 'AI (' + a.ai_provider + '), mức liên quan: ' + (a.relevance || '?') : 'Quy tắc từ khóa, không có AI'),
    a.evidence ? [h('dt', null, 'Trích nguyên văn làm căn cứ'), h('dd', null, h('blockquote', null, a.evidence))] : null,
    a.document ? [h('dt', null, 'Văn bản chính thức'),
      h('dd', null, h('a', { href: '#/docs/' + a.document.id }, a.document.doc_number + ' — ' + a.document.title))] : null,
    (a.items || []).length ? [h('dt', null, 'Các nguồn đưa tin'),
      h('dd', null, a.items.map((it) => h('div', null, ext(it.url, it.source_name + ': ' + it.title), ' ', h('span', { class: 'hint' }, labels.tier[it.source_tier]))))] : null));
}

export function onEvent(name) {
  if (name === 'alert.created' || name === 'scan.finished') load();
}
