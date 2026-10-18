package server

import (
	"context"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"os"
	"os/exec"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

func makeThumbnail(ctx context.Context, path, output, kind string) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if kind == "video" {
		cmd := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-y", "-ss", "0", "-i", path, "-frames:v", "1", "-vf", "scale=512:-2", output)
		if data, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("video poster: %w (%s)", err, data)
		}
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	config, _, err := image.DecodeConfig(f)
	if err != nil {
		cmd := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-y", "-i", path, "-frames:v", "1", "-vf", "scale=512:-2", output)
		return cmd.Run()
	}
	if config.Width > 16000 || config.Height > 16000 || int64(config.Width)*int64(config.Height) > 64_000_000 {
		return fmt.Errorf("image is too large for thumbnail")
	}
	if _, err := f.Seek(0, 0); err != nil {
		return err
	}
	img, _, err := image.Decode(f)
	if err != nil {
		cmd := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-y", "-i", path, "-frames:v", "1", "-vf", "scale=512:-2", output)
		return cmd.Run()
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	if w > 512 || h > 512 {
		if w > h {
			h = max(1, h*512/w)
			w = 512
		} else {
			w = max(1, w*512/h)
			h = 512
		}
	}
	thumb := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(thumb, thumb.Bounds(), img, img.Bounds(), draw.Over, nil)
	out, err := os.OpenFile(output, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	err = jpeg.Encode(out, thumb, &jpeg.Options{Quality: 80})
	return errorsJoin(err, out.Close())
}

func errorsJoin(a, b error) error {
	if a != nil {
		return a
	}
	return b
}
