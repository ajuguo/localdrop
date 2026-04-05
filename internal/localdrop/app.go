package localdrop

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"localdrop/web"
)

type App struct {
	cfg     Config
	logger  *log.Logger
	store   *Store
	handler http.Handler
}

func NewApp(cfg Config, logger *log.Logger) (*App, error) {
	store, err := OpenStore(cfg.DBPath, cfg.ImagesDir, cfg.FilesDir)
	if err != nil {
		return nil, err
	}

	app := &App{
		cfg:    cfg,
		logger: logger,
		store:  store,
	}

	handler, err := app.routes()
	if err != nil {
		store.Close()
		return nil, err
	}
	app.handler = handler

	return app, nil
}

func (a *App) Close() error {
	return a.store.Close()
}

func (a *App) Handler() http.Handler {
	return a.handler
}

func (a *App) routes() (http.Handler, error) {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/records", a.handleListRecords)
	mux.HandleFunc("POST /api/records/text", a.handleCreateText)
	mux.HandleFunc("POST /api/records/image", a.handleCreateImage)
	mux.HandleFunc("POST /api/records/file", a.handleCreateFile)
	mux.HandleFunc("GET /api/records/{id}/download", a.handleDownloadRecord)
	mux.HandleFunc("PATCH /api/records/{id}/top", a.handleToggleTop)
	mux.HandleFunc("DELETE /api/records/{id}", a.handleDeleteRecord)
	mux.HandleFunc("GET /api/storage", a.handleStorage)
	mux.HandleFunc("POST /api/cleanup/old-images", a.handleCleanupOldImages)
	mux.Handle("GET /media/", http.StripPrefix("/media/", http.FileServer(http.Dir(a.cfg.ImagesDir))))

	frontend, err := a.frontendHandler()
	if err != nil {
		return nil, err
	}
	mux.Handle("/", frontend)

	return logRequests(mux, a.logger), nil
}

func (a *App) frontendHandler() (http.Handler, error) {
	if a.cfg.WebDevURL != "" {
		target, err := url.Parse(a.cfg.WebDevURL)
		if err != nil {
			return nil, fmt.Errorf("parse LOCALDROP_WEB_DEV_URL: %w", err)
		}
		proxy := httputil.NewSingleHostReverseProxy(target)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/media/") {
				http.NotFound(w, r)
				return
			}
			proxy.ServeHTTP(w, r)
		}), nil
	}

	sub, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		return nil, fmt.Errorf("open embedded frontend: %w", err)
	}
	files := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.ServeFileFS(w, r, sub, "index.html")
			return
		}

		path := strings.TrimPrefix(filepath.Clean(r.URL.Path), "/")
		if path == "" || path == "." {
			http.ServeFileFS(w, r, sub, "index.html")
			return
		}
		if _, err := fs.Stat(sub, path); err == nil {
			files.ServeHTTP(w, r)
			return
		}
		http.ServeFileFS(w, r, sub, "index.html")
	}), nil
}

func (a *App) handleListRecords(w http.ResponseWriter, r *http.Request) {
	records, err := a.store.ListRecords(r.Context())
	if err != nil {
		a.writeError(w, http.StatusInternalServerError, err)
		return
	}
	a.writeJSON(w, http.StatusOK, map[string]any{"records": records})
}

func (a *App) handleCreateText(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var payload struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&payload); err != nil {
		a.writeError(w, http.StatusBadRequest, fmt.Errorf("invalid json: %w", err))
		return
	}

	payload.Content = strings.TrimSpace(payload.Content)
	if payload.Content == "" {
		a.writeError(w, http.StatusBadRequest, errors.New("content is required"))
		return
	}

	record, err := a.store.CreateText(r.Context(), payload.Content)
	if err != nil {
		a.writeError(w, http.StatusInternalServerError, err)
		return
	}

	a.writeJSON(w, http.StatusCreated, map[string]any{"record": record})
}

func (a *App) handleCreateImage(w http.ResponseWriter, r *http.Request) {
	a.handleCreateBinary(w, r, true)
}

func (a *App) handleCreateFile(w http.ResponseWriter, r *http.Request) {
	a.handleCreateBinary(w, r, false)
}

func (a *App) handleCreateBinary(w http.ResponseWriter, r *http.Request, imagesOnly bool) {
	const multipartOverhead = 8 << 20

	parseLimit := int64(32 << 20)
	if a.cfg.MaxUploadBytes > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, a.cfg.MaxUploadBytes+multipartOverhead)
		parseLimit = a.cfg.MaxUploadBytes + multipartOverhead
	}
	if err := r.ParseMultipartForm(parseLimit); err != nil {
		if isRequestTooLarge(err) {
			a.writeError(w, http.StatusRequestEntityTooLarge, errImageTooLarge)
			return
		}
		a.writeError(w, http.StatusBadRequest, fmt.Errorf("parse upload: %w", err))
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		a.writeError(w, http.StatusBadRequest, errors.New("file is required"))
		return
	}
	defer file.Close()

	record, err := a.saveUploadedBinary(r.Context(), file, header, imagesOnly)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, errUnsupportedImage):
			status = http.StatusBadRequest
		case errors.Is(err, errEmptyFile):
			status = http.StatusBadRequest
		case errors.Is(err, errImageTooLarge):
			status = http.StatusRequestEntityTooLarge
		}
		a.writeError(w, status, err)
		return
	}

	a.writeJSON(w, http.StatusCreated, map[string]any{"record": record})
}

func (a *App) handleDownloadRecord(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		a.writeError(w, http.StatusBadRequest, errors.New("invalid record id"))
		return
	}

	record, err := a.store.GetRecord(r.Context(), id)
	if err != nil {
		if errors.Is(err, errRecordNotFound) {
			a.writeError(w, http.StatusNotFound, err)
			return
		}
		a.writeError(w, http.StatusInternalServerError, err)
		return
	}

	if record.ContentType == "text" {
		a.writeError(w, http.StatusBadRequest, errors.New("text records do not have downloadable files"))
		return
	}

	target := filepath.Join(a.cfg.ImagesDir, record.ContentBody)
	if record.ContentType == "file" {
		target = filepath.Join(a.cfg.FilesDir, record.ContentBody)
	}
	_, err = os.Stat(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			a.writeError(w, http.StatusNotFound, errors.New("file not found on disk"))
			return
		}
		a.writeError(w, http.StatusInternalServerError, fmt.Errorf("stat file: %w", err))
		return
	}

	w.Header().Set("Content-Disposition", contentDisposition(record))
	if record.MimeType != "" {
		w.Header().Set("Content-Type", record.MimeType)
	}
	http.ServeFile(w, r, target)
}

func (a *App) handleToggleTop(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		a.writeError(w, http.StatusBadRequest, errors.New("invalid record id"))
		return
	}

	defer r.Body.Close()
	var payload struct {
		IsTop bool `json:"isTop"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&payload); err != nil {
		a.writeError(w, http.StatusBadRequest, fmt.Errorf("invalid json: %w", err))
		return
	}

	record, err := a.store.UpdateTopState(r.Context(), id, payload.IsTop)
	if err != nil {
		if errors.Is(err, errRecordNotFound) {
			a.writeError(w, http.StatusNotFound, err)
			return
		}
		a.writeError(w, http.StatusInternalServerError, err)
		return
	}

	a.writeJSON(w, http.StatusOK, map[string]any{"record": record})
}

func (a *App) handleDeleteRecord(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		a.writeError(w, http.StatusBadRequest, errors.New("invalid record id"))
		return
	}

	record, err := a.store.DeleteRecord(r.Context(), id)
	if err != nil {
		if errors.Is(err, errRecordNotFound) {
			a.writeError(w, http.StatusNotFound, err)
			return
		}
		a.writeError(w, http.StatusInternalServerError, err)
		return
	}

	if record.ContentType == "image" || record.ContentType == "file" {
		target := a.storagePath(record)
		if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
			a.logger.Printf("delete file %q: %v", target, err)
		}
	}

	usage, err := a.store.ComputeUsage()
	if err != nil {
		a.writeError(w, http.StatusInternalServerError, err)
		return
	}

	a.writeJSON(w, http.StatusOK, map[string]any{
		"deleted": true,
		"storage": usage,
	})
}

func (a *App) handleStorage(w http.ResponseWriter, r *http.Request) {
	usage, err := a.store.ComputeUsage()
	if err != nil {
		a.writeError(w, http.StatusInternalServerError, err)
		return
	}
	a.writeJSON(w, http.StatusOK, map[string]any{"storage": usage})
}

func (a *App) handleCleanupOldImages(w http.ResponseWriter, r *http.Request) {
	before := time.Now().UTC().Add(-7 * 24 * time.Hour)
	items, err := a.store.FindCleanupCandidates(r.Context(), before)
	if err != nil {
		a.writeError(w, http.StatusInternalServerError, err)
		return
	}

	ids := make([]int64, 0, len(items))
	for _, item := range items {
		target := filepath.Join(a.cfg.ImagesDir, item.FileName)
		if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
			a.logger.Printf("cleanup image %q: %v", target, err)
		}
		ids = append(ids, item.ID)
	}

	if err := a.store.DeleteRecordsByID(r.Context(), ids); err != nil {
		a.writeError(w, http.StatusInternalServerError, err)
		return
	}

	usage, err := a.store.ComputeUsage()
	if err != nil {
		a.writeError(w, http.StatusInternalServerError, err)
		return
	}

	a.writeJSON(w, http.StatusOK, map[string]any{
		"deletedCount": len(ids),
		"storage":      usage,
	})
}

var (
	errUnsupportedImage = errors.New("only image uploads are supported")
	errImageTooLarge    = errors.New("image exceeds max upload size")
	errEmptyFile        = errors.New("file is empty")
)

func (a *App) saveUploadedBinary(ctx context.Context, file multipart.File, header *multipart.FileHeader, imagesOnly bool) (Record, error) {
	reader := bufio.NewReader(file)
	sniff, err := reader.Peek(512)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, bufio.ErrBufferFull) {
		return Record{}, fmt.Errorf("read upload: %w", err)
	}
	if len(sniff) == 0 {
		return Record{}, errEmptyFile
	}

	contentType := http.DetectContentType(sniff)
	if imagesOnly && !strings.HasPrefix(contentType, "image/") {
		return Record{}, errUnsupportedImage
	}
	ext := uploadExtension(contentType)

	fileName := normalizedUploadName(header.Filename, ext)
	name, err := generateFileName(ext)
	if err != nil {
		return Record{}, fmt.Errorf("generate image name: %w", err)
	}
	target := a.storageTargetPath(contentType, name)
	output, err := os.Create(target)
	if err != nil {
		return Record{}, fmt.Errorf("write image file: %w", err)
	}
	size, copyErr := io.Copy(output, reader)
	closeErr := output.Close()
	if copyErr != nil {
		_ = os.Remove(target)
		return Record{}, fmt.Errorf("write image file: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(target)
		return Record{}, fmt.Errorf("close image file: %w", closeErr)
	}

	var record Record
	if strings.HasPrefix(contentType, "image/") {
		record, err = a.store.CreateImage(ctx, name, fileName, contentType, size)
	} else {
		record, err = a.store.CreateFile(ctx, name, fileName, contentType, size)
	}
	if err != nil {
		_ = os.Remove(target)
		return Record{}, err
	}

	return record, nil
}

func (a *App) storageTargetPath(contentType, name string) string {
	if strings.HasPrefix(contentType, "image/") {
		return filepath.Join(a.cfg.ImagesDir, name)
	}
	return filepath.Join(a.cfg.FilesDir, name)
}

func (a *App) storagePath(record Record) string {
	if record.ContentType == "image" {
		return filepath.Join(a.cfg.ImagesDir, record.ContentBody)
	}
	return filepath.Join(a.cfg.FilesDir, record.ContentBody)
}

func isRequestTooLarge(err error) bool {
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr) || strings.Contains(strings.ToLower(err.Error()), "request body too large")
}

func uploadExtension(contentType string) string {
	switch contentType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/bmp":
		return ".bmp"
	case "application/pdf":
		return ".pdf"
	case "text/plain; charset=utf-8":
		return ".txt"
	default:
		if exts, err := mime.ExtensionsByType(contentType); err == nil && len(exts) > 0 {
			return exts[0]
		}
		return ".bin"
	}
}

func normalizedUploadName(name, fallbackExt string) string {
	base := strings.TrimSpace(filepath.Base(name))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return "upload" + fallbackExt
	}
	if filepath.Ext(base) == "" && fallbackExt != "" {
		return base + fallbackExt
	}
	return base
}

func contentDisposition(record Record) string {
	name := record.FileName
	if name == "" {
		name = "download" + filepath.Ext(record.ContentBody)
	}
	return mime.FormatMediaType("attachment", map[string]string{"filename": name})
}

func generateFileName(ext string) (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s%s", time.Now().UTC().Format("20060102T150405Z"), hex.EncodeToString(buf[:]), ext), nil
}

func (a *App) writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		a.logger.Printf("write json: %v", err)
	}
}

func (a *App) writeError(w http.ResponseWriter, status int, err error) {
	a.writeJSON(w, status, map[string]any{"error": err.Error()})
}

func logRequests(next http.Handler, logger *log.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		var recorder responseRecorder
		recorder.ResponseWriter = w
		recorder.status = http.StatusOK
		next.ServeHTTP(&recorder, r)
		if shouldLogRequest(r, recorder.status) {
			logger.Printf("%s %s -> %d (%s)", r.Method, r.URL.Path, recorder.status, time.Since(start).Round(time.Millisecond))
		}
	})
}

func shouldLogRequest(r *http.Request, status int) bool {
	if status >= http.StatusBadRequest {
		return true
	}
	switch r.Method {
	case http.MethodPost, http.MethodPatch, http.MethodDelete, http.MethodPut:
		return true
	default:
		return false
	}
}

type responseRecorder struct {
	http.ResponseWriter
	status int
}

func (r *responseRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseRecorder) Write(data []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(data)
}
