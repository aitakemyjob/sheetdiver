package core

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/aitakemyjob/sheetdiver/internal/library"
)

// PartUse is a part that a delete would remove, with the projects that
// use it.
type PartUse struct {
	Part     library.Part `json:"part"`
	Projects []string     `json:"projects,omitempty"`
}

// DeletePlan says what deleting a part removes, so the user can confirm:
// its datasheet (if any) with every part on it, what agents and users
// recorded for that datasheet, notes, and the projects that use the
// parts.
type DeletePlan struct {
	Datasheet *library.Datasheet `json:"datasheet,omitempty"`
	Parts     []PartUse          `json:"parts"`
	// Recorded counts recorded rows by kind (parameters, figures…).
	Recorded map[string]int `json:"recorded"`
	Notes    int            `json:"notes"`
	// InProjects is true when some part is used in a project.
	InProjects bool `json:"inProjects"`
}

// DeletePreview plans deleting a part: with a datasheet, the datasheet
// and all its parts go; without one, just the part.
func (s *Service) DeletePreview(ctx context.Context, ref string) (DeletePlan, error) {
	res, err := s.Lib.Resolve(ctx, ref)
	if err != nil {
		return DeletePlan{}, err
	}
	plan := DeletePlan{Recorded: map[string]int{}, Parts: []PartUse{}}
	parts := []library.Part{res.Part}
	if res.Part.DatasheetID != 0 {
		d, err := s.Lib.Datasheet(ctx, res.Part.DatasheetID)
		if err != nil {
			return plan, err
		}
		plan.Datasheet = &d
		if parts, err = s.Lib.PartsOfDatasheet(ctx, d.ID); err != nil {
			return plan, err
		}
		if plan.Recorded, err = s.Lib.RecordedCounts(ctx, d.ID); err != nil {
			return plan, err
		}
		n, _ := s.Lib.CountNotesOf(ctx, library.TargetDatasheet, strconv.FormatInt(d.ID, 10))
		plan.Notes += n
	}
	for _, p := range parts {
		use := PartUse{Part: p}
		projects, err := s.Lib.ProjectsOfPart(ctx, p.ID)
		if err != nil {
			return plan, err
		}
		for _, pr := range projects {
			use.Projects = append(use.Projects, pr.Name)
		}
		plan.InProjects = plan.InProjects || len(use.Projects) > 0
		n, _ := s.Lib.CountNotesOf(ctx, library.TargetPart, p.MPN)
		plan.Notes += n
		plan.Parts = append(plan.Parts, use)
	}
	return plan, nil
}

// DeleteResult says what a delete removed.
type DeleteResult struct {
	Datasheet string   `json:"datasheet,omitempty"`
	Deleted   []string `json:"deleted"`
	// Kept are parts left in their projects without a datasheet.
	Kept []string `json:"kept,omitempty"`
}

// DeletePart deletes what DeletePreview planned for ref: the datasheet
// (its PDF, extracted and recorded data, notes) and its parts with their
// notes and variants. Parts used in projects are removed from them too,
// unless keepInProjects is set: then they stay in the library and the
// projects as parts without a datasheet. It can't be undone; only the
// user (in the UI) can do it, never an agent.
func (s *Service) DeletePart(ctx context.Context, ref string, keepInProjects bool) (DeleteResult, error) {
	plan, err := s.DeletePreview(ctx, ref)
	if err != nil {
		return DeleteResult{}, err
	}
	out := DeleteResult{Deleted: []string{}}
	s.importMu.Lock()
	defer s.importMu.Unlock()
	for _, use := range plan.Parts {
		if keepInProjects && len(use.Projects) > 0 {
			out.Kept = append(out.Kept, use.Part.MPN)
			continue
		}
		if err := s.Lib.DeletePart(ctx, use.Part.ID); err != nil {
			return out, fmt.Errorf("delete %s: %w", use.Part.MPN, err)
		}
		_, _ = s.Lib.DeleteNotesOf(ctx, library.TargetPart, use.Part.MPN)
		out.Deleted = append(out.Deleted, use.Part.MPN)
	}
	if d := plan.Datasheet; d != nil {
		if _, err := s.Lib.DeleteDatasheet(ctx, d.ID); err != nil {
			return out, fmt.Errorf("delete %s: %w", d.Filename, err)
		}
		_, _ = s.Lib.DeleteNotesOf(ctx, library.TargetDatasheet, strconv.FormatInt(d.ID, 10))
		_ = os.Remove(s.Lib.DatasheetPath(d.File))
		out.Datasheet = d.Filename
	}
	s.emit(EvLibrary, map[string]any{"action": "deleted"})
	s.emit(EvProject, map[string]any{"action": "deleted"})
	return out, nil
}
