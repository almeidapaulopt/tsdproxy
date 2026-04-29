// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package config

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/almeidapaulopt/tsdproxy/internal/consts"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog"
	"gopkg.in/yaml.v3"
)

type ConfigFile struct {
	data any
	log  zerolog.Logger

	onChange func(fsnotify.Event)

	filename string

	mtx sync.Mutex
}

func NewConfigFile(log zerolog.Logger, filename string, data any) *ConfigFile {
	return &ConfigFile{
		filename: filename,
		data:     data,
		log:      log.With().Str("module", "file").Str("files", filename).Logger(),
	}
}

func (f *ConfigFile) Load() error {
	data, err := os.ReadFile(f.filename)
	if err != nil {
		return err
	}

	err = unmarshalStrict(data, f.data)
	if err != nil {
		return err
	}

	return nil
}

func (f *ConfigFile) Save() error {
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

	err = os.WriteFile(f.filename, yaml, consts.PermAllRead+consts.PermOwnerWrite)
	if err != nil {
		return err
	}

	return nil
}

// OnConfigChange sets the event handler that is called when a config file changes.
func (f *ConfigFile) OnChange(run func(in fsnotify.Event)) {
	f.mtx.Lock()
	defer f.mtx.Unlock()

	f.onChange = run
}

// WatchConfig starts watching a config file for changes.
func (f *ConfigFile) Watch() {
	f.log.Debug().Str("file", f.filename).Msg("Start watching file")

	initWG := sync.WaitGroup{}
	initWG.Add(1)

	go func() {
		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			f.log.Fatal().Err(err).Msg("failed to create a new watcher")
		}
		defer watcher.Close()

		file := filepath.Clean(f.filename)
		dir, _ := filepath.Split(file)

		eventsWG := sync.WaitGroup{}
		eventsWG.Add(1)

		// Start listening for events.
		go func() {
			defer eventsWG.Done()
			f.watchEvents(watcher, file)
		}()

		err = watcher.Add(dir)
		if err != nil {
			f.log.Fatal().Err(err).Str("filename", f.filename).Msg("failed to watch config file")
		}

		initWG.Done()
		eventsWG.Wait()
	}()
	initWG.Wait()
}

func (f *ConfigFile) watchEvents(watcher *fsnotify.Watcher, file string) {
	realFile, _ := filepath.EvalSymlinks(f.filename)
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			f.handleEvent(event, file, &realFile)
		case err, ok := <-watcher.Errors:
			if ok {
				f.log.Error().Err(err).Msg("watching config file error")
			}
			return
		}
	}
}

func (f *ConfigFile) handleEvent(event fsnotify.Event, file string, realFile *string) {
	currentFile, _ := filepath.EvalSymlinks(f.filename)
	if (filepath.Clean(event.Name) == file &&
		(event.Has(fsnotify.Write) || event.Has(fsnotify.Create))) ||
		(currentFile != "" && currentFile != *realFile) {
		*realFile = currentFile

		if f.onChange != nil {
			f.onChange(event)
		}
	}
}

func unmarshalStrict(data []byte, out any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	if err := dec.Decode(out); err != nil && !errors.Is(err, io.EOF) {
		return err
	}

	return nil
}
