package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
)

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Rebuild and rerun automatically whenever source files change",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWatch()
	},
}

func runWatch() error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to start file watcher: %w", err)
	}
	defer watcher.Close()

	for _, dir := range []string{"src", "include"} {
		if err := addWatchDirRecursive(watcher, dir); err != nil {
			warnf("could not watch %s/: %v", dir, err)
		}
	}
	if err := watcher.Add("cmaker.yaml"); err != nil {
		warnf("could not watch cmaker.yaml: %v", err)
	}

	infof("Watching for changes (Ctrl+C to stop)...")

	// Do an initial build+run so `cmaker watch` is immediately useful.
	triggerRebuild()

	var debounce *time.Timer
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) == 0 {
				continue
			}
			if debounce != nil {
				debounce.Stop()
			}
			debounce = time.AfterFunc(200*time.Millisecond, triggerRebuild)
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			warnf("watcher error: %v", err)
		}
	}
}

func triggerRebuild() {
	infof("Change detected. Rebuilding...")
	if err := runBuild(false, "", 0, ""); err != nil {
		errorf("%v", err)
		return
	}
	if err := runProject("", "", nil); err != nil {
		errorf("%v", err)
	}
}

func addWatchDirRecursive(w *fsnotify.Watcher, root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return w.Add(path)
		}
		return nil
	})
}
