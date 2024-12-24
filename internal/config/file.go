// SPDX-FileCopyrightText: 2024 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT
package config

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog"
	"gopkg.in/yaml.v3"
)

type File struct {
	data any
	log  zerolog.Logger

	onChange func(fsnotify.Event)

	filename string
}

func NewFile(log zerolog.Logger, filename string, data any) *File {
	return &File{
		filename: filename,
		data:     data,
		log:      log.With().Str("module", "file").Str("files", filename).Logger(),
	}
}

func (f *File) Load() error {
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

func (f *File) Save() error {
	// create config directory
	dir, _ := filepath.Split(f.filename)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err1 := os.MkdirAll(dir, os.ModeDir); err1 != nil {
			return err1
		}
	}

	yaml, err := yaml.Marshal(f.data)
	if err != nil {
		return err
	}

	err = os.WriteFile(f.filename, yaml, 0644)
	if err != nil {
		return err
	}

	return nil
}

// OnConfigChange sets the event handler that is called when a config file changes.
func (f *File) OnChange(run func(in fsnotify.Event)) {
	f.onChange = run
}

// WatchConfig starts watching a config file for changes.
func (f *File) Watch() {
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
		realFile, _ := filepath.EvalSymlinks(f.filename)

		eventsWG := sync.WaitGroup{}
		eventsWG.Add(1)
		// Start listening for events.
		go func() {
			for {
				select {
				case event, ok := <-watcher.Events:
					if !ok {
						eventsWG.Done()
						return
					}

					currentFile, _ := filepath.EvalSymlinks(f.filename)
					if (filepath.Clean(event.Name) == file &&
						(event.Has(fsnotify.Write) || event.Has(fsnotify.Create))) ||
						(currentFile != "" && currentFile != realFile) {
						realFile = currentFile

						if f.onChange != nil {
							f.onChange(event)
						}
					} else if filepath.Clean(event.Name) == file && event.Has(fsnotify.Remove) {
						eventsWG.Done()
						return
					}
				case err1, ok := <-watcher.Errors:
					if ok {
						f.log.Error().Err(err1).Msg("watching config file error")
					}
					eventsWG.Done()
					return
				}
			}
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

func unmarshalStrict(data []byte, out any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	if err := dec.Decode(out); err != nil && !errors.Is(err, io.EOF) {
		return err
	}

	return nil
}
