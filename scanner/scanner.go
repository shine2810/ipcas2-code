package scanner

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// FileInfo contains information about a found file
type FileInfo struct {
	Path     string
	IsHidden bool
	Size     int64
}

const (
	FILE_ATTRIBUTE_HIDDEN = 0x02
)

// IsHidden checks if a file has the hidden attribute on Windows
func IsHidden(path string) bool {
	pointer, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	attrs, err := syscall.GetFileAttributes(pointer)
	if err != nil {
		return false
	}
	return attrs&FILE_ATTRIBUTE_HIDDEN != 0
}

// shouldSkip determines if a directory should be skipped during scanning
func shouldSkip(path string) bool {
	lower := strings.ToLower(path)
	
	// Skip Windows directory
	if strings.HasPrefix(lower, "c:\\windows") {
		return true
	}
	
	// Skip other system directories that may cause issues
	skipDirs := []string{
		"c:\\$recycle.bin",
		"c:\\system volume information",
		"c:\\recovery",
		"c:\\programdata\\microsoft\\windows",
	}
	
	for _, skip := range skipDirs {
		if strings.HasPrefix(lower, skip) {
			return true
		}
	}
	
	return false
}

// GetScanPaths returns the paths to scan for IPCAS2.ini
func GetScanPaths() []string {
	userProfile := os.Getenv("USERPROFILE")
	paths := []string{
		filepath.Join(userProfile, "AppData", "Local", "VirtualStore", "Windows"),
		filepath.Join(userProfile, "AppData", "Local", "VirtualStore"),
	}
	return paths
}

// ScanForIPCAS2 scans specific folders for hidden IPCAS2.ini files
// progressCallback is called with the current path being scanned
func ScanForIPCAS2(progressCallback func(string), stopChan <-chan struct{}) ([]FileInfo, error) {
	var results []FileInfo
	targetName := "ipcas2.ini"
	scanPaths := GetScanPaths()

	for _, scanRoot := range scanPaths {
		// Check if path exists
		if _, err := os.Stat(scanRoot); os.IsNotExist(err) {
			continue
		}

		filepath.Walk(scanRoot, func(path string, info os.FileInfo, err error) error {
			// Check if stop was requested
			select {
			case <-stopChan:
				return filepath.SkipAll
			default:
			}

			if err != nil {
				// Skip directories we can't access
				if info != nil && info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}

			// Report progress for directories
			if info.IsDir() {
				if progressCallback != nil {
					progressCallback(path)
				}
				return nil
			}

			// Check if this is the target file
			if strings.ToLower(info.Name()) == targetName {
				// Check if it's hidden
				isHidden := IsHidden(path)
				if isHidden {
					results = append(results, FileInfo{
						Path:     path,
						IsHidden: isHidden,
						Size:     info.Size(),
					})
				}
			}

			return nil
		})
	}

	return results, nil
}

// DeleteFile removes a file from the filesystem
func DeleteFile(path string) error {
	// Try to remove hidden attribute first
	pointer, err := syscall.UTF16PtrFromString(path)
	if err == nil {
		attrs, err := syscall.GetFileAttributes(pointer)
		if err == nil && attrs&FILE_ATTRIBUTE_HIDDEN != 0 {
			// Remove hidden attribute
			syscall.SetFileAttributes(pointer, attrs&^FILE_ATTRIBUTE_HIDDEN)
		}
	}

	return os.Remove(path)
}
