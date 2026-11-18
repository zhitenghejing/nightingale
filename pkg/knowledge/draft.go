package knowledge

import (
	"github.com/ccfos/nightingale/v6/pkg/modeloutput"
	"strings"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/opsauth"
)

// DraftInput deliberately extracts UTF-8 text only; PDF/DOCX originals remain
// available to a reviewer and never masquerade as successful OCR.
func (s *Store) DraftInput(user, group int64, ids []int64) (string, error) {
	if _, err := opsauth.Check(s.DB, user, group, true); err != nil {
		return "", err
	}
	if len(ids) == 0 || len(ids) > 5 {
		return "", opsauth.Fail(422, "source_required", "Select between one and five text sources")
	}
	var text strings.Builder
	for _, id := range ids {
		source, data, err := s.Source(user, id)
		if err != nil {
			return "", err
		}
		if source.GroupID != group {
			return "", opsauth.Fail(403, "source_forbidden", "Source belongs to another group")
		}
		if !strings.HasPrefix(source.MediaType, "text/plain") {
			return "", opsauth.Fail(422, "source_extract", "PDF and DOCX require reviewed text transcription before assisted drafting")
		}
		text.WriteString("\nSOURCE: " + source.Name + " SHA256:" + source.SHA256 + "\n")
		text.Write(data)
		if text.Len() > 64000 {
			return "", opsauth.Fail(422, "draft_input_limit", "Selected text exceeds 64 KB; provide a reviewed excerpt")
		}
	}
	return text.String(), nil
}
func (s *Store) GeneratedDraft(user, group int64, ids []int64, raw string) (*models.KnowledgeItem, error) {
	var result struct {
		Title string `json:"title"`
		Spec  Spec   `json:"spec"`
	}
	if modeloutput.JSON(raw, &result) != nil {
		return nil, opsauth.Fail(422, "draft_output", "Model did not return a valid knowledge draft")
	}
	result.Spec.SourceIDs = ids
	result.Spec.SourceNote = "Agent-generated draft from selected sources. Applicability, risks, actions and recovery checks require operator review."
	result.Spec.ValidatedAt = 0
	result.Spec.ReviewAt = 0
	// Generated operational actions are suggestions in prose until an operator
	// chooses an allowed fixed action and explicitly reviews it.
	result.Spec.Actions = []Action{}
	return s.Save(user, 0, group, 0, result.Title, result.Spec)
}
