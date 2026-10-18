package projects

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"wsapi-video/internal/core"
	"wsapi-video/internal/httpx"
)

type Shot struct {
	ID                   string   `json:"id"`
	Number               int      `json:"number"`
	Title                string   `json:"title"`
	Description          string   `json:"description"`
	Prompt               string   `json:"prompt"`
	AspectRatio          string   `json:"aspect_ratio"`
	Duration             int      `json:"duration"`
	ReferenceAssetIDs    []string `json:"reference_asset_ids"`
	ImageCandidateIDs    []string `json:"image_candidate_ids"`
	VideoCandidateIDs    []string `json:"video_candidate_ids"`
	ExcludedCandidateIDs []string `json:"excluded_candidate_ids,omitempty"`
	SelectedImageID      string   `json:"selected_image_id"`
	SelectedVideoID      string   `json:"selected_video_id"`
}
type ShotCollection struct {
	Revision int64  `json:"revision"`
	Shots    []Shot `json:"shots"`
}

func (s *Service) getShots(w http.ResponseWriter, r *http.Request) {
	// One transaction provides a coherent collection revision and shot order.
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		failDB(w)
		return
	}
	defer tx.Rollback(r.Context())
	p, err := lockedProject(r.Context(), tx, r.PathValue("id"), core.UserFrom(r.Context()).ID)
	if projectError(w, err) {
		return
	}
	rows, err := tx.Query(r.Context(), `SELECT state FROM shots WHERE project_id=$1 ORDER BY position,id`, p.ID)
	if err != nil {
		failDB(w)
		return
	}
	out := ShotCollection{Revision: p.ShotsRevision, Shots: []Shot{}}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			break
		}
		var shot Shot
		if err = json.Unmarshal(raw, &shot); err != nil {
			break
		}
		out.Shots = append(out.Shots, shot)
	}
	rowErr := rows.Err()
	rows.Close()
	if err != nil || rowErr != nil {
		failDB(w)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		failDB(w)
		return
	}
	httpx.JSON(w, 200, out)
}
func (s *Service) putShots(w http.ResponseWriter, r *http.Request) {
	var in ShotCollection
	if !httpx.Decode(w, r, &in) {
		return
	}
	if len(in.Shots) > 1000 {
		httpx.Fail(w, 422, "invalid_shots", "一个项目最多保存 1000 个镜头")
		return
	}
	if in.Shots == nil {
		in.Shots = []Shot{}
	}
	seen := map[string]bool{}
	allRefs := map[string]string{}
	for i := range in.Shots {
		shot := &in.Shots[i]
		if shot.ID == "" {
			shot.ID = core.ID("shot")
		}
		if len(shot.ID) > 100 || seen[shot.ID] {
			httpx.Fail(w, 422, "invalid_shots", "镜头标识重复或无效")
			return
		}
		seen[shot.ID] = true
		shot.Number = i + 1
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
		refs, err := shotReferences(*shot)
		if err != nil {
			httpx.Fail(w, 422, "invalid_candidates", err.Error())
			return
		}
		for id, kind := range refs {
			if old, ok := allRefs[id]; ok && old != "" && kind != "" && old != kind {
				invalidAsset(w)
				return
			}
			if kind != "" || allRefs[id] == "" {
				allRefs[id] = kind
			}
		}
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		failDB(w)
		return
	}
	defer tx.Rollback(r.Context())
	p, err := lockedProject(r.Context(), tx, r.PathValue("id"), core.UserFrom(r.Context()).ID)
	if projectError(w, err) {
		return
	}
	if p.Status != "active" {
		httpx.Fail(w, 422, "project_archived", "请先恢复已归档项目")
		return
	}
	if p.ShotsRevision != in.Revision {
		conflict(w)
		return
	}
	if err = validateAssets(r.Context(), tx, core.UserFrom(r.Context()).ID, allRefs); err != nil {
		invalidAsset(w)
		return
	}
	_, err = tx.Exec(r.Context(), `DELETE FROM asset_references WHERE project_id=$1 AND owner_type='shot'`, p.ID)
	if err == nil {
		_, err = tx.Exec(r.Context(), `DELETE FROM shots WHERE project_id=$1`, p.ID)
	}
	for index, shot := range in.Shots {
		if err != nil {
			break
		}
		raw, marshalErr := json.Marshal(shot)
		if marshalErr != nil {
			err = marshalErr
			break
		}
		_, err = tx.Exec(r.Context(), `INSERT INTO shots(id,project_id,user_id,position,state) VALUES($1,$2,$3,$4,$5)`, shot.ID, p.ID, core.UserFrom(r.Context()).ID, index, raw)
		if err == nil {
			refs, _ := shotReferences(shot)
			err = replaceRefs(r.Context(), tx, p.ID, "shot", shot.ID, refs)
		}
	}
	if err == nil {
		err = tx.QueryRow(r.Context(), `UPDATE projects SET shots_revision=shots_revision+1,updated_at=now() WHERE id=$1 RETURNING shots_revision`, p.ID).Scan(&in.Revision)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		failDB(w)
		return
	}
	httpx.JSON(w, 200, in)
}
func shotReferences(shot Shot) (map[string]string, error) {
	if len(shot.ReferenceAssetIDs) > 100 || len(shot.ImageCandidateIDs) > 500 || len(shot.VideoCandidateIDs) > 500 || len(shot.ExcludedCandidateIDs) > 1000 {
		return nil, errors.New("镜头引用或候选过多")
	}
	excluded := map[string]bool{}
	for _, id := range shot.ExcludedCandidateIDs {
		if id == "" || len(id) > 100 {
			return nil, errors.New("已移除候选的标识无效")
		}
		excluded[id] = true
	}
	ids := map[string]string{}
	for _, id := range shot.ReferenceAssetIDs {
		if id != "" {
			ids[id] = ""
		}
	}
	for _, id := range shot.ImageCandidateIDs {
		if id != "" {
			if excluded[id] {
				return nil, errors.New("已移除候选不能同时保留在候选中")
			}
			ids[id] = "image"
		}
	}
	for _, id := range shot.VideoCandidateIDs {
		if id != "" {
			if excluded[id] {
				return nil, errors.New("已移除候选不能同时保留在候选中")
			}
			if ids[id] == "image" {
				return nil, errors.New("图片与视频候选不能混用")
			}
			ids[id] = "video"
		}
	}
	if shot.SelectedImageID != "" && !contains(shot.ImageCandidateIDs, shot.SelectedImageID) {
		return nil, errors.New("请从图片候选中选择成品")
	}
	if shot.SelectedVideoID != "" && !contains(shot.VideoCandidateIDs, shot.SelectedVideoID) {
		return nil, errors.New("请从视频候选中选择成品")
	}
	return ids, nil
}
func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}
