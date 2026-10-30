package search

import (
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// maxFrames keeps a clip well inside the model's 58-frame, 8K-token window.
const maxFrames = 8

// videoFrames returns evenly spaced JPEG frames of a clip, or none when ffmpeg cannot read it.
func videoFrames(path string) [][]byte {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries", "format=duration",
		"-of", "default=nw=1:nk=1", path).Output()
	if err != nil {
		return nil
	}
	seconds, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil || seconds <= 0 || math.IsInf(seconds, 0) {
		return nil
	}
	count := min(maxFrames, max(1, int(math.Ceil(seconds/4))))
	dir, err := os.MkdirTemp("", "frames")
	if err != nil {
		return nil
	}
	defer os.RemoveAll(dir)
	rate := strconv.FormatFloat(float64(count)/seconds, 'f', 6, 64)
	cmd := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-i", path, "-vf", "fps="+rate+",scale=448:-2",
		"-frames:v", strconv.Itoa(count), filepath.Join(dir, "%02d.jpg"))
	if cmd.Run() != nil {
		return nil
	}
	names, _ := filepath.Glob(filepath.Join(dir, "*.jpg"))
	sort.Strings(names)
	var frames [][]byte
	for _, name := range names {
		if data, err := os.ReadFile(name); err == nil {
			frames = append(frames, data)
		}
	}
	return frames
}

// maxAudio bounds the soundtrack sent with a clip so frames and audio share the context window.
const maxAudio = 60

// videoAudio returns the clip's first minute as 16 kHz mono WAV, or nil when it has no sound.
func videoAudio(path string) []byte {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dir, err := os.MkdirTemp("", "audio")
	if err != nil {
		return nil
	}
	defer os.RemoveAll(dir)
	out := filepath.Join(dir, "track.wav")
	cmd := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-i", path, "-vn", "-t", strconv.Itoa(maxAudio),
		"-ac", "1", "-ar", "16000", out)
	if cmd.Run() != nil {
		return nil
	}
	data, err := os.ReadFile(out)
	if err != nil || len(data) <= 44 {
		return nil
	}
	return data
}
