package models

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	huggingface "github.com/inference-gateway/cli/internal/platform/huggingface"
)

const (
	testModel   = "acme/talker"
	testTree    = `[{"type":"directory","path":"v15","size":0},{"type":"file","path":"config.json","size":2},{"type":"file","path":"v15/unet.bin","size":7}]`
	testConfig  = "{}"
	testWeights = "WEIGHTS"
)

// hub serves one repository and counts the files it hands out.
func hub(t *testing.T, tree string) (*huggingface.Client, *atomic.Int64) {
	t.Helper()
	var served atomic.Int64
	files := map[string]string{
		"/" + testModel + "/resolve/main/config.json":  testConfig,
		"/" + testModel + "/resolve/main/v15/unet.bin": testWeights,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/models/"+testModel+"/tree/main" {
			_, _ = w.Write([]byte(tree))
			return
		}
		body, known := files[r.URL.Path]
		if !known {
			http.NotFound(w, r)
			return
		}
		served.Add(1)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return &huggingface.Client{BaseURL: srv.URL, HTTP: srv.Client()}, &served
}

func TestDownloadFetchesEveryFileIntoTheRepositoryFolder(t *testing.T) {
	dir := t.TempDir()
	client, served := hub(t, testTree)

	model, err := Download(context.Background(), client, dir, testModel, nil, nil)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	folder := filepath.Join(dir, "acme", "talker")
	want := Downloaded{ID: testModel, Path: folder, Files: 2, Bytes: int64(len(testConfig) + len(testWeights))}
	if model != want {
		t.Errorf("Download = %+v, want %+v", model, want)
	}
	weights, err := os.ReadFile(filepath.Join(folder, "v15", "unet.bin"))
	if err != nil || string(weights) != testWeights {
		t.Errorf("nested file = %q, %v", weights, err)
	}

	if _, err := Download(context.Background(), client, dir, testModel, nil, nil); err != nil {
		t.Fatalf("second Download: %v", err)
	}
	if served.Load() != 2 {
		t.Errorf("files served = %d, want 2: a file already there is kept", served.Load())
	}
}

func TestDownloadFetchesOnlyTheFilesNamed(t *testing.T) {
	dir := t.TempDir()
	client, served := hub(t, testTree)

	model, err := Download(context.Background(), client, dir, testModel, []string{"config.json"}, nil)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if model.Files != 1 || served.Load() != 1 {
		t.Errorf("files on disk = %d, served = %d, want 1 and 1", model.Files, served.Load())
	}

	_, err = Download(context.Background(), client, dir, testModel, []string{"missing.bin"}, nil)
	if err == nil || !strings.Contains(err.Error(), `no file "missing.bin"`) {
		t.Errorf("error = %v, want one naming the missing file", err)
	}
}

func TestDownloadRefusesWhatCouldLeaveTheLibrary(t *testing.T) {
	dir := t.TempDir()

	for _, id := range []string{"", "talker", "acme/talker/extra", "../acme/talker", "acme/..", "-acme/talker"} {
		client, _ := hub(t, testTree)
		if _, err := Download(context.Background(), client, dir, id, nil, nil); err == nil {
			t.Errorf("Download(%q) succeeded, want a refusal", id)
		}
	}

	client, served := hub(t, `[{"type":"file","path":"../../escape.bin","size":1}]`)
	_, err := Download(context.Background(), client, dir, testModel, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "unsafe file path") {
		t.Errorf("error = %v, want an unsafe path refusal", err)
	}
	if served.Load() != 0 {
		t.Errorf("files served = %d, want none for an unsafe listing", served.Load())
	}
}

func TestListAndDelete(t *testing.T) {
	dir := t.TempDir()
	client, _ := hub(t, testTree)

	if library, err := List(dir); err != nil || len(library) != 0 {
		t.Fatalf("List of an empty library = %+v, %v", library, err)
	}
	downloaded, err := Download(context.Background(), client, dir, testModel, nil, nil)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	partial := filepath.Join(downloaded.Path, ".big.bin.123.partial")
	if err := os.WriteFile(partial, []byte("half"), 0o644); err != nil {
		t.Fatal(err)
	}

	library, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !reflect.DeepEqual(library, []Downloaded{downloaded}) {
		t.Errorf("List = %+v, want %+v: an unfinished file does not count", library, []Downloaded{downloaded})
	}

	if err := Delete(dir, testModel); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if library, _ := List(dir); len(library) != 0 {
		t.Errorf("List after Delete = %+v, want empty", library)
	}
	if err := Delete(dir, testModel); err == nil || !strings.Contains(err.Error(), "not downloaded") {
		t.Errorf("second Delete error = %v, want not downloaded", err)
	}
	if err := Delete(dir, "../outside"); err == nil {
		t.Error("Delete of a path outside the library succeeded")
	}
}
