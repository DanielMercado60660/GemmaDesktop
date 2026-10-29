package execution

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestMediaWorkerCancellationStopsOwnedProcess(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS sandbox")
	}
	if _, e := os.Stat("/opt/homebrew/bin/ffmpeg"); e != nil {
		t.Skip("ffmpeg unavailable")
	}
	root := t.TempDir()
	input := filepath.Join(root, "input.mp4")
	if e := os.WriteFile(input, []byte("fixture"), 0600); e != nil {
		t.Fatal(e)
	}
	output := filepath.Join(root, "output")
	if e := os.Mkdir(output, 0700); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	result, e := RunMedia(ctx, MediaRequest{Program: "ffmpeg", Input: input, OutputDir: output, Args: []string{"-nostdin", "-v", "error", "-re", "-f", "lavfi", "-i", "color=size=64x64:rate=1", "-f", "null", "-"}})
	if !errors.Is(e, context.DeadlineExceeded) || !result.Cancelled || time.Since(start) > 5*time.Second {
		t.Fatalf("owned process cancellation: %+v err=%v", result, e)
	}
}
