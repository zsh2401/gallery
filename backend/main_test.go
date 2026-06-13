package main

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestImageIDRoundTrip(t *testing.T) {
	id := encodeImageID("a/b", "img_001")
	album, base, err := decodeImageID(id)
	if err != nil {
		t.Fatal(err)
	}
	if album != "a/b" || base != "img_001" {
		t.Fatalf("unexpected decode: %q %q", album, base)
	}
}

func TestResolveRejectsTraversal(t *testing.T) {
	tmp := t.TempDir()
	s := testServer(t, tmp)
	if _, err := s.resolveAlbum("../outside"); err == nil {
		t.Fatal("expected traversal album to be rejected")
	}
	if _, err := s.resolveFile("../outside.jpg"); err == nil {
		t.Fatal("expected traversal file to be rejected")
	}
}

func TestExtractLargestJPEG(t *testing.T) {
	small := testJPEG(t, 120, 90)
	large := testJPEG(t, 640, 420)
	payload := append([]byte("prefix"), small...)
	payload = append(payload, []byte("middle")...)
	payload = append(payload, large...)
	payload = append(payload, []byte("suffix")...)

	got, err := extractLargestJPEG(payload)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(got))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != 640 || cfg.Height != 420 {
		t.Fatalf("expected largest preview dimensions, got %dx%d", cfg.Width, cfg.Height)
	}
}

func TestScanAlbumUsesDirectFilesOnly(t *testing.T) {
	tmp := t.TempDir()
	mustTest(t, os.Mkdir(filepath.Join(tmp, "album"), 0o755))
	mustTest(t, os.Mkdir(filepath.Join(tmp, "album", "child"), 0o755))
	mustTest(t, os.WriteFile(filepath.Join(tmp, "album", "one.jpg"), testJPEG(t, 320, 240), 0o644))
	mustTest(t, os.WriteFile(filepath.Join(tmp, "album", "child", "two.jpg"), testJPEG(t, 320, 240), 0o644))

	s := testServer(t, tmp)
	records, err := s.scanAlbum(context.Background(), "album")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected one direct image, got %d", len(records))
	}
	if records[0].Title != "one" {
		t.Fatalf("expected direct image title one, got %q", records[0].Title)
	}
}

func TestListAlbumsKeepsHierarchy(t *testing.T) {
	tmp := t.TempDir()
	mustTest(t, os.Mkdir(filepath.Join(tmp, "album"), 0o755))
	mustTest(t, os.Mkdir(filepath.Join(tmp, "album", "child"), 0o755))
	mustTest(t, os.WriteFile(filepath.Join(tmp, "album", "one.jpg"), testJPEG(t, 320, 240), 0o644))
	mustTest(t, os.WriteFile(filepath.Join(tmp, "album", "child", "two.jpg"), testJPEG(t, 320, 240), 0o644))

	s := testServer(t, tmp)
	albums, err := s.listAlbums(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(albums) != 2 {
		t.Fatalf("expected two albums without synthetic root, got %#v", albums)
	}
	byPath := map[string]albumDTO{}
	for _, album := range albums {
		byPath[album.Path] = album
	}
	if byPath["album"].ParentPath != "" {
		t.Fatalf("top-level album parent = %q, want empty", byPath["album"].ParentPath)
	}
	if byPath["album/child"].ParentPath != "album" {
		t.Fatalf("child parent = %q, want album", byPath["album/child"].ParentPath)
	}
	if byPath["album"].PhotoCount != 1 || byPath["album"].TotalPhotoCount != 2 {
		t.Fatalf("unexpected album counts: %#v", byPath["album"])
	}
	if len(byPath["album"].CoverThumbURLs) != 2 {
		t.Fatalf("expected parent cover to sample subtree images, got %#v", byPath["album"].CoverThumbURLs)
	}
}

func TestAlbumFirstTakenAtAndSortOrder(t *testing.T) {
	tmp := t.TempDir()
	mustTest(t, os.Mkdir(filepath.Join(tmp, "older"), 0o755))
	mustTest(t, os.Mkdir(filepath.Join(tmp, "newer"), 0o755))
	mustTest(t, os.WriteFile(filepath.Join(tmp, "older", "one.jpg"), testJPEG(t, 320, 240), 0o644))
	mustTest(t, os.WriteFile(filepath.Join(tmp, "newer", "one.jpg"), testJPEG(t, 320, 240), 0o644))
	olderTime := time.Date(2022, 1, 2, 10, 0, 0, 0, time.UTC)
	newerTime := time.Date(2024, 3, 4, 10, 0, 0, 0, time.UTC)
	mustTest(t, os.Chtimes(filepath.Join(tmp, "older", "one.jpg"), olderTime, olderTime))
	mustTest(t, os.Chtimes(filepath.Join(tmp, "newer", "one.jpg"), newerTime, newerTime))

	s := testServer(t, tmp)
	albums, err := s.listAlbums(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(albums) != 2 {
		t.Fatalf("expected two albums, got %#v", albums)
	}
	if albums[0].Name != "newer" || albums[1].Name != "older" {
		t.Fatalf("albums should sort by firstTakenAt desc, got %#v", albums)
	}
	firstTakenAt, err := time.Parse(time.RFC3339, albums[1].FirstTakenAt)
	if err != nil {
		t.Fatal(err)
	}
	if !firstTakenAt.Equal(olderTime) {
		t.Fatalf("firstTakenAt = %q, want same instant as %q", albums[1].FirstTakenAt, olderTime.Format(time.RFC3339))
	}
}

func TestAlbumReadmeAndImageDTOHideFilePaths(t *testing.T) {
	tmp := t.TempDir()
	mustTest(t, os.Mkdir(filepath.Join(tmp, "album"), 0o755))
	mustTest(t, os.WriteFile(filepath.Join(tmp, "album", "README.md"), []byte("# Album\n\nA quiet note."), 0o644))
	mustTest(t, os.WriteFile(filepath.Join(tmp, "album", "one.jpg"), testJPEG(t, 320, 240), 0o644))

	s := testServer(t, tmp)
	albums, err := s.listAlbums(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(albums) != 1 || !strings.Contains(albums[0].Readme, "quiet note") {
		t.Fatalf("expected README to be exposed on album DTO, got %#v", albums)
	}
	records, err := s.scanAlbum(context.Background(), "album")
	if err != nil {
		t.Fatal(err)
	}
	dto := s.imageDTO(records[0])
	if dto.Path != "one.jpg" || strings.Contains(dto.Path, "/") {
		t.Fatalf("image DTO path should be display filename only, got %#v", dto.Path)
	}
}

func TestStatsStoreIncrementsViewsAndReactions(t *testing.T) {
	store := newMemoryStatsStore()
	first := store.IncrementView("album", "a", "test-device")
	if first.Views != 1 || first.Likes != 0 || first.Dislikes != 0 {
		t.Fatalf("unexpected first view stats: %#v", first)
	}
	// Same device viewing again should not increment
	second := store.IncrementView("album", "a", "test-device")
	if second.Views != 1 {
		t.Fatalf("duplicate view from same device should not increment, got %#v", second)
	}
	// Different device should increment
	third := store.IncrementView("album", "a", "test-device-2")
	if third.Views != 2 {
		t.Fatalf("view from different device should increment, got %#v", third)
	}
	liked, err := store.IncrementReaction("album", "a", "test-device", "like", true)
	if err != nil {
		t.Fatal(err)
	}
	if liked.Likes != 1 || liked.Dislikes != 0 {
		t.Fatalf("unexpected like stats: %#v", liked)
	}
	// Same device liking again with active=true should be a no-op
	liked2, err := store.IncrementReaction("album", "a", "test-device", "like", true)
	if err != nil {
		t.Fatal(err)
	}
	if liked2.Likes != 1 {
		t.Fatalf("duplicate like from same device should not increment: %#v", liked2)
	}
	// Switch to dislike
	disliked, err := store.IncrementReaction("album", "a", "test-device", "dislike", true)
	if err != nil {
		t.Fatal(err)
	}
	if disliked.Likes != 0 || disliked.Dislikes != 1 {
		t.Fatalf("switching reaction should decrement old and increment new: %#v", disliked)
	}
	// Remove reaction (active=false)
	removed, err := store.IncrementReaction("album", "a", "test-device", "dislike", false)
	if err != nil {
		t.Fatal(err)
	}
	if removed.Dislikes != 0 {
		t.Fatalf("removing reaction should decrement: %#v", removed)
	}
	if _, err := store.IncrementReaction("album", "a", "test-device", "wow", true); err == nil {
		t.Fatal("expected unsupported reaction to fail")
	}
}

func TestEXIFFormattingHelpers(t *testing.T) {
	if got := cleanEXIFString(`"Sony Alpha"`); got != "Sony Alpha" {
		t.Fatalf("cleanEXIFString stripped quotes incorrectly: %q", got)
	}
	if got := formatAperture("28/10"); got != "F2.8" {
		t.Fatalf("formatAperture = %q, want F2.8", got)
	}
	fields := map[string]string{
		"SonyMakerNote:ImageCount": "12345",
		"xmp:Rating":               "4",
	}
	if got := firstFieldByNames(fields, "ImageCount", "ShutterCount"); got != "12345" {
		t.Fatalf("expected normalized Sony image count lookup, got %q", got)
	}
	if got := firstFieldByNames(fields, "Rating"); got != "4" {
		t.Fatalf("expected normalized rating lookup, got %q", got)
	}
}

func TestPrettyLoggerUsesANSIColors(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(newPrettyHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	logger.Info("hello", "status", 200)
	output := buf.String()
	if !strings.Contains(output, "\x1b[32m") || !strings.Contains(output, "status=") || !strings.Contains(output, "200") {
		t.Fatalf("expected colored pretty log output, got %q", output)
	}
}

func testServer(t *testing.T, root string) *Server {
	t.Helper()
	var cfg Config
	cfg.ImageRoot = root
	cfg.Server.Bind = "127.0.0.1:0"
	cfg.Cache.Backend = "memory"
	cfg.Cache.TTL.Duration = time.Hour
	cfg.Cache.ThumbnailMaxBytes = 10 << 20
	cfg.Logging.Level = "error"
	cfg.Logging.Format = "pretty"
	cfg.Features.RawPreview = "embedded-jpeg"
	cfg.Features.MapProvider = "amap"
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := newServer(cfg, log)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func testJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.RGBA{80, 92, 87, 255}}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(width/5, height/5, width*4/5, height*4/5), &image.Uniform{C: color.RGBA{226, 226, 218, 255}}, image.Point{}, draw.Src)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func mustTest(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
