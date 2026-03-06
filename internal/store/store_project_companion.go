package store

import (
	"errors"
	"strings"
	"time"
)

func (s *Store) UpdateProjectCompanionConfig(id, configJSON string) error {
	projectID := strings.TrimSpace(id)
	if projectID == "" {
		return errors.New("project id is required")
	}
	cleanConfig := strings.TrimSpace(configJSON)
	if cleanConfig == "" {
		cleanConfig = "{}"
	}
	_, err := s.db.Exec(
		`UPDATE projects SET companion_config_json = ?, updated_at = ? WHERE id = ?`,
		cleanConfig,
		time.Now().Unix(),
		projectID,
	)
	return err
}
