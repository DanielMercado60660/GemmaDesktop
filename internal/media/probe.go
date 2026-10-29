package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"gemmadesktop.local/app/internal/execution"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type probeStream struct {
	CodecType   string            `json:"codec_type"`
	CodecName   string            `json:"codec_name"`
	Width       int               `json:"width"`
	Height      int               `json:"height"`
	PixelFormat string            `json:"pix_fmt"`
	Rate        string            `json:"r_frame_rate"`
	AverageRate string            `json:"avg_frame_rate"`
	TimeBase    string            `json:"time_base"`
	Start       string            `json:"start_time"`
	Transfer    string            `json:"color_transfer"`
	Primaries   string            `json:"color_primaries"`
	Tags        map[string]string `json:"tags"`
	SideData    []struct {
		Rotation float64 `json:"rotation"`
	} `json:"side_data_list"`
}
type probeMetadata struct {
	Streams []probeStream `json:"streams"`
	Format  struct {
		Duration string `json:"duration"`
		Start    string `json:"start_time"`
		Name     string `json:"format_name"`
	} `json:"format"`
}
type probeFrames struct {
	Frames []struct {
		PTS        *int64 `json:"best_effort_timestamp"`
		Interlaced int    `json:"interlaced_frame"`
	} `json:"frames"`
}

func mediaInputArgs(input string) []string {
	return []string{"-protocol_whitelist", "file", "-format_whitelist", "mov", "-codec_whitelist", "h264,aac", "-enable_drefs", "0", "-use_absolute_path", "0", "-threads", "1", "-i", input}
}
func probe(ctx context.Context, input, output string) (manifest, error) {
	args := []string{"-v", "error", "-max_alloc", "67108864"}
	args = append(args, mediaInputArgs(input)...)
	args = append(args, "-show_streams", "-show_format", "-of", "json")
	result, err := execution.RunMedia(ctx, execution.MediaRequest{Program: "ffprobe", Input: input, OutputDir: output, Args: args, Timeout: 30 * time.Second, MaxOutput: 256 << 10})
	if err != nil {
		return manifest{}, err
	}
	var metadata probeMetadata
	if err = json.Unmarshal([]byte(result.Stdout), &metadata); err != nil {
		return manifest{}, fmt.Errorf("decode video metadata: %w", err)
	}
	identity, err := extractorIdentity(ctx, "ffprobe", input, output)
	if err != nil {
		return manifest{}, err
	}
	stream, duration, err := validateMetadata(metadata)
	if err != nil {
		return manifest{}, err
	}
	args = []string{"-v", "error", "-max_alloc", "67108864"}
	args = append(args, mediaInputArgs(input)...)
	args = append(args, "-select_streams", "v:0", "-read_intervals", "%+91", "-show_frames", "-show_entries", "frame=best_effort_timestamp,interlaced_frame", "-of", "json")
	result, err = execution.RunMedia(ctx, execution.MediaRequest{Program: "ffprobe", Input: input, OutputDir: output, Args: args, Timeout: 90 * time.Second, MaxOutput: 2 << 20})
	if err != nil {
		return manifest{}, err
	}
	var frames probeFrames
	if err = json.Unmarshal([]byte(result.Stdout), &frames); err != nil {
		return manifest{}, fmt.Errorf("decode source frame timestamps: %w", err)
	}
	resultManifest, err := validateFrames(stream, duration, frames)
	resultManifest.ProbeIdentity = identity
	return resultManifest, err
}
func ratio(s string) (int64, int64, error) {
	p := strings.Split(s, "/")
	if len(p) != 2 {
		return 0, 0, errors.New("invalid media time base")
	}
	n, e := strconv.ParseInt(p[0], 10, 32)
	if e != nil {
		return 0, 0, e
	}
	d, e := strconv.ParseInt(p[1], 10, 32)
	if e != nil || n <= 0 || d <= 0 {
		return 0, 0, errors.New("invalid media time base")
	}
	return n, d, nil
}
func finite(s string) (float64, error) {
	v, e := strconv.ParseFloat(s, 64)
	if e != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, errors.New("missing or invalid video duration/timestamp")
	}
	return v, nil
}
func validateMetadata(p probeMetadata) (probeStream, float64, error) {
	var video probeStream
	count := 0
	for _, s := range p.Streams {
		if s.CodecType == "audio" && s.CodecName != "aac" {
			return video, 0, errors.New("optional audio must be AAC; this feature ignores audio and speech")
		}
		if s.CodecType == "video" {
			video = s
			count++
		}
	}
	if count != 1 {
		return video, 0, errors.New("choose a clip with exactly one video stream")
	}
	if video.CodecName != "h264" || !strings.Contains(p.Format.Name, "mp4") {
		return video, 0, errors.New("this visual preview supports MP4 with H.264 video only")
	}
	if video.Width < 1 || video.Height < 1 || video.Width > 1920 || video.Height > 1080 {
		return video, 0, errors.New("video dimensions must fit 1920 × 1080")
	}
	if video.PixelFormat != "yuv420p" && video.PixelFormat != "yuvj420p" {
		return video, 0, errors.New("use 8-bit SDR yuv420p video; HDR and other pixel formats are not supported")
	}
	if video.Transfer == "smpte2084" || video.Transfer == "arib-std-b67" || video.Primaries == "bt2020" {
		return video, 0, errors.New("HDR video is not supported")
	}
	if rotate := video.Tags["rotate"]; rotate != "" && rotate != "0" {
		return video, 0, errors.New("rotated video is not supported; export an upright clip")
	}
	for _, d := range video.SideData {
		if d.Rotation != 0 {
			return video, 0, errors.New("rotated video is not supported; export an upright clip")
		}
	}
	start, e := finite(video.Start)
	if e != nil || math.Abs(start) > 0.000001 {
		return video, 0, errors.New("video must start at presentation timestamp zero")
	}
	start, e = finite(p.Format.Start)
	if e != nil || math.Abs(start) > 0.000001 {
		return video, 0, errors.New("container timeline must start at zero")
	}
	duration, e := finite(p.Format.Duration)
	if e != nil || duration <= 0 || duration > 90 {
		return video, 0, errors.New("clip duration must be greater than zero and at most 90 seconds")
	}
	n, d, e := ratio(video.Rate)
	if e != nil || float64(n)/float64(d) < 1 || float64(n)/float64(d) > 60 {
		return video, 0, errors.New("CFR video must use 1 to 60 frames per second")
	}
	return video, duration, nil
}
func validateFrames(s probeStream, duration float64, p probeFrames) (manifest, error) {
	result := manifest{Duration: duration, Width: s.Width, Height: s.Height, Samples: []sample{}}
	num, den, e := ratio(s.TimeBase)
	if e != nil {
		return result, e
	}
	result.TimeBaseNum, result.TimeBaseDen = num, den
	rateN, rateD, e := ratio(s.Rate)
	if e != nil {
		return result, e
	}
	interval := float64(rateD) / float64(rateN)
	tick := float64(num) / float64(den)
	if len(p.Frames) < 1 || len(p.Frames) > 5400 {
		return result, errors.New("video has missing or too many decoded frames")
	}
	nextSecond := 0.
	previous := int64(-1)
	for i, f := range p.Frames {
		if f.PTS == nil || *f.PTS < 0 || i == 0 && *f.PTS != 0 || i > 0 && *f.PTS <= previous || f.Interlaced != 0 {
			return result, errors.New("video requires monotonic zero-based timestamps and progressive frames")
		}
		seconds := float64(*f.PTS) * tick
		if math.Abs(seconds-float64(i)*interval) > tick*1.01 {
			return result, errors.New("variable-frame-rate video is not supported; export a CFR clip")
		}
		if seconds+tick/2 >= nextSecond && nextSecond < duration {
			result.Samples = append(result.Samples, sample{Ordinal: i, PTS: *f.PTS, TimeSeconds: seconds})
			nextSecond++
		}
		previous = *f.PTS
	}
	measured := float64(previous)*tick + interval
	if math.Abs(measured-duration) > interval*1.1 || len(result.Samples) > 90 {
		return result, errors.New("container duration disagrees with the decoded video timeline")
	}
	return result, nil
}

var extractedPTS = regexp.MustCompile(`n:\s*\d+\s+pts:\s*(-?\d+)\s+pts_time:`)

func extract(ctx context.Context, input, output string, m manifest) ([]string, error) {
	terms := make([]string, len(m.Samples))
	for i, s := range m.Samples {
		terms[i] = fmt.Sprintf("eq(n\\,%d)", s.Ordinal)
	}
	filter := "select=" + strings.Join(terms, "+") + ",showinfo,scale=768:768:force_original_aspect_ratio=decrease:force_divisible_by=2"
	args := []string{"-hide_banner", "-nostdin", "-v", "info", "-max_alloc", "67108864", "-filter_threads", "1", "-filter_complex_threads", "1", "-noautorotate"}
	args = append(args, mediaInputArgs(input)...)
	args = append(args, "-map", "0:v:0", "-an", "-sn", "-dn", "-vf", filter, "-fps_mode", "passthrough", "-frames:v", strconv.Itoa(len(m.Samples)), "-threads", "1", "-c:v", "mjpeg", "-q:v", "3", "-f", "image2", "-start_number", "0", "-atomic_writing", "1", "-n", filepath.Join(output, "frame-%03d.jpg"))
	result, e := execution.RunMedia(ctx, execution.MediaRequest{Program: "ffmpeg", Input: input, OutputDir: output, Args: args, Timeout: 2 * time.Minute, MaxOutput: 64 << 10})
	if e != nil {
		return nil, e
	}
	matches := extractedPTS.FindAllStringSubmatch(result.Stderr, -1)
	if len(matches) != len(m.Samples) {
		return nil, errors.New("extractor did not report every selected source timestamp")
	}
	paths := make([]string, len(m.Samples))
	for i, match := range matches {
		pts, e := strconv.ParseInt(match[1], 10, 64)
		if e != nil || pts != m.Samples[i].PTS {
			return nil, errors.New("extracted frame timestamp differs from its source manifest")
		}
		paths[i] = filepath.Join(output, fmt.Sprintf("frame-%03d.jpg", i))
		st, e := os.Lstat(paths[i])
		if e != nil || !st.Mode().IsRegular() || st.Size() < 1 || st.Size() > 1<<20 {
			return nil, errors.New("extracted frame is missing or exceeds 1 MiB")
		}
	}
	return paths, nil
}

// Keep the executable identity alongside the fixed preprocessing version. The
// version command itself runs under the same no-network/no-fork boundary.
func extractorIdentity(ctx context.Context, program, input, output string) (string, error) {
	result, e := execution.RunMedia(ctx, execution.MediaRequest{Program: program, Input: input, OutputDir: output, Args: []string{"-version"}, Timeout: 5 * time.Second, MaxOutput: 16 << 10})
	if e != nil {
		return "", e
	}
	line := strings.SplitN(result.Stdout, "\n", 2)[0]
	if !strings.HasPrefix(line, program+" version 8.1 ") {
		return "", fmt.Errorf("unqualified %s version: require 8.1", program)
	}
	executable, e := filepath.EvalSymlinks(filepath.Join("/opt/homebrew/bin", program))
	if e != nil {
		return "", e
	}
	f, e := os.Open(executable)
	if e != nil {
		return "", e
	}
	defer f.Close()
	hash := sha256.New()
	n, e := io.Copy(hash, io.LimitReader(f, (64<<20)+1))
	if e != nil {
		return "", e
	}
	if n > 64<<20 {
		return "", errors.New("media executable exceeds identity size bound")
	}
	return line + ";sha256=" + hex.EncodeToString(hash.Sum(nil)), nil
}
