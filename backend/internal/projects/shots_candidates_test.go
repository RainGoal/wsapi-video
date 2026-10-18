package projects

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestShotCandidateExclusionsJSONCompatibility(t *testing.T) {
	var legacy Shot
	if err := json.Unmarshal([]byte(`{"id":"legacy","image_candidate_ids":["image-current"]}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.ExcludedCandidateIDs != nil {
		t.Fatal("legacy shot must keep an absent exclusion set nil")
	}
	raw, err := json.Marshal(legacy)
	if err != nil || bytes.Contains(raw, []byte(`"excluded_candidate_ids"`)) {
		t.Fatal("empty exclusions must preserve the legacy JSON shape", string(raw), err)
	}
	want := Shot{ID: "modern", ImageCandidateIDs: []string{"image-current"}, SelectedImageID: "image-current", ExcludedCandidateIDs: []string{"removed-image", "removed-video"}}
	raw, err = json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got Shot
	if err = json.Unmarshal(raw, &got); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("shot JSON round trip lost candidate removal decisions", got, err)
	}
}

func TestShotCandidateExclusionsDoNotCreateReferences(t *testing.T) {
	shot := Shot{
		ReferenceAssetIDs: []string{"reference", "removed-reference"},
		ImageCandidateIDs: []string{"image-current"},
		VideoCandidateIDs: []string{"video-current"},
		SelectedImageID:   "image-current", SelectedVideoID: "video-current",
		ExcludedCandidateIDs: []string{"removed-reference", "missing-image", "missing-video", "missing-image"},
	}
	got, err := shotReferences(shot)
	want := map[string]string{"reference": "", "removed-reference": "", "image-current": "image", "video-current": "video"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("exclusions changed active references or required removed assets", got, err)
	}
}

func TestShotCandidateExclusionsValidation(t *testing.T) {
	limit := make([]string, 1000)
	for i := range limit {
		limit[i] = fmt.Sprintf("removed-%d", i)
	}
	for _, test := range []struct {
		name  string
		shot  Shot
		valid bool
	}{
		{"absent", Shot{}, true},
		{"maximum count", Shot{ExcludedCandidateIDs: limit}, true},
		{"too many", Shot{ExcludedCandidateIDs: append(append([]string{}, limit...), "overflow")}, false},
		{"maximum ID length", Shot{ExcludedCandidateIDs: []string{strings.Repeat("a", 100)}}, true},
		{"ID too long", Shot{ExcludedCandidateIDs: []string{strings.Repeat("a", 101)}}, false},
		{"empty ID", Shot{ExcludedCandidateIDs: []string{""}}, false},
		{"repeated exclusion", Shot{ExcludedCandidateIDs: []string{"removed", "removed"}}, true},
		{"image overlap", Shot{ImageCandidateIDs: []string{"image"}, ExcludedCandidateIDs: []string{"image"}}, false},
		{"video overlap", Shot{VideoCandidateIDs: []string{"video"}, ExcludedCandidateIDs: []string{"video"}}, false},
		{"selected image overlap", Shot{ImageCandidateIDs: []string{"image"}, SelectedImageID: "image", ExcludedCandidateIDs: []string{"image"}}, false},
		{"selected video overlap", Shot{VideoCandidateIDs: []string{"video"}, SelectedVideoID: "video", ExcludedCandidateIDs: []string{"video"}}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := shotReferences(test.shot); (err == nil) != test.valid {
				t.Fatalf("validation valid=%v, want %v; error=%v", err == nil, test.valid, err)
			}
		})
	}
}

func candidateJSONRequest(t *testing.T, f *exportFixture, method, path string, input, output any, wantStatus int) {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(method, f.host.URL+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := f.host.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != wantStatus {
		t.Fatal("candidate request failed", method, path, response.StatusCode, string(body), err)
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if output != nil {
		if err = json.Unmarshal(body, &envelope); err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(envelope.Data, output); err != nil {
			t.Fatal(err)
		}
	}
}

func TestShotCandidateExclusionsPostgresPutGetDuplicate(t *testing.T) {
	f := newExportFixture(t)
	if _, err := f.db.Exec(context.Background(), "INSERT INTO canvases(project_id) VALUES($1)", f.project); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/projects/" + f.project
	input := ShotCollection{Shots: []Shot{{ID: "shot-original", Title: "Removed candidates", ExcludedCandidateIDs: []string{"missing-image", "missing-video"}}}}
	var saved ShotCollection
	candidateJSONRequest(t, f, http.MethodPut, path+"/shots", input, &saved, http.StatusOK)
	if saved.Revision != input.Revision+1 || len(saved.Shots) != 1 || !reflect.DeepEqual(saved.Shots[0].ExcludedCandidateIDs, input.Shots[0].ExcludedCandidateIDs) {
		t.Fatal("PUT failed to persist exclusions", saved)
	}
	var loaded ShotCollection
	candidateJSONRequest(t, f, http.MethodGet, path+"/shots", nil, &loaded, http.StatusOK)
	if !reflect.DeepEqual(loaded, saved) {
		t.Fatal("GET changed saved exclusions", loaded, saved)
	}
	invalid := saved
	invalid.Shots = append([]Shot(nil), saved.Shots...)
	invalid.Shots[0].ImageCandidateIDs = []string{"missing-image"}
	candidateJSONRequest(t, f, http.MethodPut, path+"/shots", invalid, nil, http.StatusUnprocessableEntity)
	candidateJSONRequest(t, f, http.MethodGet, path+"/shots", nil, &loaded, http.StatusOK)
	if !reflect.DeepEqual(loaded, saved) {
		t.Fatal("rejected candidate contradiction changed persisted state", loaded, saved)
	}
	candidateJSONRequest(t, f, http.MethodPost, path+"/duplicate", map[string]any{"shots": invalid.Shots}, nil, http.StatusUnprocessableEntity)
	var duplicate Project
	candidateJSONRequest(t, f, http.MethodPost, path+"/duplicate", map[string]string{"name": "Candidate removal copy"}, &duplicate, http.StatusCreated)
	var copied ShotCollection
	candidateJSONRequest(t, f, http.MethodGet, "/api/v1/projects/"+duplicate.ID+"/shots", nil, &copied, http.StatusOK)
	if len(copied.Shots) != 1 || copied.Shots[0].ID == saved.Shots[0].ID || !reflect.DeepEqual(copied.Shots[0].ExcludedCandidateIDs, saved.Shots[0].ExcludedCandidateIDs) {
		t.Fatal("duplicate lost exclusions or reused the original shot identity", copied)
	}
	var references, assets int
	err := f.db.QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM asset_references WHERE project_id IN ($1,$2)),
		(SELECT count(*) FROM assets WHERE id = ANY($3::text[]))`, f.project, duplicate.ID, input.Shots[0].ExcludedCandidateIDs).Scan(&references, &assets)
	if err != nil || references != 0 || assets != 0 {
		t.Fatal("excluded missing candidates became live references or required asset rows", references, assets, err)
	}
}
