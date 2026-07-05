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
  pitr        Point-in-Time Recovery (khôi phục theo thời điểm)
    setup     Cấu hình WAL archiving cho profile
    backup    Tạo base backup cho PITR
    restore   Khôi phục CSDL đến thời điểm cụ thể
    status    Hiển thị trạng thái PITR, backup, WAL
    cleanup   Xóa backup và WAL cũ theo retention policy
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
| Xác nhận restore | `[c]` clean, `[m]` create-db, `[o]` optimize, `[t/T]` table, `[h/H]` schema | Xem lại cấu hình trước khi restore. Hỗ trợ nhập filter schema/table trực tiếp. |
| Xác nhận dump | `[t]` inc-table, `[T]` exc-table, `[h]` inc-schema, `[H]` exc-schema | Xem lại cấu hình trước khi dump. Hỗ trợ nhập filter schema/table trực tiếp. |
| Xác nhận migrate | `[c]` clean, `[m]` create-db, `[s]` schema-only, `[a]` data-only, `[o]` optimize, `[t/T]` table, `[h/H]` schema | Cấu hình migrate chi tiết. Hỗ trợ nhập filter schema/table trực tiếp. |
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
| `--optimize` | Chạy VACUUM ANALYZE sau khi restore | `false` |
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
| `--optimize` | Chạy VACUUM ANALYZE trên target sau migrate | `false` |
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

### 10. Point-in-Time Recovery (`pitr`)

Khôi phục CSDL đến bất kỳ thời điểm nào trong quá khứ, dựa trên base backup và WAL archive.

#### 10.1. Cấu hình PITR (`pitr setup`)

Tự động cấu hình WAL archiving cho một profile PostgreSQL. Lệnh sẽ:
- Kiểm tra cấu hình PostgreSQL hiện tại (`wal_level`, `archive_mode`, `archive_command`)
- Tạo thư mục lưu WAL archive và base backup
- Áp dụng cấu hình qua `ALTER SYSTEM SET` (không cần sửa thủ công `postgresql.conf`)
- Yêu cầu restart PostgreSQL nếu thay đổi `wal_level` hoặc `archive_mode`

```bash
dbtool pitr setup --profile <tên-profile> [flags]
```

| Flag | Mô tả | Mặc định |
|---|---|---|
| `--profile` | Tên profile kết nối | *(bắt buộc)* |
| `--archive-dir` | Thư mục lưu WAL archive | `~/.config/dbtool/pitr/<profile>/wal` |
| `--base-backup-dir` | Thư mục lưu base backup | `~/.config/dbtool/pitr/<profile>/base` |
| `--retention-backups` | Số base backup giữ lại | `3` |
| `--retention-days` | Số ngày giữ WAL files | `7` |
| `--dry-run` | Hiển thị thay đổi sẽ áp dụng mà không thực thi | `false` |

**Ví dụ:**

```bash
# Cấu hình PITR cho profile local-dev
dbtool pitr setup --profile local-dev

# Xem trước thay đổi sẽ áp dụng
dbtool pitr setup --profile local-dev --dry-run

# Tùy chỉnh thư mục và retention
dbtool pitr setup --profile local-dev \
  --archive-dir /data/wal-archive \
  --base-backup-dir /data/base-backups \
  --retention-backups 5 \
  --retention-days 14
```

#### 10.2. Tạo base backup (`pitr backup`)

Tạo base backup bằng `pg_basebackup` và lưu metadata để sử dụng cho restore.

```bash
dbtool pitr backup --profile <tên-profile> [flags]
```

| Flag | Mô tả | Mặc định |
|---|---|---|
| `--profile` | Tên profile đã cấu hình PITR | *(bắt buộc)* |
| `--jobs` | Số luồng song song | `2` |
| `--checkpoint` | Chế độ checkpoint: `fast` hoặc `spread` | `fast` |
| `--no-compress` | Tắt nén gzip | `false` |
| `--dry-run` | Hiển thị lệnh sẽ chạy mà không thực thi | `false` |

**Ví dụ:**

```bash
# Tạo base backup
dbtool pitr backup --profile local-dev

# Backup với 4 luồng, không nén
dbtool pitr backup --profile local-dev --jobs 4 --no-compress
```

#### 10.3. Khôi phục theo thời điểm (`pitr restore`)

Khôi phục CSDL đến một thời điểm cụ thể trong quá khứ.

```bash
dbtool pitr restore --profile <profile> --to '<timestamp>' [flags]
```

| Flag | Mô tả | Mặc định |
|---|---|---|
| `--profile` | Profile nguồn đã cấu hình PITR | *(bắt buộc)* |
| `--to` | Thời điểm khôi phục (format: `2024-01-15 14:30:00`) | *(bắt buộc)* |
| `--target-profile` | Profile đích (mặc định: cùng profile nguồn) | — |
| `--timeline` | Timeline ID (mặc định: mới nhất) | `0` |
| `--clean` | Xóa objects trước khi restore | `false` |
| `--create-if-missing` | Tạo CSDL đích nếu chưa tồn tại | `false` |
| `--dry-run` | Hiển thị recovery plan mà không thực thi | `false` |

**Ví dụ:**

```bash
# Khôi phục đến thời điểm cụ thể
dbtool pitr restore --profile local-dev --to '2026-07-05 10:30:00'

# Khôi phục sang profile khác
dbtool pitr restore --profile local-dev --to '2026-07-05 10:30:00' \
  --target-profile local-dev-recovery

# Xem trước recovery plan
dbtool pitr restore --profile local-dev --to '2026-07-05 10:30:00' --dry-run
```

> ⚠️ **Cảnh báo:** PITR restore sẽ **ghi đè** CSDL đích. Lệnh tự động backup data directory trước khi restore để an toàn.

#### 10.4. Kiểm tra trạng thái (`pitr status`)

Hiển thị thông tin toàn diện về PITR: cấu hình PostgreSQL, danh sách base backup, thống kê WAL archive và phạm vi khôi phục.

```bash
dbtool pitr status --profile <tên-profile>
```

**Ví dụ output:**

```
=== PITR Status for Profile: local-dev ===

PostgreSQL Configuration:
  Archive Mode:        on
  WAL Level:           replica
  Archive Command:     cp %p /home/user/.config/dbtool/pitr/local-dev/wal/%f

Base Backups:
  [1] 20260705_100000
      Time:       2026-07-05 10:00:00 (2 hours ago)
      Timeline:   1
      Size:       256 MB
      WAL Range:  000000010000000000000001 → 000000010000000000000003

WAL Archive:
  Total Files:         150 files
  Total Size:          2.4 GB
  Last Archived:       3 minutes ago

Recovery Range:
  Earliest Point:      2026-07-05 10:00:00
  Latest Point:        2026-07-05 12:03:00
  Coverage:            2 hours
```

#### 10.5. Dọn dẹp backup cũ (`pitr cleanup`)

Xóa base backup và WAL files cũ theo retention policy.

```bash
dbtool pitr cleanup --profile <tên-profile> [flags]
```

| Flag | Mô tả | Mặc định |
|---|---|---|
| `--profile` | Tên profile | *(bắt buộc)* |
| `--dry-run` | Hiển thị file sẽ xóa mà không thực hiện | `false` |
| `--force` | Bỏ qua xác nhận | `false` |

**Ví dụ:**

```bash
# Xem trước file sẽ xóa
dbtool pitr cleanup --profile local-dev --dry-run

# Xóa không cần xác nhận
dbtool pitr cleanup --profile local-dev --force
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
│   ├── pitr.go                     # `pitr` (parent command)
│   ├── pitr_setup.go               # `pitr setup` (cấu hình WAL archiving)
│   ├── pitr_backup.go              # `pitr backup` (tạo base backup)
│   ├── pitr_restore.go             # `pitr restore` (khôi phục theo thời điểm)
│   ├── pitr_status.go              # `pitr status` (trạng thái PITR)
│   ├── pitr_cleanup.go             # `pitr cleanup` (dọn dẹp backup cũ)
│   ├── history.go                  # `history`
│   ├── cache.go                    # `cache list` / `cache clear`
│   ├── doctor.go                   # `doctor`
│   └── version.go                  # `version` (thông tin build)
└── internal/
    ├── config/                     # Đọc/ghi profiles.yaml
    ├── driver/                     # Interface Driver và registry
    │   └── postgres/               # PostgreSQL driver implementation
    │       ├── restore.go          # Restore & dump logic
    │       ├── detector.go         # Tự động nhận diện định dạng dump
    │       └── pitr.go             # PostgreSQL PITR driver helpers
    ├── tui/                        # Giao diện tương tác (bubbletea)
    ├── pitr/                       # PITR core logic
    │   ├── basebackup.go           # pg_basebackup wrapper & metadata
    │   ├── config.go               # PITR config load/save
    │   ├── validation.go           # Validate backup integrity, WAL coverage, disk space
    │   └── wal.go                  # WAL file parsing, LSN handling, range queries
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
| PITR Config | `%APPDATA%\dbtool\pitr\<profile>\config.yaml` | `~/.config/dbtool/pitr/<profile>/config.yaml` |
| PITR WAL | `%APPDATA%\dbtool\pitr\<profile>\wal\` | `~/.config/dbtool/pitr/<profile>/wal/` |
| PITR Backups | `%APPDATA%\dbtool\pitr\<profile>\base\` | `~/.config/dbtool/pitr/<profile>/base/` |
