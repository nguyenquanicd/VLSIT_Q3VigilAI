import { h, clear, fill, api, run, toast } from '../lib.js';
import { refreshStatus } from '../app.js';

export const title = 'Cài đặt';

export async function render(ctx) {
  const [s, p] = await Promise.all([api('GET', '/settings'), api('GET', '/providers')]);
  const field = {};
  const text = (key, attrs) => (field[key] = h('input', { value: s[key], ...attrs }));
  const check = (key, label) => h('label', { class: 'inline' }, (field[key] = h('input', { type: 'checkbox', checked: s[key] === '1' })), label);
  const provider = (field.ai_provider = h('select', null, [
    ['auto', 'Tự dò (Claude CLI → Codex CLI → Ollama)'], ['none', 'Không dùng AI (chỉ từ khóa)'], ['claude-cli', 'Claude CLI'],
    ['codex-cli', 'Codex CLI'], ['ollama', 'Ollama (chạy trên máy này)'], ['http-anthropic', 'Anthropic API (khóa API)'],
    ['http-openai', 'Điểm cuối kiểu OpenAI (dịch vụ khác hoặc máy chủ model cục bộ)'],
  ].map(([v, l]) => h('option', { value: v, selected: s.ai_provider === v }, l))));
  const apiKey = h('input', { type: 'password', autocomplete: 'off', placeholder: s.has_api_key ? 'Đã lưu. Nhập khóa mới để thay.' : 'Chưa có khóa' });
  const aiResult = h('div');

  const collect = (keys) => {
    const out = {};
    for (const k of keys) out[k] = field[k].type === 'checkbox' ? (field[k].checked ? '1' : '0') : field[k].value.trim();
    return out;
  };
  const save = (keys, extra) => run(async () => {
    await api('PUT', '/settings', { ...collect(keys), ...(extra ? extra() : {}) });
    toast('Đã lưu.');
    await refreshStatus();
  });

  const detected = [
    p.detected.claude_cli ? 'Claude CLI: ' + p.detected.claude_cli : 'Claude CLI: không thấy',
    p.detected.codex_cli ? 'Codex CLI: ' + p.detected.codex_cli : 'Codex CLI: không thấy',
    'Ollama: ' + (p.detected.ollama ? 'đang chạy' : 'không chạy'),
  ];

  fill(ctx.view, 
    h('div', { class: 'panel form' },
      h('h2', null, 'Lịch quét'),
      h('div', { class: 'row' },
        h('label', null, 'Bắt đầu quét từ', text('active_start', { type: 'time' })),
        h('label', null, 'Ngừng quét lúc', text('active_end', { type: 'time' })),
        h('label', null, 'Báo chí: quét mỗi (phút)', text('interval_press', { type: 'number', min: 15 })),
        h('label', null, 'Cổng chính thức: quét mỗi (phút)', text('interval_official', { type: 'number', min: 15 }))),
      h('div', { class: 'row' },
        h('label', null, 'Chỉ xét tin trong vòng (ngày)', text('lookback_days', { type: 'number', min: 1, max: 60 })),
        h('label', null, 'Giữ tin không sinh cảnh báo (ngày)', text('retention_days', { type: 'number', min: 7, max: 365 }))),
      h('p', { class: 'hint' }, 'Khi máy ngủ hoặc tắt, các lượt bị lỡ được gộp thành một lượt quét bù khi máy hoạt động lại. Đổi tần suất ở đây sẽ áp dụng cho mọi nguồn cùng loại.'),
      h('div', null, h('button', { class: 'primary', onclick: save(['active_start', 'active_end', 'interval_press', 'interval_official', 'lookback_days', 'retention_days']) }, 'Lưu lịch quét'))),

    h('div', { class: 'panel form' },
      h('h2', null, 'Thông báo'),
      h('div', { class: 'checks' },
        check('notify_warning', 'Mức Cảnh báo'), check('notify_notice', 'Mức Cần chú ý'),
        check('notify_info', 'Mức Thông tin'), check('notify_system', 'Lỗi hệ thống (nguồn hỏng)')),
      h('div', { class: 'row' },
        h('label', null, 'Giờ yên lặng từ', text('quiet_start', { type: 'time' })),
        h('label', null, 'đến', text('quiet_end', { type: 'time' })),
        h('label', null, 'Gộp thành một thông báo khi nhiều hơn (tin)', text('notify_group_over', { type: 'number', min: 0, max: 50 }))),
      h('p', { class: 'hint' }, 'Trong giờ yên lặng cảnh báo vẫn được ghi nhận; thông báo được dồn sang lúc hết giờ yên lặng.'),
      h('p', { class: 'hint' }, 'Mức Thông tin (phần lớn tin báo chí khớp từ khóa) mặc định không hiện thông báo nổi; các cảnh báo đó vẫn có trong danh sách và làm biểu tượng khay hiện chấm đỏ. Bật mức này nếu bạn muốn nhận cả loại tin đó.'),
      h('div', { class: 'toolbar' },
        h('button', { class: 'primary', onclick: save(['notify_warning', 'notify_notice', 'notify_info', 'notify_system', 'quiet_start', 'quiet_end', 'notify_group_over']) }, 'Lưu thông báo'),
        h('button', { onclick: run(async () => { await api('POST', '/notify/test'); toast('Đã gửi thông báo thử. Nếu không thấy, hãy kiểm tra "Không làm phiền" của Windows và mục Thông báo trong Cài đặt Windows.'); }) }, 'Gửi thông báo thử'))),

    h('div', { class: 'panel form' },
      h('h2', null, 'AI'),
      p.error ? h('div', { class: 'banner bad' }, 'AI đang không dùng được: ' + p.error) : null,
      h('div', { class: 'banner' }, 'Hiện tại: ' + (p.note || 'chưa rõ')),
      h('label', null, 'Loại AI', provider),
      h('div', { class: 'row' },
        h('label', null, 'Đường dẫn CLI (bỏ trống = tự tìm)', text('ai_cli_path', { placeholder: 'C:\\…\\claude.exe' })),
        h('label', null, 'Model (bỏ trống = mặc định)', text('ai_model', { placeholder: p.default_anthropic_model }))),
      h('div', { class: 'row' },
        h('label', null, 'Địa chỉ máy chủ AI (Ollama hoặc điểm cuối kiểu OpenAI)', text('ai_base_url', { placeholder: 'http://127.0.0.1:11434' })),
        h('label', null, 'Khóa API', apiKey)),
      h('div', { class: 'row' },
        h('label', null, 'Số lần gọi AI tối đa mỗi lượt quét', text('ai_max_calls', { type: 'number', min: 0, max: 500 })),
        h('label', null, 'Thời gian chờ mỗi lần gọi (giây)', text('ai_timeout_seconds', { type: 'number', min: 20, max: 600 }))),
      h('p', { class: 'hint' }, 'Tìm thấy trên máy — ' + detected.join(' · ')),
      h('p', { class: 'hint' }, 'AI chỉ nhận nội dung công khai đã tải từ các nguồn và mô tả chủ đề của bạn; nó không được cấp công cụ nào (không duyệt web, không đọc tệp). Với AI qua mạng, các nội dung đó rời khỏi máy này. Khóa API được mã hóa theo tài khoản Windows hiện tại; chép thư mục sang máy khác phải nhập lại.'),
      h('div', { class: 'toolbar' },
        h('button', { class: 'primary', onclick: run(async () => {
          await save(['ai_provider', 'ai_cli_path', 'ai_model', 'ai_base_url', 'ai_max_calls', 'ai_timeout_seconds'],
            () => (apiKey.value ? { ai_api_key: apiKey.value } : {}))();
          render(ctx);
        }) }, 'Lưu cài đặt AI'),
        h('button', { onclick: run(async () => {
          fill(aiResult, h('div', { class: 'hint' }, 'Đang gọi thử AI (có thể mất vài chục giây)…'));
          const r = await api('POST', '/providers/test');
          fill(aiResult, h('div', { class: 'banner ' + (r.ok ? 'ok' : 'bad') }, (r.provider ? r.provider + ': ' : '') + r.message));
          await refreshStatus();
        }) }, 'Kiểm tra kết nối'),
        s.has_api_key ? h('button', { class: 'link danger', onclick: run(async () => {
          await api('PUT', '/settings', { ai_api_key: '' });
          toast('Đã xóa khóa API.');
          render(ctx);
        }) }, 'Xóa khóa API đã lưu') : null),
      aiResult),

    h('div', { class: 'panel form' },
      h('h2', null, 'Hệ thống'),
      check('autostart', 'Khởi động cùng Windows'),
      h('p', { class: 'hint' }, 'Thư mục dữ liệu: ' + ctx.app.status.data_dir),
      h('div', { class: 'toolbar' },
        h('button', { class: 'primary', onclick: save(['autostart']) }, 'Lưu'),
        h('button', { onclick: run(async () => { const r = await api('POST', '/backup'); toast('Đã sao lưu: ' + r.path); }) }, 'Sao lưu cơ sở dữ liệu ngay'))),

    h('div', { class: 'panel' },
      h('h2', null, 'Giới thiệu'),
      h('p', null, 'Q3VNLaw ' + ctx.app.status.version + ' — theo dõi thay đổi pháp luật Việt Nam từ nguồn chính thống.'),
      h('p', { class: 'hint' }, 'Ứng dụng là công cụ theo dõi và tra cứu, không phải dịch vụ tư vấn pháp lý. Tóm tắt và phân loại do AI hoặc quy tắc từ khóa tạo ra có thể sai; tình trạng hiệu lực của văn bản là suy luận, trừ khi ghi rõ là dữ liệu chính thức. Luôn đối chiếu với văn bản gốc trước khi áp dụng.')));
}
