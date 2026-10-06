import { h, fill, ext, t, N } from '../lib.js';

export const title = N('Trợ giúp & giới thiệu');

const REPO = 'https://github.com/nguyenquanicd/VLSIT_Q3VNLaw_Chat_Bot';
const AUTHORS = ['Nguyễn Quân', 'Nguyễn Lê Ngọc Hân', 'Claude Sonnet 5.5 Extra'];

// A help topic is a title and one or more paragraphs; the titles and texts are
// translated when the page is drawn.
const topics = [
  [N('Bắt đầu nhanh'), [
    N('1. Vào "Chủ đề theo dõi" và tạo chủ đề đầu tiên: chọn một lĩnh vực hoặc gõ từ khóa, rồi Lưu.'),
    N('2. Bấm "Quét ngay" để thu thập tin. Cảnh báo khớp chủ đề hiện ở màn hình "Cảnh báo".'),
    N('3. Đóng cửa sổ thì ứng dụng vẫn chạy ngầm ở khay hệ thống và tự quét theo lịch. Bấm vào biểu tượng Q để mở lại.'),
  ]],
  [N('Không thấy thông báo nổi?'), [
    N('Vào Cài đặt → Thông báo → "Gửi thông báo thử". Nếu không thấy gì, hãy xem chuông ở góc dưới phải của Windows: chữ zZ nghĩa là "Không làm phiền" đang bật và Windows chặn mọi thông báo nổi.'),
    N('Mức "Thông tin" mặc định không có thông báo nổi; các cảnh báo đó vẫn có trong danh sách và làm biểu tượng khay hiện chấm đỏ.'),
  ]],
  [N('Cách đọc một cảnh báo'), [
    N('"Đã xác nhận" nghĩa là văn bản được nhắc tới đã có trên Cổng Thông tin điện tử Chính phủ. "Chưa xác nhận" là tin báo chí đưa trước, ứng dụng sẽ tra lại mỗi ngày.'),
    N('Trạng thái pháp lý (Đề xuất, Dự thảo, Đã ban hành, Đã có hiệu lực) giúp không nhầm một đề xuất với quy định đã có hiệu lực. Không có AI thì trạng thái chỉ là phỏng đoán theo câu chữ.'),
  ]],
  [N('Bật AI'), [
    N('Vào Cài đặt → AI, chọn loại AI (Claude CLI, Codex CLI, Ollama, Anthropic API hoặc điểm cuối kiểu OpenAI), lưu, rồi bấm "Kiểm tra kết nối". Không có AI thì ứng dụng vẫn chạy ở chế độ từ khóa.'),
  ]],
  [N('Dữ liệu nằm ở đâu'), [
    N('Trong thư mục "data" cạnh file chạy (xem đường dẫn ở Cài đặt → Hệ thống). Xóa thư mục là gỡ sạch dữ liệu. Ứng dụng chỉ truy cập các nguồn trong mục Nguồn và không gửi dữ liệu nào về nhà phát triển.'),
  ]],
];

export async function render(ctx) {
  const version = ctx.app.status.version;
  const link = (url, label) => h('div', null, ext(url, label + ' ↗'));
  fill(ctx.view,
    h('div', { class: 'panel' },
      h('h2', null, 'Q3VigilAI ' + version),
      h('p', null, t('Theo dõi thay đổi pháp luật Việt Nam từ nguồn chính thống và báo chí chính thống, chạy ngầm ở khay hệ thống Windows và thông báo khi có tin liên quan đến các chủ đề bạn đặt.')),
      h('p', { class: 'hint' }, t('Giấy phép Apache 2.0.'))),

    h('div', { class: 'panel' },
      h('h2', null, t('Mã nguồn và liên kết')),
      link(REPO, t('Mã nguồn trên GitHub')),
      link(REPO + '#readme', t('Hướng dẫn sử dụng đầy đủ (README)')),
      link(REPO + '/issues', t('Báo lỗi hoặc góp ý')),
      link(REPO + '/blob/main/LICENSE', t('Giấy phép (Apache License 2.0)')),
      h('p', { class: 'hint mt' }, REPO)),

    h('div', { class: 'panel' },
      h('h2', null, t('Tác giả')),
      h('ul', { class: 'authors' }, AUTHORS.map((a) => h('li', null, a)))),

    h('div', { class: 'panel' },
      h('h2', null, t('Trợ giúp nhanh')),
      topics.map(([heading, paragraphs], i) => h('details', { class: 'help', open: i === 0 },
        h('summary', null, t(heading)),
        paragraphs.map((p) => h('p', null, t(p))))),
      h('p', { class: 'hint mt' }, t('Xem thêm cách dùng từng màn hình ở tài liệu trên GitHub.'))),

    h('div', { class: 'panel' },
      h('h2', null, t('Lưu ý')),
      h('p', { class: 'hint' }, t('Ứng dụng là công cụ theo dõi và tra cứu, không phải dịch vụ tư vấn pháp lý. Tóm tắt và phân loại do AI hoặc quy tắc từ khóa tạo ra có thể sai; tình trạng hiệu lực của văn bản là suy luận, trừ khi ghi rõ là dữ liệu chính thức. Luôn đối chiếu với văn bản gốc trước khi áp dụng.')),
      h('p', { class: 'hint' }, t('Thư mục dữ liệu: {0}', ctx.app.status.data_dir))));
}
