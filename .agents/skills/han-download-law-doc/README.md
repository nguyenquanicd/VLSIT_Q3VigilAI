# Hướng dẫn dùng skill han-download-law-doc

Skill này tải tệp văn bản quy phạm pháp luật từ Cổng thông tin điện tử Chính phủ,
lưu thông tin nguồn và có thể tạo bản Word tham khảo kèm liên kết đến các văn bản
liên quan.

## Thư mục lưu tệp đã tải

- Nếu không chỉ định `-OutDir`, các tệp được lưu trong thư mục `van-ban` bên dưới
  thư mục làm việc hiện tại khi chạy PowerShell.
- Nếu người dùng chỉ định thư mục lưu, dùng đúng đường dẫn đó.
- Mỗi văn bản được lưu trong một thư mục con theo số hiệu đã chuẩn hóa; tên tệp
  đính kèm được giữ như trên Cổng thông tin. `metadata.json` nằm cùng thư mục và
  ghi tên tệp, dung lượng, đường dẫn nguồn chính thức và thông tin văn bản.

Ví dụ:

```text
<OutDir>/
└── 59_2020_QH14/
    ├── 59.signed.pdf
    ├── 59tiep.pdf
    └── metadata.json
```

## Ghi đường dẫn và tạo liên kết đến tệp

- Trong `QUAN-HE-VAN-BAN.md`, ghi đường dẫn tương đối đến từng tệp tải xuống trong
  cột **Tệp đã tải (mở trực tiếp)**. Tạo liên kết Markdown mở thẳng PDF, ví dụ
  `[59.signed.pdf](./59_2020_QH14/59.signed.pdf)`.
- Trong bản Word tham khảo, liên kết chú dẫn đến PDF đã tải trong bộ tài liệu; ưu
  tiên đường dẫn tương đối để khi di chuyển cả thư mục, liên kết vẫn dùng được.
  Nếu một văn bản có nhiều phần PDF, ghi liên kết tới từng phần.
- Khi trả lời người dùng, ghi đường dẫn đầy đủ và tạo liên kết mở từng tệp đã tải,
  đồng thời cho biết thư mục chứa cả bộ. Không dùng trang chi tiết của Cổng thông
  tin thay cho liên kết mở tệp.
- Giữ URL trang chính thức trong `metadata.json` và cột **Trang nguồn chính thức**
  để đối chiếu nguồn; đây là thông tin xuất xứ, không phải liên kết chính để mở tệp.

## Xác nhận dung lượng trước khi tải

Ước tính tổng dung lượng các tệp đính kèm chính thức đã chọn trước khi tải. Nếu
tổng dự kiến lớn hơn 50 MB (50.000.000 byte), báo dung lượng ước tính và hỏi người
dùng muốn tải toàn bộ hay thu hẹp danh sách; chờ trả lời trước khi bắt đầu. Nếu
không lấy được dung lượng đáng tin cậy và bộ tài liệu có thể vượt ngưỡng, nêu rõ
chưa xác định được dung lượng và hỏi trước. Chỉ tính tệp tải từ Cổng thông tin,
không tính Word, Markdown hay `metadata.json`. Sau khi tải, cộng các trường `bytes`
trong `metadata.json` để báo dung lượng thực tế.
