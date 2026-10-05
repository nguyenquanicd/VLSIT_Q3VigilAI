# Q3VNLaw

Ứng dụng **portable chạy ngầm ở khay hệ thống Windows**, tự theo dõi thay đổi pháp luật
Việt Nam từ nguồn chính thống và báo chí chính thống theo các chủ đề bạn đặt, rồi thông
báo cho bạn. Có thể dùng kèm AI (Claude CLI, Codex CLI, Ollama, Anthropic API hoặc điểm
cuối kiểu OpenAI) để tóm tắt và đánh giá mức liên quan; không có AI thì vẫn chạy ở chế
độ từ khóa.

Một file `q3vnlaw.exe` (khoảng 18 MB), không cần cài đặt, không cần quyền admin.

> Q3VNLaw là công cụ theo dõi và tra cứu, không phải dịch vụ tư vấn pháp lý. Tóm tắt và
> phân loại có thể sai; tình trạng hiệu lực của văn bản là suy luận. Luôn đối chiếu với
> văn bản gốc trước khi áp dụng.

## Chạy

1. Chép `q3vnlaw.exe` vào một thư mục bất kỳ rồi chạy. Lần đầu Windows SmartScreen có
   thể hỏi vì file chưa ký số: chọn **More info → Run anyway**.
2. Biểu tượng chữ **Q** xuất hiện ở khay hệ thống. Windows 11 thường xếp biểu tượng mới
   vào ngăn ẩn: bấm mũi tên **^** cạnh đồng hồ, có thể kéo biểu tượng ra thanh tác vụ.
3. Bấm vào biểu tượng để mở cửa sổ, vào **Chủ đề theo dõi** và tạo chủ đề đầu tiên.

Dữ liệu nằm trong thư mục `data\` cạnh file chạy. Xóa cả thư mục là gỡ xong. Chạy file
lần thứ hai chỉ mở lại cửa sổ của phiên bản đang chạy.

| Trên biểu tượng | Nghĩa |
|---|---|
| Chấm đỏ góc trên | Có cảnh báo chưa đọc |
| Chấm vàng góc trên | Có nguồn không quét được |
| Chấm xanh góc dưới | Đang quét |
| Nền xám | Đang tạm dừng thông báo |

Bấm chuột phải vào biểu tượng: mở cửa sổ, quét ngay, tạm dừng thông báo, bật/tắt khởi
động cùng Windows, thoát.

## Cách hoạt động

```
Nguồn (RSS, cổng Chính phủ) → tin mới → lọc theo chủ đề (không AI)
   → AI đánh giá (nếu có) → đối chiếu Cổng Chính phủ → cảnh báo → thông báo
```

- **Nguồn**: 15 nguồn dựng sẵn. Tầng 1 là Cổng Thông tin điện tử Chính phủ (văn bản quy
  phạm pháp luật, văn bản chỉ đạo điều hành, chuyên trang chính sách) và Báo Chính phủ;
  tầng 3 là báo chí có giấy phép (Nhân Dân, TTXVN, VOV, VTV, VnExpress, Tuổi Trẻ, Thanh
  Niên, Dân trí, VietNamNet, VnEconomy, Thời báo Tài chính). Ứng dụng chỉ truy cập các
  tên miền trong danh sách; mạng xã hội, diễn đàn, blog bị chặn cứng trong mã nguồn.
- **Chủ đề**: từ khóa, lĩnh vực (bộ từ khóa dựng sẵn), từ khóa loại trừ, và văn bản
  theo dõi đích danh theo số hiệu.
- **Hai loại cảnh báo tách bạch**: *tin báo chí* (để biết sớm) và *văn bản chính thức*
  (có số hiệu, file gốc, ngày hiệu lực). Tin báo chí nêu số hiệu một văn bản mới sẽ được
  tra trên Cổng Chính phủ; tìm thấy thì gắn nhãn "Đã xác nhận" kèm file gốc, chưa thấy
  thì tra lại mỗi ngày trong 30 ngày.
- **Trạng thái pháp lý**: đề xuất / dự thảo / đã ban hành / đã có hiệu lực. Có AI thì do
  AI phân loại; không có AI thì là phỏng đoán theo câu chữ và được ghi rõ là phỏng đoán.
- **Chat**: chỉ trả lời từ văn bản và bài báo đã thu thập, kèm đoạn trích làm căn cứ.
  Không tìm thấy căn cứ thì nói là không có, không gọi AI.

## AI

Vào **Cài đặt → AI**. Mặc định ứng dụng tự dò theo thứ tự Claude CLI → Codex CLI →
Ollama. AI không được cấp công cụ nào: không duyệt web, không đọc file, không chạy lệnh.

| Loại | Cần gì |
|---|---|
| Claude CLI | Đã cài và **đã đăng nhập**. Mở terminal, chạy `claude`, gõ `/login` |
| Codex CLI | Đã cài và đã đăng nhập |
| Ollama | Ollama đang chạy và đã tải ít nhất một model |
| Anthropic API | Khóa API. Model mặc định `claude-opus-5-5` |
| Điểm cuối kiểu OpenAI | Địa chỉ máy chủ, tên model, khóa API nếu có |

Nút **Kiểm tra kết nối** gọi thử một lần và báo ngay nếu AI không dùng được. Khóa API
được mã hóa theo tài khoản Windows (DPAPI); chép thư mục sang máy khác phải nhập lại.

## Build từ mã nguồn

Cần Go 1.27 trở lên.

```powershell
.\build.ps1            # chạy toàn bộ test rồi build q3vnlaw.exe
.\build.ps1 -SkipTests # chỉ build
.\build.ps1 -Live      # chạy thêm test trên các nguồn thật (cần mạng)
```

`-Live` là cách kiểm tra nguồn nào đã đổi địa chỉ RSS hay đổi cấu trúc trang.

```
cmd/q3vnlaw/     chương trình chính: khay, cửa sổ, lắp ráp
internal/
  textutil/      bỏ dấu, khớp từ khóa, số hiệu văn bản, trích chữ từ HTML
  store/         SQLite (một file) + tìm toàn văn FTS5
  fetch/         tải web: whitelist tên miền, robots.txt, giới hạn tốc độ
  sources/       bộ nối RSS, trang danh mục, cổng vanban.chinhphu.vn
  extract/       trích chữ PDF, tách theo Chương/Điều/Khoản
  ai/            bộ nối AI và lời nhắc phân loại
  pipeline/      lượt quét, lọc, cảnh báo, xác minh, lịch, thông báo
  chat/          truy hồi và trả lời có trích dẫn
  server/        API cục bộ (127.0.0.1) và giao diện web nhúng
  tray/          biểu tượng khay và thông báo (Win32)
web/             giao diện: HTML, CSS, JavaScript thuần, không có bước build
docs/            đặc tả
```

## Giới hạn cần biết

- **AI chưa được chạy thử với model thật** trong quá trình phát triển: máy phát triển
  không có CLI nào đã đăng nhập và không có khóa API. Các bộ nối được kiểm thử bằng CLI
  giả và máy chủ giả mô phỏng đúng định dạng thật; bộ nối Codex CLI chỉ dựa trên tài
  liệu. Hãy dùng nút **Kiểm tra kết nối** sau khi cấu hình.
- **Không có AI thì nhiều tin nhiễu**: lọc từ khóa bắt cả tin vụ án có chữ "thuế". Các
  tin đó ở mức "Thông tin" và mặc định không bật thông báo.
- **PDF scan không tìm kiếm được**: nhiều văn bản trên Cổng Chính phủ là bản scan. File
  vẫn được tải về, nhưng nội dung chưa hỏi đáp được cho tới khi OCR.
- **Tình trạng hiệu lực là suy luận**: Cổng Chính phủ ghi ngày ban hành và ngày có hiệu
  lực, không ghi văn bản còn hay hết hiệu lực. Hãy đối chiếu tại vbpl.vn.
- **Chỉ đọc tiêu đề và tóm tắt RSS để lọc**: bài báo chỉ nhắc từ khóa trong thân bài sẽ
  bị bỏ qua.
- **Không thấy thông báo nổi?** Có ba nguyên nhân, kiểm tra theo thứ tự:
  1. Bấm **Cài đặt → Thông báo → Gửi thông báo thử**. Không thấy gì thì lỗi nằm ở Windows:
     chuông ở góc dưới phải có chữ **zZ** nghĩa là "Không làm phiền" đang bật, khi đó
     Windows không hiện thông báo nổi mà chỉ cất vào Trung tâm thông báo (Win + N).
  2. Mức **Thông tin** mặc định không có thông báo nổi (xem mục Thông báo trong Cài
     đặt); các cảnh báo mức này vẫn có trong danh sách và làm biểu tượng có chấm đỏ.
  3. Đang trong giờ yên lặng (mặc định 21:00–07:00) hoặc đang tạm dừng thông báo.

  Mỗi lần bấm **Quét ngay**, ứng dụng luôn trả lời bằng một thông báo tóm tắt (kể cả
  khi không có tin mới), và nhật ký ghi lại mọi cảnh báo bị bỏ qua cùng lý do.
- **Nguồn có thể đổi cấu trúc**: khi đó nguồn hiện "Lỗi" hoặc "Nghi hỏng" ở mục Nguồn
  và ứng dụng báo một lần mỗi ngày; nó không im lặng.

Chi tiết thiết kế: [docs/SPECIFICATION.md](docs/SPECIFICATION.md).
