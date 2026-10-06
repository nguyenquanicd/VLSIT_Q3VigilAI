import { h, clear, fill, api, run, toast, ago, fmtDate, labels, ext, empty, t, N } from '../lib.js';
import { refreshStatus } from '../app.js';

export const title = N('Cảnh báo');

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
      h('div', { class: 'tabs' }, tab('', t('Hộp thư')), tab('filtered', t('Đã lọc bỏ')), tab('dismissed', t('Đã bỏ qua'))),
      select('severity', [['', t('Mọi mức')], ['warning', labels.severity.warning], ['notice', labels.severity.notice], ['info', labels.severity.info]]),
      select('kind', [['', t('Mọi loại')], ['press', t('Báo chí')], ['official', t('Văn bản chính thức')], ['doc_changed', t('Văn bản theo dõi')], ['effective_soon', t('Sắp có hiệu lực')]]),
      select('topic', [[0, t('Mọi chủ đề')], ...topics.map((tp) => [tp.id, tp.name])]),
      h('input', { type: 'search', placeholder: t('Tìm tiêu đề, số hiệu…'), value: filter.q, class: 'grow',
        oninput: (e) => { filter.q = e.target.value; clearTimeout(timer); timer = setTimeout(load, 300); } }),
      h('button', { onclick: run(async () => { await api('POST', '/alerts/mark-read'); await refreshStatus(); load(); }) }, t('Đánh dấu đã đọc tất cả'))),
    filter.state === 'filtered' ? h('div', { class: 'banner' },
      t('Các tin dưới đây khớp từ khóa nhưng đã bị loại (AI đánh giá không liên quan, hoặc là dự thảo trong khi chủ đề không theo dõi dự thảo). Xem qua để chắc rằng không lọc sót.')) : null,
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
      ? empty(t('Chưa có chủ đề theo dõi'), h('a', { href: '#/topics' }, t('Hãy tạo chủ đề đầu tiên để Q3VigilAI biết cần báo cho bạn điều gì.')))
      : filtering ? empty(t('Không có cảnh báo nào khớp bộ lọc'), t('Thử bỏ bớt điều kiện lọc.'))
        : filter.state ? empty(t('Danh sách trống'), '')
          : empty(t('Chưa có cảnh báo'), t('Khi có tin pháp luật khớp chủ đề của bạn, nó sẽ hiện ở đây. Bấm "Quét ngay" để kiểm tra các nguồn.')));
    return;
  }
  list.append(...data.alerts.map(card));
  if (data.total > data.alerts.length) list.append(h('div', { class: 'hint' }, t('Đang hiện {0} trên {1} cảnh báo. Dùng bộ lọc để thu hẹp.', data.alerts.length, data.total)));
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
  if (a.verified === 'confirmed') out.push(h('span', { class: 'badge ok' }, t('Đã xác nhận')));
  if (a.verified === 'unconfirmed') out.push(h('span', { class: 'badge notice' }, t('Chưa xác nhận')));
  if (a.state === 'pinned') out.push(h('span', { class: 'badge' }, t('Đã ghim')));
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
  // The names of the built-in sources are Vietnamese; they have translations.
  const source = a.source_name ? t(a.source_name) : t('Không rõ nguồn');
  el.append(
    h('div', { class: 'badges' }, badges(a)),
    h('h3', { onclick: toggle, title: t('Bấm để xem chi tiết') }, a.title),
    a.summary ? h('div', { class: 'summary' }, a.summary) : null,
    h('div', { class: 'meta' },
      h('span', null, source + (a.source_count > 1 ? ' ' + t('và {0} nguồn khác', a.source_count - 1) : '')),
      a.source_tier ? h('span', { class: 'chip' }, labels.tier[a.source_tier]) : null,
      h('span', null, ago(a.created_at)),
      h('span', null, t('Chủ đề: {0}', a.topic_name)),
      a.effective_at ? h('span', null, t('Hiệu lực từ {0}', fmtDate(a.effective_at))) : null,
      (a.doc_numbers || []).map((n) => h('span', { class: 'chip' }, n))),
    h('div', { class: 'actions' },
      a.url ? ext(a.url, a.kind === 'press' ? t('Mở bài gốc ↗') : t('Mở trên cổng chính thức ↗')) : null,
      a.state === 'unread' ? h('button', { class: 'link', onclick: setState('read') }, t('Đã đọc')) : null,
      a.state === 'read' ? h('button', { class: 'link', onclick: setState('unread') }, t('Chưa đọc')) : null,
      a.state !== 'pinned' ? h('button', { class: 'link', onclick: setState('pinned') }, t('Ghim')) : h('button', { class: 'link', onclick: setState('read') }, t('Bỏ ghim')),
      a.state === 'filtered' || a.state === 'dismissed'
        ? h('button', { class: 'link', onclick: setState('unread') }, t('Đưa lại vào hộp thư'))
        : h('button', { class: 'link', onclick: setState('dismissed') }, t('Bỏ qua')),
      a.feedback !== 'irrelevant' && a.state !== 'filtered' ? h('button', {
        class: 'link', onclick: run(async () => {
          await api('PATCH', '/alerts/' + a.id, { feedback: 'irrelevant', state: 'dismissed' });
          toast(t('Đã ghi nhận là không liên quan.'));
          await refreshStatus();
          load();
        }),
      }, t('Không liên quan')) : null,
      h('button', { class: 'link', onclick: run(async () => {
        const s = await api('POST', '/chat/sessions', { title: a.title.slice(0, 60), scope: { kind: 'alert', id: a.id } });
        location.hash = '#/chat/' + s.id;
      }) }, t('Hỏi AI về tin này'))),
    detail);
  return el;
}

function fillDetail(el, a) {
  const row = (label, value) => value ? [h('dt', null, label), h('dd', null, value)] : null;
  fill(el, h('dl', null,
    row(t('Ảnh hưởng tới'), a.affected),
    row(t('Ngày có hiệu lực'), fmtDate(a.effective_at)),
    row(t('Tình trạng xác minh'), labels.verified[a.verified]),
    row(t('Vì sao có cảnh báo này'), a.reason),
    row(t('Từ khóa khớp'), (a.matched_keywords || []).join(', ')),
    row(t('Đánh giá bởi'), a.ai_provider ? t('AI ({0}), mức liên quan: {1}', a.ai_provider, labels.relevance[a.relevance] || '?') : t('Quy tắc từ khóa, không có AI')),
    a.evidence ? [h('dt', null, t('Trích nguyên văn làm căn cứ')), h('dd', null, h('blockquote', null, a.evidence))] : null,
    a.document ? [h('dt', null, t('Văn bản chính thức')),
      h('dd', null, h('a', { href: '#/docs/' + a.document.id }, a.document.doc_number + ' — ' + a.document.title))] : null,
    (a.items || []).length ? [h('dt', null, t('Các nguồn đưa tin')),
      h('dd', null, a.items.map((it) => h('div', null, ext(it.url, t(it.source_name) + ': ' + it.title), ' ', h('span', { class: 'hint' }, labels.tier[it.source_tier]))))] : null));
}

export function onEvent(name) {
  if (name === 'alert.created' || name === 'scan.finished') load();
}
