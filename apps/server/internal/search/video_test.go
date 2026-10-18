package search

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestVideoFramesSamplesClip(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	path := filepath.Join(t.TempDir(), "clip.mp4")
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc=duration=10:size=160x120:rate=10", "-pix_fmt", "yuv420p", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot make a clip: %v %s", err, out)
	}
	frames := videoFrames(path)
	if len(frames) < 2 || len(frames) > maxFrames {
		t.Fatalf("got %d frames, want 2 to %d", len(frames), maxFrames)
	}
}

func TestVideoFramesIgnoresUnreadable(t *testing.T) {
	if frames := videoFrames(filepath.Join(t.TempDir(), "missing.mp4")); len(frames) != 0 {
		t.Fatalf("got %d frames from a missing file", len(frames))
	}
}

func TestVideoAudioReturnsSoundtrackOnly(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	loud := filepath.Join(dir, "loud.mp4")
	mute := filepath.Join(dir, "mute.mp4")
	for path, args := range map[string][]string{
		loud: {"-f", "lavfi", "-i", "testsrc=d=2:s=160x120:r=10", "-f", "lavfi", "-i", "sine=d=2", "-shortest"},
		mute: {"-f", "lavfi", "-i", "testsrc=d=2:s=160x120:r=10"},
	} {
		args = append([]string{"-v", "error"}, append(args, "-pix_fmt", "yuv420p", path)...)
		if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
			t.Skipf("cannot make a clip: %v %s", err, out)
		}
	}
	if got := videoAudio(loud); len(got) < 1000 {
		t.Fatalf("soundtrack is %d bytes", len(got))
	}
	if got := videoAudio(mute); got != nil {
		t.Fatalf("a silent clip returned %d bytes of audio", len(got))
	}
}
