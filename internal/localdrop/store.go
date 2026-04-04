package localdrop

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var errRecordNotFound = errors.New("record not found")

type Record struct {
	ID          int64      `json:"id"`
	ContentType string     `json:"contentType"`
	ContentBody string     `json:"contentBody"`
	FileName    string     `json:"fileName"`
	MimeType    string     `json:"mimeType"`
	IsTop       bool       `json:"isTop"`
	TopAt       *time.Time `json:"topAt"`
	FileSize    int64      `json:"fileSize"`
	CreatedAt   time.Time  `json:"createdAt"`
}

type StorageUsage struct {
	TotalBytes int64 `json:"totalBytes"`
	DBBytes    int64 `json:"dbBytes"`
	ImageBytes int64 `json:"imageBytes"`
}

type Store struct {
	db        *sql.DB
	dbPath    string
	imagesDir string
}

type imageCandidate struct {
	ID       int64
	FileName string
}

func OpenStore(dbPath, imagesDir string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	if err := os.MkdirAll(imagesDir, 0o755); err != nil {
		return nil, fmt.Errorf("create images dir: %w", err)
	}

	dsn := fmt.Sprintf("file:%s?_busy_timeout=5000&_journal_mode=WAL&_foreign_keys=on", dbPath)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	store := &Store{
		db:        db,
		dbPath:    dbPath,
		imagesDir: imagesDir,
	}

	if err := store.migrate(); err != nil {
		db.Close()
		return nil, err
	}

	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	schema := `
CREATE TABLE IF NOT EXISTS records (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    content_type TEXT NOT NULL,
    content_body TEXT NOT NULL,
    file_name TEXT NOT NULL DEFAULT '',
    mime_type TEXT NOT NULL DEFAULT '',
    is_top BOOLEAN NOT NULL DEFAULT 0,
    top_at DATETIME NULL,
    file_size INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_records_feed
ON records (is_top DESC, top_at DESC, created_at DESC, id DESC);
`

	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}

	hasTopAt, err := s.hasColumn("records", "top_at")
	if err != nil {
		return fmt.Errorf("check schema: %w", err)
	}
	if !hasTopAt {
		if _, err := s.db.Exec(`ALTER TABLE records ADD COLUMN top_at DATETIME NULL`); err != nil {
			return fmt.Errorf("add top_at column: %w", err)
		}
	}

	hasFileName, err := s.hasColumn("records", "file_name")
	if err != nil {
		return fmt.Errorf("check file_name column: %w", err)
	}
	if !hasFileName {
		if _, err := s.db.Exec(`ALTER TABLE records ADD COLUMN file_name TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add file_name column: %w", err)
		}
	}

	hasMimeType, err := s.hasColumn("records", "mime_type")
	if err != nil {
		return fmt.Errorf("check mime_type column: %w", err)
	}
	if !hasMimeType {
		if _, err := s.db.Exec(`ALTER TABLE records ADD COLUMN mime_type TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add mime_type column: %w", err)
		}
	}

	return nil
}

func (s *Store) hasColumn(table, column string) (bool, error) {
	rows, err := s.db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			dataType   string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultVal, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}

	return false, rows.Err()
}

func (s *Store) ListRecords(ctx context.Context) ([]Record, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, content_type, content_body, file_name, mime_type, is_top, top_at, file_size, created_at
FROM records
ORDER BY is_top DESC, top_at IS NULL ASC, top_at DESC, created_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("query records: %w", err)
	}
	defer rows.Close()

	var records []Record
	for rows.Next() {
		record, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}

	return records, rows.Err()
}

func (s *Store) CreateText(ctx context.Context, content string) (Record, error) {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `
INSERT INTO records (content_type, content_body, file_name, mime_type, file_size, created_at)
VALUES ('text', ?, '', 'text/plain; charset=utf-8', 0, ?)`, content, now)
	if err != nil {
		return Record{}, fmt.Errorf("insert text record: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return Record{}, fmt.Errorf("text record id: %w", err)
	}

	return s.GetRecord(ctx, id)
}

func (s *Store) CreateImage(ctx context.Context, relativePath, fileName, mimeType string, size int64) (Record, error) {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `
INSERT INTO records (content_type, content_body, file_name, mime_type, file_size, created_at)
VALUES ('image', ?, ?, ?, ?, ?)`, relativePath, fileName, mimeType, size, now)
	if err != nil {
		return Record{}, fmt.Errorf("insert image record: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return Record{}, fmt.Errorf("image record id: %w", err)
	}

	return s.GetRecord(ctx, id)
}

func (s *Store) CreateFile(ctx context.Context, relativePath, fileName, mimeType string, size int64) (Record, error) {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `
INSERT INTO records (content_type, content_body, file_name, mime_type, file_size, created_at)
VALUES ('file', ?, ?, ?, ?, ?)`, relativePath, fileName, mimeType, size, now)
	if err != nil {
		return Record{}, fmt.Errorf("insert file record: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return Record{}, fmt.Errorf("file record id: %w", err)
	}

	return s.GetRecord(ctx, id)
}

func (s *Store) GetRecord(ctx context.Context, id int64) (Record, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, content_type, content_body, file_name, mime_type, is_top, top_at, file_size, created_at
FROM records WHERE id = ?`, id)

	record, err := scanRecord(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Record{}, errRecordNotFound
		}
		return Record{}, err
	}
	return record, nil
}

func (s *Store) UpdateTopState(ctx context.Context, id int64, isTop bool) (Record, error) {
	var topAt any
	if isTop {
		topAt = time.Now().UTC()
	}

	result, err := s.db.ExecContext(ctx, `
UPDATE records
SET is_top = ?, top_at = ?
WHERE id = ?`, isTop, topAt, id)
	if err != nil {
		return Record{}, fmt.Errorf("update top state: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return Record{}, fmt.Errorf("check top update: %w", err)
	}
	if affected == 0 {
		return Record{}, errRecordNotFound
	}

	return s.GetRecord(ctx, id)
}

func (s *Store) DeleteRecord(ctx context.Context, id int64) (Record, error) {
	record, err := s.GetRecord(ctx, id)
	if err != nil {
		return Record{}, err
	}

	if _, err := s.db.ExecContext(ctx, `DELETE FROM records WHERE id = ?`, id); err != nil {
		return Record{}, fmt.Errorf("delete record: %w", err)
	}

	return record, nil
}

func (s *Store) FindCleanupCandidates(ctx context.Context, before time.Time) ([]imageCandidate, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, content_body
FROM records
WHERE content_type = 'image' AND created_at < ?
ORDER BY created_at ASC, id ASC`, before.UTC())
	if err != nil {
		return nil, fmt.Errorf("query cleanup candidates: %w", err)
	}
	defer rows.Close()

	var items []imageCandidate
	for rows.Next() {
		var item imageCandidate
		if err := rows.Scan(&item.ID, &item.FileName); err != nil {
			return nil, fmt.Errorf("scan cleanup candidate: %w", err)
		}
		items = append(items, item)
	}

	return items, rows.Err()
}

func (s *Store) DeleteRecordsByID(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `DELETE FROM records WHERE id = ?`)
	if err != nil {
		return fmt.Errorf("prepare cleanup delete: %w", err)
	}
	defer stmt.Close()

	for _, id := range ids {
		if _, err := stmt.ExecContext(ctx, id); err != nil {
			return fmt.Errorf("cleanup delete record %d: %w", id, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit cleanup delete: %w", err)
	}
	return nil
}

func (s *Store) ComputeUsage() (StorageUsage, error) {
	dbBytes, err := fileSize(s.dbPath)
	if err != nil {
		return StorageUsage{}, fmt.Errorf("stat db: %w", err)
	}

	imageBytes, err := dirSize(s.imagesDir)
	if err != nil {
		return StorageUsage{}, fmt.Errorf("measure images dir: %w", err)
	}

	return StorageUsage{
		DBBytes:    dbBytes,
		ImageBytes: imageBytes,
		TotalBytes: dbBytes + imageBytes,
	}, nil
}

func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	return info.Size(), nil
}

func dirSize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, os.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		return 0, err
	}
	return total, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRecord(scanner rowScanner) (Record, error) {
	var (
		record     Record
		topAtRaw   sql.NullTime
		createdRaw time.Time
	)

	if err := scanner.Scan(
		&record.ID,
		&record.ContentType,
		&record.ContentBody,
		&record.FileName,
		&record.MimeType,
		&record.IsTop,
		&topAtRaw,
		&record.FileSize,
		&createdRaw,
	); err != nil {
		return Record{}, err
	}

	createdAt := createdRaw.UTC()
	record.CreatedAt = createdAt
	if topAtRaw.Valid {
		top := topAtRaw.Time.UTC()
		record.TopAt = &top
	}

	return record, nil
}
