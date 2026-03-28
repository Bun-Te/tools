// Package watcher provides file system watching for data files.
// When watched files are modified, it notifies the application (e.g. WebSocket clients);
// the server may apply reload only after user confirmation.
package watcher

import (
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// ReloadFunc is called when a watched file changes.
// mode is the game mode name (e.g., "base", "bonus").
type ReloadFunc func(mode string) error

// FileWatcher watches files for changes and triggers reloads.
// It can be enabled/disabled at runtime.
type FileWatcher struct {
	watcher    *fsnotify.Watcher
	baseDir    string
	files      map[string]string // filename -> mode name
	onReload   ReloadFunc
	debounce   time.Duration
	stopCh     chan struct{}
	wg         sync.WaitGroup
	mu         sync.Mutex
	lastChange map[string]time.Time // debounce tracking
	enabled    bool                 // whether watching is active
	enabledMu  sync.RWMutex         // protects enabled flag
}

// NewFileWatcher creates a new watcher for files.
// files maps filenames to their mode names.
// Example: {"lookUpTable_base_0.csv": "base", "lookUpTable_bonus_0.csv": "bonus"}
func NewFileWatcher(baseDir string, files map[string]string, onReload ReloadFunc) (*FileWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	return &FileWatcher{
		watcher:    w,
		baseDir:    baseDir,
		files:      files,
		onReload:   onReload,
		debounce:   2 * time.Second, // debounce rapid changes
		stopCh:     make(chan struct{}),
		lastChange: make(map[string]time.Time),
		enabled:    true, // enabled by default when created
	}, nil
}

// Enabled returns whether the watcher is currently active.
func (fw *FileWatcher) Enabled() bool {
	fw.enabledMu.RLock()
	defer fw.enabledMu.RUnlock()
	return fw.enabled
}

// SetEnabled enables or disables the watcher.
// When disabled, file change events are ignored.
func (fw *FileWatcher) SetEnabled(enabled bool) {
	fw.enabledMu.Lock()
	defer fw.enabledMu.Unlock()
	fw.enabled = enabled
	if enabled {
		log.Println("[Watcher] Enabled")
	} else {
		log.Println("[Watcher] Disabled")
	}
}

// Start begins watching for file changes.
func (fw *FileWatcher) Start() error {
	// Watch the base directory
	if err := fw.watcher.Add(fw.baseDir); err != nil {
		return err
	}

	log.Printf("[Watcher] Watching directory: %s", fw.baseDir)
	for filename := range fw.files {
		log.Printf("[Watcher] Tracking file: %s", filename)
	}

	fw.wg.Add(1)
	go fw.run()

	return nil
}

// Stop stops watching for file changes.
func (fw *FileWatcher) Stop() {
	close(fw.stopCh)
	fw.watcher.Close()
	fw.wg.Wait()
	log.Println("[Watcher] Stopped")
}

func (fw *FileWatcher) run() {
	defer fw.wg.Done()

	for {
		select {
		case <-fw.stopCh:
			return

		case event, ok := <-fw.watcher.Events:
			if !ok {
				return
			}
			fw.handleEvent(event)

		case err, ok := <-fw.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("[Watcher] Error: %v", err)
		}
	}
}

func (fw *FileWatcher) handleEvent(event fsnotify.Event) {
	// Check if watcher is enabled
	if !fw.Enabled() {
		return
	}

	// Write/Create: normal saves. Rename: atomic replace (temp → LUT csv) used by SaveWeights.
	if !event.Has(fsnotify.Write) && !event.Has(fsnotify.Create) && !event.Has(fsnotify.Rename) {
		return
	}

	filename := filepath.Base(event.Name)

	// Check if this is a file we're tracking
	mode, ok := fw.files[filename]
	if !ok {
		return
	}

	// Debounce: ignore if last change was too recent
	fw.mu.Lock()
	lastTime, exists := fw.lastChange[filename]
	now := time.Now()
	if exists && now.Sub(lastTime) < fw.debounce {
		fw.mu.Unlock()
		return
	}
	fw.lastChange[filename] = now
	fw.mu.Unlock()

	log.Printf("[Watcher] File changed: %s (mode: %s)", filename, mode)

	// Trigger reload in a goroutine to not block the watcher
	go func(m string, f string, fullPath string) {
		// Wait for file to stabilize (stop being written to)
		// This is crucial for large files that take time to write
		if err := fw.waitForFileStable(fullPath); err != nil {
			log.Printf("[Watcher] File %s not stable, skipping reload: %v", f, err)
			return
		}

		log.Printf("[Watcher] Reloading for mode: %s", m)
		if err := fw.onReload(m); err != nil {
			log.Printf("[Watcher] Failed to reload mode %s: %v", m, err)
		} else {
			log.Printf("[Watcher] Successfully reloaded mode: %s", m)
		}
	}(mode, filename, event.Name)
}

// waitForFileStable waits until the file size stops changing.
// This prevents reading a file that is still being written.
func (fw *FileWatcher) waitForFileStable(path string) error {
	const (
		checkInterval  = 200 * time.Millisecond // How often to check file size
		stableRequired = 3                       // Number of consecutive stable checks required
		maxWait        = 30 * time.Second        // Maximum wait time
	)

	startTime := time.Now()
	var lastSize int64 = -1
	stableCount := 0

	for {
		if time.Since(startTime) > maxWait {
			log.Printf("[Watcher] File %s: max wait time exceeded, proceeding anyway", filepath.Base(path))
			return nil // Proceed anyway after max wait
		}

		info, err := os.Stat(path)
		if err != nil {
			// File might be temporarily unavailable during write
			time.Sleep(checkInterval)
			stableCount = 0
			lastSize = -1
			continue
		}

		currentSize := info.Size()

		if currentSize == lastSize && currentSize > 0 {
			stableCount++
			if stableCount >= stableRequired {
				log.Printf("[Watcher] File %s stable at %d bytes after %v",
					filepath.Base(path), currentSize, time.Since(startTime))
				return nil
			}
		} else {
			stableCount = 0
		}

		lastSize = currentSize
		time.Sleep(checkInterval)
	}
}

// SetDebounce sets the debounce duration for file changes.
func (fw *FileWatcher) SetDebounce(d time.Duration) {
	fw.mu.Lock()
	fw.debounce = d
	fw.mu.Unlock()
}

// AddFile adds a new file to watch.
func (fw *FileWatcher) AddFile(filename, mode string) {
	fw.mu.Lock()
	fw.files[filename] = mode
	fw.mu.Unlock()
	log.Printf("[Watcher] Added file: %s (mode: %s)", filename, mode)
}

// GetFiles returns the currently watched files.
func (fw *FileWatcher) GetFiles() map[string]string {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	result := make(map[string]string, len(fw.files))
	for k, v := range fw.files {
		result[k] = v
	}
	return result
}
