package filebrowse

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vaultfleet/pkg/protocol"
)

const (
	EntryTypeDir  = "dir"
	EntryTypeFile = "file"
)

type DirEntry = protocol.DirEntry

var browseTimeout = 10 * time.Second

var excludedTopLevelDirs = map[string]struct{}{
	"proc": {},
	"sys":  {},
	"dev":  {},
	"run":  {},
	"tmp":  {},
	"snap": {},
}

func Browse(fsRoot string, scanPath string, maxDepth int) ([]DirEntry, error) {
	ctx, cancel := context.WithTimeout(context.Background(), browseTimeout)
	defer cancel()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	cleanRoot := filepath.Clean(fsRoot)
	cleanScanPath := filepath.Clean(scanPath)
	baseDepth := pathDepth(cleanScanPath)
	entries := []DirEntry{}

	err := filepath.WalkDir(cleanScanPath, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		cleanPath := filepath.Clean(path)
		if cleanPath == cleanScanPath {
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if isExcludedTopLevel(cleanRoot, cleanPath) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if pathDepth(cleanPath)-baseDepth > maxDepth {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		entryType := EntryTypeFile
		if entry.IsDir() {
			entryType = EntryTypeDir
		}
		entries = append(entries, DirEntry{
			Path: cleanPath,
			Type: entryType,
			Size: info.Size(),
		})
		return nil
	})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return entries, err
		}
		return entries, err
	}
	return entries, nil
}

func isExcludedTopLevel(fsRoot string, path string) bool {
	rel, err := filepath.Rel(fsRoot, path)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || rel == ".." {
		return false
	}
	first := rel
	if idx := strings.IndexRune(rel, os.PathSeparator); idx >= 0 {
		first = rel[:idx]
	}
	_, excluded := excludedTopLevelDirs[first]
	return excluded
}

func pathDepth(path string) int {
	clean := filepath.Clean(path)
	volume := filepath.VolumeName(clean)
	clean = strings.TrimPrefix(clean, volume)
	clean = strings.Trim(clean, string(os.PathSeparator))
	if clean == "" {
		return 0
	}
	return len(strings.Split(clean, string(os.PathSeparator)))
}
