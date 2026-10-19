package projects

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
)

func TestCanvasAppearanceValidation(t *testing.T) {
	for _, tc := range []struct {
		name, fields string
		valid        bool
	}{
		{"legacy document", "", true},
		{"lines", `,"backgroundMode":"lines","showImageInfo":false`, true},
		{"dots", `,"backgroundMode":"dots","showImageInfo":true`, true},
		{"blank", `,"backgroundMode":"blank"`, true},
		{"unknown mode", `,"backgroundMode":"grid"`, false},
		{"empty mode", `,"backgroundMode":""`, false},
		{"numeric mode", `,"backgroundMode":1`, false},
		{"null mode", `,"backgroundMode":null`, false},
		{"string visibility", `,"showImageInfo":"false"`, false},
		{"numeric visibility", `,"showImageInfo":0`, false},
		{"null visibility", `,"showImageInfo":null`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			graph := json.RawMessage(`{"nodes":[],"edges":[]` + tc.fields + `}`)
			if _, err := graphReferences(graph); (err == nil) != tc.valid {
				t.Fatalf("valid=%v, want %v; error=%v", err == nil, tc.valid, err)
			}
		})
	}
}

func canvasGraphValue(t *testing.T, canvas Canvas) map[string]any {
	t.Helper()
	var graph map[string]any
	if err := json.Unmarshal(canvas.Graph, &graph); err != nil {
		t.Fatal(err)
	}
	return graph
}

func TestCanvasAppearancePostgresPutGetDuplicate(t *testing.T) {
	f := newExportFixture(t)
	if _, err := f.db.Exec(context.Background(), "INSERT INTO canvases(project_id) VALUES($1)", f.project); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/projects/" + f.project
	graph := map[string]any{"nodes": []any{}, "edges": []any{}, "backgroundMode": "dots", "showImageInfo": true}
	input := map[string]any{"graph_schema_version": 1, "revision": 0, "graph": graph}
	var saved, loaded Canvas
	candidateJSONRequest(t, f, http.MethodPut, path+"/canvas", input, &saved, http.StatusOK)
	candidateJSONRequest(t, f, http.MethodGet, path+"/canvas", nil, &loaded, http.StatusOK)
	if saved.Revision != 1 || !reflect.DeepEqual(canvasGraphValue(t, loaded), graph) {
		t.Fatal("canvas appearance did not round-trip", loaded)
	}
	var duplicate Project
	candidateJSONRequest(t, f, http.MethodPost, path+"/duplicate", map[string]string{"name": "Appearance copy"}, &duplicate, http.StatusCreated)
	candidateJSONRequest(t, f, http.MethodGet, "/api/v1/projects/"+duplicate.ID+"/canvas", nil, &loaded, http.StatusOK)
	if !reflect.DeepEqual(canvasGraphValue(t, loaded), graph) {
		t.Fatal("duplicate lost saved appearance", loaded)
	}
	input["revision"] = saved.Revision
	graph["backgroundMode"] = "invalid"
	candidateJSONRequest(t, f, http.MethodPut, path+"/canvas", input, nil, http.StatusUnprocessableEntity)
	candidateJSONRequest(t, f, http.MethodPost, path+"/duplicate", map[string]any{"graph": graph}, nil, http.StatusUnprocessableEntity)
	candidateJSONRequest(t, f, http.MethodGet, path+"/canvas", nil, &loaded, http.StatusOK)
	if loaded.Revision != saved.Revision || !reflect.DeepEqual(canvasGraphValue(t, loaded), canvasGraphValue(t, saved)) {
		t.Fatal("invalid appearance changed the saved canvas", loaded)
	}
	// Older clients may still save documents without either optional field.
	delete(graph, "backgroundMode")
	delete(graph, "showImageInfo")
	candidateJSONRequest(t, f, http.MethodPut, path+"/canvas", input, &loaded, http.StatusOK)
	if !reflect.DeepEqual(canvasGraphValue(t, loaded), graph) {
		t.Fatal("legacy document compatibility changed", loaded)
	}
}
