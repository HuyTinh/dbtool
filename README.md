# dbtool

`dbtool` là một công cụ CLI viết bằng Go để quản lý sao lưu và khôi phục cơ sở dữ liệu PostgreSQL. Nó cung cấp một giao diện thống nhất, hỗ trợ nhiều profile kết nối, lọc schema/table linh hoạt và ghi lại lịch sử thao tác.

---

## Yêu cầu

- **Go** 1.21 trở lên
- **PostgreSQL client utilities** đã được cài đặt và thêm vào `PATH`:
  - `pg_dump` — dùng cho lệnh `dump`
  - `pg_restore` — dùng cho lệnh `restore` (với định dạng custom/directory)
  - `psql` — dùng cho lệnh `restore` (với định dạng plain SQL)

---

## Cài đặt

```bash
git clone <repo_url> dbtool
cd dbtool
go install .
```

Sau khi cài đặt, lệnh `dbtool` sẽ khả dụng toàn cục trong terminal.

Hoặc có thể build và chạy trực tiếp:

```bash
go build -o dbtool.exe .
.\dbtool.exe --help
```

---

## Tổng quan các lệnh

```
dbtool [command]

Lệnh khả dụng:
  profile     Quản lý profile kết nối CSDL
    add       Thêm profile mới
    list      Liệt kê profile đã có
    init      Tự động phát hiện kết nối từ file cấu hình dự án
  tui         Giao diện tương tác (chọn profile, file dump, restore/migrate)
  dump        Sao lưu CSDL ra file dump
  restore     Khôi phục CSDL từ file dump
  migrate     Sao chép schema+dữ liệu từ profile A sang profile B
  inspect     Kiểm tra nội dung file dump (danh sách bảng, view)
  history     Xem lịch sử thao tác dump/restore
  doctor      Kiểm tra cấu hình và kết nối hệ thống
  cache       Quản lý cache nội bộ
    list      Liệt kê namespace và kích thước cache
    clear     Xóa cache (toàn bộ hoặc theo namespace)
  version     Hiển thị phiên bản và thông tin build

Global flags:
  -q, --quiet            Tắt các output tiến trình, chỉ hiển thị lỗi
  -v, --verbose          In chi tiết log subprocess
      --timeout string   Giới hạn thời gian thực thi (vd: 2h, 45m, 15s)
  -h, --help             Hiển thị trợ giúp
```

---

## Hướng dẫn sử dụng

### 1. Quản lý Profile kết nối

Profile lưu thông tin kết nối đến một máy chủ CSDL. Các profile được lưu tại:
- Windows: `%APPDATA%\dbtool\profiles.yaml`
- Linux/macOS: `~/.config/dbtool/profiles.yaml`

**Thêm profile:**

```bash
dbtool profile add <tên-profile> \
  --db <tên-database> \
  --host <host> \
  --port <port> \
  --user <username> \
  --password <password>
```

Ví dụ:

```bash
dbtool profile add local-dev \
  --db myapp_dev \
  --host localhost \
  --port 5432 \
  --user postgres \
  --password secret
```

**Liệt kê tất cả profile:**

```bash
dbtool profile list
```

```
NAME                 DRIVER     HOST:PORT                 USER            DATABASE
---------------------
local-dev            postgres   localhost:5432            postgres        myapp_dev
staging              postgres   10.0.0.5:5432             appuser         myapp_staging
```

**Tự động nhận diện kết nối từ file cấu hình dự án (`profile init`):**

dbtool có thể quét thư mục dự án và tự động tạo profile từ file cấu hình của nhiều framework:

```bash
# Quét thư mục hiện tại
dbtool profile init

# Quét thư mục cụ thể
dbtool profile init --from ./backend
```

dbtool hỗ trợ các loại cấu hình sau:

| Importer | File được quét | Cách đọc |
|---|---|---|
| **Spring Boot** | `application*.yml`, `application*.properties` | Đọc `spring.datasource.url`, `username`, `password`. Giải mã JDBC URL (`jdbc:postgresql://`, `jdbc:mysql://`). Tự động giải `${ENV_VAR}` và `${ENV_VAR:default}` từ biến môi trường. |
| **dotenv** | `.env`, `.env.*` | Đọc `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_NAME` (hoặc các biến tương tự). |
| **Docker Compose** | `docker-compose*.yml`, `compose*.yml` | Đọc biến môi trường trong service có image chứa `postgres` hoặc `mysql`. |

Sau khi quét, dbtool sẽ hỏi xác nhận và đặt tên trước khi lưu.

---

### 2. Giao diện tương tác (`tui`)

Chạy `dbtool` không có đối số hoặc `dbtool tui` để mở giao diện tương tác, giúp chọn profile, file dump và thực hiện restore/migrate mà không cần nhớ cú pháp lệnh.

```bash
# Mở TUI (tương đương chạy dbtool không có đối số)
dbtool tui
```

**Các thao tác chính:**

| Màn hình | Phím tắt | Mô tả |
|---|---|---|
| Chọn profile | `[a]` thêm, `[e]` sửa, `[d]` xóa, `[p]` đổi profile | Quản lý profile kết nối trực tiếp trong TUI. Profile đã chọn được ghi nhớ cho các thao tác tiếp theo. |
| Duyệt file dump | `/` tìm kiếm, `[enter]` chọn, `[backspace]` quay lại | Duyệt thư mục, tìm file dump theo tên. Hỗ trợ filter để tìm nhanh. |
| Xác nhận restore | `[enter]` xác nhận, `[m]` bật create-if-missing | Xem lại cấu hình trước khi restore. |
| Chọn profile đích (migrate) | `[enter]` chọn | Chọn profile đích để migrate schema+dữ liệu. |
| Đang restore/migrate | — | Thanh tiến trình và log realtime. |

**Ví dụ luồng sử dụng:**

```
1. Chọn profile nguồn: local-dev
2. Chọn chế độ: [r] Restore  hoặc  [m] Migrate
3. (Restore) Duyệt và chọn file dump: ./backups/myapp.tar
4. Xác nhận cấu hình → Restore bắt đầu
```

> **Mẹo:** Profile đã chọn được ghi nhớ trong phiên TUI. Nhấn `[p]` ở màn hình chọn chế độ để chuyển profile mà không cần quay lại bước đầu.

---

### 3. Sao lưu CSDL (`dump`)

Xuất toàn bộ schema và dữ liệu của CSDL ra file.

```bash
dbtool dump [output_file] --profile <tên-profile> [flags]
```

| Flag | Mô tả | Mặc định |
|---|---|---|
| `--profile` | Tên profile kết nối | *(bắt buộc)* |
| `--format` | Định dạng file: `custom`, `plain`, `directory` | `custom` |
| `--include-table` | Chỉ sao lưu bảng chỉ định (có thể lặp) | — |
| `--exclude-table` | Bỏ qua bảng chỉ định (có thể lặp) | — |
| `--include-schema` | Chỉ sao lưu schema chỉ định (có thể lặp) | — |
| `--exclude-schema` | Bỏ qua schema chỉ định (có thể lặp) | — |

**Ví dụ:**

```bash
# Sao lưu toàn bộ CSDL ra file custom (khuyến nghị)
dbtool dump ./backups/myapp.tar --profile local-dev

# Xuất dưới dạng plain SQL
dbtool dump ./backups/myapp.sql --profile local-dev --format plain

# Xuất dưới dạng thư mục (hỗ trợ restore song song)
dbtool dump ./backups/myapp_dir --profile local-dev --format directory

# Chỉ sao lưu schema "public"
dbtool dump backup.tar --profile local-dev --include-schema public

# Chỉ sao lưu các bảng cụ thể
dbtool dump backup.tar --profile local-dev --include-table users --include-table orders

# Bỏ qua schema và bảng không cần thiết
dbtool dump backup.tar --profile local-dev \
  --exclude-schema temp_schema \
  --exclude-table audit_logs
```

> **Lưu ý về định dạng:**
> - **`custom`** (mặc định): Hỗ trợ nén, hỗ trợ restore theo bảng lẻ. Được khuyến nghị.
> - **`plain`**: File SQL văn bản thuần, có thể mở và đọc bằng text editor.
> - **`directory`**: Mỗi bảng là một file riêng biệt trong thư mục, hỗ trợ restore song song với `-j`.

---

### 4. Kiểm tra nội dung file dump (`inspect`)

Xem danh sách các bảng và view có trong file dump **mà không cần restore**.

```bash
dbtool inspect [dump_file]
```

> **Lưu ý:** Lệnh này chỉ hoạt động với định dạng `custom` và `directory` (không hỗ trợ plain SQL).

**Ví dụ:**

```bash
dbtool inspect ./backups/myapp.tar
```

```
Dump File Archive: ./backups/myapp.tar
============================================================

Tables (5):
----------------------------------------
  - public.users (Owner: postgres)
  - public.orders (Owner: postgres)
  - public.products (Owner: postgres)
  - public.categories (Owner: postgres)
  - public.payments (Owner: postgres)

Views (1):
----------------------------------------
  - public.sales_summary (Owner: postgres)
```

---

### 5. Khôi phục CSDL (`restore`)

Khôi phục CSDL từ file dump (tự động nhận diện định dạng).

```bash
dbtool restore [file_or_directory] --profile <tên-profile> [flags]
```

| Flag | Mô tả | Mặc định |
|---|---|---|
| `--profile` | Tên profile kết nối đích | *(bắt buộc)* |
| `--format` | Định dạng file: `auto`, `custom`, `plain`, `directory` | `auto` |
| `-j, --jobs` | Số luồng restore song song (chỉ áp dụng cho định dạng `directory`) | số CPU |
| `--clean` | Xóa các object cũ trong DB trước khi restore |  `false` |
| `--create-if-missing` | Tự động tạo CSDL đích nếu chưa tồn tại | `false` |
| `--dry-run` | Hiển thị lệnh sẽ chạy mà không thực thi | `false` |
| `--include-table` | Chỉ restore bảng chỉ định (có thể lặp) | — |
| `--exclude-table` | Bỏ qua bảng chỉ định khi restore (có thể lặp) | — |
| `--include-schema` | Chỉ restore schema chỉ định (có thể lặp) | — |
| `--exclude-schema` | Bỏ qua schema chỉ định khi restore (có thể lặp) | — |

**Ví dụ:**

```bash
# Restore cơ bản (tự động nhận dạng định dạng)
dbtool restore ./backups/myapp.tar --profile local-dev

# Xem lệnh sẽ chạy mà không thực thi (dry-run)
dbtool restore ./backups/myapp.tar --profile local-dev --dry-run

# Restore và xóa sạch dữ liệu cũ trước
dbtool restore ./backups/myapp.tar --profile local-dev --clean

# Restore và tự động tạo CSDL nếu chưa tồn tại
dbtool restore ./backups/myapp.tar --profile local-dev --create-if-missing

# Restore với nhiều luồng song song (định dạng directory)
dbtool restore ./backups/myapp_dir --profile local-dev --jobs 8

# Chỉ restore schema "public"
dbtool restore ./backups/myapp.tar --profile local-dev --include-schema public

# Chỉ restore 2 bảng cụ thể
dbtool restore ./backups/myapp.tar --profile local-dev \
  --include-table users \
  --include-table orders
```

> ⚠️ **Cảnh báo `--clean`:** Tùy chọn này sẽ **XÓA TOÀN BỘ** các object (bảng, view, sequence,...) trong schema đích trước khi restore. Không sử dụng nếu CSDL đích đang được dùng chung bởi nhiều ứng dụng.

---

### 6. Sao chép CSDL giữa các profile (`migrate`)

Sao chép schema và dữ liệu từ profile A sang profile B. dbtool sẽ dump từ nguồn ra file tạm, kiểm tra kết nối đích, rồi restore sang đích.

```bash
dbtool migrate --from <profile-nguồn> --to <profile-đích> [flags]
```

| Flag | Mô tả | Mặc định |
|---|---|---|
| `--from` | Tên profile nguồn | *(bắt buộc)* |
| `--to` | Tên profile đích | *(bắt buộc)* |
| `--format` | Định dạng dump trung gian: `custom`, `plain`, `directory` | `custom` |
| `--schema-only` | Chỉ migrate schema (không dữ liệu) | `false` |
| `--data-only` | Chỉ migrate dữ liệu (không schema) | `false` |
| `--clean` | Xóa object cũ ở đích trước khi restore | `false` |
| `--create-if-missing` | Tự động tạo CSDL đích nếu chưa tồn tại | `false` |
| `-j, --jobs` | Số luồng restore song song (chỉ áp dụng cho định dạng `directory`) | `4` |
| `--keep-temp` | Giữ file dump trung gian sau khi migrate | `false` |
| `--dry-run` | Hiển thị lệnh sẽ chạy mà không thực thi | `false` |
| `--include-table` | Chỉ migrate bảng chỉ định (có thể lặp) | — |
| `--exclude-table` | Bỏ qua bảng chỉ định (có thể lặp) | — |
| `--include-schema` | Chỉ migrate schema chỉ định (có thể lặp) | — |
| `--exclude-schema` | Bỏ qua schema chỉ định (có thể lặp) | — |

**Ví dụ:**

```bash
# Migrate toàn bộ schema+dữ liệu từ local-dev sang staging
dbtool migrate --from local-dev --to staging

# Chỉ migrate schema (không dữ liệu)
dbtool migrate --from local-dev --to staging --schema-only

# Migrate với clean (xóa object cũ ở đích trước)
dbtool migrate --from local-dev --to staging --clean

# Migrate và tự động tạo CSDL đích nếu chưa có
dbtool migrate --from local-dev --to staging --create-if-missing

# Chỉ migrate schema public
dbtool migrate --from local-dev --to staging --include-schema public

# Migrate với định dạng directory và 8 luồng song song
dbtool migrate --from local-dev --to staging --format directory -j 8
```

> **Lưu ý:** Cả hai profile nguồn và đích phải cùng driver (ví dụ: cùng `postgres`). Lệnh sẽ báo lỗi nếu không khớp.

---

### 7. Lịch sử thao tác (`history`)

Xem nhật ký dump/restore đã thực hiện trước đây.

```bash
dbtool history [flags]
```

| Flag | Mô tả | Mặc định |
|---|---|---|
| `--profile` | Lọc theo profile | — |
| `--limit` | Số bản ghi hiển thị | `20` |

**Ví dụ:**

```bash
# Xem 20 thao tác gần nhất
dbtool history

# Lọc theo profile cụ thể
dbtool history --profile local-dev

# Xem 50 thao tác gần nhất
dbtool history --limit 50
```

```
TIME                      PROFILE         STATUS     DUMP FILE
-----------------------------------------------------------------------------------------------
2026-07-05 11:00:00       local-dev       SUCCESS    ./backups/myapp.tar
2026-07-04 09:30:22       staging         FAILED     ./backups/old.tar
  -> Error: pg_restore: error connecting to database
```

---

### 8. Kiểm tra hệ thống (`doctor`)

Chẩn đoán toàn diện cấu hình và kết nối của `dbtool`.

```bash
dbtool doctor
```

Lệnh này kiểm tra:
- ✅ Sự hiện diện của các công cụ client (`pg_dump`, `pg_restore`, `psql`) trong PATH
- ✅ Tính hợp lệ của file cấu hình profiles
- ✅ Quyền ghi vào thư mục cache và file lịch sử
- ✅ Kết nối thực tế đến từng server CSDL trong profile

**Ví dụ output:**

```
DBTool Doctor Report:
========================================
✓ Cache Folder       - Cache folder is writable
✓ Configuration File - Profiles file loaded successfully
✓ History File Log   - History log file is writable
✓ pg_dump presence   - Found at: C:\...\pg_dump.exe (version 15.3)
✓ pg_restore         - Found at: C:\...\pg_restore.exe
✗ Database: local-dev - Failed to connect after 3 attempts
  -> Verify host details, credentials, and ensure the server is reachable.
```

---

### 9. Quản lý cache (`cache`)

dbtool lưu cache kết quả phân tích file dump (TOC, định dạng) để tăng tốc các lần chạy sau.

```bash
# Xem các namespace và kích thước cache
dbtool cache list

# Xóa toàn bộ cache
dbtool cache clear

# Xóa cache của namespace cụ thể (vd: toc)
dbtool cache clear --namespace toc
```

---

## Cấu trúc dự án

```
dbtool/
├── main.go                         # Entry point
├── cmd/
│   ├── root.go                     # Root command & global flags
│   ├── profile.go                  # `profile add` / `profile list`
│   ├── profile_init.go             # `profile init` (tự động nhận diện kết nối)
│   ├── dump.go                     # `dump`
│   ├── restore.go                  # `restore`
│   ├── migrate.go                  # `migrate` (sao chép giữa các profile)
│   ├── tui.go                      # `tui` (giao diện tương tác)
│   ├── inspect.go                  # `inspect`
│   ├── history.go                  # `history`
│   ├── cache.go                    # `cache list` / `cache clear`
│   ├── doctor.go                   # `doctor`
│   └── version.go                  # `version` (thông tin build)
└── internal/
    ├── config/                     # Đọc/ghi profiles.yaml
    ├── driver/                     # Interface Driver và registry
    │   └── postgres/               # PostgreSQL driver implementation
    ├── tui/                        # Giao diện tương tác (bubbletea)
    ├── importer/                   # Interface Importer và registry
    │   ├── springboot/             # Spring Boot config importer
    │   ├── dotenv/                 # .env file importer
    │   └── dockercompose/          # Docker Compose file importer
    ├── history/                    # Ghi/đọc lịch sử thao tác (history.jsonl)
    ├── cache/                      # Cache kết quả phát hiện định dạng
    ├── safety/                     # Kiểm tra CSDL đích trước khi ghi đè
    └── procutil/                   # Quản lý subprocess đa nền tảng
```

---

## Vị trí lưu trữ dữ liệu

| Tệp | Windows | Linux/macOS |
|---|---|---|
| Profiles | `%APPDATA%\dbtool\profiles.yaml` | `~/.config/dbtool/profiles.yaml` |
| History | `%APPDATA%\dbtool\history.jsonl` | `~/.config/dbtool/history.jsonl` |
| Cache | `%APPDATA%\dbtool\cache\` | `~/.config/dbtool/cache/` |
