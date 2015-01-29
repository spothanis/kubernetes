// Copyright 2015 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The go2docker command compiles a go main package and forge a minimal
// docker image from the resulting static binary.
//
// usage: go2docker [-image namespace/basename] go/pkg/path | docker load
package main

import (
	"archive/tar"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	Cmd []string `json:"Cmd"`
}

type Image struct {
	ID            string    `json:"id"`
	Created       time.Time `json:"created"`
	DockerVersion string    `json:"docker_version"`
	Config        Config    `json:"config"`
	Architecture  string    `json:"architecture"`
	OS            string    `json:"os"`
}

var image = flag.String("image", "", "namespace/name for the repository, default to go2docker/$(basename)")

const (
	DockerVersion = "1.4.0"
	Arch          = "amd64"
	OS            = "linux"
	Version       = "1.0"
	Namespace     = "go2docker"
)

func main() {
	flag.Parse()
	args := []string{"."}
	if flag.NArg() > 0 {
		args = flag.Args()
	}

	fpath, err := filepath.Abs(args[0])
	ext := filepath.Ext(fpath)
	basename := filepath.Base(fpath[:len(fpath)-len(ext)])

	if *image == "" {
		if err != nil {
			log.Fatalf("failed to get absolute path: %v", err)
		}
		*image = path.Join(Namespace, basename)
	}
	tmpDir, err := ioutil.TempDir("", "")
	if err != nil {
		log.Fatalf("failed to create temp directory: %v", err)
	}
	aout := filepath.Join(tmpDir, basename)
	command := append([]string{"go", "build", "-o", aout, "-a", "-tags", "netgo", "-ldflags", "'-w'"}, args...)
	if _, err := exec.Command(command[0], command[1:]...).Output(); err != nil {
		log.Fatalf("failed to run command %q: %v", strings.Join(command, " "), err)
	}
	imageIDBytes := make([]byte, 32)
	if _, err := rand.Read(imageIDBytes); err != nil {
		log.Fatalf("failed to generate ID: %v")
	}
	imageID := hex.EncodeToString(imageIDBytes)
	repo := map[string]map[string]string{
		*image: map[string]string{
			"latest": imageID,
		},
	}
	repoJSON, err := json.Marshal(repo)
	if err != nil {
		log.Fatalf("failed to serialize repo %#v: %v", repo, err)
	}
	tw := tar.NewWriter(os.Stdout)
	if err := tw.WriteHeader(&tar.Header{
		Name: "repositories",
		Size: int64(len(repoJSON)),
	}); err != nil {
		log.Fatalf("failed to write /repository header: %v", err)
	}
	if _, err := tw.Write(repoJSON); err != nil {
		log.Fatalf(" failed to write /repository body: %v", err)
	}
	if err := tw.WriteHeader(&tar.Header{
		Name: imageID + "/VERSION",
		Size: int64(len(Version)),
	}); err != nil {
		log.Fatalf("failed to write /%s/VERSION header: %v", imageID, err)
	}
	if _, err := tw.Write([]byte(Version)); err != nil {
		log.Fatalf(" failed to write /%s/VERSION body: %v", imageID, err)
	}
	imageJSON, err := json.Marshal(Image{
		ID:            imageID,
		Created:       time.Now().UTC(),
		DockerVersion: Version,
		Config: Config{
			Cmd: []string{"/" + basename},
		},
		Architecture: Arch,
		OS:           OS,
	})
	if err := tw.WriteHeader(&tar.Header{
		Name: imageID + "/json",
		Size: int64(len(imageJSON)),
	}); err != nil {
		log.Fatalf("failed to write /%s/json header: %v", imageID, err)
	}
	if _, err := tw.Write(imageJSON); err != nil {
		log.Fatalf("failed to write /%s/json body: %v", imageID, err)
	}
	var buf bytes.Buffer
	ftw := tar.NewWriter(&buf)
	file, err := os.Open(aout)
	if err != nil {
		log.Fatalf("failed to open %q: %v", aout, err)
	}
	finfo, err := file.Stat()
	if err != nil {
		log.Fatalf("failed to get file info %q: %v", aout, err)
	}
	fheader, err := tar.FileInfoHeader(finfo, "")
	if err != nil {
		log.Fatalf("failed to get file info header %q: %v", aout, err)
	}
	fheader.Name = basename
	if err := ftw.WriteHeader(fheader); err != nil {
		log.Fatalf("failed to write /%s header: %v", aout, err)
	}
	if _, err := io.Copy(ftw, file); err != nil {
		log.Fatalf("failed to write /%s body: %v", aout, err)
	}
	if err := ftw.Close(); err != nil {
		log.Fatalf("failed to close layer.tar: %v", err)
	}
	layerBytes := buf.Bytes()
	if err := tw.WriteHeader(&tar.Header{
		Name: imageID + "/layer.tar",
		Size: int64(len(layerBytes)),
	}); err != nil {
		log.Fatalf("failed to write /%s/layer.tar header: %v", imageID, err)
	}
	if _, err := tw.Write(layerBytes); err != nil {
		log.Fatalf("failed to write /%s/layer.tar body: %v", imageID, err)
	}
	if err := tw.Close(); err != nil {
		log.Fatalf("failed to close image.tar: %v", err)
	}
}