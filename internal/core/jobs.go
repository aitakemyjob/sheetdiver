package core

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aitakemyjob/sheetdiver/internal/extract"
)

// Import job states.
const (
	JobQueued   = "queued"
	JobRunning  = "running"
	JobDone     = "done"
	JobExisting = "existing"
	JobFailed   = "failed"
)

// ImportJob tracks one datasheet import for the import queue: what is
// being imported, how far it got and how it ended.
type ImportJob struct {
	ID          int      `json:"id"`
	Name        string   `json:"name"`
	Source      string   `json:"source"`
	Status      string   `json:"status"`
	Stage       string   `json:"stage,omitempty"`
	Done        int      `json:"done"`
	Total       int      `json:"total"`
	Error       string   `json:"error,omitempty"`
	DatasheetID int64    `json:"datasheetId,omitempty"`
	Parts       []string `json:"parts,omitempty"`
	Batch       string   `json:"batch,omitempty"`
	// Language is the datasheet's language, and Translated whether it
	// was extracted from its English translation; the UI translates and
	// re-extracts non-English imports when the model is installed.
	Language   string    `json:"language,omitempty"`
	Translated bool      `json:"translated,omitempty"`
	Started    time.Time `json:"started"`
	Finished   time.Time `json:"finished"`

	req ImportRequest
}

// jobs is the import queue's state.
type jobs struct {
	mu   sync.Mutex
	seq  int
	list []*ImportJob
}

// maxJobs bounds how many finished jobs are remembered.
const maxJobs = 300

// describe names an import request for the queue.
func describe(req ImportRequest) (name, source string) {
	switch {
	case req.Path != "":
		return firstNonEmpty(req.Filename, filepath.Base(req.Path)), req.Path
	case req.URL != "":
		return req.URL, req.URL
	case req.Lookup != "":
		return req.Lookup, "part number " + req.Lookup
	}
	return firstNonEmpty(req.Filename, "datasheet.pdf"), "upload"
}

// newJob registers a queued import.
func (s *Service) newJob(req ImportRequest) *ImportJob {
	name, src := describe(req)
	s.jobs.mu.Lock()
	s.jobs.seq++
	j := &ImportJob{ID: s.jobs.seq, Name: name, Source: src, Status: JobQueued, req: req}
	j.req.Data = nil // never keep uploads in memory
	s.jobs.list = append(s.jobs.list, j)
	if n := len(s.jobs.list); n > maxJobs {
		// Forget the oldest finished jobs.
		keep := s.jobs.list[:0]
		drop := n - maxJobs
		for _, x := range s.jobs.list {
			if drop > 0 && (x.Status == JobDone || x.Status == JobExisting || x.Status == JobFailed) {
				drop--
				continue
			}
			keep = append(keep, x)
		}
		s.jobs.list = keep
	}
	snap := *j
	s.jobs.mu.Unlock()
	s.emit(EvImport, snap)
	return j
}

// updateJob changes a job and tells the UI. Progress updates are cheap
// enough to send on every change.
func (s *Service) updateJob(j *ImportJob, fn func(*ImportJob)) {
	if j == nil {
		return
	}
	s.jobs.mu.Lock()
	fn(j)
	snap := *j
	s.jobs.mu.Unlock()
	s.emit(EvImport, snap)
}

// runJob runs a job's import, recording the outcome on the job. Uploads
// pass their bytes as data; jobs never hold them.
func (s *Service) runJob(ctx context.Context, j *ImportJob, data []byte) (ImportResult, error) {
	req := j.req
	req.Data = data
	s.updateJob(j, func(j *ImportJob) {
		j.Status, j.Started, j.Error = JobRunning, time.Now(), ""
	})
	res, err := s.importPDF(ctx, req, j)
	s.updateJob(j, func(j *ImportJob) {
		j.Finished, j.Stage = time.Now(), ""
		switch {
		case err != nil:
			j.Status, j.Error = JobFailed, err.Error()
		case res.Existing:
			j.Status = JobExisting
		default:
			j.Status = JobDone
		}
		if err == nil {
			j.DatasheetID = res.Datasheet.ID
			j.Name = res.Datasheet.Filename
			j.Language = res.Datasheet.Meta["language"]
			j.Translated = res.Datasheet.Meta["translated"] != ""
			j.Parts = nil
			for _, p := range res.Parts {
				j.Parts = append(j.Parts, p.MPN)
			}
		}
	})
	return res, err
}

// ImportOutcome is one result of a batch import.
type ImportOutcome struct {
	Job    ImportJob     `json:"job"`
	Result *ImportResult `json:"result,omitempty"`
}

// ImportBatch imports several datasheets concurrently (up to the PDF
// engine's workers at a time) and waits for all of them.
func (s *Service) ImportBatch(ctx context.Context, reqs []ImportRequest) []ImportOutcome {
	batch := fmt.Sprintf("b%d", time.Now().UnixNano())
	out := make([]ImportOutcome, len(reqs))
	js := make([]*ImportJob, len(reqs))
	for i, r := range reqs {
		js[i] = s.newJob(r)
		js[i].Batch = batch
	}
	var wg sync.WaitGroup
	for i, r := range reqs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			j := js[i]
			res, err := s.runJob(ctx, j, r.Data)
			s.jobs.mu.Lock()
			out[i].Job = *j
			s.jobs.mu.Unlock()
			if err == nil {
				out[i].Result = &res
			}
		}()
	}
	wg.Wait()
	s.emit(EvLibrary, map[string]any{"action": "batch"})
	return out
}

// StartImports queues imports to run in the background and returns their
// jobs at once; the UI follows them through import events.
func (s *Service) StartImports(reqs []ImportRequest) []ImportJob {
	batch := fmt.Sprintf("b%d", time.Now().UnixNano())
	out := make([]ImportJob, 0, len(reqs))
	for _, r := range reqs {
		j := s.newJob(r)
		j.Batch = batch
		s.jobs.mu.Lock()
		out = append(out, *j)
		s.jobs.mu.Unlock()
		go s.runJob(context.Background(), j, nil) //nolint:errcheck // the outcome is on the job
	}
	return out
}

// ImportJobs lists the import queue, newest first.
func (s *Service) ImportJobs() []ImportJob {
	s.jobs.mu.Lock()
	defer s.jobs.mu.Unlock()
	out := make([]ImportJob, 0, len(s.jobs.list))
	for i := len(s.jobs.list) - 1; i >= 0; i-- {
		out = append(out, *s.jobs.list[i])
	}
	return out
}

// RetryImport runs a failed job again.
func (s *Service) RetryImport(id int) (ImportJob, error) {
	s.jobs.mu.Lock()
	var j *ImportJob
	for _, x := range s.jobs.list {
		if x.ID == id {
			j = x
		}
	}
	s.jobs.mu.Unlock()
	switch {
	case j == nil:
		return ImportJob{}, fmt.Errorf("no import job %d", id)
	case j.Status != JobFailed:
		return *j, fmt.Errorf("import job %d is %s; only failed jobs can be retried", id, j.Status)
	case j.req.Path == "" && j.req.URL == "" && j.req.Lookup == "":
		return *j, errors.New("an uploaded file can't be retried; drop it again")
	}
	s.updateJob(j, func(j *ImportJob) { j.Status, j.Error = JobQueued, "" })
	go s.runJob(context.Background(), j, nil) //nolint:errcheck // the outcome is on the job
	s.jobs.mu.Lock()
	defer s.jobs.mu.Unlock()
	return *j, nil
}

// ClearImports forgets finished jobs.
func (s *Service) ClearImports() {
	s.jobs.mu.Lock()
	keep := s.jobs.list[:0]
	for _, j := range s.jobs.list {
		if j.Status == JobQueued || j.Status == JobRunning {
			keep = append(keep, j)
		}
	}
	s.jobs.list = keep
	s.jobs.mu.Unlock()
	s.emit(EvImport, map[string]any{"cleared": true})
}

// ReextractOutdated queues re-extraction of every datasheet read by an
// older extractor, so extraction fixes reach existing libraries. What
// agents and users recorded is kept. It returns the queued jobs.
func (s *Service) ReextractOutdated(ctx context.Context) ([]ImportJob, error) {
	sheets, err := s.Lib.Datasheets(ctx)
	if err != nil {
		return nil, err
	}
	var reqs []ImportRequest
	for _, d := range sheets {
		if d.Extractor != extract.Version {
			reqs = append(reqs, ImportRequest{Path: s.Lib.DatasheetPath(d.File), Filename: d.Filename, Force: true, Translated: d.Meta["translated"] != ""})
		}
	}
	if len(reqs) == 0 {
		return nil, nil
	}
	return s.StartImports(reqs), nil
}

// maxFolderPDFs bounds a folder import.
const maxFolderPDFs = 500

// FolderRequests lists import requests for every PDF in a folder
// (recursively when asked), skipping hidden folders.
func FolderRequests(dir string, recursive bool) ([]ImportRequest, error) {
	dir = expandHome(dir)
	st, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("%s is not a folder", dir)
	}
	var out []ImportRequest
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entries are skipped
		}
		if d.IsDir() {
			if p != dir && (!recursive || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(p), ".pdf") {
			out = append(out, ImportRequest{Path: p})
			if len(out) > maxFolderPDFs {
				return fmt.Errorf("more than %d PDFs in %s; import a smaller folder", maxFolderPDFs, dir)
			}
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, err
}
