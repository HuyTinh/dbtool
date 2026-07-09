# PITR Manual Testing Guide

Hướng dẫn test manual cho Point-in-Time Recovery (PITR) feature.

## Yêu cầu

- PostgreSQL 12+ đã cài đặt
- `pg_basebackup` và `pg_ctl` trong PATH
- Database test (không phải production!)

## Bước 1: Chuẩn bị PostgreSQL

### 1.1 Tạo database test

```bash
# Tạo database test
createdb -U postgres pitr_test

# Kết nối và kiểm tra
psql -U postgres -d pitr_test
```

### 1.2 Kiểm tra cấu hình PostgreSQL

```sql
-- Kiểm tra wal_level (phải là 'replica' hoặc 'logical')
SHOW wal_level;

-- Kiểm tra archive_mode
SHOW archive_mode;

-- Kiểm tra archive_command
SHOW archive_command;
```

Nếu `wal_level` là `minimal` hoặc `archive_mode` là `off`, cần sửa `postgresql.conf`:

```bash
# Tìm file postgresql.conf
psql -U postgres -c "SHOW config_file;"

# Sửa file (cần restart PostgreSQL)
sudo nano /etc/postgresql/15/main/postgresql.conf
```

Thêm/sửa các dòng:
```ini
wal_level = replica
archive_mode = on
archive_command = ''  # Sẽ được set bởi dbtool pitr setup
```

Restart PostgreSQL:
```bash
# Linux
sudo systemctl restart postgresql

# macOS
brew services restart postgresql

# Windows
net stop postgresql-x64-15
net start postgresql-x64-15
```

## Bước 2: Tạo profile trong dbtool

```bash
# Tạo profile cho database test
dbtool profile init

# Nhập thông tin:
# Profile name: pitr_test
# Driver: postgres
# Host: localhost
# Port: 5432
# User: postgres
# Password: (nhập password)
# Database: pitr_test
```

## Bước 3: Setup PITR

```bash
# Setup PITR cho profile
dbtool pitr setup --profile pitr_test

# Kiểm tra output sẽ hiển thị:
# - Archive mode status
# - WAL level
# - Archive command được tạo
# - Thư mục archive và backup
```

**Expected output:**
```
✓ PITR setup completed for profile: pitr_test

Archive directory: ~/.config/dbtool/pitr/pitr_test/archive
Base backup directory: ~/.config/dbtool/pitr/pitr_test/backups

Archive command:
  cp %p ~/.config/dbtool/pitr/pitr_test/archive/%f

Next steps:
  1. Add the archive_command to postgresql.conf
  2. Restart PostgreSQL
  3. Run: dbtool pitr backup --profile pitr_test
```

**Áp dụng archive_command:**

Mở `postgresql.conf` và thêm:
```ini
archive_command = 'cp %p ~/.config/dbtool/pitr/pitr_test/archive/%f'
```

Restart PostgreSQL:
```bash
sudo systemctl restart postgresql
```

## Bước 4: Tạo base backup đầu tiên

```bash
# Tạo base backup
dbtool pitr backup --profile pitr_test
```

**Expected output:**
```
Starting base backup...
✓ Base backup completed
  ID: 20260705_140000
  Size: 25.5 MB
  Timeline: 1
  Duration: 12s
```

## Bước 5: Tạo dữ liệu test (Thời điểm T1)

```bash
# Kết nối database
psql -U postgres -d pitr_test
```

```sql
-- Tạo table và insert data
CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100),
    created_at TIMESTAMP DEFAULT NOW()
);

INSERT INTO users (name) VALUES
    ('Alice'),
    ('Bob'),
    ('Charlie');

-- Kiểm tra
SELECT * FROM users;
-- Lưu ý thời gian hiện tại: 2026-07-05 14:10:00 (T1)

-- Thoát
\q
```

**Ghi nhớ thời điểm T1: 2026-07-05 14:10:00**

## Bước 6: Chờ 1 phút, tạo thêm data (Thời điểm T2)

```bash
# Chờ 1 phút
sleep 60

# Kết nối lại
psql -U postgres -d pitr_test
```

```sql
-- Thêm data mới
INSERT INTO users (name) VALUES
    ('David'),
    ('Eve');

-- Kiểm tra
SELECT * FROM users;
-- Lưu ý thời gian: 2026-07-05 14:11:00 (T2)

\q
```

**Ghi nhớ thời điểm T2: 2026-07-05 14:11:00**

## Bước 7: Chờ thêm 1 phút, tạo data "lỗi" (Thời điểm T3)

```bash
# Chờ 1 phút
sleep 60

# Kết nối lại
psql -U postgres -d pitr_test
```

```sql
-- Thêm data "lỗi" (simulating accidental delete)
DELETE FROM users WHERE name IN ('Alice', 'Bob', 'Charlie');

-- Kiểm tra (chỉ còn David, Eve)
SELECT * FROM users;
-- Lưu ý thời gian: 2026-07-05 14:12:00 (T3)

\q
```

**Ghi nhớ thời điểm T3: 2026-07-05 14:12:00**

## Bước 8: Kiểm tra status

```bash
# Xem PITR status
dbtool pitr status --profile pitr_test
```

**Expected output:**
```
=== PITR Status for Profile: pitr_test ===

PostgreSQL Configuration:
  Archive Mode:        on
  WAL Level:           replica
  Archive Command:     cp %p ~/.config/dbtool/pitr/pitr_test/archive/%f

Base Backups:
  [1] 20260705_140000
      Time:       2026-07-05 14:00:00 (12 minutes ago)
      Timeline:   1
      Size:       25.5 MB
      WAL Range:  0/1000000 → 0/1500000
      Duration:   12s

WAL Archive:
  Total Files:        15 files
  Total Size:         240 MB
  First WAL:          000000010000000000000001 (timeline 1)
  Last WAL:           00000001000000000000000F (timeline 1)
  Last Archived:      30 seconds ago

Recovery Range:
  Earliest Point:     2026-07-05 14:00:00 (backup: 20260705_140000)
  Latest Point:       2026-07-05 14:12:00 (last WAL)
  Coverage:           12 minutes
```

## Bước 9: Restore về thời điểm T2 (trước khi xóa data)

```bash
# Restore về T2 (2026-07-05 14:11:00)
dbtool pitr restore \
  --profile pitr_test \
  --to "2026-07-05 14:11:00"
```

**Expected prompts:**
```
Searching for base backup before 2026-07-05 14:11:00...
✓ Selected backup: 20260705_140000 (2026-07-05 14:00:00)

Validating WAL archive coverage...
✓ WAL files: 15 files (240 MB)

⚠ Will OVERWRITE database for profile 'pitr_test'
Continue? [y/N]: y

Recovery Plan:
  Source profile: pitr_test
  Target profile: pitr_test
  Base backup: 20260705_140000 (2026-07-05 14:00:00)
  Target time: 2026-07-05 14:11:00
  Timeline: 1

  Base backup size: 25.5 MB
  WAL files to apply: 15 files (240 MB)
  Estimated time: ~35 seconds

⚠ This will OVERWRITE target database
Continue? [y/N]: y

Stopping PostgreSQL...
Backing up data directory: /var/lib/postgresql/15/main → /var/lib/postgresql/15/main.backup_20260705_141500
Extracting base backup...
Configuring recovery...
Starting PostgreSQL...
Monitoring recovery progress...
Waiting for PostgreSQL to start...
Recovery in progress...
✓ Recovery completed

✓ PITR recovery completed
  Target time: 2026-07-05 14:11:00
  Timeline: 1
  Database: pitr_test
```

## Bước 10: Verify data sau restore

```bash
# Kết nối database
psql -U postgres -d pitr_test
```

```sql
-- Kiểm tra data
SELECT * FROM users ORDER BY id;

-- Expected result (5 users: Alice, Bob, Charlie, David, Eve)
 id |  name   |     created_at
----+---------+---------------------
  1 | Alice   | 2026-07-05 14:10:00
  2 | Bob     | 2026-07-05 14:10:00
  3 | Charlie | 2026-07-05 14:10:00
  4 | David   | 2026-07-05 14:11:00
  5 | Eve     | 2026-07-05 14:11:00
(5 rows)

-- ✓ Data đã được restore về thời điểm T2
-- ✓ Alice, Bob, Charlie không bị xóa
-- ✓ David, Eve vẫn có
-- ✓ DELETE tại T3 không được apply

\q
```

## Bước 11: Test restore về thời điểm T1

```bash
# Restore về T1 (2026-07-05 14:10:00)
dbtool pitr restore \
  --profile pitr_test \
  --to "2026-07-05 14:10:00"
```

**Verify:**
```bash
psql -U postgres -d pitr_test
```

```sql
SELECT * FROM users ORDER BY id;

-- Expected result (3 users: Alice, Bob, Charlie)
 id |  name   |     created_at
----+---------+---------------------
  1 | Alice   | 2026-07-05 14:10:00
  2 | Bob     | 2026-07-05 14:10:00
  3 | Charlie | 2026-07-05 14:10:00
(3 rows)

-- ✓ Chỉ có 3 users ban đầu
-- ✓ David, Eve không có (chưa được insert tại T1)

\q
```

## Bước 12: Test cleanup

```bash
# Tạo thêm 2 base backups nữa
dbtool pitr backup --profile pitr_test
sleep 10
dbtool pitr backup --profile pitr_test

# Xem status (sẽ có 3 backups)
dbtool pitr status --profile pitr_test

# Preview cleanup (dry-run)
dbtool pitr cleanup --profile pitr_test --dry-run
```

**Expected output:**
```
=== PITR Cleanup for Profile: pitr_test ===

Retention Policy:
  Keep base backups:  3
  Keep WAL days:      7

Base Backups:
  Total: 3 (keeping 3, deleting 0)

WAL Archive:
  Total: 45 (deleting 0 older than 7 days)

✓ Nothing to clean up.
```

```bash
# Tạo thêm backup thứ 4
dbtool pitr backup --profile pitr_test

# Cleanup sẽ xóa backup cũ nhất
dbtool pitr cleanup --profile pitr_test --dry-run
```

**Expected output:**
```
Base Backups:
  Total: 4 (keeping 3, deleting 1)

  Backups to DELETE:
    - 20260705_140000 (45 minutes old, 25.5 MB)

Remove --dry-run to perform cleanup.
```

```bash
# Thực sự cleanup
dbtool pitr cleanup --profile pitr_test
```

## Test Cases Bổ Sung

### Test 1: Restore với target-profile khác

```bash
# Tạo profile mới
dbtool profile init
# Profile name: pitr_test_restored
# ... (cùng thông tin, nhưng database name: pitr_test_restored)

# Tạo database mới
createdb -U postgres pitr_test_restored

# Restore sang database khác
dbtool pitr restore \
  --profile pitr_test \
  --target-profile pitr_test_restored \
  --to "2026-07-05 14:10:00"
```

### Test 2: Dry-run mode

```bash
# Preview restore plan
dbtool pitr restore \
  --profile pitr_test \
  --to "2026-07-05 14:10:00" \
  --dry-run
```

### Test 3: Restore với timeline cụ thể

```bash
# Sau khi restore lần đầu, PostgreSQL tạo timeline mới (timeline 2)
# Restore về timeline 1
dbtool pitr restore \
  --profile pitr_test \
  --to "2026-07-05 14:10:00" \
  --timeline 1
```

## Troubleshooting

### Lỗi: "archive_mode is OFF"

```bash
# Sửa postgresql.conf
sudo nano /etc/postgresql/15/main/postgresql.conf

# Thêm/sửa:
archive_mode = on

# Restart PostgreSQL
sudo systemctl restart postgresql
```

### Lỗi: "No base backups found"

```bash
# Tạo base backup
dbtool pitr backup --profile pitr_test
```

### Lỗi: "WAL archive incomplete"

```bash
# Kiểm tra WAL files
ls -lh ~/.config/dbtool/pitr/pitr_test/archive/

# Nếu thiếu, cần chạy backup mới
dbtool pitr backup --profile pitr_test
```

### Lỗi: "Cannot connect to target database"

```bash
# Kiểm tra PostgreSQL đang chạy
sudo systemctl status postgresql

# Start nếu chưa chạy
sudo systemctl start postgresql
```

### Lỗi: "Recovery timeout"

```bash
# Kiểm tra PostgreSQL logs
sudo tail -f /var/log/postgresql/postgresql-15-main.log

# Kiểm tra recovery status
psql -U postgres -d pitr_test -c "SELECT pg_is_in_recovery();"
```

## Checklist

- [ ] PostgreSQL đã cài đặt và chạy
- [ ] `wal_level = replica` trong postgresql.conf
- [ ] `archive_mode = on` trong postgresql.conf
- [ ] Profile đã tạo trong dbtool
- [ ] PITR setup thành công
- [ ] Base backup đầu tiên tạo thành công
- [ ] Data test tạo tại T1, T2, T3
- [ ] Restore về T2 thành công (data đầy đủ)
- [ ] Restore về T1 thành công (chỉ có data ban đầu)
- [ ] Status hiển thị đúng
- [ ] Cleanup hoạt động

## Notes

- **Không test trên production database!**
- Mỗi lần restore, PostgreSQL tạo timeline mới
- WAL files được archive tự động khi PostgreSQL ghi log
- Base backup nên tạo định kỳ (ví dụ: mỗi ngày)
- Retention policy mặc định: giữ 3 backups, 7 ngày WAL
