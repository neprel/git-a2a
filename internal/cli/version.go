package cli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var releaseAPI = "https://api.github.com/repos/neprel/git-a2a/releases"
var releaseDownloads = "https://github.com/neprel/git-a2a/releases/download"
var errNoStableRelease = errors.New("no stable release is published")

type githubRelease struct {
	TagName string `json:"tag_name"`
	Body    string `json:"body"`
}

func (a *App) version(args []string) int {
	check := false
	for _, arg := range args {
		if arg == "--check" {
			check = true
		} else {
			fmt.Fprintf(a.Err, "version: unknown option %s\n", arg)
			return 2
		}
	}
	fmt.Fprintf(a.Out, "git-a2a %s (%s, %s, channel=%s)\n", Version, Commit, Target, Channel)
	if !check {
		return 0
	}
	release, err := getRelease("latest")
	if err != nil {
		if errors.Is(err, errNoStableRelease) {
			fmt.Fprintln(a.Err, "no stable release is published; prereleases are not treated as latest")
			return 0
		}
		fmt.Fprintf(a.Err, "version check failed: %v\n", err)
		return 1
	}
	latest := strings.TrimPrefix(release.TagName, "v")
	comparison, comparable := compareCoreVersions(Version, latest)
	if comparable && comparison >= 0 {
		if comparison > 0 {
			fmt.Fprintf(a.Err, "git-a2a %s is current (latest stable: %s)\n", Version, latest)
			return 0
		}
		fmt.Fprintf(a.Err, "git-a2a %s is current\n", Version)
		return 0
	}
	fmt.Fprintf(a.Out, "latest: %s\nupdate: %s\n", latest, updateCommand(Channel))
	fmt.Fprintf(a.Err, "git-a2a %s is available\n", latest)
	return 1
}

func compareCoreVersions(left, right string) (int, bool) {
	parse := func(value string) ([3]int, bool) {
		var result [3]int
		parts := strings.Split(value, ".")
		if len(parts) != len(result) {
			return result, false
		}
		for index, part := range parts {
			if part == "" {
				return result, false
			}
			number, err := strconv.Atoi(part)
			if err != nil || number < 0 {
				return result, false
			}
			result[index] = number
		}
		return result, true
	}
	leftParts, leftOK := parse(left)
	rightParts, rightOK := parse(right)
	if !leftOK || !rightOK {
		return 0, false
	}
	for index := range leftParts {
		if leftParts[index] < rightParts[index] {
			return -1, true
		}
		if leftParts[index] > rightParts[index] {
			return 1, true
		}
	}
	return 0, true
}

func updateCommand(channel string) string {
	switch channel {
	case "brew":
		return "brew upgrade git-a2a"
	case "scoop":
		return "scoop update git-a2a"
	case "npm":
		return "npm install -g git-a2a@latest"
	case "pypi":
		return "uv tool upgrade git-a2a (or pipx upgrade git-a2a)"
	case "deb":
		return "sudo apt update && sudo apt install git-a2a"
	case "rpm":
		return "sudo dnf upgrade git-a2a"
	case "apk":
		return "sudo apk upgrade git-a2a"
	case "docker":
		return "pull a newer ghcr.io/neprel/git-a2a tag and rebuild the image"
	case "binary":
		return "git-a2a upgrade"
	default:
		return "go install github.com/neprel/git-a2a/cmd/git-a2a@latest"
	}
}

func (a *App) upgrade(args []string) int {
	to := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--to" {
			if i+1 >= len(args) {
				fmt.Fprintln(a.Err, "upgrade: --to needs a version")
				return 2
			}
			i++
			to = strings.TrimPrefix(args[i], "v")
		} else {
			fmt.Fprintf(a.Err, "upgrade: unknown option %s\n", args[i])
			return 2
		}
	}
	if Channel != "binary" {
		fmt.Fprintf(a.Err, "upgrade refused: channel %s is managed externally; use %s\n", Channel, updateCommand(Channel))
		return 1
	}
	which := "latest"
	if to != "" {
		which = "tags/v" + to
	}
	release, err := getRelease(which)
	if err != nil {
		fmt.Fprintf(a.Err, "upgrade: %v\n", err)
		return 1
	}
	version := strings.TrimPrefix(release.TagName, "v")
	if version == Version {
		fmt.Fprintf(a.Err, "git-a2a %s is already installed\n", Version)
		return 0
	}
	archive := archiveName(version)
	checksums, err := download(releaseDownloads + "/v" + version + "/checksums.txt")
	if err != nil {
		fmt.Fprintf(a.Err, "upgrade: checksums: %v\n", err)
		return 1
	}
	payload, err := download(releaseDownloads + "/v" + version + "/" + archive)
	if err != nil {
		fmt.Fprintf(a.Err, "upgrade: download: %v\n", err)
		return 1
	}
	if err = verifyChecksum(archive, payload, checksums); err != nil {
		fmt.Fprintf(a.Err, "upgrade: %v\n", err)
		return 1
	}
	binary, err := extractBinary(archive, payload)
	if err != nil {
		fmt.Fprintf(a.Err, "upgrade: %v\n", err)
		return 1
	}
	executable, err := os.Executable()
	if err == nil {
		executable, err = filepath.EvalSymlinks(executable)
	}
	if err != nil {
		fmt.Fprintf(a.Err, "upgrade: executable: %v\n", err)
		return 1
	}
	tmp, err := os.CreateTemp(filepath.Dir(executable), ".git-a2a-upgrade-*")
	if err != nil {
		fmt.Fprintf(a.Err, "upgrade: %v\n", err)
		return 1
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err = tmp.Chmod(0o755); err == nil {
		_, err = tmp.Write(binary)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = replaceExecutable(executable, tmpName)
	}
	if err != nil {
		fmt.Fprintf(a.Err, "upgrade: atomic replacement failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(a.Err, "upgraded git-a2a %s -> %s\n", Version, version)
	if strings.TrimSpace(release.Body) != "" {
		fmt.Fprintln(a.Out, strings.TrimSpace(release.Body))
	}
	return 0
}

func getRelease(which string) (githubRelease, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, releaseAPI+"/"+which, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return githubRelease{}, err
	}
	defer resp.Body.Close()
	if which == "latest" && resp.StatusCode == http.StatusNotFound {
		return githubRelease{}, errNoStableRelease
	}
	if resp.StatusCode != http.StatusOK {
		return githubRelease{}, fmt.Errorf("GitHub returned HTTP %d", resp.StatusCode)
	}
	var release githubRelease
	err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&release)
	return release, err
}
func download(url string) ([]byte, error) {
	client := http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 200<<20))
}
func archiveName(version string) string {
	ext := "tar.gz"
	if runtime.GOOS == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("git-a2a_%s_%s_%s.%s", version, runtime.GOOS, runtime.GOARCH, ext)
}
func verifyChecksum(name string, payload, checksums []byte) error {
	want := ""
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == name {
			want = fields[0]
			break
		}
	}
	if want == "" {
		return fmt.Errorf("checksum for %s is missing", name)
	}
	sum := sha256.Sum256(payload)
	if hex.EncodeToString(sum[:]) != strings.ToLower(want) {
		return fmt.Errorf("checksum mismatch for %s", name)
	}
	return nil
}
func extractBinary(name string, payload []byte) ([]byte, error) {
	suffix := "git-a2a"
	if strings.HasSuffix(name, ".zip") {
		suffix = "git-a2a.exe"
		reader, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
		if err != nil {
			return nil, err
		}
		for _, file := range reader.File {
			if filepath.Base(file.Name) != suffix {
				continue
			}
			r, err := file.Open()
			if err != nil {
				return nil, err
			}
			defer r.Close()
			return io.ReadAll(io.LimitReader(r, 100<<20))
		}
	} else {
		gz, err := gzip.NewReader(bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		tr := tar.NewReader(gz)
		for {
			header, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			if filepath.Base(header.Name) == suffix {
				return io.ReadAll(io.LimitReader(tr, 100<<20))
			}
		}
	}
	return nil, fmt.Errorf("archive does not contain %s", suffix)
}
