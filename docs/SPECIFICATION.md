# Q3VNLaw — Đặc tả chi tiết (Specification)

| | |
|---|---|
| Phiên bản tài liệu | 0.2 |
| Ngày lập | 2026-10-04 |
| Trạng thái | Đã hiện thực ở phiên bản ứng dụng 0.1.0. Mục 15 ghi hiện trạng, số đo thực tế và các điểm khác thiết kế ban đầu |

Q3VNLaw là một ứng dụng **portable chạy ngầm ở khay hệ thống Windows**, định kỳ quét
các nguồn pháp luật chính thống và báo chí chính thống của Việt Nam theo chủ đề người
dùng đặt, dùng AI (Claude CLI, Codex CLI hoặc model khác) để tóm tắt và đánh giá mức
liên quan, rồi **thông báo / cảnh báo** cho người dùng. Cửa sổ chat là phần phụ, mở khi
người dùng bấm vào biểu tượng.

---

## 1. Mục tiêu và phạm vi

### 1.1 Mục tiêu

1. Chạy ngầm, nhẹ, không cần cài đặt, không cần quyền admin.
2. Tự quét theo lịch, phát hiện thông tin pháp luật mới hoặc thay đổi liên quan tới
   chủ đề người dùng theo dõi.
3. Chỉ lấy từ nguồn chính thống và báo chí có giấy phép; không bao giờ lấy từ diễn
   đàn, mạng xã hội, blog.
4. Tách bạch "tin báo chí" (biết sớm) và "văn bản chính thức" (dùng làm căn cứ).
5. Hoạt động được với nhiều AI khác nhau, và vẫn hữu ích khi không có AI nào.
6. Hỏi đáp (chat) trên kho văn bản và cảnh báo đã thu thập, luôn kèm trích dẫn.

### 1.2 Ngoài phạm vi (phiên bản 1)

- Không phải dịch vụ tư vấn pháp lý; mọi kết quả đều kèm nguồn để người dùng tự kiểm.
- Không nhiều người dùng, không máy chủ, không đồng bộ đám mây.
- Không sao chép toàn bộ cơ sở dữ liệu pháp luật; chỉ lưu những gì khớp chủ đề.
- Không hỗ trợ macOS/Linux ở v1 (kiến trúc không chặn việc mở rộng sau).
- Không tìm kiếm ngữ nghĩa bằng vector ở v1 (xem mục 6.9).

### 1.3 Thuật ngữ

| Thuật ngữ | Nghĩa |
|---|---|
| Nguồn (source) | Một trang/kênh được phép quét, có tầng tin cậy |
| Chủ đề theo dõi (topic) | Bộ điều kiện do người dùng đặt: từ khóa, lĩnh vực, số hiệu |
| Mục tin (item) | Một bài báo hoặc một văn bản lấy về từ nguồn |
| Cảnh báo (alert) | Thông báo sinh ra khi một mục tin khớp một chủ đề |
| Bộ nối AI (provider) | Lớp gọi một AI cụ thể qua CLI hoặc HTTP |

---

## 2. Phân tích các chatbot và công cụ mã nguồn mở

Phần này dựa trên hiểu biết sẵn có về các dự án, chưa đối chiếu lại với bản phát hành
mới nhất của từng dự án tại ngày lập tài liệu.

### 2.1 Nhóm giao diện chat

| Dự án | Front-end | Back-end | Lưu trữ | Cách chạy | Nhận xét cho bài toán này |
|---|---|---|---|---|---|
| Open WebUI | SvelteKit | Python (FastAPI) | SQLite/Postgres + vector DB | Docker hoặc pip | Trừu tượng hóa provider tốt, RAG có trích dẫn. Quá nặng, kiểu máy chủ |
| LibreChat | React | Node (Express) | MongoDB + Meilisearch | Docker Compose | Nhiều người dùng, nhiều dịch vụ phụ. Không hợp portable |
| AnythingLLM | React trong Electron | Node | SQLite + LanceDB | Bộ cài desktop | Mô hình "workspace + tài liệu" đáng học. Dung lượng hàng trăm MB |
| Jan | Web trong Tauri | Rust + engine llama.cpp | File cục bộ | Bộ cài desktop | Vỏ Tauri nhẹ; trọng tâm là chạy model cục bộ |
| LobeChat, NextChat | Next.js | Phần lớn chạy phía client | Trình duyệt / DB tùy chọn | Web, có bản desktop | Nhẹ ở client nhưng không có tác vụ nền |
| Chatbox, Cherry Studio | Web trong Electron | Node | File cục bộ | Bộ cài desktop | Đa provider; vẫn là Electron |
| Khoj | Web + client | Python (Django) | Postgres | Máy chủ + client | Có "automation" chạy truy vấn theo lịch rồi gửi kết quả: gần chức năng ta cần nhất |

### 2.2 Nhóm chạy ngầm và theo dõi thay đổi

| Dự án | Kiến trúc | Bài học |
|---|---|---|
| Ollama | Một binary Go, REST API cục bộ, biểu tượng khay trên Windows | Mẫu "daemon + tray + API cục bộ" |
| Syncthing | Một binary Go, giao diện web nhúng phục vụ tại 127.0.0.1 | Giao diện web nhúng trong binary, không cần framework desktop |
| changedetection.io | Python (Flask), theo dõi trang, so khác biệt, thông báo | Mô hình watch → diff → notify, lịch riêng từng watch, bộ lọc |
| Miniflux | Một binary Go, đọc RSS | Dùng ETag/Last-Modified, khử trùng bằng hash, xử lý feed lỗi |
| Huginn, n8n | Nền tảng agent/workflow | Quá tổng quát và nặng cho một việc cụ thể |

### 2.3 Kết luận

Không dự án nào dùng lại nguyên được:

- Nhóm chat **không có** lịch chạy nền, không kiểm soát nguồn, không có khái niệm hiệu
  lực văn bản.
- Nhóm theo dõi thay đổi **không hiểu** nội dung pháp luật và không có chat.
- Hầu hết kéo theo Electron, Docker, Node hoặc Python, trái với yêu cầu portable gọn nhẹ.

Vì vậy giải pháp là **viết mới, nhỏ, và mượn các mẫu thiết kế đã được kiểm chứng**:

| Mượn từ | Mẫu thiết kế |
|---|---|
| Syncthing, Ollama | Một binary: tiến trình nền + khay + máy chủ HTTP cục bộ + giao diện web nhúng |
| changedetection.io, Miniflux | Đường ống quét: lấy về → chuẩn hóa → khử trùng → lọc → thông báo |
| Open WebUI | Interface provider chung; câu trả lời kèm trích dẫn |
| Khoj | Tác vụ theo lịch gắn với chủ đề của người dùng |

---

## 3. Giải pháp đề xuất

### 3.1 So sánh lựa chọn công nghệ

Số liệu dung lượng và RAM là ước lượng theo kinh nghiệm, chưa đo.

| Phương án | Dung lượng | RAM khi nghỉ | Portable 1 file | Đánh giá |
|---|---|---|---|---|
| Electron + Node | ~150 MB+ | ~150–250 MB | Khó | Loại: quá nặng cho app chạy ngầm cả ngày |
| Python + PyInstaller | ~50–100 MB | ~50–80 MB | Được | Khởi động chậm, hay bị antivirus báo nhầm |
| .NET 8 tự chứa | ~60–150 MB | ~40–60 MB | Được | Hợp Windows, nhưng nặng hoặc phụ thuộc runtime |
| Tauri 2 (Rust) | ~10 MB | ~30–50 MB | Được | Nhẹ; Rust khó bảo trì hơn, build phức tạp |
| **Go + giao diện web nhúng** | **~15–25 MB** | **~20–40 MB** | **Được** | **Chọn** |

### 3.2 Lựa chọn

| Thành phần | Chọn | Lý do |
|---|---|---|
| Ngôn ngữ lõi | Go | Một `.exe` tĩnh, không runtime, đồng thời tốt cho quét nền |
| Biểu tượng khay | Thư viện systray thuần Go | Không cần CGO |
| Cửa sổ giao diện | Microsoft Edge ở chế độ `--app` trỏ về máy chủ cục bộ; dự phòng: trình duyệt mặc định | Edge có sẵn trên Windows 11, không tốn thêm MB nào, trông như cửa sổ ứng dụng |
| Front-end | HTML + CSS + JavaScript thuần, không thư viện ngoài, **không bước build** | Không cần Node; sửa file là xong; không cần nới chính sách bảo mật nội dung (CSP) |
| Cơ sở dữ liệu | SQLite (driver thuần Go) + FTS5 | Một file, tìm toàn văn tiếng Việt, không cần dịch vụ ngoài |
| Thông báo | Balloon của biểu tượng khay (Windows 10/11 hiển thị thành toast) | Không cần đăng ký AppUserModelID hay shortcut, đúng tinh thần portable |
| AI | Interface provider; gọi CLI bằng tiến trình con hoặc HTTP | Đổi model không đụng phần còn lại |

Điều kiện build: máy phát triển cần cài Go (hiện chưa có trên máy này). Máy người dùng
cuối không cần cài gì.

### 3.3 Sơ đồ tổng thể

```
┌────────────────────────── q3vnlaw.exe (1 tiến trình) ──────────────────────────┐
│                                                                                │
│  Tray ──┬─ menu, trạng thái          HTTP server 127.0.0.1:<cổng ngẫu nhiên>   │
│         └─ balloon/toast             ├─ /            giao diện web nhúng       │
│                                      ├─ /api/*       REST                      │
│  Scheduler ──► Pipeline              └─ /api/events  SSE (sự kiện trực tiếp)   │
│                 │                                                              │
│                 ├─ Source registry + Connectors (rss, htmllist, vanban)        │
│                 ├─ Fetcher (whitelist tên miền, giới hạn tốc độ, ETag)         │
│                 ├─ Extract (HTML→text, PDF→text)                               │
│                 ├─ Prefilter (từ khóa, không AI)                               │
│                 ├─ AI Provider (claude-cli | codex-cli | ollama | http | none) │
│                 ├─ Verifier (đối chiếu nguồn chính thức)                       │
│                 └─ Notifier                                                    │
│                                                                                │
│  Chat service ──► Retrieval (FTS5) ──► AI Provider                             │
│                                                                                │
│  Store: data\q3vnlaw.db (SQLite)   data\docs\ (PDF gốc)   data\logs\           │
└────────────────────────────────────────────────────────────────────────────────┘
        ▲                                   │
        │ Edge --app / trình duyệt          ▼ chỉ tới tên miền trong whitelist
     Người dùng                     Nguồn Nhà nước, báo chí chính thống, AI
```

---

## 4. Front-end

### 4.1 Biểu tượng khay

| Trạng thái | Biểu tượng | Tooltip |
|---|---|---|
| Bình thường | Biểu tượng gốc | `Q3VNLaw — quét gần nhất 07:00, không có tin mới` |
| Có cảnh báo chưa đọc | Thêm chấm đỏ | `Q3VNLaw — 3 cảnh báo chưa đọc` |
| Đang quét | Thêm vòng xoay | `Q3VNLaw — đang quét 4/12 nguồn` |
| Có lỗi | Thêm dấu chấm than vàng | `Q3VNLaw — 2 nguồn quét thất bại` |
| Tạm dừng | Biểu tượng xám | `Q3VNLaw — tạm dừng đến 09:00` |

- Bấm trái: mở cửa sổ chính, vào màn hình Cảnh báo.
- Bấm phải: menu

```
Mở Q3VNLaw
Cảnh báo chưa đọc (3)
──────────────
Quét ngay
Tạm dừng thông báo  ▸  1 giờ | Đến sáng mai | Bật lại
──────────────
AI: Claude CLI (sẵn sàng)
Khởi động cùng Windows   ✓
──────────────
Thoát
```

### 4.2 Thông báo

| Mức | Khi nào | Hành vi |
|---|---|---|
| Thông tin | Tin báo chí khớp chủ đề; dự thảo, đề xuất | Hiện trong danh sách; mặc định **không** bật thông báo |
| Cần chú ý | Văn bản chính thức mới khớp chủ đề; văn bản sắp có hiệu lực | Bật thông báo |
| Cảnh báo | Văn bản người dùng theo dõi đích danh bị sửa đổi, thay thế, hết hiệu lực | Bật thông báo, giữ chấm đỏ đến khi đọc |
| Hệ thống | Nguồn quét thất bại liên tiếp ≥ 3 lần; AI không gọi được | Bật thông báo một lần mỗi ngày |

Quy tắc:

- Nội dung thông báo: dòng 1 là nhãn mức và trạng thái pháp lý, dòng 2 là tiêu đề, dòng
  3 là nguồn. Ví dụ: `[Cần chú ý · Đã ban hành] Nghị định …/2026/NĐ-CP về … — Công báo`.
- Một lần quét sinh trên 3 cảnh báo thì gộp thành một thông báo tổng hợp.
- Bấm vào thông báo mở cửa sổ chính tại cảnh báo đó.
- Có "giờ yên lặng" (mặc định 21:00–07:00): cảnh báo vẫn được ghi, thông báo dồn sang
  đầu giờ hoạt động.

### 4.3 Cửa sổ chính

Một trang đơn (SPA) có thanh điều hướng trái, giao diện tiếng Việt, hỗ trợ sáng/tối
theo hệ thống. Kích thước mặc định 1100×720, tối thiểu 800×560.

```
┌──────────────┬──────────────────────────────────────────────┐
│ Q3VNLaw      │  [thanh tiêu đề màn hình]      [Quét ngay]   │
│              ├──────────────────────────────────────────────┤
│ ● Cảnh báo 3 │                                              │
│   Chat       │              nội dung màn hình               │
│   Chủ đề     │                                              │
│   Nguồn      │                                              │
│   Văn bản    │                                              │
│   Nhật ký    │                                              │
│   Cài đặt    │                                              │
├──────────────┤                                              │
│ AI: Claude ✓ │                                              │
│ Quét: 07:00  │                                              │
└──────────────┴──────────────────────────────────────────────┘
```

#### 4.3.1 Cảnh báo (màn hình mặc định)

- Bộ lọc: mức, loại (báo chí / văn bản chính thức), chủ đề, trạng thái đọc, khoảng ngày.
- Mỗi thẻ cảnh báo hiển thị:
  - nhãn mức; nhãn trạng thái pháp lý (Đề xuất / Dự thảo / Đã ban hành / Đã có hiệu lực);
  - nhãn xác minh (Đã xác nhận từ nguồn chính thức / Chưa xác nhận);
  - tiêu đề, tóm tắt 2–3 câu, "ảnh hưởng tới ai", ngày hiệu lực nếu có;
  - nguồn và tầng nguồn, thời điểm lấy tin, link bài gốc;
  - số hiệu văn bản được nhắc tới (bấm để mở trong màn hình Văn bản);
  - nếu nhiều báo cùng đưa: "và 4 nguồn khác" (mở ra danh sách).
- Thao tác: đánh dấu đã đọc, ghim, bỏ qua, "không liên quan" (phản hồi dùng để tinh
  chỉnh chủ đề), "hỏi AI về tin này" (mở Chat với ngữ cảnh là tin đó).
- Tab phụ "Đã lọc bỏ": các mục tin khớp từ khóa nhưng AI đánh giá không liên quan, để
  người dùng kiểm tra việc lọc sót.

#### 4.3.2 Chat

- Danh sách phiên bên trái, khung hội thoại bên phải, câu trả lời hiện dần (streaming).
- Mỗi câu trả lời có khối **Căn cứ**: danh sách trích dẫn `[1] Số hiệu, Điều/Khoản,
  nguồn`, bấm vào hiện đoạn nguyên văn và link gốc.
- Khi không tìm thấy căn cứ trong kho, câu trả lời phải nói rõ là không có căn cứ trong
  dữ liệu đã thu thập, và gợi ý thêm chủ đề hoặc tải văn bản.
- Chọn phạm vi tìm: toàn bộ kho / một chủ đề / một văn bản / một cảnh báo.
- Dòng chú thích cố định cuối khung: kết quả mang tính tham khảo, cần đối chiếu văn bản gốc.

#### 4.3.3 Chủ đề theo dõi

Danh sách chủ đề và biểu mẫu tạo/sửa:

| Trường | Kiểu | Ghi chú |
|---|---|---|
| Tên chủ đề | Văn bản | Bắt buộc |
| Từ khóa | Danh sách | Khớp không phân biệt hoa/thường và dấu; hỗ trợ cụm từ |
| Từ khóa loại trừ | Danh sách | Có thì bỏ qua mục tin |
| Lĩnh vực | Chọn nhiều | Thuế, Lao động, BHXH, Doanh nghiệp, Đầu tư, Đất đai, Kế toán, Hải quan, … |
| Văn bản theo dõi đích danh | Danh sách số hiệu | Ví dụ `59/2020/QH14`; mọi thay đổi về văn bản này là mức Cảnh báo |
| Mô tả ngữ cảnh cho AI | Văn bản dài | Ví dụ "công ty TNHH 20 nhân viên, ngành thiết kế vi mạch" |
| Nguồn áp dụng | Tất cả / chọn riêng | |
| Loại tin | Báo chí, Văn bản chính thức, Dự thảo | |
| Nhắc trước ngày hiệu lực | Số ngày | Mặc định 7 |
| Bật/tắt | Công tắc | |

Nút "Chạy thử": quét ngay với chủ đề này trên dữ liệu 30 ngày gần nhất đã có, hiện số
mục tin sẽ khớp, để người dùng chỉnh từ khóa trước khi lưu.

#### 4.3.4 Nguồn

- Bảng: tên, tên miền, tầng, loại bộ nối, tần suất, lần quét gần nhất, kết quả, bật/tắt.
- Nguồn dựng sẵn không xóa được, chỉ bật/tắt và đổi tần suất.
- Thêm nguồn: nhập URL RSS hoặc URL trang danh mục → app kiểm tra tên miền, đọc
  `robots.txt`, quét thử, hiện 5 mục tin đầu. Nguồn tự thêm nằm ở tầng 2 hoặc 3, không
  thể đặt tầng 1.
- Danh sách chặn hiển thị dạng chỉ đọc (mạng xã hội, diễn đàn, blog).

#### 4.3.5 Văn bản

- Thư viện văn bản chính thức đã tải: số hiệu, tên, loại, cơ quan ban hành, ngày ban
  hành, ngày hiệu lực, tình trạng (kèm nhãn "suy luận" khi không có dữ liệu chính thức).
- Trang chi tiết: metadata, file gốc (mở bằng trình xem PDF của hệ thống), bản chữ,
  cây quan hệ (sửa đổi / hướng dẫn / thay thế / hợp nhất), các cảnh báo liên quan.
- Thao tác: "Tải theo số hiệu", "Theo dõi văn bản này", "Mở thư mục chứa file".

#### 4.3.6 Nhật ký

Bảng các lần quét: thời điểm, số nguồn thành công/thất bại, số mục tin mới, số cảnh
báo, số lần gọi AI, thời lượng. Mở từng dòng xem lỗi theo nguồn. Có nút xuất log.

#### 4.3.7 Cài đặt

| Nhóm | Mục |
|---|---|
| Lịch | Tần suất mặc định cho từng tầng nguồn; khung giờ hoạt động; giờ yên lặng; quét bù khi máy vừa thức dậy |
| AI | Provider đang dùng (tự dò hoặc chọn tay); đường dẫn CLI; model; khóa API; hạn mức lần gọi mỗi lần quét; nút "Kiểm tra kết nối" |
| Thông báo | Bật/tắt theo mức; gộp thông báo; âm thanh |
| Hệ thống | Khởi động cùng Windows; thư mục dữ liệu; thời gian lưu giữ; sao lưu/khôi phục; proxy |
| Giới thiệu | Phiên bản, giấy phép, tuyên bố miễn trừ |

### 4.4 Kỹ thuật front-end

```
web/
├── index.html
├── css/app.css
└── js/
    ├── app.js            khởi tạo, định tuyến theo hash (#/alerts, #/chat, …), kết nối SSE
    ├── lib.js            dựng DOM an toàn, gọi API, định dạng ngày giờ, nhãn tiếng Việt
    └── views/            alerts.js, chat.js, topics.js, sources.js, docs.js, logs.js, settings.js
```

- Toàn bộ thư mục `web/` được nhúng vào binary lúc build; không tải tài nguyên từ CDN.
- Trạng thái nằm ở back-end; front-end chỉ giữ trạng thái giao diện (bộ lọc, tab).
- Cập nhật trực tiếp qua SSE: `scan.started`, `scan.progress`, `scan.finished`,
  `alert.created`, `provider.changed`.
- Chat streaming qua SSE của chính yêu cầu gửi tin nhắn.
- Nội dung từ nguồn ngoài (tiêu đề, tóm tắt) luôn chèn dạng văn bản, không chèn HTML.

---

## 5. Back-end

### 5.1 Cấu trúc mã nguồn

```
cmd/q3vnlaw/main.go         khởi động, một phiên bản duy nhất, vòng đời
internal/
├── config/                 đọc/ghi cấu hình, giá trị mặc định
├── store/                  SQLite, migration, truy vấn
├── tray/                   biểu tượng, menu, balloon
├── server/                 HTTP, REST, SSE, phục vụ web nhúng, xác thực token
├── scheduler/              lịch quét, quét bù, tạm dừng
├── sources/                registry nguồn + connectors (rss, htmllist, vanban)
├── fetch/                  HTTP client: whitelist, giới hạn tốc độ, ETag, robots
├── extract/                HTML → chữ, PDF → chữ, tách Điều/Khoản
├── pipeline/               các bước xử lý một lần quét
├── ai/                     interface Provider + claudecli, codexcli, ollama, httpapi, none
├── verify/                 đối chiếu nguồn chính thức
├── chat/                   truy hồi + sinh câu trả lời
└── notify/                 quy tắc thông báo, gộp, giờ yên lặng
web/                        front-end (nhúng)
docs/                       tài liệu
```

### 5.2 Vòng đời tiến trình

1. Kiểm tra một phiên bản duy nhất bằng named mutex; nếu đã chạy thì yêu cầu phiên bản
   đang chạy mở cửa sổ rồi thoát.
2. Xác định thư mục dữ liệu: `data\` cạnh file `.exe`; nếu không ghi được thì dùng
   `%LOCALAPPDATA%\Q3VNLaw` và báo cho người dùng.
3. Mở SQLite, chạy migration.
4. Khởi động HTTP server ở `127.0.0.1`, cổng ngẫu nhiên, sinh token phiên.
5. Dò AI provider.
6. Tạo biểu tượng khay, khởi động scheduler.
7. Khi thoát: dừng nhận việc mới, chờ lần quét đang chạy tối đa 10 giây, đóng DB.

### 5.3 Scheduler

- Mỗi nguồn có `interval_minutes`. Mặc định: báo chí 60 phút, cổng chính thức 180 phút,
  kiểm tra văn bản theo dõi đích danh 1 lần/ngày.
- Chỉ chạy trong khung giờ hoạt động (mặc định 07:00–21:00).
- Mỗi lần chạy cộng độ lệch ngẫu nhiên ±10% để không dồn yêu cầu vào đúng đầu giờ.
- Máy ngủ rồi thức dậy: các lượt bị lỡ gộp thành **một** lượt quét bù.
- Không có mạng: bỏ lượt, không tính là lỗi nguồn.
- "Quét ngay" từ khay hoặc giao diện chạy toàn bộ nguồn đang bật, bỏ qua lịch.
- Không chạy hai lần quét chồng nhau.

### 5.4 Nguồn và tầng tin cậy

| Tầng | Loại | Vai trò |
|---|---|---|
| 1 | Nguồn chính thức của Nhà nước | Căn cứ để gắn nhãn "đã xác nhận" |
| 2 | Cơ sở dữ liệu pháp luật có uy tín cho phép truy cập tự động | Phát hiện sớm, đối chiếu; không tự là căn cứ |
| 3 | Báo chí chính thống có giấy phép | Cảnh báo loại "tin báo chí" |
| Chặn | Diễn đàn, mạng xã hội, blog, trang hỏi đáp | Không bao giờ truy cập |

Danh sách dựng sẵn dự kiến. **Từng địa chỉ, đường dẫn RSS và `robots.txt` phải được kiểm
tra lại ở mốc M1** trước khi đưa vào bản phát hành:

| Tầng | Nguồn | Bộ nối |
|---|---|---|
| 1 | vanban.chinhphu.vn | `vanban` (chuyển logic từ skill `han-download-law-doc`) |
| 1 | congbao.chinhphu.vn | `htmllist` |
| 1 | vbpl.vn | `htmllist` |
| 1 | chinhphu.vn, xaydungchinhsach.chinhphu.vn | `rss` hoặc `htmllist` |
| 1 | Cổng của bộ/ngành theo lĩnh vực người dùng chọn (`*.gov.vn`) | `rss` hoặc `htmllist` |
| 3 | baochinhphu.vn, vietnamplus.vn, nhandan.vn, vov.vn, vtv.vn | `rss` |
| 3 | baophapluat.vn, thoibaotaichinhvietnam.vn | `rss` |
| 3 | tuoitre.vn, thanhnien.vn, vnexpress.net (chuyên mục pháp luật/chính sách) | `rss` |

Ghi chú về tầng 2: thuvienphapluat.vn đặt lớp chống bot và không cho truy cập tự động
(đã ghi nhận trong skill `han-download-law-doc`), nên **không** nằm trong danh sách quét.
App không vượt lớp bảo vệ của bất kỳ trang nào. Tầng 2 chỉ gồm nguồn cho phép truy cập
tự động; có thể để trống ở v1.

#### Giao diện bộ nối

```go
type Connector interface {
    // List trả về các mục tin mới nhất của nguồn (chưa tải nội dung đầy đủ).
    List(ctx context.Context, src Source, since time.Time) ([]ItemRef, error)
    // Fetch tải nội dung đầy đủ của một mục tin.
    Fetch(ctx context.Context, src Source, ref ItemRef) (Item, error)
}
```

| Bộ nối | Dùng cho | Cấu hình |
|---|---|---|
| `rss` | Báo có RSS/Atom | URL feed |
| `htmllist` | Trang danh mục không có RSS | URL trang, bộ chọn CSS cho dòng/tiêu đề/link/ngày |
| `vanban` | vanban.chinhphu.vn | Lớp văn bản, loại văn bản; hỗ trợ tìm theo số hiệu, lấy metadata và file đính kèm |

### 5.5 Fetcher

- **Whitelist tên miền cứng**: yêu cầu tới tên miền ngoài danh sách bị từ chối trong
  code, kể cả khi đến từ chuyển hướng. Mỗi lần chuyển hướng kiểm tra lại.
- Chỉ HTTPS. User-Agent tự nhận diện rõ là Q3VNLaw kèm phiên bản.
- Tôn trọng `robots.txt` (lưu đệm 24 giờ).
- Giới hạn tốc độ: tối đa 1 yêu cầu/giây cho mỗi tên miền, không song song trên cùng
  tên miền.
- Dùng `ETag` / `If-Modified-Since`; `304` thì bỏ qua.
- Thử lại tối đa 2 lần với khoảng chờ tăng dần cho lỗi 5xx và timeout.
- Giới hạn kích thước: trang HTML 5 MB, file PDF 50 MB.
- Hỗ trợ proxy hệ thống.

### 5.6 Đường ống xử lý một lần quét

```
1 List      mỗi nguồn → danh sách ItemRef
2 Dedupe    bỏ mục đã có (theo URL chuẩn hóa)
3 Fetch     tải nội dung, trích chữ, tính content_hash
4 Cluster   gộp bài cùng sự kiện (cùng số hiệu được nhắc, hoặc tiêu đề gần giống)
5 Prefilter khớp từ khóa/lĩnh vực/số hiệu với từng chủ đề   ← không dùng AI
6 Classify  AI: liên quan không, trạng thái pháp lý, tóm tắt  ← chỉ mục qua bước 5
7 Verify    tra số hiệu được nhắc trên nguồn tầng 1
8 Alert     tạo cảnh báo, tính mức
9 Notify    áp quy tắc gộp, giờ yên lặng, gửi thông báo
```

Chi tiết từng bước:

- **Bước 2**: URL chuẩn hóa bằng cách bỏ tham số theo dõi (`utm_*`, `fbclid`…), bỏ
  fragment, hạ chữ thường phần host.
- **Bước 3**: HTML → chữ bằng cách lấy khối nội dung chính, bỏ menu/quảng cáo. Nội dung
  đổi `content_hash` so với lần trước được coi là "cập nhật" và xử lý lại.
- **Bước 4**: trích số hiệu văn bản bằng biểu thức chính quy (mẫu
  `số/năm/ký hiệu`, ví dụ `13/2023/NĐ-CP`, `59/2020/QH14`, `80/2021/TT-BTC`), chuẩn hóa
  `Đ`/`D` và khoảng trắng.
- **Bước 5**: so khớp trên chữ đã gập dấu và hạ chữ thường. Mục tin không khớp chủ đề
  nào vẫn lưu metadata (30 ngày) nhưng không gọi AI.
- **Bước 6**: mỗi cặp (mục tin, chủ đề) gọi AI tối đa một lần; kết quả lưu đệm theo
  `content_hash + topic_version`. Có hạn mức số lần gọi mỗi lần quét (mặc định 30); vượt
  hạn mức thì phần còn lại xử lý ở lần quét sau.
- **Không có AI**: bỏ bước 6. Cảnh báo vẫn được tạo từ bước 5, tóm tắt là đoạn mở đầu
  bài, trạng thái pháp lý ghi "chưa phân loại".
- **Bước 7**: tìm thấy văn bản trên nguồn tầng 1 → lưu vào thư viện, gắn nhãn "đã xác
  nhận". Không thấy → nhãn "chưa xác nhận", đưa vào hàng chờ kiểm tra lại mỗi ngày trong
  30 ngày; khi xuất hiện thì sinh cảnh báo cập nhật.

### 5.7 Phân loại và mức cảnh báo

Trạng thái pháp lý (AI gán, bắt buộc một trong các giá trị):

| Giá trị | Nghĩa |
|---|---|
| `proposal` | Đề xuất, kiến nghị, đang nghiên cứu |
| `draft` | Dự thảo đang lấy ý kiến hoặc trình |
| `issued` | Đã ban hành, chưa tới ngày hiệu lực |
| `effective` | Đã có hiệu lực |
| `other` | Tin giải thích, hướng dẫn, xử lý vi phạm… |
| `unknown` | Không xác định được, hoặc không có AI |

Quy tắc tính mức:

| Điều kiện | Mức |
|---|---|
| Liên quan tới văn bản theo dõi đích danh (sửa đổi, thay thế, bãi bỏ, hết hiệu lực) | Cảnh báo |
| Văn bản từ nguồn tầng 1 khớp chủ đề, trạng thái `issued` hoặc `effective` | Cần chú ý |
| Văn bản đã lưu còn N ngày tới ngày hiệu lực | Cần chú ý |
| Tin báo chí khớp chủ đề, AI đánh giá mức liên quan cao và trạng thái `issued`/`effective` | Cần chú ý, kèm nhãn "chưa xác nhận" cho tới khi qua bước 7 |
| Tin báo chí khác khớp chủ đề; `proposal`, `draft` | Thông tin |

### 5.8 Bộ nối AI

```go
type Provider interface {
    Name() string
    Available(ctx context.Context) error            // kiểm tra sẵn sàng
    Complete(ctx context.Context, r Request) (Response, error)
    Stream(ctx context.Context, r Request, onDelta func(string)) (Response, error)
}

type Request struct {
    System     string
    Prompt     string
    JSONSchema []byte        // nếu có: yêu cầu trả JSON theo schema
    MaxTokens  int
    Timeout    time.Duration
}
```

| Provider | Cách gọi | Ghi chú |
|---|---|---|
| `claude-cli` | Tiến trình con, chế độ không tương tác, prompt qua stdin, đầu ra JSON | Dùng đăng nhập và hạn mức sẵn có của người dùng. Tắt mọi công cụ (không web, không file, không shell), giới hạn một lượt |
| `codex-cli` | Tiến trình con, chế độ `exec`, sandbox chỉ đọc | Tương tự |
| `ollama` | HTTP `localhost:11434` | Chạy hoàn toàn offline |
| `http-openai` | HTTP kiểu `/v1/chat/completions`, URL gốc tùy chỉnh | Bao phủ phần lớn dịch vụ và máy chủ model cục bộ tương thích |
| `http-anthropic` | HTTP Messages API | Dùng khóa API riêng |
| `none` | Không gọi gì | Chế độ chỉ từ khóa |

Tên cờ dòng lệnh cụ thể của từng CLI phải được xác minh với phiên bản cài trên máy khi
hiện thực (mốc M2), không ghi cứng theo trí nhớ.

**Tự dò** theo thứ tự `claude-cli` → `codex-cli` → `ollama` → `none`. Việc dò tìm trong
PATH và cả các vị trí cài đặt quen thuộc (`%USERPROFILE%\.local\bin`, `%APPDATA%\npm`,
…), vì CLI có thể đã cài nhưng không nằm trong PATH. Ghi nhận tại ngày lập tài liệu: trên
máy phát triển, lệnh `claude`, `codex`, `ollama` đều **không** tìm thấy trong PATH của
PowerShell. Người dùng luôn có thể chỉ đường dẫn bằng tay trong Cài đặt.

**Nguyên tắc an toàn khi gọi AI**

- AI không có công cụ nào: không duyệt web, không đọc/ghi file, không chạy lệnh. Nó chỉ
  nhận văn bản app đưa vào và trả văn bản.
- Nội dung lấy từ web được đặt trong khối dữ liệu có ranh giới rõ; system prompt nêu rõ
  nội dung trong khối là dữ liệu, không phải chỉ dẫn.
- Đầu ra phân loại phải là JSON hợp lệ theo schema; sai schema thì thử lại một lần, vẫn
  sai thì coi như không có AI cho mục tin đó.
- Mọi trích dẫn AI đưa ra được app kiểm lại: đoạn trích phải xuất hiện nguyên văn trong
  văn bản nguồn, nếu không thì loại bỏ trích dẫn đó.

**Schema đầu ra bước phân loại**

```json
{
  "relevant": true,
  "relevance": "high | medium | low",
  "legal_status": "proposal | draft | issued | effective | other | unknown",
  "summary": "2-3 câu tiếng Việt",
  "who_is_affected": "đối tượng chịu tác động",
  "effective_date": "YYYY-MM-DD hoặc null",
  "doc_numbers": ["13/2023/NĐ-CP"],
  "evidence_quote": "một câu nguyên văn trong bài làm căn cứ",
  "reason": "vì sao liên quan hoặc không liên quan tới chủ đề"
}
```

### 5.9 Chat

1. Nhận câu hỏi và phạm vi tìm.
2. Truy hồi: tìm FTS5 trên `chunks` (văn bản đã tách theo Điều/Khoản và các bài báo đã
   lưu), ưu tiên nguồn tầng 1, lấy tối đa 8 đoạn. Nếu câu hỏi chứa số hiệu thì lấy thẳng
   văn bản đó.
3. Ghép prompt: câu hỏi + các đoạn có đánh số + quy tắc "chỉ trả lời từ các đoạn được
   cung cấp, ghi chỉ số trích dẫn, không đủ căn cứ thì nói không đủ".
4. Gọi `Provider.Stream`, đẩy dần về front-end.
5. Hậu kiểm trích dẫn, lưu tin nhắn và danh sách căn cứ.

Không có AI: Chat chuyển thành ô tìm kiếm, trả về các đoạn khớp kèm nguồn.

**Vì sao v1 không dùng vector**: FTS5 với bộ tách từ gập dấu đủ tốt cho truy vấn pháp
luật, vốn giàu thuật ngữ và số hiệu cố định; vector kéo theo model embedding (thêm hàng
trăm MB hoặc thêm một dịch vụ ngoài), trái yêu cầu gọn nhẹ. Thiết kế `chunks` để sau này
thêm cột embedding mà không đổi phần còn lại.

### 5.10 Trích chữ và tách cấu trúc

- PDF có lớp chữ: trích trực tiếp.
- PDF scan: v1 chỉ lưu file và metadata, đánh dấu "chưa có bản chữ"; người dùng có thể
  chạy OCR bằng skill `han-scan-to-word` (cần Tesseract) rồi nhập lại. Tích hợp OCR tự
  động để ở mốc sau, vì Tesseract không gói gọn được vào một file `.exe`.
- Tách văn bản theo `Chương` → `Điều` → `Khoản` → `Điểm`; mỗi Khoản là một chunk, mang
  theo đường dẫn cấu trúc để trích dẫn chính xác.

### 5.11 Tình trạng hiệu lực

Cổng vanban.chinhphu.vn ghi ngày ban hành và ngày có hiệu lực nhưng **không** ghi văn
bản còn hay hết hiệu lực. Do đó:

- Trường `validity` có giá trị `in_force`, `not_yet`, `superseded`, `amended`, `unknown`.
- Trường `validity_basis` ghi căn cứ: `official` (lấy được từ nguồn có ghi tình trạng),
  `inferred` (suy luận từ văn bản sửa đổi/thay thế tìm được), `none`.
- Giao diện luôn hiện nhãn "suy luận" khi `validity_basis` khác `official`.

---

## 6. Mô hình dữ liệu (SQLite)

```sql
CREATE TABLE sources (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  domain TEXT NOT NULL,
  tier INTEGER NOT NULL,              -- 1, 2, 3
  connector TEXT NOT NULL,            -- rss | htmllist | vanban
  config TEXT NOT NULL,               -- JSON theo bộ nối
  interval_minutes INTEGER NOT NULL,
  builtin INTEGER NOT NULL DEFAULT 0,
  enabled INTEGER NOT NULL DEFAULT 1,
  etag TEXT, last_modified TEXT,
  last_scan_at TEXT, last_status TEXT, fail_count INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE topics (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  keywords TEXT NOT NULL,             -- JSON array
  exclude_keywords TEXT NOT NULL,     -- JSON array
  fields TEXT NOT NULL,               -- JSON array lĩnh vực
  watched_docs TEXT NOT NULL,         -- JSON array số hiệu
  ai_context TEXT,
  source_ids TEXT,                    -- NULL = tất cả
  kinds TEXT NOT NULL,                -- JSON: press, official, draft
  remind_days INTEGER NOT NULL DEFAULT 7,
  version INTEGER NOT NULL DEFAULT 1, -- tăng khi sửa, làm mất hiệu lực đệm AI
  enabled INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE items (
  id INTEGER PRIMARY KEY,
  source_id INTEGER NOT NULL REFERENCES sources(id),
  url TEXT NOT NULL UNIQUE,           -- đã chuẩn hóa
  title TEXT NOT NULL,
  published_at TEXT, fetched_at TEXT NOT NULL,
  content TEXT, content_hash TEXT,
  doc_numbers TEXT,                   -- JSON array số hiệu trích được
  cluster_id INTEGER                  -- gộp bài cùng sự kiện
);

CREATE TABLE documents (
  id INTEGER PRIMARY KEY,
  doc_number TEXT NOT NULL UNIQUE,    -- số hiệu đã chuẩn hóa
  title TEXT, doc_type TEXT, issuer TEXT, signer TEXT,
  issued_at TEXT, effective_at TEXT,
  validity TEXT NOT NULL DEFAULT 'unknown',
  validity_basis TEXT NOT NULL DEFAULT 'none',
  source_id INTEGER REFERENCES sources(id),
  source_url TEXT, files TEXT,        -- JSON: đường dẫn tương đối trong data\docs
  has_text INTEGER NOT NULL DEFAULT 0,
  fetched_at TEXT
);

CREATE TABLE doc_relations (
  from_doc INTEGER NOT NULL REFERENCES documents(id),
  to_doc INTEGER NOT NULL REFERENCES documents(id),
  relation TEXT NOT NULL,             -- amends | guides | replaces | consolidates | penalizes | other
  basis TEXT NOT NULL,                -- stated | read | inferred
  PRIMARY KEY (from_doc, to_doc, relation)
);

CREATE TABLE chunks (
  id INTEGER PRIMARY KEY,
  owner_kind TEXT NOT NULL,           -- document | item
  owner_id INTEGER NOT NULL,
  path TEXT,                          -- "Chương II > Điều 12 > Khoản 3"
  text TEXT NOT NULL
);
CREATE VIRTUAL TABLE chunks_fts USING fts5(
  text, content='chunks', content_rowid='id',
  tokenize = "unicode61 remove_diacritics 2"
);

CREATE TABLE ai_results (
  content_hash TEXT NOT NULL,
  topic_id INTEGER NOT NULL,
  topic_version INTEGER NOT NULL,
  provider TEXT NOT NULL,
  result TEXT NOT NULL,               -- JSON theo schema mục 5.8
  created_at TEXT NOT NULL,
  PRIMARY KEY (content_hash, topic_id, topic_version)
);

CREATE TABLE alerts (
  id INTEGER PRIMARY KEY,
  topic_id INTEGER NOT NULL REFERENCES topics(id),
  cluster_id INTEGER, item_id INTEGER REFERENCES items(id),
  document_id INTEGER REFERENCES documents(id),
  kind TEXT NOT NULL,                 -- press | official | effective_soon | doc_changed | system
  severity TEXT NOT NULL,             -- info | notice | warning
  legal_status TEXT NOT NULL,
  verified TEXT NOT NULL,             -- confirmed | unconfirmed | n/a
  title TEXT NOT NULL, summary TEXT, affected TEXT, effective_at TEXT,
  state TEXT NOT NULL DEFAULT 'unread', -- unread | read | pinned | dismissed | filtered
  feedback TEXT,                      -- irrelevant | NULL
  created_at TEXT NOT NULL, notified_at TEXT
);

CREATE TABLE scans (
  id INTEGER PRIMARY KEY,
  started_at TEXT NOT NULL, finished_at TEXT,
  trigger TEXT NOT NULL,              -- schedule | manual | catchup
  sources_ok INTEGER, sources_failed INTEGER,
  items_new INTEGER, alerts_new INTEGER, ai_calls INTEGER,
  errors TEXT                         -- JSON: lỗi theo nguồn
);

CREATE TABLE chat_sessions (id INTEGER PRIMARY KEY, title TEXT, scope TEXT, created_at TEXT NOT NULL);
CREATE TABLE chat_messages (
  id INTEGER PRIMARY KEY,
  session_id INTEGER NOT NULL REFERENCES chat_sessions(id),
  role TEXT NOT NULL,                 -- user | assistant
  content TEXT NOT NULL,
  citations TEXT,                     -- JSON: chunk_id, đoạn trích, nguồn
  created_at TEXT NOT NULL
);

CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
```

Lưu giữ: mục tin không sinh cảnh báo xóa sau 30 ngày; cảnh báo và văn bản giữ đến khi
người dùng xóa; log giữ 14 ngày.

---

## 7. API cục bộ

Tất cả dưới `http://127.0.0.1:<cổng>/api`, yêu cầu header `X-Q3-Token`. Trả JSON.

| Phương thức | Đường dẫn | Chức năng |
|---|---|---|
| GET | `/status` | Phiên bản, provider, lần quét gần nhất, số chưa đọc |
| GET | `/events` | SSE: sự kiện trực tiếp |
| GET | `/alerts` | Danh sách, có lọc và phân trang |
| GET | `/alerts/{id}` | Chi tiết kèm các nguồn cùng cụm |
| PATCH | `/alerts/{id}` | Đổi `state`, ghi `feedback` |
| POST | `/alerts/mark-read` | Đánh dấu đã đọc hàng loạt |
| GET, POST | `/topics` | Liệt kê, tạo |
| GET, PUT, DELETE | `/topics/{id}` | Xem, sửa, xóa |
| POST | `/topics/preview` | Chạy thử một chủ đề chưa lưu |
| GET, POST | `/sources` | Liệt kê, thêm nguồn tùy chỉnh |
| PATCH, DELETE | `/sources/{id}` | Bật/tắt, đổi tần suất; xóa nguồn tùy chỉnh |
| POST | `/sources/test` | Kiểm tra một URL nguồn trước khi thêm |
| GET | `/documents` | Thư viện văn bản |
| GET | `/documents/{id}` | Chi tiết, quan hệ, cảnh báo liên quan |
| POST | `/documents/fetch` | Tải văn bản theo số hiệu từ nguồn tầng 1 |
| GET | `/documents/{id}/file/{n}` | Mở file gốc |
| POST | `/scan` | Quét ngay |
| GET | `/scans` | Nhật ký quét |
| GET, POST | `/chat/sessions` | Liệt kê, tạo phiên |
| GET, DELETE | `/chat/sessions/{id}` | Lịch sử, xóa phiên |
| POST | `/chat/sessions/{id}/messages` | Gửi câu hỏi; phản hồi dạng SSE |
| GET, PUT | `/settings` | Đọc, ghi cấu hình |
| GET | `/providers` | Các provider dò được và trạng thái |
| POST | `/providers/test` | Gọi thử provider |

Định dạng lỗi thống nhất: `{"error": {"code": "source_blocked", "message": "…"}}`.

---

## 8. Bảo mật và quyền riêng tư

- Máy chủ chỉ lắng nghe `127.0.0.1`, cổng ngẫu nhiên mỗi lần chạy.
- Token phiên ngẫu nhiên, truyền cho cửa sổ qua URL khởi động rồi chuyển sang cookie
  `HttpOnly`; mọi yêu cầu API đều kiểm token.
- Kiểm tra header `Host` và `Origin` để chặn DNS rebinding và yêu cầu chéo trang.
- Content-Security-Policy: chỉ cho tài nguyên từ chính nó, không script nội tuyến.
- Khóa API mã hóa bằng DPAPI theo tài khoản Windows. Hệ quả: chép thư mục sang máy khác
  thì phải nhập lại khóa; các dữ liệu khác vẫn dùng được.
- Không thu thập dữ liệu sử dụng, không gọi về máy chủ nào của dự án.
- Dữ liệu gửi cho AI chỉ gồm nội dung công khai lấy từ nguồn và mô tả chủ đề của người
  dùng. Giao diện Cài đặt nêu rõ điều này; với provider đám mây, mô tả ngữ cảnh công ty
  của người dùng sẽ rời khỏi máy.
- Không bao giờ vượt lớp chống bot, CAPTCHA hay tường phí của bất kỳ trang nào.
- Bản quyền báo chí: chỉ hiển thị tiêu đề, tóm tắt ngắn và link về bài gốc; nội dung đầy
  đủ chỉ dùng nội bộ để lọc và tìm kiếm.

---

## 9. Đóng gói và triển khai

```
Q3VNLaw\
├── q3vnlaw.exe
└── data\
    ├── q3vnlaw.db
    ├── config.json            cấu hình tối thiểu cần trước khi mở DB
    ├── docs\<số_hiệu>\         file gốc + metadata.json
    └── logs\q3vnlaw-YYYYMMDD.log
```

- Portable thật sự: xóa thư mục là gỡ xong. Ngoại lệ duy nhất là mục "Khởi động cùng
  Windows", ghi một giá trị vào `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` khi
  người dùng bật, và xóa khi tắt.
- Đổi chỗ thư mục: lần chạy sau tự cập nhật đường dẫn khởi động nếu mục đó đang bật.
- Cập nhật phiên bản: thay file `.exe`; migration DB chạy tự động và sao lưu DB trước
  khi nâng cấp.
- Chưa ký số ở v1; SmartScreen có thể cảnh báo ở lần chạy đầu. Ký số là việc của bản
  phát hành công khai.

---

## 10. Yêu cầu phi chức năng

| Chỉ tiêu | Mục tiêu |
|---|---|
| Dung lượng file `.exe` | ≤ 25 MB |
| RAM khi nghỉ (không mở cửa sổ) | ≤ 40 MB |
| CPU khi nghỉ | Xấp xỉ 0% |
| Thời gian khởi động tới khi có biểu tượng khay | ≤ 1 giây |
| Một lần quét 15 nguồn, không gọi AI | ≤ 2 phút |
| Mở cửa sổ chính | ≤ 1 giây |
| Hệ điều hành | Windows 10 21H2 trở lên, Windows 11 |

Các mục tiêu này là đích thiết kế, sẽ được đo ở mốc M5.

---

## 11. Xử lý lỗi

| Tình huống | Hành vi |
|---|---|
| Một nguồn lỗi | Ghi log, tăng `fail_count`, các nguồn khác tiếp tục. Lỗi 3 lần liên tiếp → thông báo hệ thống |
| Cấu trúc trang nguồn thay đổi (bộ nối trả 0 mục nhiều lần liên tiếp ở nguồn vốn có tin) | Đánh dấu nguồn "nghi hỏng", hiện rõ ở màn hình Nguồn và khay. Không được im lặng |
| Mất mạng | Bỏ lượt, không tính lỗi |
| AI lỗi hoặc hết hạn mức | Lần quét đó chạy chế độ từ khóa; cảnh báo ghi "chưa phân loại"; thử lại ở lần sau |
| AI trả sai schema | Thử lại 1 lần, rồi bỏ qua AI cho mục tin đó |
| DB hỏng | Khôi phục từ bản sao lưu gần nhất, báo người dùng |
| Thư mục dữ liệu không ghi được | Chuyển sang `%LOCALAPPDATA%\Q3VNLaw`, báo người dùng |
| Edge không có | Mở bằng trình duyệt mặc định |

Nguyên tắc: **không có tin mới** và **không quét được** phải là hai trạng thái hiển thị
khác nhau ở mọi nơi.

---

## 12. Kiểm thử

- Kiểm thử đơn vị: chuẩn hóa URL và số hiệu, so khớp từ khóa có dấu/không dấu, tách
  Điều/Khoản, quy tắc tính mức, quy tắc gộp thông báo, whitelist tên miền (kể cả chuyển
  hướng).
- Kiểm thử bộ nối bằng bản chụp HTML/RSS lưu sẵn, không gọi mạng.
- Provider giả để kiểm thử đường ống: trả JSON đúng, sai schema, timeout.
- Bộ dữ liệu vàng: khoảng 50 bài báo và văn bản đã gán nhãn tay (liên quan/không, trạng
  thái pháp lý) để đo chất lượng phân loại mỗi khi đổi prompt hoặc model.
- Kiểm thử tay trên Windows 11: khay, thông báo, ngủ/thức, khởi động cùng Windows, chạy
  từ USB, chạy từ thư mục chỉ đọc.

---

## 13. Lộ trình

| Mốc | Nội dung | Tiêu chí hoàn thành |
|---|---|---|
| M0 | Khung: tiến trình, khay, HTTP server, web nhúng, SQLite, cài đặt | Chạy `.exe`, có biểu tượng khay, mở được cửa sổ rỗng có điều hướng |
| M1 | Nguồn + fetcher + bộ nối `rss`; chủ đề; lọc từ khóa; cảnh báo; thông báo; lịch | Tạo chủ đề, tới lịch tự quét báo chí và hiện thông báo, không cần AI. Danh sách nguồn dựng sẵn đã được kiểm tra thực tế |
| M2 | Bộ nối AI (`claude-cli`, `codex-cli`, `ollama`, HTTP) + bước phân loại | Cảnh báo có tóm tắt, trạng thái pháp lý; đo trên bộ dữ liệu vàng |
| M3 | Bộ nối `vanban`, `htmllist`; xác minh; thư viện văn bản; theo dõi đích danh; nhắc ngày hiệu lực | Tin báo chí nhắc số hiệu được nâng lên "đã xác nhận" kèm file gốc |
| M4 | Chat có trích dẫn | Hỏi về văn bản trong thư viện, câu trả lời có căn cứ kiểm chứng được |
| M5 | Hoàn thiện: nhật ký, sao lưu, đo chỉ tiêu mục 10, tài liệu người dùng | Đạt các chỉ tiêu phi chức năng; README hướng dẫn dùng |

---

## 14. Vấn đề còn mở

1. **Người dùng mục tiêu**: chỉ dùng nội bộ hay phát hành rộng? Ảnh hưởng tới ký số,
   tài liệu, và mức đầu tư vào bộ nối nguồn.
2. **Lĩnh vực ưu tiên**: nhóm lĩnh vực nào cần cổng bộ/ngành dựng sẵn ngay từ M1
   (thuế, lao động, BHXH…)?
3. **AI mặc định**: máy hiện chưa có `claude`, `codex`, `ollama` trong PATH. Cần chốt sẽ
   cài CLI nào, hay dùng khóa API.
4. **OCR tự động** cho PDF scan: chấp nhận yêu cầu cài Tesseract riêng, hay để ngoài v1?
5. **Tầng 2**: có nguồn cơ sở dữ liệu pháp luật nào cho phép truy cập tự động đáng đưa
   vào không, hay bỏ hẳn tầng này ở v1?

---

## 15. Hiện trạng hiện thực (phiên bản 0.1.0, ngày 2026-10-04)

### 15.1 Đã làm

Toàn bộ các mốc M0–M5 của mục 13 đã có mã chạy được: khay hệ thống và thông báo, máy
chủ cục bộ và giao diện 7 màn hình, 15 nguồn dựng sẵn, chủ đề, lọc từ khóa, bộ nối AI,
xác minh với Cổng Chính phủ, thư viện văn bản, theo dõi văn bản đích danh, nhắc ngày
hiệu lực, chat có trích dẫn, nhật ký, sao lưu.

### 15.2 Số đo thực tế

Đo trên máy phát triển (Windows 11, màn hình 1920×1080), bản build `-s -w -H windowsgui`.

| Chỉ tiêu | Mục tiêu (mục 10) | Đo được |
|---|---|---|
| Dung lượng `q3vnlaw.exe` | ≤ 25 MB | 17,9 MB |
| RAM khi nghỉ, sau lượt quét đầu | ≤ 40 MB | 35,9 MB (working set); 20 MB ngay sau khi khởi động |
| CPU khi nghỉ | Xấp xỉ 0% | 0 ms CPU trong 30 giây |
| Khởi động tới khi máy chủ và khay sẵn sàng | ≤ 1 giây | 0,2 giây |
| Một lượt quét 15 nguồn, không gọi AI | ≤ 2 phút | 8 giây, 1002 tin mới (lượt đầu) |

Chưa đo: thời gian mở cửa sổ chính (phụ thuộc Edge đã chạy sẵn hay chưa).

### 15.3 Khác với thiết kế ban đầu

| Mục | Thiết kế | Hiện thực | Lý do |
|---|---|---|---|
| 3.2, 4.4 | Alpine.js | JavaScript thuần | Alpine cần `unsafe-eval` trong CSP; bỏ đi thì giữ được CSP chặt và không phải tải thư viện |
| 5.4 | Có nguồn `congbao.chinhphu.vn`, `vbpl.vn` | Chưa có | RSS của Công báo rỗng và `vbpl.vn` không có RSS khi kiểm tra; cần viết bộ nối riêng. Nguồn chính thức hiện là vanban.chinhphu.vn và hai chuyên trang RSS của Chính phủ |
| 5.4 | Tầng 2 | Để trống | Không có cơ sở dữ liệu pháp luật nào cho phép truy cập tự động được xác nhận |
| 5.4 | Bộ nối `htmllist` dùng bộ chọn CSS | Lấy mọi liên kết có tiêu đề đủ dài trên cùng tên miền, có thể lọc bằng biểu thức chính quy | Không cần thêm thư viện; đủ cho trang chuyên mục |
| 5.6 | Chỉ xét tin mới | Thêm bước "áp dụng chủ đề": khi tạo hoặc sửa chủ đề, chủ đề được chạy lại trên các tin đã thu thập trong cửa sổ nhìn lại | Nếu không, chủ đề tạo sau lượt quét đầu sẽ không thấy gì |
| 5.6 bước 4 | Gộp tin theo số hiệu hoặc tiêu đề gần giống | Gộp theo số hiệu, hoặc tiêu đề trùng khít | Đo độ giống tiêu đề để ngoài phiên bản này |
| 5.6 bước 5 | Khớp trên toàn bài | Khớp trên tiêu đề và tóm tắt RSS; toàn bài chỉ được tải cho tin đã khớp | Tránh tải hàng trăm bài mỗi lượt; đổi lại có thể sót bài chỉ nhắc từ khóa trong thân bài |
| 5.7 | Không có AI thì trạng thái "chưa phân loại" | Phỏng đoán trạng thái theo câu chữ ("ban hành", "dự thảo", "đề xuất"…), ghi rõ là phỏng đoán | Để chế độ không AI vẫn phân biệt được tin đáng thông báo |
| 5.8 | Ghi nhận máy không có CLI nào | Tìm thấy Claude CLI đi kèm ứng dụng Claude desktop, nhưng **chưa đăng nhập** | Ứng dụng gọi thử AI lúc khởi động và báo rõ khi AI không dùng được |
| 5.9 | Lấy 8 đoạn khớp nhất | Thêm điều kiện đoạn trích phải chứa đủ tỷ lệ từ của câu hỏi | Truy vấn OR trả về mọi đoạn trùng một từ; không lọc thì câu hỏi lạc đề vẫn có "căn cứ" |
| 5.10 | Trích chữ theo vị trí | Trích theo thứ tự luồng nội dung của PDF | Cách theo vị trí làm xáo trộn dòng trên file thật của Cổng Chính phủ |
| 6 | Cột `cluster_id` | Bảng `alert_items` và khóa `cluster_key` | Một cảnh báo có nhiều nguồn đưa tin |
| 7 | — | Thêm `/api/meta`, `/api/show`, `/api/backup`, `/api/documents/{id}/open-folder` | Phục vụ giao diện và việc mở lại cửa sổ khi chạy file lần hai |
| 8 | Token qua URL rồi cookie | Đúng như vậy, qua `/auth?t=…` | — |

### 15.4 Kiểm thử đã chạy

- **Tự động, không cần mạng** (`.\build.ps1`): 10 gói, gồm đường ống quét đầu-cuối trên
  web giả (RSS, bài báo, cổng văn bản) và AI giả; CLI giả mô phỏng đúng đầu ra của Claude
  Code 2.1.286 kể cả trường hợp chưa đăng nhập; kiểm tra bảo mật máy chủ (token, Host,
  Origin, CSP, đường dẫn file); bố cục cấu trúc Win32 của biểu tượng khay.
- **Tự động, trên mạng thật** (`.\build.ps1 -Live`): cả 15 nguồn dựng sẵn trả tin và
  đọc được ngày; tra số hiệu trên Cổng Chính phủ.
- **Chạy thật**: quét thật 15 nguồn; tạo 3 chủ đề, sinh 77 cảnh báo; tải 12 văn bản
  chính thức kèm PDF; xác minh tin báo chí với Cổng Chính phủ; văn bản theo dõi
  13/2023/NĐ-CP được lưu vào thư viện; giao diện 7 màn hình không lỗi console; chat trả
  về đúng văn bản; Windows ghi nhận biểu tượng khay với tooltip đúng; bấm biểu tượng và
  bấm thông báo (mô phỏng bằng thông điệp cửa sổ) mở đúng màn hình; menu chuột phải hiển thị đúng (đã chụp lại); cửa sổ ứng dụng mở
  bằng Edge hiển thị đúng (đã chụp lại); chạy file lần hai chỉ
  mở cửa sổ của phiên bản đang chạy; bật/tắt khởi động cùng Windows ghi và xóa đúng
  registry; thoát sạch.

### 15.5 Chưa kiểm chứng được

- **AI với model thật**: không có CLI đã đăng nhập hay khóa API trên máy phát triển.
  Phân loại bằng AI và câu trả lời chat do AI soạn mới chỉ chạy với AI giả.
- **Codex CLI và Ollama thật**: chưa cài trên máy phát triển.
- **Thông báo nổi nhìn bằng mắt**: máy phát triển bật "Không làm phiền" nên Windows
  không hiện thông báo nổi. Lệnh gửi thông báo đã chạy không lỗi và việc bấm vào thông
  báo đã được mô phỏng, nhưng hình thức hiển thị của thông báo chưa được xem.
- **Bộ dữ liệu vàng** để đo chất lượng phân loại (mục 12) chưa được lập.
