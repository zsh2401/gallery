package main

import (
	"bytes"
	"container/list"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"io/fs"
	"log/slog"
	"math"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rwcarlsen/goexif/exif"
	"github.com/rwcarlsen/goexif/tiff"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	"gopkg.in/yaml.v3"
)

const (
	defaultBind              = "127.0.0.1:8080"
	defaultCacheTTL          = 24 * time.Hour
	defaultThumbnailMaxBytes = 512 * 1024 * 1024
	defaultThumbSize         = 520
	maxThumbSize             = 2048
)

var (
	compatibleExtPriority = []string{".jpg", ".jpeg", ".png", ".webp"}
	rawExtPriority        = []string{".arw", ".cr2", ".cr3", ".nef", ".dng", ".raf", ".rw2", ".orf", ".raw", ".pef", ".srw"}
	heifExts              = map[string]bool{".heic": true, ".heif": true, ".hif": true}
)

type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	if value.Value == "" {
		return nil
	}
	parsed, err := time.ParseDuration(value.Value)
	if err != nil {
		return err
	}
	d.Duration = parsed
	return nil
}

type Config struct {
	ImageRoot string `yaml:"imageRoot"`
	Server    struct {
		Bind string `yaml:"bind"`
	} `yaml:"server"`
	Cache struct {
		Backend           string   `yaml:"backend"`
		TTL               Duration `yaml:"ttl"`
		ThumbnailMaxBytes int64    `yaml:"thumbnailMaxBytes"`
	} `yaml:"cache"`
	Logging struct {
		Level  string `yaml:"level"`
		Format string `yaml:"format"`
	} `yaml:"logging"`
	Features struct {
		RawPreview  string `yaml:"rawPreview"`
		MapProvider string `yaml:"mapProvider"`
	} `yaml:"features"`
	CORS struct {
		AllowedOrigins []string `yaml:"allowedOrigins"`
	} `yaml:"cors"`
	Stats struct {
		Backend  string `yaml:"backend"` // "sqlite" (default), "postgres", "mysql", "memory"
		SQLite   struct {
			Path string `yaml:"path"` // defaults to "gallery.db"
		} `yaml:"sqlite"`
		Postgres struct {
			DSN string `yaml:"dsn"`
		} `yaml:"postgres"`
		MySQL struct {
			DSN string `yaml:"dsn"`
		} `yaml:"mysql"`
	} `yaml:"stats"`
}

func loadConfig(path string) (Config, error) {
	var cfg Config
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	if cfg.ImageRoot == "" {
		return cfg, errors.New("imageRoot is required")
	}
	imageRoot := cfg.ImageRoot
	if !filepath.IsAbs(imageRoot) {
		configDir, err := filepath.Abs(filepath.Dir(path))
		if err != nil {
			return cfg, err
		}
		imageRoot = filepath.Join(configDir, imageRoot)
	}
	root, err := filepath.Abs(imageRoot)
	if err != nil {
		return cfg, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return cfg, fmt.Errorf("imageRoot: %w", err)
	}
	if !info.IsDir() {
		return cfg, fmt.Errorf("imageRoot must be a directory: %s", root)
	}
	cfg.ImageRoot = root
	if cfg.Server.Bind == "" {
		cfg.Server.Bind = defaultBind
	}
	if cfg.Cache.Backend == "" {
		cfg.Cache.Backend = "memory"
	}
	if cfg.Cache.TTL.Duration == 0 {
		cfg.Cache.TTL.Duration = defaultCacheTTL
	}
	if cfg.Cache.ThumbnailMaxBytes <= 0 {
		cfg.Cache.ThumbnailMaxBytes = defaultThumbnailMaxBytes
	}
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "info"
	}
	if cfg.Logging.Format == "" {
		cfg.Logging.Format = "pretty"
	}
	if cfg.Features.RawPreview == "" {
		cfg.Features.RawPreview = "embedded-jpeg"
	}
	if cfg.Features.MapProvider == "" {
		cfg.Features.MapProvider = "amap"
	}
	if len(cfg.CORS.AllowedOrigins) == 0 {
		cfg.CORS.AllowedOrigins = []string{"http://localhost:5173", "http://127.0.0.1:5173"}
	}
	return cfg, nil
}

func newLogger(cfg Config) (*slog.Logger, error) {
	var level slog.Level
	switch strings.ToLower(cfg.Logging.Level) {
	case "debug":
		level = slog.LevelDebug
	case "info", "":
		level = slog.LevelInfo
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		return nil, fmt.Errorf("unsupported logging.level: %s", cfg.Logging.Level)
	}
	opts := &slog.HandlerOptions{Level: level, AddSource: level <= slog.LevelDebug}
	switch strings.ToLower(cfg.Logging.Format) {
	case "json":
		return slog.New(slog.NewJSONHandler(os.Stdout, opts)), nil
	case "pretty", "text", "":
		return slog.New(newPrettyHandler(os.Stdout, opts)), nil
	default:
		return nil, fmt.Errorf("unsupported logging.format: %s", cfg.Logging.Format)
	}
}

type prettyHandler struct {
	out    io.Writer
	opts   *slog.HandlerOptions
	attrs  []slog.Attr
	groups []string
	mu     *sync.Mutex
}

func newPrettyHandler(out io.Writer, opts *slog.HandlerOptions) slog.Handler {
	if opts == nil {
		opts = &slog.HandlerOptions{}
	}
	return &prettyHandler{out: out, opts: opts, mu: &sync.Mutex{}}
}

func (h *prettyHandler) Enabled(_ context.Context, level slog.Level) bool {
	if h.opts != nil && h.opts.Level != nil {
		return level >= h.opts.Level.Level()
	}
	return level >= slog.LevelInfo
}

func (h *prettyHandler) Handle(_ context.Context, record slog.Record) error {
	var b strings.Builder
	levelColor := colorForLevel(record.Level)
	timeColor := "\033[2m"
	reset := "\033[0m"
	if record.Time.IsZero() {
		record.Time = time.Now()
	}
	fmt.Fprintf(&b, "%s%s%s %s%-5s%s %s", timeColor, record.Time.Format("15:04:05.000"), reset, levelColor, record.Level.String(), reset, record.Message)
	if h.opts != nil && h.opts.AddSource && record.PC != 0 {
		if source := sourceLocation(record.PC); source != "" {
			fmt.Fprintf(&b, " %ssource=%s%s", timeColor, source, reset)
		}
	}
	attrs := make([]slog.Attr, 0, len(h.attrs)+record.NumAttrs())
	attrs = append(attrs, h.attrs...)
	record.Attrs(func(attr slog.Attr) bool {
		attrs = append(attrs, attr)
		return true
	})
	for _, attr := range attrs {
		writePrettyAttr(&b, h.groups, attr)
	}
	b.WriteByte('\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := h.out.Write([]byte(b.String()))
	return err
}

func (h *prettyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := *h
	next.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &next
}

func (h *prettyHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	next := *h
	next.groups = append(append([]string{}, h.groups...), name)
	return &next
}

func colorForLevel(level slog.Level) string {
	switch {
	case level >= slog.LevelError:
		return "\033[1;31m"
	case level >= slog.LevelWarn:
		return "\033[1;33m"
	case level <= slog.LevelDebug:
		return "\033[36m"
	default:
		return "\033[32m"
	}
}

func writePrettyAttr(b *strings.Builder, groups []string, attr slog.Attr) {
	attr.Value = attr.Value.Resolve()
	if attr.Equal(slog.Attr{}) {
		return
	}
	key := attr.Key
	if len(groups) > 0 {
		key = strings.Join(append(append([]string{}, groups...), key), ".")
	}
	if attr.Value.Kind() == slog.KindGroup {
		for _, child := range attr.Value.Group() {
			writePrettyAttr(b, append(groups, attr.Key), child)
		}
		return
	}
	fmt.Fprintf(b, " \033[2m%s=\033[0m%s", key, prettyValue(attr.Value))
}

func prettyValue(value slog.Value) string {
	switch value.Kind() {
	case slog.KindString:
		return value.String()
	case slog.KindDuration:
		return value.Duration().String()
	case slog.KindTime:
		return value.Time().Format(time.RFC3339)
	default:
		return value.String()
	}
}

func sourceLocation(pc uintptr) string {
	frames := runtime.CallersFrames([]uintptr{pc})
	frame, _ := frames.Next()
	if frame.File == "" {
		return ""
	}
	return fmt.Sprintf("%s:%d", filepath.Base(frame.File), frame.Line)
}

type Server struct {
	cfg        Config
	log        *slog.Logger
	meta       *metaCache
	raw        *rawPreviewCache
	thumbs     *byteLRU
	stats      StatsStore
	pwSessions *sessionStore
	rootReal   string
}

func newServer(cfg Config, log *slog.Logger) (*Server, error) {
	rootReal, err := filepath.EvalSymlinks(cfg.ImageRoot)
	if err != nil {
		rootReal = cfg.ImageRoot
	}
	s := &Server{
		cfg:        cfg,
		log:        log,
		meta:       newMetaCache(cfg.Cache.TTL.Duration),
		raw:        newRawPreviewCache(cfg.Cache.TTL.Duration),
		thumbs:     newByteLRU(cfg.Cache.ThumbnailMaxBytes, cfg.Cache.TTL.Duration),
		stats:      mustNewStatsStore(cfg),
		pwSessions: newSessionStore(),
		rootReal:   rootReal,
	}
	go s.cleanCaches()
	return s, nil
}

func (s *Server) cleanCaches() {
	interval := s.cfg.Cache.TTL.Duration / 2
	if interval < time.Minute {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		s.meta.clean()
		s.raw.clean()
		s.thumbs.clean()
	}
}

func main() {
	configPath := flag.String("config", "", "path to config YAML")
	flag.Parse()
	if *configPath == "" {
		fmt.Fprintln(os.Stderr, "--config is required")
		os.Exit(2)
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}
	logger, err := newLogger(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "logger: %v\n", err)
		os.Exit(1)
	}
	if cfg.Cache.Backend != "memory" {
		logger.Warn("unsupported cache backend configured; falling back to memory", "backend", cfg.Cache.Backend)
	}

	server, err := newServer(cfg, logger)
	if err != nil {
		logger.Error("initialize server", "error", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	server.routes(mux)
	handler := server.withRequestLogging(server.withCORS(mux))

	logger.Info("starting server", "bind", cfg.Server.Bind)
	if err := http.ListenAndServe(cfg.Server.Bind, handler); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func (s *Server) routes(mux *http.ServeMux) {
	mux.HandleFunc("/api/list-albums", s.api(s.handleListAlbums))
	mux.HandleFunc("/api/list-images", s.api(s.handleListImages))
	mux.HandleFunc("/api/get-image-detail", s.api(s.handleGetImageDetail))
	mux.HandleFunc("/api/get-status", s.api(s.handleGetStatus))
	mux.HandleFunc("/api/record-view", s.api(s.handleRecordView))
	mux.HandleFunc("/api/react-item", s.api(s.handleReactItem))
	mux.HandleFunc("/api/verify-album-password", s.api(s.handleVerifyAlbumPassword))
	mux.HandleFunc("/media/thumb/", s.handleThumb)
	mux.HandleFunc("/media/original/", s.handleOriginal)
	mux.HandleFunc("/media/raw/", s.handleRaw)
}

type apiHandler func(context.Context, json.RawMessage) (any, error)

func (s *Server) api(fn apiHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, envelope{OK: false, Error: "api endpoints require POST"})
			return
		}
		defer r.Body.Close()
		var raw json.RawMessage
		body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, envelope{OK: false, Error: "read request body failed"})
			return
		}
		if len(strings.TrimSpace(string(body))) == 0 {
			raw = json.RawMessage(`{}`)
		} else if !json.Valid(body) {
			writeJSON(w, http.StatusBadRequest, envelope{OK: false, Error: "invalid JSON body"})
			return
		} else {
			raw = body
		}
		data, err := fn(r.Context(), raw)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, errNotFound) || errors.Is(err, errBadRequest) {
				status = http.StatusBadRequest
			}
			s.log.Warn("api error", "path", r.URL.Path, "error", err)
			writeJSON(w, status, envelope{OK: false, Error: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, envelope{OK: true, Data: data})
	}
}

type envelope struct {
	OK    bool   `json:"ok"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, body envelope) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

var (
	errNotFound   = errors.New("not found")
	errBadRequest = errors.New("bad request")
)

type albumDTO struct {
	AlbumID         string    `json:"albumId"`
	Name            string    `json:"name"`
	Path            string    `json:"path"`
	ParentPath      string    `json:"parentPath"`
	Depth           int       `json:"depth"`
	Readme          string    `json:"readme"`
	CoverThumbURL   string    `json:"coverThumbUrl"`
	CoverThumbURLs  []string  `json:"coverThumbUrls"`
	PhotoCount      int       `json:"photoCount"`
	TotalPhotoCount int       `json:"totalPhotoCount"`
	FirstTakenAt    string    `json:"firstTakenAt"`
	TakenAt         string    `json:"takenAt"`
	Stats           itemStats `json:"stats"`
	HasPassword     bool      `json:"hasPassword"`
	PasswordHint    string    `json:"passwordHint,omitempty"`
}

type albumConfig struct {
	Password *struct {
		Value string `yaml:"value"`
		Hint  string `yaml:"hint"`
	} `yaml:"password"`
	Readme string `yaml:"readme"`
}

type imageDTO struct {
	ImageID     string    `json:"imageId"`
	AlbumID     string    `json:"albumId"`
	AlbumPath   string    `json:"albumPath"`
	Title       string    `json:"title"`
	FileName    string    `json:"fileName"`
	Path        string    `json:"path"`
	TakenAt     string    `json:"takenAt"`
	Width       int       `json:"width"`
	Height      int       `json:"height"`
	ThumbURL    string    `json:"thumbUrl"`
	OriginalURL string    `json:"originalUrl"`
	HasRaw      bool      `json:"hasRaw"`
	Stats       itemStats `json:"stats"`
}

type imageDetailDTO struct {
	ImageID             string            `json:"imageId"`
	AlbumID             string            `json:"albumId"`
	AlbumPath           string            `json:"albumPath"`
	Title               string            `json:"title"`
	FileName            string            `json:"fileName"`
	Path                string            `json:"path"`
	TakenAt             string            `json:"takenAt"`
	Width               int               `json:"width"`
	Height              int               `json:"height"`
	OriginalURL         string            `json:"originalUrl"`
	OriginalDownloadURL string            `json:"originalDownloadUrl"`
	ThumbURL            string            `json:"thumbUrl"`
	RawDownloadURL      string            `json:"rawDownloadUrl,omitempty"`
	Files               []string          `json:"files"`
	Exif                map[string]string `json:"exif"`
	Summary             exifSummary       `json:"summary"`
	GPS                 *gpsDTO           `json:"gps,omitempty"`
	Stats               itemStats         `json:"stats"`
}

type exifSummary struct {
	Camera       string `json:"camera,omitempty"`
	Lens         string `json:"lens,omitempty"`
	ExposureTime string `json:"exposureTime,omitempty"`
	Aperture     string `json:"aperture,omitempty"`
	ISO          string `json:"iso,omitempty"`
	FocalLength  string `json:"focalLength,omitempty"`
	ShutterCount string `json:"shutterCount,omitempty"`
	Rating       string `json:"rating,omitempty"`
}

type gpsDTO struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	MapURL    string  `json:"mapUrl"`
}

type itemStats struct {
	Views    int64 `json:"views"`
	Likes    int64 `json:"likes"`
	Dislikes int64 `json:"dislikes"`
}

func (s *Server) handleListAlbums(ctx context.Context, raw json.RawMessage) (any, error) {
	albums, err := s.listAlbums(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"albums": albums}, nil
}

func (s *Server) handleListImages(ctx context.Context, raw json.RawMessage) (any, error) {
	var req struct {
		AlbumID string `json:"albumId"`
		Cursor  string `json:"cursor"`
		Limit   int    `json:"limit"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, errBadRequest
	}
	if req.AlbumID == "" {
		return nil, fmt.Errorf("%w: albumId is required", errBadRequest)
	}
	if req.Limit <= 0 || req.Limit > 200 {
		req.Limit = 80
	}
	start := 0
	if req.Cursor != "" {
		parsed, err := strconv.Atoi(req.Cursor)
		if err != nil || parsed < 0 {
			return nil, fmt.Errorf("%w: invalid cursor", errBadRequest)
		}
		start = parsed
	}
	albumRel, err := decodeAlbumID(req.AlbumID)
	if err != nil {
		return nil, err
	}
	if err := s.checkAlbumAccess(albumRel, raw); err != nil {
		return nil, err
	}
	records, err := s.scanAlbum(ctx, albumRel)
	if err != nil {
		return nil, err
	}
	sortRecords(records)
	if start > len(records) {
		start = len(records)
	}
	end := start + req.Limit
	if end > len(records) {
		end = len(records)
	}
	next := ""
	if end < len(records) {
		next = strconv.Itoa(end)
	}
	items := make([]imageDTO, 0, end-start)
	for _, rec := range records[start:end] {
		items = append(items, s.imageDTO(rec))
	}
	return map[string]any{"images": items, "nextCursor": next, "total": len(records)}, nil
}

func (s *Server) handleGetImageDetail(ctx context.Context, raw json.RawMessage) (any, error) {
	var req struct {
		ImageID   string `json:"imageId"`
		AlbumPath string `json:"albumPath"`
		FileName  string `json:"fileName"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, errBadRequest
	}
	rec, err := s.findImageRequest(ctx, req.ImageID, req.AlbumPath, req.FileName)
	if err != nil {
		return nil, err
	}
	if err := s.checkAlbumAccess(rec.AlbumRel, raw); err != nil {
		return nil, err
	}
	meta, err := s.metadataFor(rec)
	if err != nil {
		s.log.Debug("metadata parse failed", "imageId", rec.ID, "error", err)
	}
	detail := imageDetailDTO{
		ImageID:             rec.ID,
		AlbumID:             rec.AlbumID,
		AlbumPath:           rec.AlbumRel,
		Title:               rec.Title,
		FileName:            rec.FileName,
		Path:                rec.FileName,
		TakenAt:             rec.TakenAt.Format(time.RFC3339),
		Width:               rec.Width,
		Height:              rec.Height,
		OriginalURL:         mediaURL("original", rec.ID),
		OriginalDownloadURL: mediaURL("original", rec.ID) + "?download=1",
		ThumbURL:            mediaURL("thumb", rec.ID),
		Files:               displayFileNames(rec.Files),
		Exif:                meta.Fields,
		Summary:             meta.Summary,
		Stats:               s.stats.Snapshot("image", rec.ID),
	}
	if rec.RawRel != "" {
		detail.RawDownloadURL = mediaURL("raw", rec.ID)
	}
	if meta.GPS != nil {
		detail.GPS = meta.GPS
	}
	return detail, nil
}

func (s *Server) handleGetStatus(ctx context.Context, raw json.RawMessage) (any, error) {
	return map[string]any{
		"ready": true,
		"cache": map[string]any{
			"backend":           "memory",
			"ttlSeconds":        int(s.cfg.Cache.TTL.Seconds()),
			"thumbnailMaxBytes": s.cfg.Cache.ThumbnailMaxBytes,
			"thumbnailBytes":    s.thumbs.bytes(),
		},
		"features": map[string]any{
			"rawPreview":  s.cfg.Features.RawPreview,
			"mapProvider": s.cfg.Features.MapProvider,
		},
	}, nil
}

func (s *Server) handleRecordView(ctx context.Context, raw json.RawMessage) (any, error) {
	var req struct {
		TargetType string `json:"targetType"`
		TargetID   string `json:"targetId"`
		DeviceID   string `json:"deviceId"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, errBadRequest
	}
	if err := s.validateStatsTarget(ctx, req.TargetType, req.TargetID); err != nil {
		return nil, err
	}
	if err := s.checkStatsTargetAccess(req.TargetType, req.TargetID, raw); err != nil {
		return nil, err
	}
	deviceID := req.DeviceID
	if deviceID == "" {
		deviceID = "unknown"
	}
	stats := s.stats.IncrementView(req.TargetType, req.TargetID, deviceID)
	return map[string]any{"stats": stats}, nil
}

func (s *Server) handleReactItem(ctx context.Context, raw json.RawMessage) (any, error) {
	var req struct {
		TargetType string `json:"targetType"`
		TargetID   string `json:"targetId"`
		Reaction   string `json:"reaction"`
		DeviceID   string `json:"deviceId"`
		Active     bool   `json:"active"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, errBadRequest
	}
	if err := s.validateStatsTarget(ctx, req.TargetType, req.TargetID); err != nil {
		return nil, err
	}
	if err := s.checkStatsTargetAccess(req.TargetType, req.TargetID, raw); err != nil {
		return nil, err
	}
	deviceID := req.DeviceID
	if deviceID == "" {
		deviceID = "unknown"
	}
	stats, err := s.stats.IncrementReaction(req.TargetType, req.TargetID, deviceID, req.Reaction, req.Active)
	if err != nil {
		return nil, err
	}
	return map[string]any{"stats": stats}, nil
}

func (s *Server) handleVerifyAlbumPassword(ctx context.Context, raw json.RawMessage) (any, error) {
	var req struct {
		AlbumID  string `json:"albumId"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, errBadRequest
	}
	albumRel, err := decodeAlbumID(req.AlbumID)
	if err != nil {
		return nil, errBadRequest
	}
	cfg := s.readAlbumYAML(albumRel)
	if cfg == nil || cfg.Password == nil || cfg.Password.Value == "" {
		return nil, fmt.Errorf("%w: album has no password", errBadRequest)
	}
	if cfg.Password.Value != req.Password {
		return nil, fmt.Errorf("%w: incorrect password", errBadRequest)
	}
	token := s.pwSessions.create(albumRel)
	return map[string]any{"token": token}, nil
}

// checkAlbumAccess validates that if the given album is password-protected,
// the request includes a valid session token.
func (s *Server) checkAlbumAccess(albumRel string, raw json.RawMessage) error {
	cfg := s.readAlbumYAML(albumRel)
	if cfg == nil || cfg.Password == nil || cfg.Password.Value == "" {
		return nil // No password required
	}
	var req struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(raw, &req)
	if req.Token == "" || !s.pwSessions.validate(req.Token, albumRel) {
		return fmt.Errorf("%w: password required", errBadRequest)
	}
	return nil
}

func (s *Server) validateStatsTarget(ctx context.Context, targetType, targetID string) error {
	switch targetType {
	case "album":
		albumRel, err := decodeAlbumID(targetID)
		if err != nil {
			return err
		}
		_, err = s.resolveAlbum(albumRel)
		return err
	case "image":
		if _, err := s.findImage(ctx, targetID); err != nil {
			return err
		}
		return nil
	default:
		return fmt.Errorf("%w: unsupported targetType", errBadRequest)
	}
}

func (s *Server) checkStatsTargetAccess(targetType, targetID string, raw json.RawMessage) error {
	var albumRel string
	switch targetType {
	case "album":
		var err error
		albumRel, err = decodeAlbumID(targetID)
		if err != nil {
			return err
		}
	case "image":
		var err error
		albumRel, _, err = decodeImageID(targetID)
		if err != nil {
			return err
		}
	default:
		return nil
	}
	return s.checkAlbumAccess(albumRel, raw)
}

func (s *Server) listAlbums(ctx context.Context) ([]albumDTO, error) {
	var albums []albumDTO
	err := filepath.WalkDir(s.cfg.ImageRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			s.log.Warn("walk image root failed", "path", path, "error", err)
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if path != s.cfg.ImageRoot && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		rel, err := filepath.Rel(s.cfg.ImageRoot, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(filepath.Clean(rel))
		records, err := s.scanAlbum(ctx, rel)
		if err != nil {
			s.log.Warn("scan album failed", "album", rel, "error", err)
			return nil
		}
		subtreeRecords, err := s.collectSubtreeRecords(ctx, rel)
		if err != nil {
			s.log.Warn("count album subtree failed", "album", rel, "error", err)
		}
		totalPhotoCount := len(subtreeRecords)
		if len(records) == 0 && (rel == "." || totalPhotoCount == 0) {
			return nil
		}
		sortRecords(records)
		sortRecords(subtreeRecords)
		albumTakenAt := ""
		coverThumbURL := ""
		if len(subtreeRecords) > 0 {
			first := subtreeRecords[0]
			latest := subtreeRecords[len(subtreeRecords)-1]
			albumTakenAt = first.TakenAt.Format(time.RFC3339)
			coverThumbURL = mediaURL("thumb", latest.ID)
		}
		coverThumbURLs := sampledThumbURLs(subtreeRecords, 9)
		readme := s.readAlbumReadme(rel)
		albumID := encodeAlbumID(rel)
		hasPassword, passwordHint := false, ""
		if cfg := s.readAlbumYAML(rel); cfg != nil && cfg.Password != nil && cfg.Password.Value != "" {
			hasPassword = true
			passwordHint = cfg.Password.Hint
		}
		albums = append(albums, albumDTO{
			AlbumID:         albumID,
			Name:            albumName(rel),
			Path:            rel,
			ParentPath:      parentAlbumPath(rel),
			Depth:           albumDepth(rel),
			Readme:          readme,
			CoverThumbURL:   coverThumbURL,
			CoverThumbURLs:  coverThumbURLs,
			PhotoCount:      len(records),
			TotalPhotoCount: totalPhotoCount,
			FirstTakenAt:    albumTakenAt,
			TakenAt:         albumTakenAt,
			Stats:           s.stats.Snapshot("album", albumID),
			HasPassword:     hasPassword,
			PasswordHint:    passwordHint,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(albums, func(i, j int) bool {
		if albums[i].ParentPath == albums[j].ParentPath {
			return albumLess(albums[i], albums[j])
		}
		if albums[i].Depth == albums[j].Depth {
			return albums[i].Path < albums[j].Path
		}
		return albums[i].Depth < albums[j].Depth
	})
	return albums, nil
}

func albumLess(a, b albumDTO) bool {
	aTime := parseRFC3339OrZero(a.FirstTakenAt)
	bTime := parseRFC3339OrZero(b.FirstTakenAt)
	if !aTime.Equal(bTime) {
		if aTime.IsZero() {
			return false
		}
		if bTime.IsZero() {
			return true
		}
		return aTime.After(bTime)
	}
	if a.Name != b.Name {
		return a.Name < b.Name
	}
	return a.Path < b.Path
}

func parseRFC3339OrZero(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func (s *Server) readAlbumReadme(albumRel string) string {
	// Priority: ALBUM.yaml readme > README.md > empty
	cfg := s.readAlbumYAML(albumRel)
	if cfg != nil && cfg.Readme != "" {
		return strings.TrimSpace(cfg.Readme)
	}
	albumPath, err := s.resolveAlbum(albumRel)
	if err != nil {
		return ""
	}
	for _, name := range []string{"README.md", "readme.md"} {
		path := filepath.Join(albumPath, name)
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || info.Size() > 256*1024 {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		return strings.TrimSpace(string(data))
	}
	return ""
}

func (s *Server) readAlbumYAML(albumRel string) *albumConfig {
	albumPath, err := s.resolveAlbum(albumRel)
	if err != nil {
		return nil
	}
	for _, name := range []string{"ALBUM.yaml", "album.yaml"} {
		path := filepath.Join(albumPath, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var cfg albumConfig
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			continue
		}
		return &cfg
	}
	return nil
}

func (s *Server) collectSubtreeRecords(ctx context.Context, albumRel string) ([]imageRecord, error) {
	albumPath, err := s.resolveAlbum(albumRel)
	if err != nil {
		return nil, err
	}
	var all []imageRecord
	err = filepath.WalkDir(albumPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if path != albumPath && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		rel, err := filepath.Rel(s.cfg.ImageRoot, path)
		if err != nil {
			return nil
		}
		records, err := s.scanAlbum(ctx, filepath.ToSlash(filepath.Clean(rel)))
		if err == nil {
			all = append(all, records...)
		}
		return nil
	})
	return all, err
}

type imageRecord struct {
	ID         string
	AlbumID    string
	AlbumRel   string
	BaseLower  string
	Title      string
	FileName   string
	DisplayRel string
	RawRel     string
	PreviewRaw bool
	Files      []string
	TakenAt    time.Time
	Width      int
	Height     int
}

func (s *Server) scanAlbum(ctx context.Context, albumRel string) ([]imageRecord, error) {
	albumPath, err := s.resolveAlbum(albumRel)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(albumPath)
	if err != nil {
		return nil, err
	}
	type group struct {
		files map[string]string
	}
	groups := map[string]*group{}
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if !isKnownImageExt(ext) {
			continue
		}
		base := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		key := strings.ToLower(base)
		if groups[key] == nil {
			groups[key] = &group{files: map[string]string{}}
		}
		rel := joinRel(albumRel, entry.Name())
		groups[key].files[ext] = rel
	}

	records := make([]imageRecord, 0, len(groups))
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		g := groups[key]
		displayRel := firstByPriority(g.files, compatibleExtPriority)
		rawRel := firstByPriority(g.files, rawExtPriority)
		previewRaw := false
		if displayRel == "" && rawRel != "" {
			rawAbs, err := s.resolveFile(rawRel)
			if err == nil {
				if _, err := s.rawPreview(rawAbs); err == nil {
					displayRel = rawRel
					previewRaw = true
				} else {
					s.log.Info("hiding raw file without embedded jpeg preview", "file", rawRel, "error", err)
				}
			}
		}
		if displayRel == "" {
			if heifRel := firstHeif(g.files); heifRel != "" {
				s.log.Info("hiding unsupported heif-only image", "file", heifRel)
			}
			continue
		}
		rec, err := s.buildRecord(albumRel, key, displayRel, rawRel, previewRaw, g.files)
		if err != nil {
			s.log.Warn("hiding image after record build failed", "album", albumRel, "base", key, "error", err)
			continue
		}
		records = append(records, rec)
	}
	return records, nil
}

func (s *Server) buildRecord(albumRel, baseLower, displayRel, rawRel string, previewRaw bool, files map[string]string) (imageRecord, error) {
	displayAbs, err := s.resolveFile(displayRel)
	if err != nil {
		return imageRecord{}, err
	}
	var width, height int
	if previewRaw {
		preview, err := s.rawPreview(displayAbs)
		if err != nil {
			return imageRecord{}, err
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(preview))
		if err != nil {
			return imageRecord{}, err
		}
		width, height = cfg.Width, cfg.Height
	} else {
		width, height = imageDimensions(displayAbs)
	}

	stat, err := os.Stat(displayAbs)
	if err != nil {
		return imageRecord{}, err
	}
	takenAt := stat.ModTime()
	if meta, err := s.metadataForPath(displayAbs); err == nil && !meta.TakenAt.IsZero() {
		takenAt = meta.TakenAt
	}

	allFiles := make([]string, 0, len(files))
	for _, rel := range files {
		allFiles = append(allFiles, rel)
	}
	sort.Strings(allFiles)
	id := encodeImageID(albumRel, baseLower)
	title := displayTitle(displayRel)
	return imageRecord{
		ID:         id,
		AlbumID:    encodeAlbumID(albumRel),
		AlbumRel:   albumRel,
		BaseLower:  baseLower,
		Title:      title,
		FileName:   filepath.Base(filepath.FromSlash(displayRel)),
		DisplayRel: displayRel,
		RawRel:     rawRel,
		PreviewRaw: previewRaw,
		Files:      allFiles,
		TakenAt:    takenAt,
		Width:      width,
		Height:     height,
	}, nil
}

func (s *Server) findImage(ctx context.Context, id string) (imageRecord, error) {
	albumRel, baseLower, err := decodeImageID(id)
	if err != nil {
		return imageRecord{}, err
	}
	records, err := s.scanAlbum(ctx, albumRel)
	if err != nil {
		return imageRecord{}, err
	}
	for _, rec := range records {
		if rec.BaseLower == baseLower {
			return rec, nil
		}
	}
	return imageRecord{}, errNotFound
}

func (s *Server) findImageRequest(ctx context.Context, imageID, albumPath, fileName string) (imageRecord, error) {
	if imageID != "" {
		return s.findImage(ctx, imageID)
	}
	if albumPath == "" || fileName == "" {
		return imageRecord{}, fmt.Errorf("%w: imageId or albumPath + fileName is required", errBadRequest)
	}
	albumPath = filepath.ToSlash(filepath.Clean(albumPath))
	if albumPath == "" {
		albumPath = "."
	}
	records, err := s.scanAlbum(ctx, albumPath)
	if err != nil {
		return imageRecord{}, err
	}
	for _, rec := range records {
		if rec.FileName == fileName || rec.Title == strings.TrimSuffix(fileName, filepath.Ext(fileName)) {
			return rec, nil
		}
	}
	return imageRecord{}, errNotFound
}

func (s *Server) imageDTO(rec imageRecord) imageDTO {
	return imageDTO{
		ImageID:     rec.ID,
		AlbumID:     rec.AlbumID,
		AlbumPath:   rec.AlbumRel,
		Title:       rec.Title,
		FileName:    rec.FileName,
		Path:        rec.FileName,
		TakenAt:     rec.TakenAt.Format(time.RFC3339),
		Width:       rec.Width,
		Height:      rec.Height,
		ThumbURL:    mediaURL("thumb", rec.ID),
		OriginalURL: mediaURL("original", rec.ID),
		HasRaw:      rec.RawRel != "",
		Stats:       s.stats.Snapshot("image", rec.ID),
	}
}

func sortRecords(records []imageRecord) {
	sort.Slice(records, func(i, j int) bool {
		if records[i].TakenAt.Equal(records[j].TakenAt) {
			return records[i].Title < records[j].Title
		}
		return records[i].TakenAt.Before(records[j].TakenAt)
	})
}

func sampledThumbURLs(records []imageRecord, limit int) []string {
	if len(records) == 0 || limit <= 0 {
		return nil
	}
	if len(records) <= limit {
		urls := make([]string, 0, len(records))
		for _, rec := range records {
			urls = append(urls, mediaURL("thumb", rec.ID))
		}
		return urls
	}
	urls := make([]string, 0, limit)
	used := map[int]bool{}
	for i := 0; i < limit; i++ {
		idx := int(math.Round(float64(i) * float64(len(records)-1) / float64(limit-1)))
		if used[idx] {
			for idx < len(records)-1 && used[idx] {
				idx++
			}
		}
		used[idx] = true
		urls = append(urls, mediaURL("thumb", records[idx].ID))
	}
	return urls
}

func firstByPriority(files map[string]string, priority []string) string {
	for _, ext := range priority {
		if rel := files[ext]; rel != "" {
			return rel
		}
	}
	return ""
}

func firstHeif(files map[string]string) string {
	for ext, rel := range files {
		if heifExts[ext] {
			return rel
		}
	}
	return ""
}

func isKnownImageExt(ext string) bool {
	if heifExts[ext] {
		return true
	}
	for _, candidate := range compatibleExtPriority {
		if ext == candidate {
			return true
		}
	}
	for _, candidate := range rawExtPriority {
		if ext == candidate {
			return true
		}
	}
	return false
}

func isRawExt(ext string) bool {
	for _, candidate := range rawExtPriority {
		if ext == candidate {
			return true
		}
	}
	return false
}

func mediaURL(kind, id string) string {
	return "/media/" + kind + "/" + url.PathEscape(id)
}

func albumName(rel string) string {
	if rel == "." || rel == "" {
		return "Root"
	}
	return filepath.Base(filepath.FromSlash(rel))
}

func parentAlbumPath(rel string) string {
	if rel == "." || rel == "" {
		return ""
	}
	parent := filepath.ToSlash(filepath.Dir(filepath.FromSlash(rel)))
	if parent == "." {
		return ""
	}
	return parent
}

func albumDepth(rel string) int {
	if rel == "." || rel == "" {
		return 0
	}
	return len(strings.Split(rel, "/"))
}

func displayTitle(rel string) string {
	name := filepath.Base(filepath.FromSlash(rel))
	return strings.TrimSuffix(name, filepath.Ext(name))
}

func displayFileNames(files []string) []string {
	names := make([]string, 0, len(files))
	seen := map[string]bool{}
	for _, file := range files {
		name := filepath.Base(filepath.FromSlash(file))
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func joinRel(albumRel, name string) string {
	if albumRel == "." || albumRel == "" {
		return filepath.ToSlash(name)
	}
	return filepath.ToSlash(filepath.Join(filepath.FromSlash(albumRel), name))
}

func encodeAlbumID(rel string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(rel))
}

func decodeAlbumID(id string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		return "", fmt.Errorf("%w: invalid albumId", errBadRequest)
	}
	rel := filepath.ToSlash(filepath.Clean(string(raw)))
	if rel == "" {
		rel = "."
	}
	return rel, nil
}

func encodeImageID(albumRel, baseLower string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(albumRel + "\x00" + baseLower))
}

func decodeImageID(id string) (string, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		return "", "", fmt.Errorf("%w: invalid imageId", errBadRequest)
	}
	parts := strings.SplitN(string(raw), "\x00", 2)
	if len(parts) != 2 || parts[1] == "" {
		return "", "", fmt.Errorf("%w: invalid imageId", errBadRequest)
	}
	albumRel := filepath.ToSlash(filepath.Clean(parts[0]))
	if albumRel == "" {
		albumRel = "."
	}
	return albumRel, parts[1], nil
}

func (s *Server) resolveAlbum(rel string) (string, error) {
	if rel == "" {
		rel = "."
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean) {
		return "", fmt.Errorf("%w: unsafe album path", errBadRequest)
	}
	path := filepath.Join(s.cfg.ImageRoot, clean)
	if err := s.ensureInsideRoot(path); err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%w: album is not a directory", errBadRequest)
	}
	return path, nil
}

func (s *Server) resolveFile(rel string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean) {
		return "", fmt.Errorf("%w: unsafe file path", errBadRequest)
	}
	path := filepath.Join(s.cfg.ImageRoot, clean)
	if err := s.ensureInsideRoot(path); err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%w: file path is a directory", errBadRequest)
	}
	return path, nil
}

func (s *Server) ensureInsideRoot(path string) error {
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		realPath = path
	}
	rel, err := filepath.Rel(s.rootReal, realPath)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("%w: path escapes imageRoot", errBadRequest)
	}
	return nil
}

func imageDimensions(path string) (int, int) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

type parsedMeta struct {
	Fields  map[string]string
	Summary exifSummary
	TakenAt time.Time
	GPS     *gpsDTO
}

func (s *Server) metadataFor(rec imageRecord) (parsedMeta, error) {
	path := rec.DisplayRel
	if rec.RawRel != "" && isRawExt(strings.ToLower(filepath.Ext(rec.RawRel))) {
		path = rec.RawRel
	}
	abs, err := s.resolveFile(path)
	if err != nil {
		return parsedMeta{Fields: map[string]string{}}, err
	}
	return s.metadataForPath(abs)
}

func (s *Server) metadataForPath(abs string) (parsedMeta, error) {
	info, err := os.Stat(abs)
	if err != nil {
		return parsedMeta{Fields: map[string]string{}}, err
	}
	key := fileCacheKey(abs, info)
	if meta, ok := s.meta.get(key); ok {
		return meta, nil
	}
	meta, err := parseEXIF(abs, s.cfg.Features.MapProvider)
	if err != nil {
		meta = parsedMeta{Fields: map[string]string{}}
	}
	s.meta.set(key, meta)
	return meta, err
}

func parseEXIF(path, mapProvider string) (parsedMeta, error) {
	file, err := os.Open(path)
	if err != nil {
		return parsedMeta{}, err
	}
	defer file.Close()
	x, err := exif.Decode(file)
	if err != nil {
		return parsedMeta{}, err
	}
	walker := &exifWalker{fields: map[string]string{}}
	_ = x.Walk(walker)
	mergeEmbeddedTextFields(path, walker.fields)
	meta := parsedMeta{Fields: walker.fields}
	meta.Summary = exifSummary{
		Camera:       joinNonEmpty(tagString(x, exif.Make), tagString(x, exif.Model)),
		Lens:         firstNonEmpty(tagString(x, exif.LensModel), tagString(x, exif.LensMake)),
		ExposureTime: tagString(x, exif.ExposureTime),
		Aperture:     formatAperture(firstNonEmpty(tagString(x, exif.FNumber), tagString(x, exif.ApertureValue))),
		ISO:          tagString(x, exif.ISOSpeedRatings),
		FocalLength:  tagString(x, exif.FocalLength),
		ShutterCount: firstFieldByNames(walker.fields, "ShutterCount", "Shutter Count", "ImageCount", "Image Count", "Sony Shutter Count"),
		Rating:       firstFieldByNames(walker.fields, "Rating", "RatingPercent", "XmpRating"),
	}
	meta.TakenAt = firstEXIFTime(
		tagString(x, exif.DateTimeOriginal),
		tagString(x, exif.DateTimeDigitized),
		tagString(x, exif.DateTime),
	)
	if lat, lng, err := x.LatLong(); err == nil && !math.IsNaN(lat) && !math.IsNaN(lng) {
		meta.GPS = &gpsDTO{
			Latitude:  lat,
			Longitude: lng,
			MapURL:    gpsMapURL(mapProvider, lat, lng),
		}
		meta.Fields["GPSLatitudeDecimal"] = strconv.FormatFloat(lat, 'f', 6, 64)
		meta.Fields["GPSLongitudeDecimal"] = strconv.FormatFloat(lng, 'f', 6, 64)
	}
	return meta, nil
}

type exifWalker struct {
	fields map[string]string
}

func (w *exifWalker) Walk(name exif.FieldName, tag *tiff.Tag) error {
	value := cleanEXIFString(tag.String())
	if value != "" {
		w.fields[string(name)] = value
	}
	return nil
}

func tagString(x *exif.Exif, name exif.FieldName) string {
	tag, err := x.Get(name)
	if err != nil || tag == nil {
		return ""
	}
	return cleanEXIFString(tag.String())
}

var (
	whitespaceRE        = regexp.MustCompile(`\s+`)
	apertureRationalRE  = regexp.MustCompile(`^\s*([0-9]+(?:\.[0-9]+)?)\s*/\s*([0-9]+(?:\.[0-9]+)?)\s*$`)
	firstNumberRE       = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?`)
	xmpAttributeFieldRE = regexp.MustCompile(`(?is)([A-Za-z0-9_:\-]+)\s*=\s*"([^"]*)"`)
	xmpElementFieldRE   = regexp.MustCompile(`(?is)<([A-Za-z0-9_:\-]+)[^>]*>\s*([^<]+?)\s*</[A-Za-z0-9_:\-]+>`)
)

func cleanEXIFString(value string) string {
	value = strings.ReplaceAll(value, "\x00", "")
	value = strings.TrimSpace(value)
	for len(value) >= 2 {
		first, last := value[0], value[len(value)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			value = strings.TrimSpace(value[1 : len(value)-1])
			continue
		}
		break
	}
	return whitespaceRE.ReplaceAllString(value, " ")
}

func formatAperture(value string) string {
	value = cleanEXIFString(value)
	if value == "" {
		return ""
	}
	normalized := strings.TrimSpace(value)
	upper := strings.ToUpper(normalized)
	upper = strings.TrimPrefix(upper, "F/")
	upper = strings.TrimPrefix(upper, "F")
	upper = strings.TrimSpace(upper)
	if match := apertureRationalRE.FindStringSubmatch(upper); len(match) == 3 {
		numerator, nErr := strconv.ParseFloat(match[1], 64)
		denominator, dErr := strconv.ParseFloat(match[2], 64)
		if nErr == nil && dErr == nil && denominator != 0 {
			return "F" + compactFloat(numerator/denominator)
		}
	}
	if number := firstNumberRE.FindString(upper); number != "" {
		parsed, err := strconv.ParseFloat(number, 64)
		if err == nil {
			return "F" + compactFloat(parsed)
		}
		return "F" + number
	}
	return "F" + upper
}

func compactFloat(value float64) string {
	return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(value, 'f', 2, 64), "0"), ".")
}

func firstFieldByNames(fields map[string]string, names ...string) string {
	if len(fields) == 0 {
		return ""
	}
	normalizedTargets := make([]string, 0, len(names))
	for _, name := range names {
		if value := fields[name]; value != "" {
			return value
		}
		normalizedTargets = append(normalizedTargets, normalizeMetaKey(name))
	}
	for key, value := range fields {
		if value == "" {
			continue
		}
		keyNorm := normalizeMetaKey(key)
		for _, target := range normalizedTargets {
			if keyNorm == target {
				return value
			}
		}
	}
	for key, value := range fields {
		if value == "" {
			continue
		}
		keyNorm := normalizeMetaKey(key)
		for _, target := range normalizedTargets {
			if strings.Contains(keyNorm, target) || strings.Contains(target, keyNorm) {
				return value
			}
		}
	}
	return ""
}

func normalizeMetaKey(value string) string {
	value = strings.ToLower(value)
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func mergeEmbeddedTextFields(path string, fields map[string]string) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()
	buf := make([]byte, 4<<20)
	n, err := file.Read(buf)
	if err != nil && err != io.EOF {
		return
	}
	text := string(buf[:n])
	interesting := map[string]string{
		"rating":                    "Rating",
		"xmprating":                 "Rating",
		"ratingpercent":             "RatingPercent",
		"auximagestatus":            "ImageStatus",
		"auximagecount":             "ImageCount",
		"auxshuttercount":           "ShutterCount",
		"sonymakernoteimagecount":   "ImageCount",
		"sonymakernoteshuttercount": "ShutterCount",
		"shuttercount":              "ShutterCount",
		"imagecount":                "ImageCount",
	}
	for _, match := range xmpAttributeFieldRE.FindAllStringSubmatch(text, -1) {
		mergeTextField(fields, interesting, match[1], match[2])
	}
	for _, match := range xmpElementFieldRE.FindAllStringSubmatch(text, -1) {
		mergeTextField(fields, interesting, match[1], match[2])
	}
}

func mergeTextField(fields map[string]string, interesting map[string]string, rawKey, rawValue string) {
	key := normalizeMetaKey(rawKey)
	target, ok := interesting[key]
	if !ok {
		return
	}
	value := cleanEXIFString(rawValue)
	if value == "" || fields[target] != "" {
		return
	}
	fields[target] = value
}

func firstEXIFTime(values ...string) time.Time {
	for _, value := range values {
		value = cleanEXIFString(value)
		if value == "" {
			continue
		}
		for _, layout := range []string{"2006:01:02 15:04:05", "2006-01-02 15:04:05", time.RFC3339} {
			if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
				return parsed
			}
		}
	}
	return time.Time{}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func joinNonEmpty(values ...string) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, " ")
}

func gpsMapURL(provider string, lat, lng float64) string {
	switch strings.ToLower(provider) {
	case "amap", "gaode", "高德地图":
		return fmt.Sprintf("https://uri.amap.com/marker?position=%f,%f&name=%s", lng, lat, url.QueryEscape("Photo"))
	case "openstreetmap", "osm":
		return fmt.Sprintf("https://www.openstreetmap.org/?mlat=%f&mlon=%f#map=16/%f/%f", lat, lng, lat, lng)
	case "google":
		return fmt.Sprintf("https://www.google.com/maps/search/?api=1&query=%f,%f", lat, lng)
	default:
		return fmt.Sprintf("https://uri.amap.com/marker?position=%f,%f&name=%s", lng, lat, url.QueryEscape("Photo"))
	}
}

func (s *Server) handleThumb(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "media endpoints require GET", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/media/thumb/")
	id, _ = url.PathUnescape(id)
	size := defaultThumbSize
	if raw := r.URL.Query().Get("size"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			size = parsed
		}
	}
	if size > maxThumbSize {
		size = maxThumbSize
	}
	rec, err := s.findImage(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	etag := s.thumbnailETag(rec, size)
	if etag != "" && r.Header.Get("If-None-Match") == etag {
		w.Header().Set("ETag", etag)
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "private, max-age=3600, stale-while-revalidate=86400")
		w.WriteHeader(http.StatusNotModified)
		return
	}
	data, err := s.thumbnail(rec, size)
	if err != nil {
		s.log.Warn("thumbnail failed", "imageId", id, "error", err)
		http.Error(w, "thumbnail failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=3600, stale-while-revalidate=86400")
	if etag != "" {
		w.Header().Set("ETag", etag)
	}
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}

func (s *Server) thumbnailETag(rec imageRecord, size int) string {
	version, err := s.recordVersion(rec)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%s", rec.ID, size, version)))
	return `"` + hex.EncodeToString(sum[:])[:24] + `"`
}

func (s *Server) handleOriginal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "media endpoints require GET", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/media/original/")
	id, _ = url.PathUnescape(id)
	rec, err := s.findImage(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if rec.PreviewRaw {
		abs, err := s.resolveFile(rec.DisplayRel)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		data, err := s.rawPreview(abs)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "private, max-age=3600")
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write(data)
		}
		return
	}
	abs, err := s.resolveFile(rec.DisplayRel)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(abs)))
	}
	serveFileInline(w, r, abs)
}

func (s *Server) handleRaw(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "media endpoints require GET", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/media/raw/")
	id, _ = url.PathUnescape(id)
	rec, err := s.findImage(r.Context(), id)
	if err != nil || rec.RawRel == "" {
		http.NotFound(w, r)
		return
	}
	abs, err := s.resolveFile(rec.RawRel)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(abs)))
	serveFileInline(w, r, abs)
}

func serveFileInline(w http.ResponseWriter, r *http.Request, abs string) {
	file, err := os.Open(abs)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(abs)))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeContent(w, r, filepath.Base(abs), stat.ModTime(), file)
}

func (s *Server) thumbnail(rec imageRecord, size int) ([]byte, error) {
	version, err := s.recordVersion(rec)
	if err != nil {
		return nil, err
	}
	key := fmt.Sprintf("%s:%d:%s", rec.ID, size, version)
	if data, ok := s.thumbs.get(key); ok {
		return data, nil
	}
	img, orientation, err := s.decodeRecordImage(rec)
	if err != nil {
		return nil, err
	}
	img = orientImage(img, orientation)
	thumb := resizeFit(img, size)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, thumb, &jpeg.Options{Quality: 84}); err != nil {
		return nil, err
	}
	data := buf.Bytes()
	s.thumbs.set(key, data)
	return data, nil
}

func (s *Server) decodeRecordImage(rec imageRecord) (image.Image, int, error) {
	abs, err := s.resolveFile(rec.DisplayRel)
	if err != nil {
		return nil, 1, err
	}
	var reader io.Reader
	if rec.PreviewRaw {
		data, err := s.rawPreview(abs)
		if err != nil {
			return nil, 1, err
		}
		reader = bytes.NewReader(data)
	} else {
		file, err := os.Open(abs)
		if err != nil {
			return nil, 1, err
		}
		defer file.Close()
		reader = file
	}
	img, _, err := image.Decode(reader)
	if err != nil {
		return nil, 1, err
	}
	orientation := 1
	if meta, err := s.metadataForPath(abs); err == nil {
		if raw := meta.Fields["Orientation"]; raw != "" {
			orientation = parseOrientation(raw)
		}
	}
	return img, orientation, nil
}

func (s *Server) recordVersion(rec imageRecord) (string, error) {
	abs, err := s.resolveFile(rec.DisplayRel)
	if err != nil {
		return "", err
	}
	stat, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	return fileCacheKey(abs, stat), nil
}

func parseOrientation(raw string) int {
	fields := strings.Fields(raw)
	for _, field := range fields {
		field = strings.Trim(field, "(),")
		if value, err := strconv.Atoi(field); err == nil && value >= 1 && value <= 8 {
			return value
		}
	}
	return 1
}

func resizeFit(img image.Image, maxSide int) image.Image {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 {
		return img
	}
	scale := float64(maxSide) / float64(max(w, h))
	if scale > 1 {
		scale = 1
	}
	nw := max(1, int(math.Round(float64(w)*scale)))
	nh := max(1, int(math.Round(float64(h)*scale)))
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, bounds, draw.Over, nil)
	return dst
}

func orientImage(img image.Image, orientation int) image.Image {
	switch orientation {
	case 3:
		return rotate180(img)
	case 6:
		return rotate90CW(img)
	case 8:
		return rotate90CCW(img)
	default:
		return img
	}
}

func rotate180(src image.Image) image.Image {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			dst.Set(b.Dx()-1-x, b.Dy()-1-y, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

func rotate90CW(src image.Image) image.Image {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dy(), b.Dx()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			dst.Set(b.Dy()-1-y, x, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

func rotate90CCW(src image.Image) image.Image {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dy(), b.Dx()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			dst.Set(y, b.Dx()-1-x, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (s *Server) rawPreview(abs string) ([]byte, error) {
	stat, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	key := fileCacheKey(abs, stat)
	if data, ok := s.raw.get(key); ok {
		return data, nil
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	preview, err := extractLargestJPEG(data)
	if err != nil {
		return nil, err
	}
	s.raw.set(key, preview)
	return preview, nil
}

func extractLargestJPEG(data []byte) ([]byte, error) {
	const minPreviewBytes = 1024
	var best []byte
	for i := 0; i < len(data)-1; i++ {
		if data[i] != 0xff || data[i+1] != 0xd8 {
			continue
		}
		for j := i + 2; j < len(data)-1; j++ {
			if data[j] == 0xff && data[j+1] == 0xd9 {
				candidate := data[i : j+2]
				if len(candidate) > len(best) {
					best = candidate
				}
				i = j + 1
				break
			}
		}
	}
	if len(best) < minPreviewBytes {
		return nil, errors.New("no embedded jpeg preview found")
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(best))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, errors.New("embedded jpeg preview is invalid")
	}
	return append([]byte(nil), best...), nil
}

type metaEntry struct {
	key       string
	value     parsedMeta
	expiresAt time.Time
}

type metaCache struct {
	mu  sync.Mutex
	ttl time.Duration
	m   map[string]metaEntry
}

func newMetaCache(ttl time.Duration) *metaCache {
	return &metaCache{ttl: ttl, m: map[string]metaEntry{}}
}

func (c *metaCache) get(key string) (parsedMeta, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.m[key]
	if !ok || time.Now().After(entry.expiresAt) {
		delete(c.m, key)
		return parsedMeta{}, false
	}
	return entry.value, true
}

func (c *metaCache) set(key string, value parsedMeta) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = metaEntry{key: key, value: value, expiresAt: time.Now().Add(c.ttl)}
}

func (c *metaCache) clean() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for key, entry := range c.m {
		if now.After(entry.expiresAt) {
			delete(c.m, key)
		}
	}
}

type rawEntry struct {
	value     []byte
	expiresAt time.Time
}

type rawPreviewCache struct {
	mu  sync.Mutex
	ttl time.Duration
	m   map[string]rawEntry
}

func newRawPreviewCache(ttl time.Duration) *rawPreviewCache {
	return &rawPreviewCache{ttl: ttl, m: map[string]rawEntry{}}
}

func (c *rawPreviewCache) get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.m[key]
	if !ok || time.Now().After(entry.expiresAt) {
		delete(c.m, key)
		return nil, false
	}
	return append([]byte(nil), entry.value...), true
}

func (c *rawPreviewCache) set(key string, value []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = rawEntry{value: append([]byte(nil), value...), expiresAt: time.Now().Add(c.ttl)}
}

func (c *rawPreviewCache) clean() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for key, entry := range c.m {
		if now.After(entry.expiresAt) {
			delete(c.m, key)
		}
	}
}

type byteLRU struct {
	mu       sync.Mutex
	maxBytes int64
	ttl      time.Duration
	used     int64
	ll       *list.List
	items    map[string]*list.Element
}

type byteEntry struct {
	key       string
	value     []byte
	size      int64
	expiresAt time.Time
}

func newByteLRU(maxBytes int64, ttl time.Duration) *byteLRU {
	return &byteLRU{
		maxBytes: maxBytes,
		ttl:      ttl,
		ll:       list.New(),
		items:    map[string]*list.Element{},
	}
}

func (c *byteLRU) get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	entry := el.Value.(*byteEntry)
	if time.Now().After(entry.expiresAt) {
		c.removeElement(el)
		return nil, false
	}
	c.ll.MoveToFront(el)
	return append([]byte(nil), entry.value...), true
}

func (c *byteLRU) set(key string, value []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.removeElement(el)
	}
	entry := &byteEntry{
		key:       key,
		value:     append([]byte(nil), value...),
		size:      int64(len(value)),
		expiresAt: time.Now().Add(c.ttl),
	}
	el := c.ll.PushFront(entry)
	c.items[key] = el
	c.used += entry.size
	for c.used > c.maxBytes && c.ll.Len() > 0 {
		c.removeElement(c.ll.Back())
	}
}

func (c *byteLRU) clean() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for el := c.ll.Back(); el != nil; {
		prev := el.Prev()
		if now.After(el.Value.(*byteEntry).expiresAt) {
			c.removeElement(el)
		}
		el = prev
	}
}

func (c *byteLRU) bytes() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.used
}

func (c *byteLRU) removeElement(el *list.Element) {
	if el == nil {
		return
	}
	c.ll.Remove(el)
	entry := el.Value.(*byteEntry)
	delete(c.items, entry.key)
	c.used -= entry.size
	if c.used < 0 {
		c.used = 0
	}
}

func fileCacheKey(abs string, info os.FileInfo) string {
	return fmt.Sprintf("%s:%d:%d", abs, info.Size(), info.ModTime().UnixNano())
}

// StatsStore is the interface for persisting and querying item stats.
type StatsStore interface {
	Snapshot(targetType, targetID string) itemStats
	IncrementView(targetType, targetID, deviceID string) itemStats
	IncrementReaction(targetType, targetID, deviceID, reaction string, active bool) (itemStats, error)
}

func newStatsStore(cfg Config) (StatsStore, error) {
	switch cfg.Stats.Backend {
	case "postgres":
		if cfg.Stats.Postgres.DSN == "" {
			return nil, errors.New("stats.postgres.dsn is required")
		}
		return newSQLStatsStore("pgx", cfg.Stats.Postgres.DSN)
	case "mysql":
		if cfg.Stats.MySQL.DSN == "" {
			return nil, errors.New("stats.mysql.dsn is required")
		}
		return newSQLStatsStore("mysql", cfg.Stats.MySQL.DSN)
	case "memory":
		return newMemoryStatsStore(), nil
	default:
		// sqlite is the default
		path := cfg.Stats.SQLite.Path
		if path == "" {
			path = "gallery.db"
		}
		return newSQLStatsStore("sqlite", path)
	}
}

func mustNewStatsStore(cfg Config) StatsStore {
	store, err := newStatsStore(cfg)
	if err != nil {
		panic(fmt.Sprintf("failed to initialize stats store: %v", err))
	}
	return store
}

// ── In-memory stats store ──

type memoryStatsStore struct {
	mu              sync.Mutex
	items           map[string]itemStats
	deviceViews     map[string]map[string]bool
	deviceReactions map[string]map[string]string
}

func newMemoryStatsStore() *memoryStatsStore {
	return &memoryStatsStore{
		items:           map[string]itemStats{},
		deviceViews:     map[string]map[string]bool{},
		deviceReactions: map[string]map[string]string{},
	}
}

func (s *memoryStatsStore) Snapshot(targetType, targetID string) itemStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.items[statsKey(targetType, targetID)]
}

func (s *memoryStatsStore) IncrementView(targetType, targetID, deviceID string) itemStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := statsKey(targetType, targetID)

	if s.deviceViews[deviceID] == nil {
		s.deviceViews[deviceID] = map[string]bool{}
	}
	if s.deviceViews[deviceID][key] {
		return s.items[key]
	}
	s.deviceViews[deviceID][key] = true

	stats := s.items[key]
	stats.Views++
	s.items[key] = stats
	return stats
}

func (s *memoryStatsStore) IncrementReaction(targetType, targetID, deviceID, reaction string, active bool) (itemStats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if reaction != "like" && reaction != "dislike" {
		return itemStats{}, fmt.Errorf("%w: unsupported reaction", errBadRequest)
	}

	key := statsKey(targetType, targetID)
	stats := s.items[key]

	if s.deviceReactions[deviceID] == nil {
		s.deviceReactions[deviceID] = map[string]string{}
	}
	existing := s.deviceReactions[deviceID][key]

	if active {
		if existing == reaction {
			return stats, nil
		}
		switch existing {
		case "like":
			if stats.Likes > 0 {
				stats.Likes--
			}
		case "dislike":
			if stats.Dislikes > 0 {
				stats.Dislikes--
			}
		}
		switch reaction {
		case "like":
			stats.Likes++
		case "dislike":
			stats.Dislikes++
		}
		s.deviceReactions[deviceID][key] = reaction
	} else {
		if existing != reaction {
			return stats, nil
		}
		switch reaction {
		case "like":
			if stats.Likes > 0 {
				stats.Likes--
			}
		case "dislike":
			if stats.Dislikes > 0 {
				stats.Dislikes--
			}
		}
		delete(s.deviceReactions[deviceID], key)
	}

	s.items[key] = stats
	return stats, nil
}



func statsKey(targetType, targetID string) string {
	return targetType + ":" + targetID
}

// ── Password session store ──

type sessionStore struct {
	mu     sync.Mutex
	tokens map[string]string // token -> albumID
}

func newSessionStore() *sessionStore {
	return &sessionStore{tokens: map[string]string{}}
}

func (ss *sessionStore) create(albumID string) string {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	token := randomToken()
	ss.tokens[token] = albumID
	return token
}

func (ss *sessionStore) validate(token, albumID string) bool {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	actual, ok := ss.tokens[token]
	return ok && actual == albumID
}

func randomToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, origin := range s.cfg.CORS.AllowedOrigins {
		allowed[origin] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && (allowed["*"] || allowed[origin]) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "POST, GET, HEAD, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Request-ID")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) withRequestLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = randomID()
		}
		w.Header().Set("X-Request-ID", requestID)
		capture := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(capture, r)
		s.log.Info("request",
			"request_id", requestID,
			"method", r.Method,
			"path", r.URL.Path,
			"status", capture.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"remote", r.RemoteAddr,
		)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func randomID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(buf[:])
}

func init() {
	image.RegisterFormat("png", "\x89PNG\r\n\x1a\n", png.Decode, png.DecodeConfig)
}
