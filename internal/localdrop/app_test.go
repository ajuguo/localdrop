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

func newTestApp(t *testing.T) *App {
	t.Helper()

	root := t.TempDir()
	cfg := Config{
		Addr:           "127.0.0.1:0",
		DataDir:        root,
		DBPath:         filepath.Join(root, "localdrop.db"),
		ImagesDir:      filepath.Join(root, "images"),
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

func listRecords(t *testing.T, app *App) []Record {
	t.Helper()
	payload := callJSON[struct {
		Records []Record `json:"records"`
	}](t, app.Handler(), http.MethodGet, "/api/records", "", nil)
	return payload.Records
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
