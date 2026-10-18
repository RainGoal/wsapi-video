package projects

import (
	"encoding/json"
	"net/http"
	"strings"

	"wsapi-video/internal/core"
	"wsapi-video/internal/httpx"
)

// A copy shares immutable asset identities and task provenance. Only layout and
// editable shot identities are copied; it cannot replay a generation request.
func (s *Service) duplicate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name  string          `json:"name"`
		Graph json.RawMessage `json:"graph"`
		Shots *[]Shot         `json:"shots"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if len([]rune(in.Name)) > 120 {
		httpx.Fail(w, 422, "invalid_name", "项目名不能超过 120 个字符")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		failDB(w)
		return
	}
	defer tx.Rollback(r.Context())
	u := core.UserFrom(r.Context())
	source, err := lockedProject(r.Context(), tx, r.PathValue("id"), u.ID)
	if projectError(w, err) {
		return
	}
	var savedGraph json.RawMessage
	var schemaVersion int
	err = tx.QueryRow(r.Context(), `SELECT graph,graph_schema_version FROM canvases WHERE project_id=$1`, source.ID).Scan(&savedGraph, &schemaVersion)
	if err != nil {
		failDB(w)
		return
	}
	if len(in.Graph) > 0 {
		savedGraph = in.Graph
	}
	refs, err := graphReferences(savedGraph)
	if err != nil {
		httpx.Fail(w, 422, "invalid_graph", "画布结构无效")
		return
	}
	rows, err := tx.Query(r.Context(), `SELECT state FROM shots WHERE project_id=$1 ORDER BY position,id`, source.ID)
	if err != nil {
		failDB(w)
		return
	}
	shots := []Shot{}
	shotIDs := map[string]string{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			break
		}
		var shot Shot
		if err = json.Unmarshal(raw, &shot); err != nil {
			break
		}
		shots = append(shots, shot)
	}
	rowErr := rows.Err()
	rows.Close()
	if err != nil || rowErr != nil {
		failDB(w)
		return
	}
	if in.Shots != nil {
		shots = *in.Shots
	}
	if len(shots) > 1000 {
		httpx.Fail(w, 422, "invalid_shots", "一个项目最多保存 1000 个镜头")
		return
	}
	seenShotIDs := map[string]bool{}
	for index := range shots {
		shot := &shots[index]
		oldID := shot.ID
		if len(oldID) > 100 || (oldID != "" && seenShotIDs[oldID]) {
			httpx.Fail(w, 422, "invalid_shots", "镜头标识重复或无效")
			return
		}
		seenShotIDs[oldID] = true
		shot.ID = core.ID("shot")
		if oldID != "" {
			shotIDs[oldID] = shot.ID
		}
		shot.Number = index + 1
		shot.Title = strings.TrimSpace(shot.Title)
		if len([]rune(shot.Title)) > 200 || len([]rune(shot.Description)) > 8000 || len([]rune(shot.Prompt)) > 12000 || shot.Duration < 0 || shot.Duration > 3600 || len(shot.AspectRatio) > 20 {
			httpx.Fail(w, 422, "invalid_shot", "镜头标题、提示词或时长超出限制")
			return
		}
		if shot.ReferenceAssetIDs == nil {
			shot.ReferenceAssetIDs = []string{}
		}
		if shot.ImageCandidateIDs == nil {
			shot.ImageCandidateIDs = []string{}
		}
		if shot.VideoCandidateIDs == nil {
			shot.VideoCandidateIDs = []string{}
		}
		if _, validationErr := shotReferences(*shot); validationErr != nil {
			httpx.Fail(w, 422, "invalid_candidates", validationErr.Error())
			return
		}
	}
	if len(shotIDs) > 0 {
		savedGraph, err = remapShotIDs(savedGraph, shotIDs)
		if err != nil {
			failDB(w)
			return
		}
	}
	allRefs := map[string]string{}
	for id, kind := range refs {
		allRefs[id] = kind
	}
	for _, shot := range shots {
		shotRefs, _ := shotReferences(shot)
		for id, kind := range shotRefs {
			if previous, exists := allRefs[id]; exists && previous != "" && kind != "" && previous != kind {
				invalidAsset(w)
				return
			}
			if kind != "" || allRefs[id] == "" {
				allRefs[id] = kind
			}
		}
	}
	if source.CoverAssetID != "" {
		allRefs[source.CoverAssetID] = "image"
	}
	if err = validateAssets(r.Context(), tx, u.ID, allRefs); err != nil {
		invalidAsset(w)
		return
	}
	if in.Name == "" {
		in.Name = source.Name + " · 副本"
		if len([]rune(in.Name)) > 120 {
			in.Name = string([]rune(source.Name)[:115]) + " · 副本"
		}
	}
	id := core.ID("prj")
	project, err := scanProject(tx.QueryRow(r.Context(), `INSERT INTO projects(id,user_id,name,description,cover_asset_id) VALUES($1,$2,$3,$4,nullif($5,'')) RETURNING `+projectFields, id, u.ID, in.Name, source.Description, source.CoverAssetID))
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO canvases(project_id,graph_schema_version,graph) VALUES($1,$2,$3)`, id, schemaVersion, savedGraph)
	}
	if err == nil {
		err = replaceRefs(r.Context(), tx, id, "canvas", id, refs)
	}
	if err == nil && source.CoverAssetID != "" {
		err = replaceRefs(r.Context(), tx, id, "cover", id, map[string]string{source.CoverAssetID: "image"})
	}
	for index, shot := range shots {
		if err != nil {
			break
		}
		raw, _ := json.Marshal(shot)
		_, err = tx.Exec(r.Context(), `INSERT INTO shots(id,project_id,user_id,position,state) VALUES($1,$2,$3,$4,$5)`, shot.ID, id, u.ID, index, raw)
		if err == nil {
			shotRefs, _ := shotReferences(shot)
			err = replaceRefs(r.Context(), tx, id, "shot", shot.ID, shotRefs)
		}
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		failDB(w)
		return
	}
	httpx.JSON(w, 201, project)
}

func remapShotIDs(raw json.RawMessage, ids map[string]string) (json.RawMessage, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	var visit func(any)
	visit = func(value any) {
		switch value := value.(type) {
		case map[string]any:
			for key, child := range value {
				if key == "shot_id" || key == "shotId" {
					if original, ok := child.(string); ok {
						if mapped, found := ids[original]; found {
							value[key] = mapped
						}
					}
				}
				visit(child)
			}
		case []any:
			for _, child := range value {
				visit(child)
			}
		}
	}
	visit(value)
	return json.Marshal(value)
}
