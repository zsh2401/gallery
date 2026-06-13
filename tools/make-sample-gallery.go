package main

import (
	"flag"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"log"
	"os"
	"path/filepath"
)

func main() {
	out := flag.String("out", ".sample-gallery", "output gallery directory")
	flag.Parse()

	must(os.RemoveAll(*out))
	must(os.MkdirAll(filepath.Join(*out, "studio"), 0o755))
	must(os.MkdirAll(filepath.Join(*out, "walks", "morning"), 0o755))
	must(os.WriteFile(filepath.Join(*out, "studio", "README.md"), []byte("# Studio\n\nSoft desk light, quiet objects, and simple textures."), 0o644))
	must(os.WriteFile(filepath.Join(*out, "walks", "README.md"), []byte("# Walks\n\nA small timeline of street walks and morning windows."), 0o644))

	writeJPEG(filepath.Join(*out, "studio", "lamp.jpg"), 960, 640, color.RGBA{52, 66, 61, 255}, color.RGBA{205, 211, 196, 255})
	writePNG(filepath.Join(*out, "studio", "desk.png"), 900, 900, color.RGBA{85, 70, 88, 255}, color.RGBA{220, 222, 214, 255})
	writeJPEG(filepath.Join(*out, "walks", "street.jpg"), 1080, 720, color.RGBA{39, 70, 83, 255}, color.RGBA{225, 229, 219, 255})
	writeJPEG(filepath.Join(*out, "walks", "morning", "window.jpg"), 720, 1080, color.RGBA{80, 92, 87, 255}, color.RGBA{226, 226, 218, 255})

	log.Printf("sample gallery written to %s", *out)
}

func writeJPEG(path string, width, height int, bg, fg color.RGBA) {
	img := sampleImage(width, height, bg, fg)
	file, err := os.Create(path)
	must(err)
	defer file.Close()
	must(jpeg.Encode(file, img, &jpeg.Options{Quality: 88}))
}

func writePNG(path string, width, height int, bg, fg color.RGBA) {
	img := sampleImage(width, height, bg, fg)
	file, err := os.Create(path)
	must(err)
	defer file.Close()
	must(png.Encode(file, img))
}

func sampleImage(width, height int, bg, fg color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: bg}, image.Point{}, draw.Src)
	margin := min(width, height) / 8
	draw.Draw(img, image.Rect(margin, margin, width-margin, height-margin), &image.Uniform{C: fg}, image.Point{}, draw.Src)
	for i := 0; i < min(width, height); i += 18 {
		c := color.RGBA{uint8((int(bg.R) + i) % 255), uint8((int(fg.G) + i/2) % 255), uint8((int(bg.B) + i/3) % 255), 255}
		draw.Draw(img, image.Rect(i, 0, min(i+6, width), height), &image.Uniform{C: c}, image.Point{}, draw.Src)
	}
	return img
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
