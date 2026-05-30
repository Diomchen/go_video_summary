package transcribe

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"go_subtitle_whisper/internal/service"
)

type LocalWhisperTranscriber struct {
	binPath   string
	modelPath string
	argsTpl   string
	noGPU     bool
}

func NewLocalWhisperTranscriber(binPath, modelPath, argsTpl string, noGPU bool) *LocalWhisperTranscriber {
	return &LocalWhisperTranscriber{
		binPath:   binPath,
		modelPath: modelPath,
		argsTpl:   argsTpl,
		noGPU:     noGPU,
	}
}

func (t *LocalWhisperTranscriber) TranscribeFile(ctx context.Context, filename string, data []byte, language string) (string, error) {
	return t.TranscribeFileWithProgress(ctx, filename, data, language, nil)
}

func (t *LocalWhisperTranscriber) TranscribeFileWithProgress(ctx context.Context, filename string, data []byte, language string, onProgress func(service.ProgressUpdate)) (string, error) {
	if strings.TrimSpace(t.modelPath) == "" {
		return "", fmt.Errorf("WHISPER_LOCAL_MODEL is required when WHISPER_BACKEND=local")
	}
	if onProgress != nil {
		onProgress(service.ProgressUpdate{Percent: 1, Message: "preparing"})
	}

	tmpDir, err := os.MkdirTemp("", "local-whisper-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)

	inputPath := filepath.Join(tmpDir, filepath.Base(filename))
	if filepath.Ext(inputPath) == "" {
		inputPath += ".wav"
	}
	if err := os.WriteFile(inputPath, data, 0o600); err != nil {
		return "", err
	}

	wavInputPath, err := maybeConvertToWAV(ctx, inputPath, tmpDir)
	if err != nil {
		return "", err
	}
	if onProgress != nil {
		onProgress(service.ProgressUpdate{Percent: 8, Message: "preparing"})
	}

	outputPrefix := filepath.Join(tmpDir, "result")
	argsLine := t.renderArgs(wavInputPath, outputPrefix, language)
	args, err := splitArgs(argsLine)
	if err != nil {
		return "", err
	}
	args = ensureProgressFlag(args)
	args = ensureGPUPreference(args, t.noGPU)

	cmd := exec.CommandContext(ctx, t.binPath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}

	var output bytes.Buffer
	done := make(chan struct{}, 2)
	var outputMu sync.Mutex
	go streamWhisperOutput(stdout, &output, &outputMu, onProgress, done)
	go streamWhisperOutput(stderr, &output, &outputMu, onProgress, done)
	<-done
	<-done

	err = cmd.Wait()
	if err != nil {
		return "", fmt.Errorf("local whisper failed: %w: %s", err, strings.TrimSpace(output.String()))
	}

	textData, err := os.ReadFile(outputPrefix + ".txt")
	if err != nil {
		return "", fmt.Errorf("read whisper output: %w", err)
	}
	if onProgress != nil {
		onProgress(service.ProgressUpdate{Percent: 100, Message: "transcribing"})
	}
	return strings.TrimSpace(string(textData)), nil
}

func (t *LocalWhisperTranscriber) renderArgs(inputPath, outputPrefix, language string) string {
	replacer := strings.NewReplacer(
		"{{model}}", t.modelPath,
		"{{input}}", inputPath,
		"{{output}}", outputPrefix,
		"{{language}}", strings.TrimSpace(language),
		"{{language_flag}}", languageFlag(language),
	)
	return replacer.Replace(t.argsTpl)
}

func ensureProgressFlag(args []string) []string {
	for _, arg := range args {
		if arg == "-pp" || arg == "--print-progress" {
			return args
		}
	}
	return append(args, "-pp")
}

func ensureGPUPreference(args []string, noGPU bool) []string {
	hasNoGPU := false
	for _, arg := range args {
		if arg == "-ng" || arg == "--no-gpu" {
			hasNoGPU = true
			break
		}
	}
	if noGPU {
		if hasNoGPU {
			return args
		}
		return append(args, "-ng")
	}
	if !hasNoGPU {
		return args
	}
	filtered := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "-ng" || arg == "--no-gpu" {
			continue
		}
		filtered = append(filtered, arg)
	}
	return filtered
}

var validLanguageCode = regexp.MustCompile(`^[a-zA-Z]{2,10}$`)

func languageFlag(language string) string {
	language = strings.TrimSpace(language)
	if language == "" || strings.EqualFold(language, "auto") {
		return ""
	}
	if !validLanguageCode.MatchString(language) {
		return ""
	}
	return "-l " + language
}

func splitArgs(input string) ([]string, error) {
	var args []string
	var current strings.Builder
	var quote rune

	flush := func() {
		if current.Len() == 0 {
			return
		}
		args = append(args, current.String())
		current.Reset()
	}

	for _, r := range input {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				current.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote = r
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			flush()
		default:
			current.WriteRune(r)
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quote in WHISPER_LOCAL_ARGS")
	}
	flush()
	return args, nil
}

func maybeConvertToWAV(ctx context.Context, inputPath, tmpDir string) (string, error) {
	if strings.EqualFold(filepath.Ext(inputPath), ".wav") {
		return inputPath, nil
	}

	ffmpegBin := resolveFFmpegBin()
	if ffmpegBin == "" {
		return inputPath, nil
	}

	outputPath := filepath.Join(tmpDir, "converted.wav")
	cmd := exec.CommandContext(
		ctx,
		ffmpegBin,
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-i", inputPath,
		"-ar", "16000",
		"-ac", "1",
		outputPath,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("ffmpeg convert failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return outputPath, nil
}

var percentPattern = regexp.MustCompile(`(\d{1,3})%`)

func streamWhisperOutput(reader io.Reader, output *bytes.Buffer, outputMu *sync.Mutex, onProgress func(service.ProgressUpdate), done chan<- struct{}) {
	defer func() { done <- struct{}{} }()

	scanner := bufio.NewScanner(reader)
	scanner.Split(scanCRLF)
	lastPercent := -1.0
	for scanner.Scan() {
		line := scanner.Text()
		outputMu.Lock()
		output.WriteString(line)
		output.WriteByte('\n')
		outputMu.Unlock()

		if onProgress == nil {
			continue
		}
		match := percentPattern.FindStringSubmatch(line)
		if len(match) < 2 {
			continue
		}
		percentText := match[1]
		var percent float64
		fmt.Sscanf(percentText, "%f", &percent)
		if percent > lastPercent {
			lastPercent = percent
			onProgress(service.ProgressUpdate{
				Percent: percent,
				Message: "transcribing",
			})
		}
	}
}

func scanCRLF(data []byte, atEOF bool) (advance int, token []byte, err error) {
	for i, b := range data {
		if b == '\n' || b == '\r' {
			return i + 1, dropCR(data[:i]), nil
		}
	}
	if atEOF && len(data) > 0 {
		return len(data), dropCR(data), nil
	}
	return 0, nil, nil
}

func dropCR(data []byte) []byte {
	if len(data) > 0 && data[len(data)-1] == '\r' {
		return data[:len(data)-1]
	}
	return data
}

func resolveFFmpegBin() string {
	if path, err := exec.LookPath("ffmpeg"); err == nil {
		return path
	}

	candidates := []string{
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "WinGet", "Links", "ffmpeg.exe"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "WindowsApps", "ffmpeg.exe"),
	}
	patterns := []string{
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "WinGet", "Packages", "*", "*", "bin", "ffmpeg.exe"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "WinGet", "Packages", "*", "*", "ffmpeg.exe"),
	}
	for _, pattern := range patterns {
		if matches, err := filepath.Glob(pattern); err == nil {
			candidates = append(candidates, matches...)
		}
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}
