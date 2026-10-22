package server

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (s *Server) removeMedia(owner string) error {
	s.outputs.jobs.Lock()
	defer s.outputs.jobs.Unlock()
	dir := filepath.Join(s.config.DataDir, "media-jobs")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var problems []error
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		decoded, err := hex.DecodeString(id)
		if err != nil || len(decoded) != 16 || hex.EncodeToString(decoded) != id {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			problems = append(problems, fmt.Errorf("read media job %s: %w", id, err))
			continue
		}
		var job mediaJob
		if json.Unmarshal(data, &job) != nil || job.ID != id || job.Owner == "" || job.Owner != owner || job.Conversation == "" || job.Message == "" || job.Call == "" || job.Kind != "image" && job.Kind != "video" {
			continue
		}
		if err := os.Remove(filepath.Join(dir, id+".bin")); err != nil && !errors.Is(err, os.ErrNotExist) {
			problems = append(problems, fmt.Errorf("remove media output %s: %w", id, err))
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			problems = append(problems, fmt.Errorf("remove media job %s: %w", id, err))
		}
	}
	return errors.Join(problems...)
}
