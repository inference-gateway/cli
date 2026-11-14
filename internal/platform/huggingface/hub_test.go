package huggingface

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func hubServing(t *testing.T, routes map[string]string) (*Client, *[]string) {
	t.Helper()
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.RequestURI())
		body, known := routes[r.URL.Path]
		if !known {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL, HTTP: srv.Client()}, &asked
}

func TestSearchAsksForTheMostDownloadedMatches(t *testing.T) {
	hub, asked := hubServing(t, map[string]string{
		"/api/models": `[{"id":"mlx-community/MuseTalk-1.5-fp16","downloads":1362,"likes":6,"pipeline_tag":"image-to-image","tags":["mlx"]}]`,
	})

	found, err := hub.Search(context.Background(), "muse talk", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	want := []Model{{ID: "mlx-community/MuseTalk-1.5-fp16", Downloads: 1362, Likes: 6, PipelineTag: "image-to-image"}}
	if !reflect.DeepEqual(found, want) {
		t.Errorf("Search = %+v, want %+v", found, want)
	}
	for _, param := range []string{"search=muse+talk", "limit=5", "sort=downloads", "direction=-1"} {
		if !strings.Contains((*asked)[0], param) {
			t.Errorf("request %q lacks %q", (*asked)[0], param)
		}
	}
}

func TestFilesListsFilesAndLeavesFoldersOut(t *testing.T) {
	hub, asked := hubServing(t, map[string]string{
		"/api/models/TMElyralab/MuseTalk/tree/main": `[
			{"type":"directory","path":"musetalkV15","size":0},
			{"type":"file","path":"musetalkV15/musetalk.json","size":748},
			{"type":"file","path":"musetalkV15/unet.pth","size":3400074924}
		]`,
	})

	files, err := hub.Files(context.Background(), Repo{ID: "TMElyralab/MuseTalk"})
	if err != nil {
		t.Fatalf("Files: %v", err)
	}

	want := []File{{Path: "musetalkV15/musetalk.json", Size: 748}, {Path: "musetalkV15/unet.pth", Size: 3400074924}}
	if !reflect.DeepEqual(files, want) {
		t.Errorf("Files = %+v, want %+v", files, want)
	}
	if !strings.HasSuffix((*asked)[0], "?recursive=true") {
		t.Errorf("request %q does not ask for nested files", (*asked)[0])
	}
}

func TestHubErrorsNameTheStatus(t *testing.T) {
	hub, _ := hubServing(t, nil)

	if _, err := hub.Files(context.Background(), Repo{ID: "nobody/nothing"}); err == nil || !strings.Contains(err.Error(), "status 404") {
		t.Errorf("Files error = %v, want one naming status 404", err)
	}
	if _, err := hub.Search(context.Background(), "x", 1); err == nil || !strings.Contains(err.Error(), "status 404") {
		t.Errorf("Search error = %v, want one naming status 404", err)
	}
}
