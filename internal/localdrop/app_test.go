package localdrop

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRecordLifecycleAndStorage(t *testing.T) {
	app := newTestApp(t)

	imageRecord := uploadTestImage(t, app, "sample.png", tinyPNG)
	textRecord := postTextRecord(t, app, "hello from clipboard")

	storage := getStorage(t, app)
	if storage.ImageBytes <= 0 {
		t.Fatalf("expected image bytes to be tracked, got %+v", storage)
	}
	if storage.TotalBytes < storage.ImageBytes {
		t.Fatalf("expected total bytes to include image bytes, got %+v", storage)
	}

	toggleTop(t, app, imageRecord.ID, true)
	records := listRecords(t, app)
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	if records[0].ID != imageRecord.ID || !records[0].IsTop {
		t.Fatalf("expected pinned image first, got %+v", records[0])
	}
	if records[1].ID != textRecord.ID {
		t.Fatalf("expected text record second, got %+v", records[1])
	}

	imagePath := filepath.Join(app.cfg.ImagesDir, imageRecord.ContentBody)
	if _, err := os.Stat(imagePath); err != nil {
		t.Fatalf("expected image file to exist before delete: %v", err)
	}

	deleteRecord(t, app, imageRecord.ID)
	if _, err := os.Stat(imagePath); !os.IsNotExist(err) {
		t.Fatalf("expected image file to be removed, got err=%v", err)
	}

	records = listRecords(t, app)
	if len(records) != 1 || records[0].ID != textRecord.ID {
		t.Fatalf("expected only text record after delete, got %+v", records)
	}
}

func TestFileUploadAndDownload(t *testing.T) {
	app := newTestApp(t)

	fileRecord := uploadTestFile(t, app, "notes.txt", []byte("hello file"))
	if fileRecord.ContentType != "file" {
		t.Fatalf("expected file record, got %+v", fileRecord)
	}
	if fileRecord.FileName != "notes.txt" {
		t.Fatalf("expected original file name to be kept, got %+v", fileRecord)
	}
	filePath := filepath.Join(app.cfg.FilesDir, fileRecord.ContentBody)
	if _, err := os.Stat(filePath); err != nil {
		t.Fatalf("expected uploaded file to exist in files dir: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/records/"+itoa(fileRecord.ID)+"/download", nil)
	recorder := httptest.NewRecorder()
	app.Handler().ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("download returned %d: %s", recorder.Code, recorder.Body.String())
	}
	if body := recorder.Body.String(); body != "hello file" {
		t.Fatalf("unexpected download body: %q", body)
	}
	if disposition := recorder.Header().Get("Content-Disposition"); !strings.Contains(disposition, "notes.txt") {
		t.Fatalf("expected content disposition to include file name, got %q", disposition)
	}
}

func TestUpdateRecordMeta(t *testing.T) {
	app := newTestApp(t)

	imageRecord := uploadTestImage(t, app, "sample.png", tinyPNG)
	fileRecord := uploadTestFile(t, app, "notes.txt", []byte("hello file"))
	textRecord := postTextRecord(t, app, "tag me")

	imageRecord = updateRecordMeta(t, app, imageRecord.ID, stringPtr("cover.png"), []string{"设计", "待发", "设计"})
	if imageRecord.FileName != "cover.png" {
		t.Fatalf("expected image file name to update, got %+v", imageRecord)
	}
	if got := strings.Join(imageRecord.Tags, ","); got != "设计,待发" {
		t.Fatalf("unexpected image tags: %+v", imageRecord.Tags)
	}

	fileRecord = updateRecordMeta(t, app, fileRecord.ID, stringPtr("meeting-notes.txt"), []string{"工作", "同步"})
	if fileRecord.FileName != "meeting-notes.txt" {
		t.Fatalf("expected file name to update, got %+v", fileRecord)
	}
	if got := strings.Join(fileRecord.Tags, ","); got != "工作,同步" {
		t.Fatalf("unexpected file tags: %+v", fileRecord.Tags)
	}

	textRecord = updateRecordMeta(t, app, textRecord.ID, nil, []string{"临时", "剪贴"})
	if got := strings.Join(textRecord.Tags, ","); got != "临时,剪贴" {
		t.Fatalf("unexpected text tags: %+v", textRecord.Tags)
	}

	records := listRecords(t, app)
	for _, record := range records {
		switch record.ID {
		case imageRecord.ID:
			if record.FileName != "cover.png" || len(record.Tags) != 2 {
				t.Fatalf("expected updated image metadata in list, got %+v", record)
			}
		case fileRecord.ID:
			if record.FileName != "meeting-notes.txt" || len(record.Tags) != 2 {
				t.Fatalf("expected updated file metadata in list, got %+v", record)
			}
		case textRecord.ID:
			if len(record.Tags) != 2 {
				t.Fatalf("expected updated text tags in list, got %+v", record)
			}
		}
	}
}

func TestCleanupOldImagesKeepsTextAndToleratesMissingFiles(t *testing.T) {
	app := newTestApp(t)

	oldImage := uploadTestImage(t, app, "old.png", tinyPNG)
	recentImage := uploadTestImage(t, app, "recent.png", tinyPNG)
	textRecord := postTextRecord(t, app, "keep me")

	oldCreatedAt := time.Now().UTC().Add(-8 * 24 * time.Hour)
	if _, err := app.store.db.ExecContext(context.Background(), `UPDATE records SET created_at = ? WHERE id = ?`, oldCreatedAt, oldImage.ID); err != nil {
		t.Fatalf("set old created_at: %v", err)
	}

	oldPath := filepath.Join(app.cfg.ImagesDir, oldImage.ContentBody)
	if err := os.Remove(oldPath); err != nil {
		t.Fatalf("remove old image manually: %v", err)
	}

	payload := callJSON[struct {
		DeletedCount int          `json:"deletedCount"`
		Storage      StorageUsage `json:"storage"`
	}](t, app.Handler(), http.MethodPost, "/api/cleanup/old-images", "", nil)

	if payload.DeletedCount != 1 {
		t.Fatalf("expected 1 deleted image, got %d", payload.DeletedCount)
	}
	if payload.Storage.ImageBytes <= 0 {
		t.Fatalf("expected storage to keep recent image bytes, got %+v", payload.Storage)
	}

	records := listRecords(t, app)
	if len(records) != 2 {
		t.Fatalf("expected 2 records after cleanup, got %d", len(records))
	}

	var foundRecentImage bool
	var foundText bool
	for _, record := range records {
		if record.ID == oldImage.ID {
			t.Fatalf("old image should be removed from records")
		}
		if record.ID == recentImage.ID {
			foundRecentImage = true
		}
		if record.ID == textRecord.ID {
			foundText = true
		}
	}
	if !foundRecentImage || !foundText {
		t.Fatalf("expected recent image and text to remain, got %+v", records)
	}
}

func TestImportBookmarksReplacesExistingSet(t *testing.T) {
	app := newTestApp(t)

	firstSync := importBookmarks(t, app, sampleBookmarksHTML)
	if firstSync.ImportedCount != 3 {
		t.Fatalf("expected 3 imported bookmarks, got %d", firstSync.ImportedCount)
	}
	if firstSync.SyncedAt == nil {
		t.Fatal("expected bookmark sync time to be returned")
	}

	bookmarks := listBookmarks(t, app)
	if len(bookmarks) != 3 {
		t.Fatalf("expected 3 bookmarks after import, got %d", len(bookmarks))
	}
	if bookmarks[0].Title != "LocalDrop" || bookmarks[0].FolderPath != "Toolbar / Tools" {
		t.Fatalf("unexpected first bookmark: %+v", bookmarks[0])
	}
	if bookmarks[1].Title != "Go" || bookmarks[1].FolderPath != "Toolbar / Tools" {
		t.Fatalf("unexpected second bookmark: %+v", bookmarks[1])
	}
	if bookmarks[2].Title != "Vue" || bookmarks[2].FolderPath != "Reading" {
		t.Fatalf("unexpected third bookmark: %+v", bookmarks[2])
	}

	secondSync := importBookmarks(t, app, replacementBookmarksHTML)
	if secondSync.ImportedCount != 1 {
		t.Fatalf("expected replacement sync to keep 1 bookmark, got %d", secondSync.ImportedCount)
	}

	bookmarks = listBookmarks(t, app)
	if len(bookmarks) != 1 {
		t.Fatalf("expected bookmarks to be replaced on sync, got %d", len(bookmarks))
	}
	if bookmarks[0].Title != "OpenAI" || bookmarks[0].URL != "https://openai.com/" {
		t.Fatalf("unexpected replacement bookmark: %+v", bookmarks[0])
	}
}

func newTestApp(t *testing.T) *App {
	t.Helper()

	root := t.TempDir()
	cfg := Config{
		Addr:           "127.0.0.1:0",
		DataDir:        root,
		DBPath:         filepath.Join(root, "localdrop.db"),
		ImagesDir:      filepath.Join(root, "images"),
		FilesDir:       filepath.Join(root, "files"),
		MaxUploadBytes: 5 << 20,
	}

	app, err := NewApp(cfg, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() {
		if err := app.Close(); err != nil {
			t.Fatalf("close app: %v", err)
		}
	})
	return app
}

func postTextRecord(t *testing.T, app *App, content string) Record {
	t.Helper()

	payload := callJSON[struct {
		Record Record `json:"record"`
	}](t, app.Handler(), http.MethodPost, "/api/records/text", "application/json", strings.NewReader(`{"content":"`+content+`"}`))
	return payload.Record
}

func uploadTestImage(t *testing.T, app *App, filename string, data []byte) Record {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	payload := callJSON[struct {
		Record Record `json:"record"`
	}](t, app.Handler(), http.MethodPost, "/api/records/image", writer.FormDataContentType(), &body)
	return payload.Record
}

func uploadTestFile(t *testing.T, app *App, filename string, data []byte) Record {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	payload := callJSON[struct {
		Record Record `json:"record"`
	}](t, app.Handler(), http.MethodPost, "/api/records/file", writer.FormDataContentType(), &body)
	return payload.Record
}

func importBookmarks(t *testing.T, app *App, htmlContent string) struct {
	ImportedCount int        `json:"importedCount"`
	SyncedAt      *time.Time `json:"syncedAt"`
} {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "bookmarks.html")
	if err != nil {
		t.Fatalf("create bookmark form file: %v", err)
	}
	if _, err := part.Write([]byte(htmlContent)); err != nil {
		t.Fatalf("write bookmark form file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close bookmark multipart writer: %v", err)
	}

	return callJSON[struct {
		ImportedCount int        `json:"importedCount"`
		SyncedAt      *time.Time `json:"syncedAt"`
	}](t, app.Handler(), http.MethodPost, "/api/bookmarks/import", writer.FormDataContentType(), &body)
}

func listRecords(t *testing.T, app *App) []Record {
	t.Helper()
	payload := callJSON[struct {
		Records []Record `json:"records"`
	}](t, app.Handler(), http.MethodGet, "/api/records", "", nil)
	return payload.Records
}

func listBookmarks(t *testing.T, app *App) []Bookmark {
	t.Helper()
	payload := callJSON[struct {
		Bookmarks []Bookmark `json:"bookmarks"`
	}](t, app.Handler(), http.MethodGet, "/api/bookmarks", "", nil)
	return payload.Bookmarks
}

func getStorage(t *testing.T, app *App) StorageUsage {
	t.Helper()
	payload := callJSON[struct {
		Storage StorageUsage `json:"storage"`
	}](t, app.Handler(), http.MethodGet, "/api/storage", "", nil)
	return payload.Storage
}

func toggleTop(t *testing.T, app *App, id int64, isTop bool) {
	t.Helper()
	body := strings.NewReader(`{"isTop":` + boolString(isTop) + `}`)
	callJSON[struct {
		Record Record `json:"record"`
	}](t, app.Handler(), http.MethodPatch, "/api/records/"+itoa(id)+"/top", "application/json", body)
}

func deleteRecord(t *testing.T, app *App, id int64) {
	t.Helper()
	callJSON[map[string]any](t, app.Handler(), http.MethodDelete, "/api/records/"+itoa(id), "", nil)
}

func updateRecordMeta(t *testing.T, app *App, id int64, fileName *string, tags []string) Record {
	t.Helper()

	payload := map[string]any{
		"tags": tags,
	}
	if fileName != nil {
		payload["fileName"] = *fileName
	}

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal record meta payload: %v", err)
	}

	response := callJSON[struct {
		Record Record `json:"record"`
	}](t, app.Handler(), http.MethodPatch, "/api/records/"+itoa(id)+"/meta", "application/json", bytes.NewReader(body))
	return response.Record
}

func callJSON[T any](t *testing.T, handler http.Handler, method, path, contentType string, body io.Reader) T {
	t.Helper()

	req := httptest.NewRequest(method, path, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code < 200 || recorder.Code >= 300 {
		t.Fatalf("%s %s returned %d: %s", method, path, recorder.Code, recorder.Body.String())
	}

	var payload T
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response %s %s: %v\nbody=%s", method, path, err, recorder.Body.String())
	}
	return payload
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func itoa(value int64) string {
	return strconv.FormatInt(value, 10)
}

func stringPtr(value string) *string {
	return &value
}

var tinyPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41,
	0x54, 0x78, 0x9c, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
	0x00, 0x03, 0x01, 0x01, 0x00, 0xc9, 0xfe, 0x92,
	0xef, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e,
	0x44, 0xae, 0x42, 0x60, 0x82,
}

const sampleBookmarksHTML = `<!DOCTYPE NETSCAPE-Bookmark-file-1>
<META HTTP-EQUIV="Content-Type" CONTENT="text/html; charset=UTF-8">
<TITLE>Bookmarks</TITLE>
<H1>Bookmarks Menu</H1>
<DL><p>
  <DT><H3>Toolbar</H3>
  <DL><p>
    <DT><H3>Tools</H3>
    <DL><p>
      <DT><A HREF="https://localdrop.test/">LocalDrop</A>
      <DT><A HREF="https://go.dev/">Go</A>
    </DL><p>
  </DL><p>
  <DT><H3>Reading</H3>
  <DL><p>
    <DT><A HREF="https://vuejs.org/">Vue</A>
  </DL><p>
</DL><p>`

const replacementBookmarksHTML = `<!DOCTYPE NETSCAPE-Bookmark-file-1>
<DL><p>
  <DT><A HREF="https://openai.com/">OpenAI</A>
</DL><p>`
