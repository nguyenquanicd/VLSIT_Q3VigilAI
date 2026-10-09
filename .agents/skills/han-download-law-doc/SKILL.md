---
name: han-download-law-doc
description: Find and download original Vietnamese legal documents from the Government portal, identify current amending and implementing texts, and prepare a Word reference copy of a main law or code with article-level links. Use for Vietnamese law, decree or circular lookups, including requests about document numbers, applicable instruments, thuvienphapluat.vn or vbpl.vn.
---

# Download official Vietnamese legal documents

This skill retrieves the **original files published by the State** (usually
digitally signed PDFs) from `vanban.chinhphu.vn`, with a `metadata.json` recording
the document number, issue date, effective date, issuing body, signer and source
URL. By default it downloads a **complete set**: the document the user needs, the
implementing and amending documents currently applied, and a `QUAN-HE-VAN-BAN.md`
file explaining how they relate. When preparing the complete set for a main law or
code, also create a separate Word reference copy that links each affected provision
to the latest applicable amending or implementing legal documents; make those
links open the downloaded local files, not portal detail pages, and keep the
digitally signed originals unchanged. See [README.md](README.md) for output-folder,
file-path and download-size instructions.

Run the script with Windows PowerShell 5.1 or later (built into Windows 10/11), or
PowerShell 7, with internet access to chinhphu.vn.

Reply to the user in the language they use. Vietnamese legal terms that appear in
commands or on the portal are kept in Vietnamese throughout this skill: *số hiệu*
(document number), *trích yếu* (the portal's one-line summary/title), *nghị định*
(decree), *thông tư* (circular), *văn bản hợp nhất* (consolidated text).

## Why not thuvienphapluat.vn

Users often name thuvienphapluat.vn out of habit, but what they need is a correct,
usable document. That site is a commercial service run by a private company; it
sits behind Cloudflare bot protection (scripts get HTTP 403, browsers get a "verify
you are human" challenge) and locks file downloads behind paid accounts. Do not try
to get past that protection: no browser emulation, no CAPTCHA solving, no
Cloudflare-evasion libraries.

The Government portal permits automated access (`robots.txt`: `Allow: /`), is free,
and is the officially referenceable source - which serves the goal of "official
documents" better. If the user mentions thuvienphapluat.vn, give the reason in one
sentence and download from the Government portal. Only when they need something
specific to that site (document diagrams, English translations, validity notes)
should you tell them to open it in their own browser.

## Commands

Everything goes through one script, `scripts/vanban.ps1` (path relative to this
skill folder). Run it with PowerShell:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File "<skill folder>\scripts\vanban.ps1" <action> ...
```

### 1. `search`

```powershell
... vanban.ps1 search -Keyword "59/2020/QH14"
... vanban.ps1 search -Keyword "bảo vệ dữ liệu cá nhân" -Top 20
... vanban.ps1 search -Keyword "doanh nghiệp" -Loai luat -Year 2025
```

Each result line reads `[docId] số hiệu | issue date | trích yếu | files: n`.

| Parameter | Meaning |
|---|---|
| `-Keyword` | A document number or a phrase from the trích yếu. The portal matches literal strings, so type Vietnamese **with diacritics** and use a short, distinctive phrase ("dữ liệu cá nhân" works better than a long sentence). |
| `-Loai` | Document type: `hienphap` (constitution), `sacluat` (decree-law), `luat` (laws and ordinances), `nghidinh` (decrees), `quyetdinh` (decisions), `thongtu` (circulars). Empty = all types. |
| `-Year` | Year of issue. |
| `-Top` | Maximum number of results (default 20, maximum 500). |
| `-Class` | `1` = legal normative documents (default). `2` = directive and administrative documents (directives, official telegrams, official letters, individual decisions of the Prime Minister). If nothing is found in class 1, try class 2. |
| `-Json` | Emit JSON instead of text, for further processing. |

### 2. `info`

```powershell
... vanban.ps1 info -DocId 216536
```

Prints all attributes and attachment links without downloading anything. Use it to
check the effective date or the signer before deciding to download.

### 3. `download`

```powershell
... vanban.ps1 download -SoHieu "59/2020/QH14; 13/2023/NĐ-CP" -OutDir ".\van-ban"
... vanban.ps1 download -DocId 216536,216510 -OutDir ".\van-ban"
```

- `-SoHieu`: one or more document numbers separated by `;`. A document is
  downloaded only on an **exact** match (ignoring case, whitespace, `Đ`/`D`, and
  the Cyrillic look-alike letters the portal occasionally contains). Typing
  `13/2023/ND-CP` without diacritics still works.
- `-DocId`: use when you already have ids from a search - the most reliable option
  when searching by topic.
- `-OutDir`: defaults to `.\van-ban` in the current working folder. Use the
  location the user names, if any.
- `-Force`: overwrite files that already exist (existing files are skipped by
  default).

Before downloading, estimate the combined size of all selected official attachment
files (for example, from their `Content-Length` headers). If the expected total is
over 50 MB (50,000,000 bytes), tell the user the estimated size and ask whether to
download the full set or a smaller selection; wait for their answer before starting
the download. If reliable sizes are unavailable and the set could exceed the limit,
explain that uncertainty and ask before downloading. Count only official files
being downloaded; exclude generated Word/Markdown files and `metadata.json`. After
download, report the actual combined size using the byte counts in `metadata.json`.
If an unexpected size increase would take a later batch over the limit, pause and
ask before starting that batch.

For each document the output shows the issue date, the effective date, and
`** NOT YET IN FORCE **` when the effective date is still in the future.

Result on disk:

```
van-ban/
└── 59_2020_QH14/
    ├── 59.signed.pdf      <- file names are kept as published
    ├── 59tiep.pdf
    └── metadata.json
```

Exit code `2` means at least one document was not found or has no file. The script
still downloads the others and prints a `NOT FOUND` entry with the closest results.

### 4. `related`

```powershell
... vanban.ps1 related -SoHieu "45/2019/QH14"
... vanban.ps1 related -DocId 198540 -Keyword "tuổi nghỉ hưu;lao động nước ngoài;hợp đồng lao động"
```

Takes exactly one base document and searches the portal (classes 1 and 2) for every
document that mentions it by **number** or by **name**. The first line is the base
document with its issue date, effective date, and the `** NOT YET IN FORCE **` flag
where applicable. Each following line is a candidate:

```
[docId] số hiệu | issue date | AFTER or BEFORE | C1 or C2 | trích yếu
```

- `AFTER` / `BEFORE`: issued after or before the base document. A document that
  implements a law must be issued after it, so the `AFTER` group is where the
  currently applied documents are; the `BEFORE` group mostly belongs to the previous
  version of the law.
- `-Keyword`: additional search phrases separated by `;`. This matters, because the
  script only searches by the base document's name and number - see "Why extra
  keywords are needed" below.

The script only **gathers candidates**. Use the trích yếu to triage them, then read
the full text to classify their relationship and map any reference to a specific
provision of the base document.

## Workflow

By default the user needs the **complete set**, not a single file: the document
they asked about, the decrees, circulars, implementing guidance and amendments
currently applied, and a file explaining the relationships. A law on its own is
rarely enough to work with, because most operational detail lives in the
implementing documents. Download a single document only when the user says
explicitly that they need just that one.

0. **No document named** -> ask before doing anything else. If the skill is invoked
   with no name, number or topic (for example only the skill name, or "download a
   legal document for me"), ask one short question: which document do they need -
   name or number, and the year of issue if they remember it. Do not pick a document
   on their behalf and do not run the script before they answer.
1. **Identify the base document** with `search` (by number or name; see "When the
   name the user gives does not match").
2. **Select the latest version in force** of the base document - see "Always take
   the latest version in force".
3. **Gather related documents** with `related`, running it again with `-Keyword`
   for the main subject areas of the base document.
4. **Classify and filter** each candidate: how it relates to the base document, and
   whether it still applies or has been replaced. Drop candidates that are unrelated
   (keyword matches on a different subject).
5. **Download** everything selected into one folder with a single `download -DocId
   ... -OutDir "van-ban\<set name>"`, for example
   `van-ban\Bo-luat-Lao-dong-45_2019_QH14`. First apply the 50 MB size check above.
   If the set exceeds 20 documents, tell the user the count and ask whether they
   want all of them or only the core group.
6. **Write the relationship file** `QUAN-HE-VAN-BAN.md` into that folder - see the
   template below.
7. **Create the Word reference copy** when the complete set is for a law or code.
   If the user explicitly asks for only one official file or only a PDF, do not add
   a Word copy unless they request it. For other base-document types, create one only
   if the user asks. Follow “Word reference copy with provision-level links” below.
8. **Report**: the base document (number, name, issue date, effective date), the
   number and total size of documents downloaded per group, clickable links to the
   actual downloaded files and their folder, relationship file and any Word copy,
   and **every special note**. Do not use a portal detail page as the file link.

## Always take the latest version in force

The Government portal **does not state** whether a document is still in force. This
step is therefore inference from evidence, and you must tell the user it is
inference. How to do it:

- **Base document.** Search by name for a document of the same name and type issued
  later (the 2013 and 2024 Land Laws; the 2012 and 2019 Labor Codes). If one exists,
  the latest is the one to download. If the user named the number of an older
  version, still use the latest as the base document and say clearly that the one
  they named has been replaced. Download the older version as well only if they need
  it (for example, to handle a matter that arose before the new version took
  effect).
- **Not yet in force.** If the script reports `NOT YET IN FORCE`, the latest version
  does not apply yet: download both the new version and the one currently in force,
  and state the changeover date.
- **Amending documents.** A trích yếu of the form "Luật sửa đổi, bổ sung một số
  điều của ..." (law amending and supplementing ...) issued after the base document
  is part of the set: the base document must be read together with it. Also look
  for a consolidated text (*văn bản hợp nhất*, code `VBHN`); if there is one,
  download it too, since it already merges the amendments.
- **Implementing documents.** Take only documents in the `AFTER` group that
  genuinely implement the base document. Within that group, when two documents
  cover the same subject (same trích yếu, or the later one says "thay thế"
  (replaces) or "sửa đổi, bổ sung Nghị định số ..." (amends decree no. ...)), the
  later one is the one applied; list the earlier one in the relationship file
  without downloading it - unless it is only partly amended, in which case download
  both.
- **`BEFORE` group.** These implement the previous version of the law: do not
  download them, but list the main ones in the "not downloaded" section of the
  relationship file so the user knows they exist.
- **When stronger evidence is needed** for the base document or an ambiguous case:
  the "Hiệu lực thi hành" (entry into force) article at the end of a document states
  which documents it replaces or repeals. Use the `han-scan-to-word` skill with
  `-Pages` on the last few pages of the downloaded file to read that article, then
  record it in the "Basis" column.

Never state "in force" as a verified fact. Say "based on the documents found on the
Government portal, no replacing document was found" and remind the user to
cross-check at vbpl.vn.

## Word reference copy with provision-level links

Create an editable `.docx` working copy of the selected main law or code, alongside
the unchanged official PDF. The Word copy is a navigation aid, not the official
signed file. Label it as a reference copy and state its precise source and the date
the references were checked. On its first page, state that the assessment of current
applicability is not an official validity certification and direct readers to
cross-check at vbpl.vn. Do not manually merge amendment wording into the base law or
represent OCR/conversion as an official text.

If an official consolidated text (`VBHN`) is available and is the latest suitable
source, use it as the Word copy's statutory text. Otherwise, use the selected base
document without merging amendment wording into it, and make clear that the linked
amending instruments have not been incorporated into the text.

To map references to provisions:

- Read the relevant provisions in the base document and the full text of each
  candidate amending or implementing document. A title or search-result snippet
  alone is not enough to assign a document to a specific article.
- For an amendment, identify the exact provision in the amending document and the
  article, clause or point of the base law that it changes, supplements, repeals or
  replaces. For an implementing document, identify the provisions that expressly
  guide or detail the relevant article or clause of the base law.
- Link only the latest version that appears applicable based on the evidence
  collected. Check effective dates, replacement/amendment clauses and transition
  rules. If an older instrument remains applicable to a transition or unaffected
  provisions, include it with that limitation clearly stated.
- For the article-level normative references, include only documents classified as
  normative legal documents (Class 1). Do not present an official letter, directive
  or other administrative document (Class 2) as a văn bản quy phạm pháp luật. If a
  non-normative document is useful context, label it separately and only include it
  when the user asks for it or it materially clarifies the research trail.
- If a document is relevant to the law generally but no particular article can be
  confirmed, put it in a clearly separate general-references list; do not attach it
  to an article by inference. If even the general relationship is uncertain, mark
  it as unconfirmed in the relationship file.

In the Word copy, place a visually distinct “Văn bản liên quan” note immediately
after the affected article or provision, without changing the article's wording,
numbering or legal text. Each link label should identify the instrument's document
number and short title, the relevant article(s) of that instrument, and the kind of
relationship (for example, “sửa đổi, bổ sung” or “quy định chi tiết”). Hyperlink
the label to the corresponding file downloaded into this set, using a relative path
from the Word file where possible. Do not hyperlink the article note to a portal
detail page when the document file is present locally. If an instrument is split
into multiple downloaded files, provide a direct link to each part (or a clearly
labelled list of the parts). Check that each target exists and opens the displayed
document. Keep the official page URL from `metadata.json` available as provenance
in the relationship file; never invent a URL or link to an unofficial copy as if
it were official.

Keep reference notes clearly separate from the statutory text so readers cannot
mistake them for part of the law. When a precise provision match is unavailable,
leave the law text unannotated and explain the limitation in the relationship file.
State that “latest/currently applicable” is an evidence-based assessment, not an
official validity certification, and direct the user to cross-check at vbpl.vn.

Use the `documents` skill to create or edit the `.docx` and render it for visual
verification. If the source PDF is scanned or requires OCR, use `han-scan-to-word`
and check the converted text against the official PDF, especially article numbers,
cross-references, dates and amendment language. Render and verify the final Word
copy after links are inserted.

### Why extra keywords are needed

`related` searches by the base document's name and number, so it misses
implementing documents that do not name the law in their trích yếu. With the 2019
Labor Code, for example, a name search returns only Decree 145/2020/NĐ-CP; the
decrees on retirement age and on foreign workers have a trích yếu that does not
contain "Bộ luật Lao động".

To compensate, list the main subject areas of the base document and run `related`
or `search` with each phrase ("tuổi nghỉ hưu", "lao động nước ngoài", "mức lương
tối thiểu", "xử phạt vi phạm hành chính" plus the field, and so on). What you
already know about which documents implement which law is a **lead to search
with**, not a result: every document number you recall must be found on the portal
before it goes into the set, and your knowledge may be out of date - a decree you
remember may have been replaced since.

For that reason the set **must not be presented as complete**. Say so in the
relationship file.

## The relationship file `QUAN-HE-VAN-BAN.md`

Place it at the root of the set folder. Write its contents in **Vietnamese** - it
describes Vietnamese legal documents for the people who will use them - unless the
user asks for another language. It is the first thing the user opens, so it must
stand on its own: any reader should understand what the set contains, which
document depends on which, and what needs care. Use this structure, including the
Word-reference section whenever a Word copy is created:

```markdown
# Quan hệ giữa các văn bản: <tên văn bản gốc>

Lập ngày <ngày>. Nguồn: vanban.chinhphu.vn.

## Văn bản gốc

| Số hiệu | Tên | Ngày ban hành | Có hiệu lực từ | Tệp đã tải (mở trực tiếp) | Thư mục |
|---|---|---|---|---|---|

## Bản Word tham khảo có liên kết

- Tệp: <tên tệp .docx>. Đây là bản tham khảo có chú dẫn, không thay thế bản PDF
  chính thức.
- Ngày đối chiếu liên kết: <ngày>.

| Điều/khoản của văn bản gốc | Văn bản được dẫn chiếu và điều khoản liên quan | Quan hệ | Tệp đã tải (mở trực tiếp) | Trang nguồn chính thức |
|---|---|---|---|---|

### Văn bản liên quan chung, chưa xác định điều khoản cụ thể

| Số hiệu | Tên / trích yếu | Căn cứ xác định | Tệp đã tải (mở trực tiếp) | Trang nguồn chính thức |
|---|---|---|---|---|

## Lưu ý đặc biệt

- <mỗi lưu ý một dòng; nếu không có thì ghi "Không có lưu ý đặc biệt.">

## Sơ đồ quan hệ

<cây chữ: văn bản gốc ở trên, văn bản sửa đổi và hướng dẫn thụt vào bên dưới,
văn bản hướng dẫn nghị định thụt thêm một cấp>

## Các văn bản đã tải

| Số hiệu | Tên / trích yếu | Ngày ban hành | Có hiệu lực từ | Quan hệ với văn bản gốc | Căn cứ xác định | Tệp đã tải (mở trực tiếp) | Thư mục |
|---|---|---|---|---|---|---|---|

## Văn bản liên quan không tải

| Số hiệu | Trích yếu | Ngày ban hành | Lý do không tải |
|---|---|---|---|

## Giới hạn của bản tổng hợp này

- Tình trạng hiệu lực là suy luận từ ngày ban hành và trích yếu, không phải dữ liệu
  chính thức. Đối chiếu tại vbpl.vn trước khi trích dẫn.
- Danh sách có thể chưa đầy đủ: <nêu các mảng đã tìm và các từ khóa đã dùng>.
```

The sections are, in order: base document, Word reference copy (when created),
special notes, relationship tree, downloaded documents, related documents not
downloaded, and limitations.

In the **"Quan hệ với văn bản gốc"** (relationship to the base document) column, use
one of these fixed labels so the user can filter:

| Label to write | Meaning | When to use |
|---|---|---|
| Sửa đổi, bổ sung văn bản gốc | Amends the base document | A document of the same rank issued later that changes some articles of the base document. |
| Văn bản hợp nhất | Consolidated text | The base document merged with its amendments. |
| Quy định chi tiết / hướng dẫn thi hành | Details / implements | A decree or circular that elaborates articles of the base document. |
| Hướng dẫn văn bản hướng dẫn | Second-level guidance | A circular implementing, or a decree amending, a decree in the set. |
| Xử phạt vi phạm | Penalties | The decree on administrative penalties in the base document's field. |
| Văn bản gốc thay thế văn bản này | Replaced by the base document | An earlier version of the base document (usually in the "not downloaded" section). |
| Liên quan khác | Other | Refers to the base document but fits none of the above; state what it is. |

The **"Căn cứ xác định"** (basis) column records why you concluded that
relationship: "trích yếu ghi rõ" (stated in the title), "điều khoản thi hành của văn
bản (đã đọc)" (entry-into-force article, read), or "suy luận từ nội dung và ngày ban
hành" (inferred from subject and issue date). The user needs to know which
conclusions are firm and which are judgement.

## Special notes that must be reported to the user

The following change how far the user may rely on the set, so state them **both in
your reply and in the relationship file**; do not leave them buried in a table:

- The base document, or an important document in the set, is **not yet in force**,
  with the date it takes effect.
- The document the user asked for **has been replaced by a newer one**, and you
  downloaded the newer one.
- The base document **has been amended** by another document: they must be read
  together.
- There is a **transition period**: old and new versions coexist, or implementing
  documents of the old version still apply temporarily.
- The document is new and **no implementing documents were found** on the portal.
- A document in the set **has no attached file**, or the downloaded file is invalid.
- A document you know to be related **could not be found on the portal**.
- The conclusion about the validity or relationship of an important document is
  only **judgement**.

If there are none, say explicitly that there are none rather than leaving the
section empty.

## When the name the user gives does not match

Users usually remember a document name approximately: misspelt, without diacritics,
by a colloquial name ("luật bảo vệ thông tin cá nhân" instead of "Luật Bảo vệ dữ
liệu cá nhân"), or with the wrong number or year. The portal matches literal
strings, so one failed search does not mean the document does not exist. Before
reporting "not found", try similar names yourself:

- **Reduce to the core phrase**: drop the document-type word and filler ("Luật",
  "Nghị định về", "quy định") and keep the distinctive part - "dữ liệu cá nhân",
  "đất đai".
- **Fix typing errors**: add Vietnamese diacritics, correct spelling, try another
  spelling of the same word.
- **Try a synonym or the official name** you know: "sổ đỏ" -> "giấy chứng nhận quyền
  sử dụng đất", "luật lao động" -> "Bộ luật Lao động".
- **Loosen the filters**: remove `-Year`, remove `-Loai`, switch to `-Class 2`.
- **A number that looks wrong**: if `download -SoHieu` reports `NOT FOUND`, look at
  the "closest results" it prints, and search by the document's name if the user
  gave one - they may have misremembered the number, year or issuing-body code.

After searching with a similar name:

- One result clearly matches the user's intent -> use it, and **say explicitly**
  which document you used in place of the name they gave, for example: "There is no
  document named 'Luật bảo vệ thông tin cá nhân'; I downloaded the Law on Personal
  Data Protection, no. 91/2025/QH15." The user needs to know a substitution happened
  so they can check it, since they may cite this document.
- Several results could each be right -> give a short list (number, date, trích
  yếu) for them to choose from.
- Several variants tried and nothing close -> report that it was not found, list
  the names you tried, and ask for a more exact name or number.

## What to be honest about with the user

- **Validity status**: the portal records the issue date and effective date, but
  not whether a document is "in force", "expired", amended or replaced. Do not
  assert that a document is in force merely because it could be downloaded. Any
  statement about validity is inference; say so, and recommend cross-checking at
  vbpl.vn (the Ministry of Justice's national legal database).
- **Not found**: the portal is strong on documents of the National Assembly, the
  Government, the Prime Minister and the ministries; local-government or very old
  documents may be missing. When `search` finds nothing, retry with a shorter
  keyword, without `-Loai`/`-Year`, and with `-Class 2`. If there is still nothing,
  say plainly that it is not on the Government portal and suggest the user look at
  vbpl.vn or congbao.chinhphu.vn themselves - do not invent content, and do not take
  it from an unofficial source and call it official.
- **Scanned PDFs**: many files are scans with a red seal and no text layer. If the
  user wants to read or quote the content, a separate OCR step is needed (the
  `han-scan-to-word` skill); make clear that OCR text can contain errors and does
  not replace the original.
- **One document, several files**: long documents are often split into parts
  (`59.signed.pdf`, `59tiep.pdf`) or have separate appendices. All parts are
  downloaded; remind the user that the content spans several files.

## Be considerate to the server

The script pauses about 0.8 seconds between requests and retries transient
failures. This is a public server: do not lower `-DelayMs`, and do not run several
processes in parallel to go faster. Downloading a few dozen documents in one go is
normal; if the user wants to copy the whole database, ask what it is for first.

## When the script reports an error

- `Search form not found ...`: the portal changed its layout. Open
  `https://vanban.chinhphu.vn/he-thong-van-ban?classid=1&mode=1`, look at the new
  structure, and fix the expressions in `Search-Documents` / `ConvertFrom-ResultPage`.
- `Request failed ... HTTP 5xx` or a timeout: the portal is overloaded. Wait a few
  minutes and run again; files already downloaded are skipped.
- `downloaded-but-not-a-valid-pdf`: the server returned something that is not a PDF
  (usually an error page). Tell the user and give them the source link from
  `metadata.json` so they can check it themselves.
