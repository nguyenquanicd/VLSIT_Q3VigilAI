import { h, clear, fill, api, run, toast, labels, ext, empty } from '../lib.js';

export const title = 'Chat';

let current = 0;

export async function render(ctx) {
  current = Number(ctx.params[0]) || 0;
  const { sessions } = await api('GET', '/chat/sessions');
  const list = h('div', { class: 'chat-list' },
    h('button', { class: 'primary', onclick: run(async () => {
      const s = await api('POST', '/chat/sessions', { scope: { kind: 'all' } });
      location.hash = '#/chat/' + s.id;
    }) }, '+ Cuộc trò chuyện mới'),
    sessions.map((s) => h('a', { href: '#/chat/' + s.id, class: s.id === current ? 'on' : '', title: s.title }, s.title || 'Chưa đặt tên')));
  const main = h('div', { class: 'chat-main' });
  fill(ctx.view, h('div', { class: 'chat' }, list, main));
  if (!current) {
    main.append(empty('Hỏi đáp trên dữ liệu đã thu thập',
      'Q3VNLaw chỉ trả lời từ các văn bản và bài báo nó đã tải về, và luôn kèm đoạn trích làm căn cứ. Bấm "Cuộc trò chuyện mới" để bắt đầu, hoặc bấm "Hỏi AI về tin này" trên một cảnh báo.'));
    return;
  }
  const data = await api('GET', '/chat/sessions/' + current);
  const scope = JSON.parse(data.session.scope || '{}');
  const msgs = h('div', { class: 'msgs' });
  const input = h('textarea', { rows: 2, placeholder: 'Nhập câu hỏi… (Enter để gửi, Shift+Enter để xuống dòng)' });
  const sendBtn = h('button', { class: 'primary' }, 'Gửi');
  const send = run(async () => {
    const text = input.value.trim();
    if (!text || sendBtn.disabled) return;
    input.value = '';
    sendBtn.disabled = true;
    msgs.append(message({ role: 'user', content: text }));
    const live = h('div', { class: 'text' }, '…');
    const holder = h('div', { class: 'msg assistant' }, live);
    msgs.append(holder);
    msgs.scrollTop = msgs.scrollHeight;
    try {
      await stream(current, text, (delta, first) => {
        if (first) live.textContent = '';
        live.append(delta);
        msgs.scrollTop = msgs.scrollHeight;
      }, (final) => holder.replaceWith(message(final)));
    } finally {
      sendBtn.disabled = false;
      input.focus();
    }
  });
  sendBtn.addEventListener('click', send);
  input.addEventListener('keydown', (e) => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send(); } });
  if (data.messages.length === 0) {
    msgs.append(h('div', { class: 'hint' }, scope.kind === 'alert' ? 'Phạm vi: chỉ các nguồn của cảnh báo đã chọn.'
      : scope.kind === 'document' ? 'Phạm vi: chỉ văn bản đã chọn.' : 'Phạm vi: toàn bộ văn bản và bài báo đã thu thập.'));
  }
  msgs.append(...data.messages.map(message));
  main.append(
    h('div', { class: 'toolbar', }, h('span', { class: 'grow' }),
      h('button', { class: 'link danger', onclick: run(async () => {
        if (!confirm('Xóa cuộc trò chuyện này?')) return;
        await api('DELETE', '/chat/sessions/' + current);
        location.hash = '#/chat';
      }) }, 'Xóa cuộc trò chuyện')),
    msgs,
    h('div', { class: 'composer' }, input, sendBtn),
    h('div', { class: 'disclaimer' }, 'Câu trả lời chỉ mang tính tham khảo, không thay thế tư vấn pháp lý. Luôn đối chiếu với văn bản gốc trước khi áp dụng.'));
  msgs.scrollTop = msgs.scrollHeight;
  input.focus();
}

function message(m) {
  if (m.role === 'user') return h('div', { class: 'msg user' }, m.content);
  const cites = m.citations || [];
  return h('div', { class: 'msg assistant' },
    h('div', { class: 'text' }, m.content),
    cites.length ? h('div', { class: 'cites' },
      h('div', { class: 'hint' }, 'Căn cứ' + (m.provider ? '' : ' (kết quả tìm kiếm)') + ':'),
      cites.map((c) => h('details', { class: 'cite' + (c.used ? '' : ' unused') },
        h('summary', null, `[${c.n}] ${c.label || 'Không rõ'}${c.path ? ' — ' + c.path : ''} · ${c.source || ''}${c.tier ? ' (' + labels.tier[c.tier] + ')' : ''}${c.used ? '' : ' · không được dùng trong câu trả lời'}`),
        h('div', { class: 'quote' }, c.quote),
        c.url ? h('div', null, ext(c.url, 'Mở nguồn ↗')) : null))) : null);
}

// stream posts a question and reads the answer as server-sent events.
async function stream(sessionId, content, onDelta, onDone) {
  const resp = await fetch(`/api/chat/sessions/${sessionId}/messages`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, credentials: 'same-origin', body: JSON.stringify({ content }),
  });
  if (!resp.ok) {
    let msg = 'Lỗi ' + resp.status;
    try { msg = (await resp.json()).error.message; } catch (e) { /* keep default */ }
    throw new Error(msg);
  }
  const reader = resp.body.getReader();
  const decoder = new TextDecoder();
  let buf = '';
  let first = true;
  for (;;) {
    const { value, done } = await reader.read();
    if (done) break;
    buf += decoder.decode(value, { stream: true });
    let i;
    while ((i = buf.indexOf('\n\n')) >= 0) {
      const block = buf.slice(0, i);
      buf = buf.slice(i + 2);
      const event = (/^event: (.*)$/m.exec(block) || [])[1];
      const raw = (/^data: (.*)$/m.exec(block) || [])[1];
      if (!event || !raw) continue;
      const data = JSON.parse(raw);
      if (event === 'delta') { onDelta(data.text, first); first = false; }
      else if (event === 'done') onDone(data);
      else if (event === 'error') {
        if (data.stored && data.stored.content) onDone(data.stored);
        toast(data.message, true);
      }
    }
  }
}
