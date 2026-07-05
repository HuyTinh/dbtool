# Plan chi tiết: Point-in-Time Recovery (PITR) cho dbtool

## 1. Tổng quan

PITR cho phép khôi phục database về một thời điểm cụ thể trong quá khứ (ví dụ: "quay lại 2 giờ trước khi xảy ra lỗi"). Hiện tại, quy trình thủ công rất phức tạp và dễ sai sót.

**Mục tiêu**: Đơn giản hóa PITR thành các lệnh:
- `dbtool pitr setup` — cấu hình WAL archiving cho profile
- `dbtool pitr backup` — chạy base backup
- `dbtool pitr restore --to "2024-01-15 14:30:00"` — restore về thời điểm chỉ định
- `dbtool pitr status` — hiển thị trạng thái PITR
- `dbtool pitr cleanup` — dọn dẹp backups/WAL cũ theo retention policy

---

## 2. Kiến thức nền

### WAL (Write-Ahead Log)
- PostgreSQL ghi mọi thay đổi vào WAL files trước khi commit
- WAL files được lưu trong `pg_wal/` (mỗi file ~16MB)
- Khi `archive_mode = on`, PostgreSQL copy WAL files sang thư mục archive trước khi xóa

### Base Backup
- Snapshot toàn bộ data directory tại thời điểm chạy `pg_basebackup`
- Kết hợp với WAL archive → có thể restore về bất kỳ thời điểm nào sau base backup

### Timeline
- Sau mỗi PITR, PostgreSQL tạo timeline mới (để tránh conflict với WAL cũ)
- Cần track timeline ID khi restore nhiều lần

---

## 3. Prerequisites

### Yêu cầu hệ thống
- PostgreSQL server phải có quyền ghi vào thư mục archive
- Disk space đủ cho WAL archive (tùy workload, có thể 10-100GB/ngày)
- `pg_basebackup` binary trong PATH
- SSH access hoặc shared storage để lưu archive (nếu remote)

### Yêu cầu cấu hình PostgreSQL
- `wal_level = replica` (hoặc `logical`)
- `archive_mode = on`
- `archive_command` — lệnh copy WAL file sang archive dir

---

## 4. Architecture

### Cấu trúc thư mục
```
~/.config/dbtool/
├── profiles.yaml          # Existing
├── history.jsonl          # Existing
└── pitr/                  # New
    └── <profile-name>/
        ├── config.yaml    # WAL archive config
        ├── base/          # Base backups
        │   ├── 20240115_120000/
        │   │   ├── base.tar.gz
        │   │   ├── pg_wal.tar.gz
        │   │   └── metadata.json
        │   └── 20240116_120000/
        └── wal/           # WAL archive
            ├── 000000010000000000000001
            ├── 000000010000000000000002
            └── ...
```

### Data Model

#### PITRConfig

```go
type PITRConfig struct {
    ProfileName     string            `yaml:"profile_name"`
    Driver          string            `yaml:"driver"` // only "postgres"
    ArchiveDir      string            `yaml:"archive_dir"`
    BaseBackupDir   string            `yaml:"base_backup_dir"`
    Retention       RetentionPolicy   `yaml:"retention"`
    CreatedAt       time.Time         `yaml:"created_at"`
    LastBaseBackup  time.Time         `yaml:"last_base_backup"`
}

type RetentionPolicy struct {
    KeepBaseBackups int `yaml:"keep_base_backups"` // default: 3
    KeepWALDays     int `yaml:"keep_wal_days"`     // default: 7
}
```

#### BaseBackup Metadata

```go
type BaseBackupMetadata struct {
    ID            string    `json:"id"`            // Format: 20240115_143000
    StartTime     time.Time `json:"start_time"`
    EndTime       time.Time `json:"end_time"`
    Timeline      int       `json:"timeline"`
    Size          int64     `json:"size"`          // bytes
    WALStart      string    `json:"wal_start"`     // LSN: "0/1000000"
    WALEnd        string    `json:"wal_end"`       // LSN: "0/1500000"
    Checkpoint    string    `json:"checkpoint"`    // LSN
    Format        string    `json:"format"`        // "tar.gz"
    Compressed    bool      `json:"compressed"`
    Duration      int       `json:"duration_sec"`
}
```

#### WAL File Info

```go
type WALFileInfo struct {
    Filename      string    // "000000010000000000000001"
    Timeline      int       // 1
    Log           int       // 0
    Segment       int       // 1
    FullPath      string
    Size          int64
    ModTime       time.Time
}
```

---

## 5. Command: `dbtool pitr setup`

### 5.1 Input

```bash
dbtool pitr setup --profile <name> [flags]

Flags:
  --archive-dir <path>           # Default: ~/.config/dbtool/pitr/<profile>/wal
  --base-backup-dir <path>       # Default: ~/.config/dbtool/pitr/<profile>/base
  --retention-backups <N>        # Default: 3
  --retention-days <N>           # Default: 7
  --dry-run                      # Show what would be configured
```

### 5.2 Processing Steps

#### Step 1: Load và validate profile
```
1.1. Load profiles.yaml
1.2. Tìm profile theo --profile flag
1.3. Validate:
     - Profile tồn tại? → Nếu không: error "Profile 'X' not found"
     - Driver = "postgres"? → Nếu không: error "PITR only supports PostgreSQL"
     - Profile có đầy đủ host, port, user, password, database? → Nếu không: error "Incomplete profile"
```

#### Step 2: Kết nối PostgreSQL và kiểm tra cấu hình hiện tại
```
2.1. Kết nối PG bằng pgx (timeout: 5s)
     - Nếu fail: error "Cannot connect to PostgreSQL: <err>"
     - Suggest: "Kiểm tra host/port/user/password trong profile"

2.2. Query các tham số quan trọng:
     SELECT name, setting FROM pg_settings 
     WHERE name IN ('wal_level', 'archive_mode', 'archive_command', 'data_directory', 'pg_version');
     
     Expected result:
     - wal_level: 'replica' hoặc 'logical'
     - archive_mode: 'on' hoặc 'off'
     - archive_command: string
     - data_directory: path
     - pg_version: int

2.3. Validate wal_level:
     - Nếu wal_level = 'minimal':
       → Warning: "wal_level = minimal, cần set wal_level = replica"
       → Set flag need_restart = true
     - Nếu wal_level = 'replica' hoặc 'logical':
       → OK

2.4. Validate archive_mode:
     - Nếu archive_mode = 'off':
       → Warning: "archive_mode = off, cần set archive_mode = on"
       → Set flag need_restart = true
     - Nếu archive_mode = 'on':
       → OK

2.5. Validate archive_command:
     - Nếu archive_command = '' hoặc '(disabled)':
       → Warning: "archive_command chưa được cấu hình"
       → Set flag need_archive_command = true
     - Nếu archive_command đã có:
       → Parse xem có trỏ đến đúng archive_dir không
       → Nếu không: warning "archive_command trỏ đến thư mục khác"
```

#### Step 3: Tạo thư mục PITR
```
3.1. Xác định paths:
     config_dir = ~/.config/dbtool/pitr/<profile>/
     archive_dir = --archive-dir hoặc ~/.config/dbtool/pitr/<profile>/wal
     base_backup_dir = --base-backup-dir hoặc ~/.config/dbtool/pitr/<profile>/base

3.2. Tạo thư mục:
     - MkdirAll(config_dir, 0755)
       → Nếu fail: error "Cannot create config dir: <err>"
     - MkdirAll(archive_dir, 0755)
       → Nếu fail: error "Cannot create archive dir: <err>"
     - MkdirAll(base_backup_dir, 0755)
       → Nếu fail: error "Cannot create base backup dir: <err>"

3.3. Kiểm tra quyền ghi:
     - Tạo file test: archive_dir/.write_test
     - Ghi "test" → Xóa file
     - Nếu fail: error "Archive directory is not writable"

3.4. Kiểm tra disk space:
     - Query disk free space của archive_dir
     - Nếu free < 10GB:
       → Warning: "Disk space thấp (< 10GB), WAL archive có thể fill disk"
       → Suggest: "Cân nhắc --archive-dir sang disk khác"
```

#### Step 4: Generate archive_command
```
4.1. Determine archive command:
     Nếu OS = Windows:
       archive_command = 'copy "%p" "<archive_dir>\%f"'
     Nếu OS = Linux/macOS:
       archive_command = 'cp %p <archive_dir>/%f'
     
     Note: %p = full path của WAL file, %f = filename only

4.2. Validate archive command:
     - Thử chạy archive_command với 1 file test:
       + Tạo file: archive_dir/test_file
       + Chạy: archive_command (với %p = test_file, %f = test_file)
       + Kiểm tra file đã được copy chưa
       + Xóa file test
     - Nếu fail: error "Archive command test failed: <err>"
```

#### Step 5: Hiển thị kết quả và hướng dẫn
```
5.1. Nếu need_restart = false và need_archive_command = false:
     → "✓ PITR đã được cấu hình cho profile 'X'"
     → "  Archive dir: <archive_dir>"
     → "  Base backup dir: <base_backup_dir>"
     → ""
     → "Chạy base backup đầu tiên:"
     → "  dbtool pitr backup --profile <name>"

5.2. Nếu need_restart = true hoặc need_archive_command = true:
     → "⚠ PostgreSQL cần được cấu hình lại để enable PITR"
     → ""
     → "Thêm vào postgresql.conf (data_directory: <data_dir>):"
     → ""
     → "  wal_level = replica"
     → "  archive_mode = on"
     → "  archive_command = '<archive_command>'"
     → ""
     → "Sau đó restart PostgreSQL:"
     → "  sudo systemctl restart postgresql  # Linux"
     → "  brew services restart postgresql   # macOS"
     → "  net stop postgresql-x64-15 && net start postgresql-x64-15  # Windows"
     → ""
     → "Sau khi restart, chạy:"
     → "  dbtool pitr backup --profile <name>"

5.3. Lưu PITRConfig:
     - Marshal PITRConfig → YAML
     - Write vào config_dir/config.yaml
     - Nếu fail: error "Cannot save PITR config: <err>"

5.4. Nếu --dry-run:
     → Chỉ hiển thị, không lưu config
     → Exit 0
```

### 5.3 Edge Cases

| Case | Handling |
|------|----------|
| Profile không tồn tại | Error rõ ràng, suggest `dbtool profile list` |
| Driver != postgres | Error "PITR only supports PostgreSQL" |
| Không kết nối được PG | Error + suggest kiểm tra profile |
| wal_level = minimal | Warning + hướng dẫn chỉnh postgresql.conf |
| archive_mode = off | Warning + hướng dẫn chỉnh postgresql.conf |
| archive_command đã có nhưng trỏ khác | Warning + hỏi user có muốn override không |
| Không tạo được thư mục | Error + suggest kiểm tra permissions |
| Disk full | Warning + suggest cleanup hoặc đổi archive_dir |
| Archive command test fail | Error + suggest kiểm tra path và permissions |
| User cancel giữa chừng | Cleanup: xóa config.yaml nếu đã tạo |

---

## 6. Command: `dbtool pitr backup`

### 6.1 Input

```bash
dbtool pitr backup --profile <name> [flags]

Flags:
  --jobs <N>                     # Default: 2 (parallel jobs)
  --checkpoint <fast|spread>     # Default: fast
  --no-compress                  # Disable gzip compression
  --dry-run                      # Show what would run
```

### 6.2 Processing Steps

#### Step 1: Load và validate PITR config
```
1.1. Load PITRConfig từ ~/.config/dbtool/pitr/<profile>/config.yaml
     - Nếu file không tồn tại:
       → Error "PITR chưa được setup cho profile 'X'"
       → Suggest: "Chạy: dbtool pitr setup --profile <name>"

1.2. Validate:
     - Archive dir tồn tại và writable? → Nếu không: error
     - Base backup dir tồn tại và writable? → Nếu không: error
```

#### Step 2: Kiểm tra PostgreSQL archive_mode
```
2.1. Kết nối PG, query:
     SELECT setting FROM pg_settings WHERE name = 'archive_mode';

2.2. Nếu archive_mode = 'off':
     → Error "archive_mode = off, WAL archiving chưa được enable"
     → Suggest: "Chỉnh postgresql.conf và restart PostgreSQL"
     → Suggest: "Xem lại: dbtool pitr setup --profile <name>"

2.3. Kiểm tra WAL archive có hoạt động không:
     - List files trong archive_dir
     - Tìm file WAL mới nhất (theo modtime)
     - Nếu không có file nào:
       → Warning "Chưa có WAL file nào trong archive, archive_command có thể chưa hoạt động"
       → Suggest: "Kiểm tra archive_command trong postgresql.conf"
     - Nếu có file:
       → Kiểm tra file mới nhất có modtime < 1h không
       → Nếu > 1h: warning "WAL file cũ nhất là <X> giờ trước, archive có thể bị delay"
```

#### Step 3: Generate backup ID và validate
```
3.1. Generate backup ID:
     format = "20060102_150405" (YYYYMMDD_HHMMSS)
     backup_id = time.Now().Format(format)

3.2. Kiểm tra trùng:
     - Path: base_backup_dir/<backup_id>
     - Nếu tồn tại: error "Backup ID đã tồn tại, chờ 1s và retry"
     - Retry tối đa 3 lần

3.3. Tạo thư mục backup:
     - MkdirAll(base_backup_dir/<backup_id>, 0755)
     - Nếu fail: error "Cannot create backup dir: <err>"
```

#### Step 4: Kiểm tra disk space
```
4.1. Estimate backup size:
     - Query PG: SELECT pg_database_size(current_database());
     - Estimate compressed size = db_size * 0.3 (nếu compress)
     - Estimate uncompressed size = db_size

4.2. Kiểm tra disk free:
     - Query disk free của base_backup_dir
     - Nếu free < estimate * 1.2:
       → Error "Không đủ disk space (cần ~<X>GB, có <Y>GB)"
       → Suggest: "Cleanup base backups cũ: dbtool pitr cleanup --profile <name>"
```

#### Step 5: Chạy pg_basebackup
```
5.1. Build command:
     args = [
       "-h", profile.Host,
       "-p", fmt.Sprint(profile.Port),
       "-U", profile.User,
       "-D", base_backup_dir/<backup_id>,
       "-Ft",                    # tar format
       "-Xfetch",                # fetch WAL during backup
       "-P",                     # progress
       "--checkpoint=" + checkpoint,
     ]
     
     Nếu !no-compress:
       args = append(args, "-z")  # gzip
     
     Nếu jobs > 1:
       args = append(args, "-j", fmt.Sprint(jobs))

5.2. Set environment:
     env = os.Environ()
     env = append(env, "PGPASSWORD=" + profile.Password)

5.3. Execute pg_basebackup:
     cmd = exec.Command("pg_basebackup", args...)
     cmd.Env = env
     cmd.Stdout = os.Stdout (nếu verbose)
     cmd.Stderr = capture stderr
     
     start_time = time.Now()
     err = cmd.Run()
     duration = time.Since(start_time)

5.4. Parse progress (nếu không verbose):
     - pg_basebackup output: "123456/123456 kB (100%), 1/1 tablespace"
     - Parse để hiển thị progress bar

5.5. Handle errors:
     - Nếu err != nil:
       → Cleanup: xóa thư mục <backup_id>
       → Parse stderr để tìm lỗi cụ thể:
         + "could not connect": error kết nối
         + "permission denied": error permissions
         + "no space left": error disk full
         + "archive_mode": error archive chưa setup
       → Error với message rõ ràng
```

#### Step 6: Parse metadata từ backup
```
6.1. Đọc file backup_label (trong tar):
     - Nếu compress: gunzip trước
     - Extract backup_label từ tar
     - Parse các dòng:
       START WAL LOCATION: 0/1000000 (file 000000010000000000000001)
       CHECKPOINT LOCATION: 0/1000000
       BACKUP METHOD: streamed
       BACKUP FROM: primary
       START TIME: 2024-01-15 14:30:00 UTC
       LABEL: pg_basebackup base backup
       START TIMELINE: 1

6.2. Extract metadata:
     - WALStart: "0/1000000"
     - Checkpoint: "0/1000000"
     - StartTime: parse từ "START TIME"
     - Timeline: parse từ "START TIMELINE"

6.3. Tính WALEnd:
     - List files trong backup dir: base.tar.gz, pg_wal.tar.gz
     - Nếu có pg_wal.tar.gz:
       + Extract pg_wal.tar.gz
       + List WAL files trong đó
       + Sort theo filename
       + WALEnd = filename cuối cùng (ví dụ: "000000010000000000000005")
     - Nếu không có pg_wal.tar.gz:
       + WALEnd = WALStart (assume no WAL generated during backup)

6.4. Tính size:
     - Stat base.tar.gz + pg_wal.tar.gz (nếu có)
     - Size = tổng size

6.5. Tạo BaseBackupMetadata:
     metadata = BaseBackupMetadata{
       ID: backup_id,
       StartTime: start_time,
       EndTime: time.Now(),
       Timeline: timeline,
       Size: size,
       WALStart: wal_start,
       WALEnd: wal_end,
       Checkpoint: checkpoint,
       Format: "tar.gz" (hoặc "tar" nếu no-compress),
       Compressed: !no-compress,
       Duration: int(duration.Seconds()),
     }

6.6. Lưu metadata:
     - Marshal metadata → JSON
     - Write vào base_backup_dir/<backup_id>/metadata.json
     - Nếu fail: warning "Cannot save metadata: <err>" (không fail cả backup)
```

#### Step 7: Apply retention policy
```
7.1. List base backups:
     - ReadDir(base_backup_dir)
     - Filter các thư mục có tên match format "20060102_150405"
     - Sort theo tên (descending, mới nhất trước)

7.2. Xóa backups vượt quá retention:
     Nếu len(backups) > retention.keep_base_backups:
       - backups_to_delete = backups[retention.keep_base_backups:]
       - For each backup in backups_to_delete:
         + Print "Xóa base backup cũ: <backup_id>"
         + RemoveAll(base_backup_dir/<backup_id>)
         + Nếu fail: warning "Cannot delete old backup: <err>"
```

#### Step 8: Output kết quả
```
8.1. Success message:
     "✓ Base backup completed: <backup_id>"
     "  Size: <size> (compressed)"
     "  WAL range: <wal_start> - <wal_end>"
     "  Timeline: <timeline>"
     "  Duration: <duration>s"
     ""
     "Recovery range:"
     "  Earliest: <start_time>"
     "  Latest: <latest_wal_time> (from WAL archive)"
```

### 6.3 Edge Cases

| Case | Handling |
|------|----------|
| PITR config không tồn tại | Error + suggest setup |
| archive_mode = off | Error + suggest restart PG |
| WAL archive không hoạt động | Warning + suggest kiểm tra archive_command |
| Backup ID trùng | Retry với timestamp mới (tối đa 3 lần) |
| Không đủ disk space | Error + suggest cleanup |
| pg_basebackup không tồn tại | Error "pg_basebackup not found in PATH" |
| pg_basebackup fail | Parse stderr, error rõ ràng, cleanup |
| Connection timeout | Error + suggest kiểm tra profile |
| Không parse được backup_label | Warning, dùng default values |
| Không tìm thấy pg_wal.tar.gz | Warning, WALEnd = WALStart |
| Metadata save fail | Warning, không fail cả backup |
| Retention cleanup fail | Warning, không fail cả backup |

---

## 7. Command: `dbtool pitr restore`

### 7.1 Input

```bash
dbtool pitr restore --profile <name> --to <timestamp> [flags]

Flags:
  --to <timestamp>               # Required: "2024-01-15 14:30:00"
  --target-profile <name>        # Optional: restore sang profile khác
  --timeline <N>                 # Optional: specify timeline (default: latest)
  --create-if-missing            # Optional: tạo DB đích nếu chưa có
  --clean                        # Optional: drop objects trước khi restore
  --dry-run                      # Show plan without executing
```

### 7.2 Processing Steps

#### Step 1: Parse và validate input
```
1.1. Parse --to timestamp:
     - Thử parse với format "2006-01-02 15:04:05"
     - Nếu fail: thử format "2006-01-02T15:04:05"
     - Nếu fail: thử format "2006-01-02"
     - Nếu vẫn fail: error "Invalid timestamp format: <to>"
     - Suggest: "Format: '2024-01-15 14:30:00'"

1.2. Validate timestamp:
     - Nếu target_time > time.Now():
       → Error "Target time ở tương lai: <to>"
     - Nếu target_time < time.Now().AddDate(-10, 0, 0):
       → Warning "Target time quá cũ (> 10 năm), kiểm tra lại"

1.3. Load PITRConfig:
     - Load từ ~/.config/dbtool/pitr/<profile>/config.yaml
     - Nếu không tồn tại: error "PITR chưa được setup cho profile 'X'"
```

#### Step 2: Tìm base backup phù hợp
```
2.1. List base backups:
     - ReadDir(base_backup_dir)
     - Filter các thư mục match format "20060102_150405"
     - Sort theo tên (descending, mới nhất trước)

2.2. Nếu không có backup nào:
     → Error "Không có base backup nào"
     → Suggest: "Chạy: dbtool pitr backup --profile <name>"

2.3. Tìm base backup phù hợp:
     For each backup (từ mới nhất đến cũ nhất):
       - Load metadata.json
       - Nếu metadata.StartTime <= target_time:
         → Đây là base backup phù hợp
         → Break
     
     Nếu không tìm được:
       → Error "Không có base backup nào trước <target_time>"
       → List các backup có sẵn với StartTime
       → Suggest: "Chạy backup mới hoặc chọn target_time sau <earliest_backup_time>"

2.4. Validate WAL range:
     - Base backup có WALStart và WALEnd
     - Cần WAL từ WALEnd đến target_time
     - List WAL files trong archive_dir:
       + Filter files match pattern "000000010000000000000001" (24 hex chars)
       + Sort theo filename
       + Tìm WAL file đầu tiên >= WALEnd
       + Tìm WAL file cuối cùng có timeline = base.Timeline
     
     - Nếu không có WAL file nào >= WALEnd:
       → Error "WAL archive không có files từ <WALEnd>, không thể restore"
     
     - Estimate WAL coverage:
       + Parse WAL filename cuối cùng để lấy LSN
       + Nếu LSN cuối < target_time (estimate):
         → Warning "WAL archive có thể không cover đến <target_time>"
         → Suggest: "Kiểm tra lại target_time hoặc chạy backup mới"
```

#### Step 3: Xác định target profile
```
3.1. Nếu --target-profile không set:
     → target_profile = source_profile (restore in-place)
     → Warning "Sẽ OVERWRITE database hiện tại của profile 'X'"
     → Prompt confirm: "Continue? [y/N]"
     → Nếu user cancel: exit 0

3.2. Nếu --target-profile set:
     - Load profile từ profiles.yaml
     - Nếu không tồn tại: error "Target profile 'X' not found"
     - Nếu target_profile.Driver != source_profile.Driver:
       → Error "Cross-driver PITR not supported"
     - Nếu target_profile.Name == source_profile.Name:
       → Error "Target profile phải khác source profile"
```

#### Step 4: Safety check trên target
```
4.1. Kết nối target DB (timeout: 5s):
     - Nếu fail và --create-if-missing:
       → Gọi EnsureDatabaseExists(target_profile)
       → Retry connect
       → Nếu vẫn fail: error "Cannot connect to target DB: <err>"
     - Nếu fail và không --create-if-missing:
       → Error "Cannot connect to target DB: <err>"
       → Suggest: "Dùng --create-if-missing để tạo DB"

4.2. Kiểm tra DB có data không:
     - Query: SELECT count(*) FROM information_schema.tables WHERE table_schema NOT IN ('pg_catalog', 'information_schema');
     - Nếu count > 0:
       → Nếu --clean:
         + Warning "⚠ --clean sẽ XÓA TOÀN BỘ objects trong target DB"
         + Prompt confirm: "Continue? [y/N]"
       → Nếu không --clean:
         + Warning "Target DB đã có data (<count> tables)"
         + Prompt confirm: "Overwrite? [y/N]"
     - Nếu prompt cancel: exit 0

4.3. Kiểm tra PostgreSQL version:
     - Query source: SELECT version();
     - Query target: SELECT version();
     - Nếu target major version < source major version:
       → Error "Target PG version (<X>) < Source PG version (<Y>), không compatible"
     - Nếu target major version > source major version:
       → Warning "Target PG version khác source, có thể có issues"
       → Prompt confirm: "Continue? [y/N]"
```

#### Step 5: Hiển thị recovery plan
```
5.1. Tính estimate:
     - Base backup size: <size>
     - WAL files to apply: count files từ WALEnd đến target_time
     - WAL size: sum size của WAL files
     - Estimated time: (base_size + wal_size) / (50MB/s) + 30s overhead

5.2. Display plan:
     "Recovery Plan:"
     "  Source profile: <source_profile>"
     "  Target profile: <target_profile>"
     "  Base backup: <backup_id> (<start_time>)"
     "  Target time: <target_time>"
     "  Timeline: <timeline>"
     ""
     "  Base backup size: <size>"
     "  WAL files to apply: <count> files (<wal_size>)"
     "  Estimated time: <estimate>"
     ""
     "⚠ This will OVERWRITE target database"
     "Continue? [y/N]"
     
     Nếu user cancel: exit 0
```

#### Step 6: Stop PostgreSQL target (nếu running)
```
6.1. Kiểm tra PG có đang running không:
     - Thử connect
     - Nếu connect OK: PG đang running
     - Nếu connect fail: PG có thể đã stop

6.2. Nếu PG đang running:
     - Warning "PostgreSQL đang running, cần stop trước khi restore"
     - Prompt: "Stop PostgreSQL? [y/N]"
     - Nếu user confirm:
       + Detect OS:
         * Linux: sudo systemctl stop postgresql
         * macOS: brew services stop postgresql
         * Windows: net stop postgresql-x64-<version>
       + Chờ 5s
       + Verify đã stop (thử connect lại)
       + Nếu vẫn running: error "Cannot stop PostgreSQL, please stop manually"
     - Nếu user cancel: exit 0

6.3. Backup data directory hiện tại (safety):
     - data_dir = query pg_settings WHERE name = 'data_directory'
     - backup_dir = data_dir + ".backup_" + time.Now().Format("20060102_150405")
     - Print "Backing up current data directory: <data_dir> → <backup_dir>"
     - Rename data_dir → backup_dir
       + Nếu fail: error "Cannot backup data directory: <err>"
       + Suggest: "Kiểm tra permissions"
```

#### Step 7: Extract base backup
```
7.1. Tạo data directory mới:
     - MkdirAll(data_dir, 0700)
     - Nếu fail: error "Cannot create data directory: <err>"

7.2. Extract base.tar.gz:
     - Path: base_backup_dir/<backup_id>/base.tar.gz
     - Nếu compress:
       + gunzip base.tar.gz → base.tar
     - Extract base.tar vào data_dir:
       + tar -xf base.tar -C data_dir
     - Nếu fail:
       + Cleanup: xóa data_dir
       + Restore: rename backup_dir → data_dir
       + Error "Cannot extract base backup: <err>"

7.3. Set permissions:
     - chmod 0700 data_dir
     - chown postgres:postgres data_dir (nếu Linux)
     - Nếu fail: warning "Cannot set permissions: <err>"
```

#### Step 8: Configure recovery
```
8.1. Detect PostgreSQL version:
     - Query pg_settings hoặc parse pg_ctl --version
     - version_major = 12, 13, 14, 15, etc.

8.2. Tạo recovery config:
     Nếu version_major >= 12:
       - Tạo file: data_dir/recovery.signal (empty file)
       - Chỉnh data_dir/postgresql.auto.conf:
         restore_command = 'cp <archive_dir>/%f %p'  # Linux
         restore_command = 'copy "<archive_dir>\%f" "%p"'  # Windows
         recovery_target_time = '<target_time>'
         recovery_target_action = 'promote'
     
     Nếu version_major < 12:
       - Tạo file: data_dir/recovery.conf
         restore_command = 'cp <archive_dir>/%f %p'
         recovery_target_time = '<target_time>'
         recovery_target_action = 'promote'

8.3. Validate recovery config:
     - Kiểm tra file đã tạo chưa
     - Parse lại để verify syntax
     - Nếu fail: error "Cannot configure recovery: <err>"
```

#### Step 9: Start PostgreSQL
```
9.1. Start PostgreSQL:
     - Detect OS:
       * Linux: sudo systemctl start postgresql
       * macOS: brew services start postgresql
       * Windows: net start postgresql-x64-<version>
     - Nếu fail: error "Cannot start PostgreSQL: <err>"

9.2. Chờ PostgreSQL ready:
     - Poll mỗi 2s, tối đa 60s:
       + Thử connect
       + Nếu OK: break
       + Nếu fail: continue
     - Nếu timeout: error "PostgreSQL không ready sau 60s"

9.3. Kiểm tra recovery mode:
     - Query: SELECT pg_is_in_recovery();
     - Nếu = true: đang recovery → OK
     - Nếu = false: đã recovery xong (nhanh) → OK
```

#### Step 10: Monitor recovery progress
```
10.1. Poll recovery status:
      Loop (mỗi 2s, tối đa 30 phút):
        - Query: SELECT pg_is_in_recovery();
        - Nếu = false:
          → Recovery xong → break
        
        - Query recovery progress (nếu có):
          SELECT 
            pg_wal_lsn_diff(pg_current_wal_lsn(), '<wal_start>') AS applied,
            pg_wal_lsn_diff('<wal_end>', '<wal_start>') AS total;
          
          progress = applied / total * 100
          Print progress bar
        
        - Kiểm tra error trong PostgreSQL log:
          + Tail log file (nếu biết path)
          + Tìm "FATAL", "PANIC", "could not"
          + Nếu có error: warning + continue (PG có thể tự retry)

10.2. Nếu timeout (30 phút):
      → Error "Recovery timeout (> 30 phút)"
      → Suggest: "Kiểm tra PostgreSQL log"
```

#### Step 11: Verify recovery
```
11.1. Kiểm tra timeline mới:
      - Query: SELECT timeline_id FROM pg_control_checkpoint();
      - Nếu timeline > base_timeline:
        → OK, timeline mới đã tạo
      - Nếu timeline = base_timeline:
        → Warning "Timeline không thay đổi, có thể recovery chưa hoàn tất"

11.2. Kiểm tra current time:
      - Query: SELECT now();
      - Nếu now() < target_time (chênh lệch > 1 phút):
        → Warning "Database time < target time, recovery có thể chưa đến đích"
      - Nếu now() > target_time (chênh lệch > 5 phút):
        → Warning "Database time > target time, recovery đã vượt quá đích"

11.3. Verify data integrity:
      - Query: SELECT count(*) FROM information_schema.tables;
      - Nếu count = 0:
        → Error "Database empty sau recovery, có thể recovery fail"
      - Nếu count > 0:
        → OK
```

#### Step 12: Cleanup và record history
```
12.1. Xóa backup data directory cũ:
      - Print "Cleanup backup: <backup_dir>"
      - RemoveAll(backup_dir)
      - Nếu fail: warning "Cannot cleanup backup: <err>"

12.2. Record history:
      - historyRec = HistoryRecord{
          File: base_backup_dir/<backup_id>,
          Profile: target_profile.Name,
          Time: time.Now(),
          Success: true,
          Command: "pitr restore: base=<backup_id>, target=<target_time>",
        }
      - AppendHistory(historyRec)
      - Nếu fail: warning "Cannot record history: <err>"

12.3. Success message:
      "✓ PITR recovery completed"
      "  Target time: <target_time>"
      "  New timeline: <timeline>"
      "  Database: <target_profile.Database>"
      ""
      "Verify data:"
      "  psql -h <host> -p <port> -U <user> -d <database>"
```

### 7.3 Edge Cases

| Case | Handling |
|------|----------|
| Timestamp format sai | Error + suggest formats |
| Target time ở tương lai | Error rõ ràng |
| PITR config không tồn tại | Error + suggest setup |
| Không có base backup | Error + suggest backup |
| Không có base backup trước target_time | Error + list backups có sẵn |
| WAL archive không cover target_time | Error + suggest backup mới |
| Target profile không tồn tại | Error rõ ràng |
| Cross-driver PITR | Error rõ ràng |
| Không kết nối được target DB | Error + suggest --create-if-missing |
| Target DB có data, không --clean | Prompt confirm overwrite |
| Target PG version < source | Error rõ ràng |
| Không stop được PostgreSQL | Error + suggest stop manual |
| Không backup được data directory | Error + suggest check permissions |
| Không extract được base backup | Cleanup + restore backup + error |
| Không configure được recovery | Error rõ ràng |
| Không start được PostgreSQL | Error rõ ràng |
| Recovery timeout | Error + suggest check log |
| Timeline không thay đổi | Warning |
| Database time != target time | Warning |
| Database empty sau recovery | Error rõ ràng |
| Cleanup backup fail | Warning |
| Record history fail | Warning |

---

## 8. Command: `dbtool pitr status`

### 8.1 Input

```bash
dbtool pitr status --profile <name>
```

### 8.2 Processing Steps

```
1. Load PITRConfig
   - Nếu không tồn tại: error "PITR chưa được setup"

2. Kiểm tra archive_mode:
   - Connect PG, query archive_mode
   - Display: "Archive mode: ✓ enabled" hoặc "✗ disabled"

3. List base backups:
   - ReadDir, filter, sort
   - Load metadata cho mỗi backup
   - Display table: ID, Time, Size, Timeline

4. Tính WAL archive stats:
   - List WAL files
   - Count files
   - Sum size
   - Tìm file mới nhất, cũ nhất
   - Display: "WAL archive: <count> files, <size>, covers <days> days"

5. Tính recovery range:
   - Earliest = base backup cũ nhất.StartTime
   - Latest = WAL file mới nhất.ModTime (estimate)
   - Display: "Recovery range: <earliest> - <latest>"

6. Display timeline hiện tại:
   - Query pg_control_checkpoint()
   - Display: "Current timeline: <N>"
```

---

## 9. Command: `dbtool pitr cleanup`

### 9.1 Input

```bash
dbtool pitr cleanup --profile <name> [flags]

Flags:
  --dry-run                      # Show what would delete
  --force                        # Skip confirmation
```

### 9.2 Processing Steps

```
1. Load PITRConfig

2. Cleanup base backups:
   - List backups, sort by name (descending)
   - backups_to_delete = backups[retention.keep_base_backups:]
   - Display list sẽ xóa
   - Nếu !dry-run && !force:
     → Prompt confirm
   - Xóa từng backup
   - Report: "Deleted <N> base backups, freed <size>"

3. Cleanup WAL files:
   - cutoff_time = time.Now().AddDate(0, 0, -retention.keep_wal_days)
   - Tìm base backup cũ nhất còn giữ
   - cutoff_wal = base_oldest.WALStart (không xóa WAL trước cutoff_wal)
   - List WAL files, filter:
     + ModTime < cutoff_time
     + Filename < cutoff_wal (parse LSN)
   - Display list sẽ xóa
   - Nếu !dry-run && !force:
     → Prompt confirm
   - Xóa từng file
   - Report: "Deleted <N> WAL files, freed <size>"

4. Summary:
   "Cleanup completed:"
   "  Base backups: deleted <N>, kept <M>"
   "  WAL files: deleted <N>, kept <M>"
   "  Total freed: <size>"
```

---

## 10. Implementation Phases

### Phase 1: Core infrastructure (3 giờ)
1. Tạo `internal/pitr/config.go` — PITRConfig, RetentionPolicy, BaseBackup structs
2. Tạo `internal/pitr/config.go` — Load/Save PITRConfig
3. Tạo `internal/pitr/wal.go` — parse WAL filename, validate WAL range
4. Tạo `internal/pitr/basebackup.go` — wrapper cho `pg_basebackup`

### Phase 2: CLI commands (4 giờ)
1. `cmd/pitr.go` — root command + subcommands
2. `cmd/pitr_setup.go` — setup logic
3. `cmd/pitr_backup.go` — backup logic
4. `cmd/pitr_restore.go` — restore logic (phức tạp nhất)
5. `cmd/pitr_status.go` — status logic
6. `cmd/pitr_cleanup.go` — cleanup logic

### Phase 3: PostgreSQL integration (3 giờ)
1. `internal/driver/postgres/pitr.go` — implement PITR-specific methods
   - `SetupArchive(config)` — generate archive_command, check wal_level
   - `RunBaseBackup(ctx, opts)` — gọi pg_basebackup, parse output
   - `RestoreBase(backupPath, dataDir)` — extract base backup
   - `ConfigureRecovery(targetTime, restoreCommand)` — write recovery config
   - `WaitRecovery(ctx)` — poll pg_is_in_recovery()
   - `GetCurrentTimeline()` — query pg_control_checkpoint()

### Phase 4: Testing & edge cases (2 giờ)
1. Test trên PG 12, 13, 14, 15 (recovery.signal vs recovery.conf)
2. Handle case: WAL archive incomplete
3. Handle case: base backup fail giữa chừng
4. Handle case: target DB đang running (cần stop trước)
5. Handle case: disk full trong quá trình restore
6. Handle timeline conflicts

---

## 11. Testing Strategy

### Unit tests
- Parse WAL filename
- Validate WAL range
- Retention policy logic

### Integration tests (cần PG instance)
1. Setup PITR trên test DB
2. Insert data tại T1, T2, T3
3. Restore về T2 → verify data = T2 state
4. Verify timeline mới đã tạo
5. Verify WAL archive vẫn intact

### Manual test scenarios
1. Restore về thời điểm trước khi table bị drop
2. Restore về thời điểm trước khi DELETE nhầm
3. Restore sau khi base backup đã cũ (cần apply nhiều WAL)
4. Restore với target-profile khác profile nguồn

---

## 12. Future Enhancements

1. **Auto backup schedule** — chạy `pitr backup` tự động theo cron (ví dụ: mỗi ngày 2h sáng)
2. **Remote archive storage** — sync WAL archive sang S3/GCS
3. **TUI integration** — thêm menu "PITR" trong TUI để setup/restore
4. **Incremental base backup** — chỉ backup changes từ base trước (dùng `pg_basebackup --incremental`)
5. **Point-in-time preview** — hiển thị SQL queries tại thời điểm target (nếu có log)

---

## 13. Summary

**Tổng effort**: 12-15 giờ

**Coverage**: ~90% edge cases được handle với error messages rõ ràng và recovery mechanisms.

**Safety mechanisms**:
- Backup data directory trước khi restore
- Prompt confirm trước các operation nguy hiểm
- Cleanup tự động khi fail
- Validation ở mỗi bước
- Error messages rõ ràng với suggest

**Giá trị**:
- Giảm thời gian PITR từ 2-4 giờ xuống 5-10 phút
- Giảm rủi ro mất data do sai sót thủ công
- Feature differentiate dbtool với các tool khác

**Rủi ro**:
- Cần SSH access hoặc shared storage cho archive (không phải ai cũng có)
- WAL archive có thể rất lớn (cần retention policy tốt)
- Recovery fail nếu WAL corrupt (cần backup strategy dự phòng)
