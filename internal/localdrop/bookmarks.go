package localdrop

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type Bookmark struct {
	ID         int64     `json:"id"`
	Title      string    `json:"title"`
	URL        string    `json:"url"`
	FolderPath string    `json:"folderPath"`
	SortOrder  int       `json:"sortOrder"`
	CreatedAt  time.Time `json:"createdAt"`
}

type bookmarkImportItem struct {
	Title      string
	URL        string
	FolderPath string
	SortOrder  int
}

var htmlTagPattern = regexp.MustCompile(`(?is)<[^>]+>`)

func (s *Store) ListBookmarks(ctx context.Context) ([]Bookmark, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, title, url, folder_path, sort_order, created_at
FROM bookmarks
ORDER BY sort_order ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("query bookmarks: %w", err)
	}
	defer rows.Close()

	var bookmarks []Bookmark
	for rows.Next() {
		var (
			item       Bookmark
			createdRaw time.Time
		)
		if err := rows.Scan(&item.ID, &item.Title, &item.URL, &item.FolderPath, &item.SortOrder, &createdRaw); err != nil {
			return nil, fmt.Errorf("scan bookmark: %w", err)
		}
		item.CreatedAt = createdRaw.UTC()
		bookmarks = append(bookmarks, item)
	}

	return bookmarks, rows.Err()
}

func (s *Store) ReplaceBookmarks(ctx context.Context, items []bookmarkImportItem) (int, time.Time, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("begin bookmark sync tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM bookmarks`); err != nil {
		return 0, time.Time{}, fmt.Errorf("clear bookmarks: %w", err)
	}

	syncedAt := time.Now().UTC()
	if len(items) > 0 {
		stmt, err := tx.PrepareContext(ctx, `
INSERT INTO bookmarks (title, url, folder_path, sort_order, created_at)
VALUES (?, ?, ?, ?, ?)`)
		if err != nil {
			return 0, time.Time{}, fmt.Errorf("prepare bookmark insert: %w", err)
		}
		defer stmt.Close()

		for _, item := range items {
			if _, err := stmt.ExecContext(ctx, item.Title, item.URL, item.FolderPath, item.SortOrder, syncedAt); err != nil {
				return 0, time.Time{}, fmt.Errorf("insert bookmark %q: %w", item.URL, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, time.Time{}, fmt.Errorf("commit bookmark sync tx: %w", err)
	}

	return len(items), syncedAt, nil
}

func ParseBookmarksHTML(data []byte) ([]bookmarkImportItem, error) {
	input := string(data)
	if strings.TrimSpace(input) == "" {
		return nil, errors.New("bookmark html is empty")
	}

	matches := htmlTagPattern.FindAllStringIndex(input, -1)
	if len(matches) == 0 {
		return nil, errors.New("invalid bookmark html")
	}

	var (
		items         []bookmarkImportItem
		folderStack   []string
		pendingFolder string
		currentHref   string
		lastEnd       int
		order         int
		inAnchor      bool
		inFolderTitle bool
		anchorText    strings.Builder
		folderText    strings.Builder
	)

	for _, match := range matches {
		if match[0] > lastEnd {
			text := input[lastEnd:match[0]]
			if inAnchor {
				anchorText.WriteString(text)
			}
			if inFolderTitle {
				folderText.WriteString(text)
			}
		}

		tag := input[match[0]:match[1]]
		name, closing := htmlTagName(tag)
		switch name {
		case "a":
			if closing {
				if inAnchor {
					title := cleanBookmarkText(anchorText.String())
					link := normalizeBookmarkURL(currentHref)
					if link != "" {
						if title == "" {
							title = link
						}
						items = append(items, bookmarkImportItem{
							Title:      title,
							URL:        link,
							FolderPath: strings.Join(folderStack, " / "),
							SortOrder:  order,
						})
						order++
					}
				}
				inAnchor = false
				currentHref = ""
				anchorText.Reset()
				break
			}
			inAnchor = true
			currentHref = htmlTagAttr(tag, "href")
			anchorText.Reset()
		case "h3":
			if closing {
				if inFolderTitle {
					pendingFolder = cleanBookmarkText(folderText.String())
				}
				inFolderTitle = false
				folderText.Reset()
				break
			}
			inFolderTitle = true
			folderText.Reset()
		case "dl":
			if closing {
				if len(folderStack) > 0 {
					folderStack = folderStack[:len(folderStack)-1]
				}
				break
			}
			if pendingFolder != "" {
				folderStack = append(folderStack, pendingFolder)
				pendingFolder = ""
			}
		}

		lastEnd = match[1]
	}

	if len(items) == 0 {
		return nil, errors.New("no http bookmarks found in html export")
	}

	return items, nil
}

func htmlTagName(tag string) (string, bool) {
	tag = strings.TrimSpace(tag)
	tag = strings.TrimPrefix(tag, "<")
	tag = strings.TrimSuffix(tag, ">")
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return "", false
	}
	if strings.HasPrefix(tag, "!") || strings.HasPrefix(tag, "?") {
		return "", false
	}

	closing := false
	if strings.HasPrefix(tag, "/") {
		closing = true
		tag = strings.TrimSpace(strings.TrimPrefix(tag, "/"))
	}

	end := 0
	for end < len(tag) {
		ch := tag[end]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') {
			end++
			continue
		}
		break
	}
	if end == 0 {
		return "", closing
	}
	return strings.ToLower(tag[:end]), closing
}

func htmlTagAttr(tag, name string) string {
	pattern := regexp.MustCompile(`(?is)\b` + regexp.QuoteMeta(name) + `\s*=\s*("([^"]*)"|'([^']*)'|([^\s>]+))`)
	match := pattern.FindStringSubmatch(tag)
	if len(match) == 0 {
		return ""
	}

	for _, value := range match[2:] {
		if value != "" {
			return html.UnescapeString(strings.TrimSpace(value))
		}
	}
	return ""
}

func cleanBookmarkText(value string) string {
	parts := strings.Fields(html.UnescapeString(value))
	return strings.TrimSpace(strings.Join(parts, " "))
}

func normalizeBookmarkURL(raw string) string {
	raw = strings.TrimSpace(html.UnescapeString(raw))
	if raw == "" {
		return ""
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	if parsed.Host == "" {
		return ""
	}
	return parsed.String()
}

func bookmarkSyncTime(ctx context.Context, db *sql.DB) (*time.Time, error) {
	var syncedAt sql.NullTime
	if err := db.QueryRowContext(ctx, `
SELECT created_at
FROM bookmarks
ORDER BY created_at DESC, id DESC
LIMIT 1`).Scan(&syncedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("query bookmark sync time: %w", err)
	}
	if !syncedAt.Valid {
		return nil, nil
	}
	value := syncedAt.Time.UTC()
	return &value, nil
}
