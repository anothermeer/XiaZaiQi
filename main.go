package main

import (
	"archive/zip"
	"bufio"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

//go:embed bin/*
var embeddedTools embed.FS

func main() {
	a := app.New()
	w := a.NewWindow("万能下载器 - https://melonlinks.top")
	w.Resize(fyne.NewSize(480, 330))
	w.SetFixedSize(true)

	urlEntry := widget.NewEntry()
	urlEntry.SetPlaceHolder("https://")

	downloadsDir := detectDownloadDir()
	pathEntry := widget.NewEntry()
	pathEntry.SetText(downloadsDir)

	selectPathBtn := widget.NewButton("...", func() {
		dialog.ShowFolderOpen(func(u fyne.ListableURI, err error) {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if u == nil {
				return
			}
			pathEntry.SetText(u.Path())
		}, w)
	})
	selectPathBtn.Importance = widget.MediumImportance

	statusLabel := widget.NewLabel("")
	statusLabel.Wrapping = fyne.TextWrapWord
	progressBar := widget.NewProgressBar()
	progressBar.Min = 0
	progressBar.Max = 1
	progressBar.SetValue(0)
	progressBar.Hide()
	var videoBtn *widget.Button
	var audioBtn *widget.Button

	setBusyState := func(busy bool) {
		if videoBtn != nil {
			videoBtn.Disable()
		}
		if audioBtn != nil {
			audioBtn.Disable()
		}
		urlEntry.Disable()
		pathEntry.Disable()
		selectPathBtn.Disable()
		if busy {
			statusLabel.SetText("正在下载，请稍候...")
			progressBar.SetValue(0)
			progressBar.Show()
			return
		}
		if videoBtn != nil {
			videoBtn.Enable()
		}
		if audioBtn != nil {
			audioBtn.Enable()
		}
		urlEntry.Enable()
		pathEntry.Enable()
		selectPathBtn.Enable()
		progressBar.Hide()
		statusLabel.SetText("")
	}

	runDownload := func(audioOnly bool) {
		url := strings.TrimSpace(urlEntry.Text)
		if url == "" {
			dialog.ShowInformation("提示", "请输入视频/音频链接。", w)
			return
		}

		outDir := strings.TrimSpace(pathEntry.Text)
		if outDir == "" {
			dialog.ShowInformation("提示", "请选择下载地点。", w)
			return
		}

		if err := os.MkdirAll(outDir, 0o755); err != nil {
			dialog.ShowError(fmt.Errorf("无法创建下载目录: %w", err), w)
			return
		}

		ytdlpPath, ffmpegPath, err := resolveTools()
		if err != nil {
			dialog.ShowError(err, w)
			return
		}

		setBusyState(true)
		go func() {
			args := []string{
				"--no-playlist",
				"--newline",
				"--ignore-errors",
				"--ffmpeg-location", filepath.Dir(ffmpegPath),
				"-o", filepath.Join(outDir, "%(title)s.%(ext)s"),
			}

			if audioOnly {
				args = append(args,
					"-f", "bestaudio/best",
					"-x",
					"--audio-format", "mp3",
					"--audio-quality", "0",
				)
			} else {
				args = append(args,
					"-f", "bv*+ba/best",
					"--merge-output-format", "mp4",
				)
			}

			args = append(args, url)

			cmd := exec.Command(ytdlpPath, args...)
			stdout, pipeErr := cmd.StdoutPipe()
			if pipeErr != nil {
				fyne.Do(func() {
					setBusyState(false)
					dialog.ShowError(fmt.Errorf("下载失败: %w", pipeErr), w)
					statusLabel.SetText("下载失败。")
				})
				return
			}
			cmd.Stderr = cmd.Stdout

			if startErr := cmd.Start(); startErr != nil {
				fyne.Do(func() {
					setBusyState(false)
					dialog.ShowError(fmt.Errorf("启动下载失败: %w", startErr), w)
					statusLabel.SetText("下载失败。")
				})
				return
			}

			percentRe := regexp.MustCompile(`\[download\]\s+(\d+(?:\.\d+)?)%`)
			var outputLines []string
			scanner := bufio.NewScanner(stdout)
			for scanner.Scan() {
				line := scanner.Text()
				outputLines = append(outputLines, line)
				if len(outputLines) > 80 {
					outputLines = outputLines[len(outputLines)-80:]
				}

				matches := percentRe.FindStringSubmatch(line)
				if len(matches) == 2 {
					if p, convErr := strconv.ParseFloat(matches[1], 64); convErr == nil {
						value := p / 100.0
						if value > 1 {
							value = 1
						}
						fyne.Do(func() {
							progressBar.SetValue(value)
							if p >= 100 {
								statusLabel.SetText("正在处理文件")
								return
							}
							statusLabel.SetText(fmt.Sprintf("正在下载: %.1f%%", p))
						})
					}
				}

				if strings.Contains(line, "[Merger]") ||
					strings.Contains(line, "[ExtractAudio]") ||
					strings.Contains(line, "Deleting original file") {
					fyne.Do(func() {
						progressBar.SetValue(1)
						statusLabel.SetText("正在处理文件")
					})
				}
			}
			if scanErr := scanner.Err(); scanErr != nil {
				outputLines = append(outputLines, scanErr.Error())
			}
			cmdErr := cmd.Wait()
			result := strings.TrimSpace(strings.Join(outputLines, "\n"))

			fyne.Do(func() {
				setBusyState(false)
				if cmdErr != nil {
					if result == "" {
						result = cmdErr.Error()
					}
					dialog.ShowError(fmt.Errorf("下载失败: %s", result), w)
					statusLabel.SetText("下载失败。")
					return
				}
				dialog.ShowInformation("完成", "下载成功！", w)
				statusLabel.SetText("下载完成。")
			})
		}()
	}

	videoBtn = widget.NewButton("下载视频", func() {
		runDownload(false)
	})
	audioBtn = widget.NewButton("下载音频", func() {
		runDownload(true)
	})

	//title := widget.NewLabelWithStyle("万能下载器", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	//closeBtn := widget.NewButton("X", func() { w.Close() })
	//closeBtn.Importance = widget.DangerImportance

	urlSection := container.NewVBox(
		widget.NewLabelWithStyle("视频/音频链接", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		urlEntry,
	)

	pathSection := container.NewVBox(
		widget.NewLabelWithStyle("下载地点", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		container.NewBorder(nil, nil, nil, selectPathBtn, pathEntry),
	)

	buttons := container.NewHBox(
		layout.NewSpacer(),
		videoBtn,
		audioBtn,
		layout.NewSpacer(),
	)

	content := container.NewPadded(container.NewVBox(
		//container.NewBorder(nil, nil, title, closeBtn, widget.NewLabel("")),
		urlSection,
		pathSection,
		layout.NewSpacer(),
		buttons,
		progressBar,
		statusLabel,
		layout.NewSpacer(),
		widget.NewLabelWithStyle("Anothermeer © 2026", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
	))

	w.SetContent(content)
	w.ShowAndRun()
}

func detectDownloadDir() string {
	if runtime.GOOS == "windows" {
		home, err := os.UserHomeDir()
		if err == nil && home != "" {
			d := filepath.Join(home, "Downloads")
			if _, statErr := os.Stat(d); statErr == nil {
				return d
			}
		}
	}

	if d, err := os.UserHomeDir(); err == nil {
		return d
	}
	return "."
}

func resolveTools() (string, string, error) {
	ytdlpPath, ytdlpFound := findOnPath("yt-dlp")
	ffmpegPath, ffmpegFound := findOnPath("ffmpeg")

	if ytdlpFound && ffmpegFound {
		return ytdlpPath, ffmpegPath, nil
	}

	baseDir, err := os.UserCacheDir()
	if err != nil {
		return "", "", fmt.Errorf("无法定位缓存目录: %w", err)
	}
	targetDir := filepath.Join(baseDir, "wnxzq-tools")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", "", fmt.Errorf("无法创建工具目录: %w", err)
	}

	extractedYt, extractedFf, extractErr := extractEmbeddedTools(targetDir)
	if extractErr != nil {
		return "", "", extractErr
	}

	if !ytdlpFound {
		ytdlpPath = extractedYt
	}
	if !ffmpegFound {
		ffmpegPath = extractedFf
	}

	if ytdlpPath == "" || ffmpegPath == "" {
		return "", "", errors.New("未找到 yt-dlp 或 ffmpeg。请将对应文件放入 bin/ 后重新构建")
	}
	return ytdlpPath, ffmpegPath, nil
}

func findOnPath(name string) (string, bool) {
	if p, err := exec.LookPath(name); err == nil {
		return p, true
	}
	if runtime.GOOS == "windows" {
		if p, err := exec.LookPath(name + ".exe"); err == nil {
			return p, true
		}
	}
	return "", false
}

func extractEmbeddedTools(targetDir string) (string, string, error) {
	var ytPath, ffPath string

	entries, err := fs.ReadDir(embeddedTools, "bin")
	if err != nil {
		return "", "", fmt.Errorf("读取内嵌工具失败: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		lower := strings.ToLower(name)

		srcPath := filepath.ToSlash(filepath.Join("bin", name))
		if strings.HasSuffix(lower, ".zip") {
			if zErr := extractZipFromEmbedded(srcPath, targetDir); zErr != nil {
				log.Printf("extract zip failed: %v", zErr)
			}
			continue
		}

		data, readErr := embeddedTools.ReadFile(srcPath)
		if readErr != nil {
			return "", "", fmt.Errorf("读取文件失败(%s): %w", name, readErr)
		}
		dstPath := filepath.Join(targetDir, name)
		if writeErr := os.WriteFile(dstPath, data, 0o755); writeErr != nil {
			return "", "", fmt.Errorf("写入文件失败(%s): %w", name, writeErr)
		}

		if strings.Contains(lower, "yt-dlp") {
			ytPath = dstPath
		}
		if strings.Contains(lower, "ffmpeg") {
			ffPath = dstPath
		}
	}

	// Secondary probe after unzip, in case the binaries came from archives.
	if ytPath == "" || ffPath == "" {
		scanErr := filepath.WalkDir(targetDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			lower := strings.ToLower(filepath.Base(path))
			if ytPath == "" && strings.Contains(lower, "yt-dlp") {
				ytPath = path
			}
			if ffPath == "" && strings.Contains(lower, "ffmpeg") {
				ffPath = path
			}
			return nil
		})
		if scanErr != nil {
			return "", "", scanErr
		}
	}

	return ytPath, ffPath, nil
}

func extractZipFromEmbedded(embeddedZipPath, targetDir string) error {
	data, err := embeddedTools.ReadFile(filepath.ToSlash(embeddedZipPath))
	if err != nil {
		return err
	}

	tmpZip := filepath.Join(targetDir, "_tmp_tools.zip")
	if err := os.WriteFile(tmpZip, data, 0o644); err != nil {
		return err
	}
	defer os.Remove(tmpZip)

	zr, err := zip.OpenReader(tmpZip)
	if err != nil {
		return err
	}
	defer zr.Close()

	for _, f := range zr.File {
		destPath := filepath.Join(targetDir, filepath.FromSlash(f.Name))
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(destPath, 0o755); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return err
		}

		in, err := f.Open()
		if err != nil {
			return err
		}

		out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			in.Close()
			return err
		}

		_, copyErr := io.Copy(out, in)
		closeErr := out.Close()
		inCloseErr := in.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if inCloseErr != nil {
			return inCloseErr
		}
	}
	return nil
}
