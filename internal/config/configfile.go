// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/almeidapaulopt/tsdproxy/internal/consts"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
)

const configReloadDebounce = 500 * time.Millisecond

type File struct {
	log        zerolog.Logger
	data       any
	onChange   func(fsnotify.Event)
	debounce   *time.Timer
	stopCh     chan struct{}
	done       chan struct{}
	filename   string
	onChangeMu sync.RWMutex
	closeOnce  sync.Once
	debounceMu sync.Mutex
	mtx        sync.Mutex
	watching   atomic.Bool
}

func NewConfigFile(log zerolog.Logger, filename string, data any) *File {
	return &File{
		filename: filename,
		data:     data,
		log:      log.With().Str("module", "file").Str("files", filename).Logger(),
		stopCh:   make(chan struct{}),
		done:     make(chan struct{}),
	}
}

func (f *File) Load() error {
	data, err := os.ReadFile(f.filename)
	if err != nil {
		return err
	}

	err = unmarshalNormalized(data, f.data)
	if err != nil {
		return err
	}

	return nil
}

func (f *File) Save() error {
	// create config directory
	dir, _ := filepath.Split(f.filename)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err1 := os.MkdirAll(dir, consts.PermOwnerAll); err1 != nil {
			return err1
		}
	}

	yaml, err := yaml.Marshal(f.data)
	if err != nil {
		return err
	}

	err = os.WriteFile(f.filename, yaml, consts.PermOwnerRead|consts.PermOwnerWrite)
	if err != nil {
		return err
	}

	return nil
}

// OnConfigChange sets the event handler that is called when a config file changes.
func (f *File) OnChange(run func(in fsnotify.Event)) {
	f.onChangeMu.Lock()
	defer f.onChangeMu.Unlock()

	f.onChange = run
}

// Watch starts watching a config file for changes.
func (f *File) Watch() error {
	f.log.Debug().Str("file", f.filename).Msg("Start watching file")

	f.watching.Store(true)

	errChan := make(chan error, 1)

	go func() {
		defer close(f.done)

		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			errChan <- fmt.Errorf("failed to create a new watcher: %w", err)
			return
		}
		defer watcher.Close()

		file := filepath.Clean(f.filename)
		dir, _ := filepath.Split(file)

		eventsWG := sync.WaitGroup{}
		eventsWG.Add(1)

		go func() {
			defer eventsWG.Done()
			f.watchEvents(watcher, file)
		}()

		if err := watcher.Add(dir); err != nil {
			errChan <- fmt.Errorf("failed to watch config file %s: %w", f.filename, err)
			return
		}

		errChan <- nil

		eventsWG.Wait()
	}()

	return <-errChan
}

// Close signals the Watch goroutine to stop and waits for it to exit.
// It is safe to call Close multiple times.
// If Watch was never called, Close is a no-op.
func (f *File) Close() {
	if !f.watching.Load() {
		return
	}
	f.closeOnce.Do(func() {
		close(f.stopCh)

		f.debounceMu.Lock()
		if f.debounce != nil {
			f.debounce.Stop()
		}
		f.debounceMu.Unlock()
	})
	<-f.done // wait outside closeOnce so all callers observe completion
}

func (f *File) watchEvents(watcher *fsnotify.Watcher, file string) {
	realFile, err := filepath.EvalSymlinks(f.filename)
	if err != nil {
		f.log.Warn().Err(err).Msg("failed to resolve symlinks, using raw path")
		realFile = file
	}
	for {
		select {
		case <-f.stopCh:
			f.debounceMu.Lock()
			if f.debounce != nil {
				f.debounce.Stop()
			}
			f.debounceMu.Unlock()
			return
		case event, ok := <-watcher.Events:
			if !ok {
				f.debounceMu.Lock()
				if f.debounce != nil {
					f.debounce.Stop()
				}
				f.debounceMu.Unlock()
				return
			}
			f.handleEvent(event, file, &realFile)
		case err, ok := <-watcher.Errors:
			if ok {
				f.log.Error().Err(err).Msg("watching config file error")
			}

			f.debounceMu.Lock()
			if f.debounce != nil {
				f.debounce.Stop()
			}
			f.debounceMu.Unlock()
			return
		}
	}
}

func (f *File) handleEvent(event fsnotify.Event, file string, realFile *string) {
	currentFile, err := filepath.EvalSymlinks(f.filename)
	if err != nil {
		currentFile = file
	}
	if (filepath.Clean(event.Name) == file &&
		(event.Has(fsnotify.Write) || event.Has(fsnotify.Create))) ||
		(currentFile != "" && currentFile != *realFile) {
		*realFile = currentFile

		f.debounceMu.Lock()
		if f.debounce != nil {
			f.debounce.Stop()
		}
		f.debounce = time.AfterFunc(configReloadDebounce, func() {
			f.onChangeMu.RLock()
			fn := f.onChange
			f.onChangeMu.RUnlock()
			if fn != nil {
				fn(event)
			}
		})
		f.debounceMu.Unlock()
	}
}

func unmarshalNormalized(data []byte, out any) error {
	lookup := buildKeyLookup(out)
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return err
	}
	if root.Kind == 0 || (root.Kind == yaml.DocumentNode && len(root.Content) == 0) {
		return nil
	}
	issues := normalizeNodeKeys(&root, lookup, log.Logger)
	if len(issues) > 0 {
		var b strings.Builder
		for _, iss := range issues {
			fmt.Fprintf(&b, "line %d column %d: unknown field %q", iss.Line, iss.Column, iss.Original)
			if len(iss.Suggestions) > 0 {
				fmt.Fprintf(&b, " — did you mean %s?", quoteList(iss.Suggestions))
			}
			b.WriteString("\n")
		}
		return fmt.Errorf("configuration contains unknown fields:\n%s", b.String())
	}
	if err := root.Decode(out); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}
