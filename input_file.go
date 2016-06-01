package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// FileInput can read requests generated by FileOutput
type FileInput struct {
	data          chan []byte
	path          string
	currentFile   *os.File
	currentReader *bufio.Reader
	speedFactor   float64
}

// NewFileInput constructor for FileInput. Accepts file path as argument.
func NewFileInput(path string) (i *FileInput) {
	i = new(FileInput)
	i.data = make(chan []byte)
	i.path = path
	i.speedFactor = 1

	if err := i.updateFile(); err != nil {
		return
	}

	go i.emit()

	return
}

// path can be a pattern
// It sort paths lexicographically and tries to choose next one
func (i *FileInput) updateFile() (err error) {
	var matches []string

	if matches, err = filepath.Glob(i.path); err != nil {
		log.Println("Wrong file pattern", i.path, err)
		return
	}

	if len(matches) == 0 {
		log.Println("No files match pattern: ", i.path)
		return errors.New("No matching files")
	}

	sort.Strings(matches)

	if i.currentFile == nil {
		if i.currentFile, err = os.Open(matches[0]); err != nil {
			log.Println("Can't read file ", matches[0], err)
			return
		}
	} else {
		found := false
		for idx, p := range matches {
			if p == i.currentFile.Name() && idx != len(matches)-1 {
				if i.currentFile, err = os.Open(matches[idx+1]); err != nil {
					log.Println("Can't read file ", matches[idx+1], err)
					return
				}

				found = true
			}
		}

		if !found {
			return errors.New("There is no new files")
		}
	}

	if strings.HasSuffix(i.currentFile.Name(), ".gz") {
		gzReader, err := gzip.NewReader(i.currentFile)
		if err != nil {
			log.Fatal(err)
		}
		i.currentReader = bufio.NewReader(gzReader)
	} else {
		i.currentReader = bufio.NewReader(i.currentFile)
	}

	return nil
}

func (i *FileInput) Read(data []byte) (int, error) {
	buf := <-i.data
	copy(data, buf)

	return len(buf), nil
}

func (i *FileInput) String() string {
	return "File input: " + i.path
}

func (i *FileInput) emit() {
	var lastTime int64

	payloadSeparatorAsBytes := []byte(payloadSeparator)

	var buffer bytes.Buffer

	for {
		line, err := i.currentReader.ReadBytes('\n')

		if err != nil {
			if err != io.EOF {
				log.Fatal(err)
			}

			// If our path pattern match multiple files, try to find them
			if err == io.EOF {
				if i.updateFile() != nil {
					break
				}

				continue
			}
		}

		if bytes.Equal(payloadSeparatorAsBytes[1:], line) {
			asBytes := buffer.Bytes()
			buffer.Reset()

			meta := payloadMeta(asBytes)

			if len(meta) > 2 && meta[0][0] == RequestPayload {
				ts, _ := strconv.ParseInt(string(meta[2]), 10, 64)

				if lastTime != 0 {
					timeDiff := ts - lastTime

					if i.speedFactor != 1 {
						timeDiff = int64(float64(timeDiff) / i.speedFactor)
					}

					time.Sleep(time.Duration(timeDiff))
				}

				lastTime = ts
			}

			// Bytes() returns only pointer, so to remove data-race copy the data to an array
			newBuf := make([]byte, len(asBytes)-1)
			copy(newBuf, asBytes)

			i.data <- newBuf
		} else {
			buffer.Write(line)
		}

	}

	log.Printf("FileInput: end of file '%s'\n", i.path)
}
