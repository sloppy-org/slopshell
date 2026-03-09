package store

import (
	"database/sql"
	"errors"
	"strings"
)

func normalizeExternalContainerType(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "project":
		return "project"
	case "collection":
		return "collection"
	case "notebook":
		return "notebook"
	case "tag":
		return "tag"
	case "label":
		return "label"
	case "calendar":
		return "calendar"
	case "folder":
		return "folder"
	default:
		return ""
	}
}

func normalizeExternalContainerRef(raw string) string {
	return strings.TrimSpace(raw)
}

func normalizeExternalContainerProjectID(raw *string) (*string, error) {
	if raw == nil {
		return nil, nil
	}
	clean := strings.TrimSpace(*raw)
	if clean == "" {
		return nil, errors.New("external container mapping project_id is required")
	}
	return &clean, nil
}

func scanExternalContainerMapping(
	row interface {
		Scan(dest ...any) error
	},
) (ExternalContainerMapping, error) {
	var (
		out         ExternalContainerMapping
		workspaceID sql.NullInt64
		projectID   sql.NullString
		sphere      sql.NullString
	)
	if err := row.Scan(
		&out.ID,
		&out.Provider,
		&out.ContainerType,
		&out.ContainerRef,
		&workspaceID,
		&projectID,
		&sphere,
	); err != nil {
		return ExternalContainerMapping{}, err
	}
	out.Provider = normalizeExternalAccountProvider(out.Provider)
	out.ContainerType = normalizeExternalContainerType(out.ContainerType)
	out.ContainerRef = normalizeExternalContainerRef(out.ContainerRef)
	out.WorkspaceID = nullInt64Pointer(workspaceID)
	if projectID.Valid {
		clean := strings.TrimSpace(projectID.String)
		if clean != "" {
			out.ProjectID = &clean
		}
	}
	if sphere.Valid {
		clean := normalizeExternalAccountSphere(sphere.String)
		if clean != "" {
			out.Sphere = &clean
		}
	}
	return out, nil
}

func (s *Store) GetContainerMapping(provider, containerType, containerRef string) (ExternalContainerMapping, error) {
	cleanProvider := normalizeExternalAccountProvider(provider)
	if cleanProvider == "" {
		return ExternalContainerMapping{}, errors.New("external container mapping provider is required")
	}
	cleanType := normalizeExternalContainerType(containerType)
	if cleanType == "" {
		return ExternalContainerMapping{}, errors.New("external container mapping container_type is required")
	}
	cleanRef := normalizeExternalContainerRef(containerRef)
	if cleanRef == "" {
		return ExternalContainerMapping{}, errors.New("external container mapping container_ref is required")
	}
	return scanExternalContainerMapping(s.db.QueryRow(
		`SELECT id, provider, container_type, container_ref, workspace_id, project_id, sphere
		 FROM external_container_mappings
		 WHERE lower(provider) = lower(?) AND lower(container_type) = lower(?) AND lower(container_ref) = lower(?)`,
		cleanProvider,
		cleanType,
		cleanRef,
	))
}

func (s *Store) SetContainerMapping(provider, containerType, containerRef string, workspaceID *int64, projectID *string, sphere *string) (ExternalContainerMapping, error) {
	cleanProvider := normalizeExternalAccountProvider(provider)
	if cleanProvider == "" {
		return ExternalContainerMapping{}, errors.New("external container mapping provider is required")
	}
	cleanType := normalizeExternalContainerType(containerType)
	if cleanType == "" {
		return ExternalContainerMapping{}, errors.New("external container mapping container_type is required")
	}
	cleanRef := normalizeExternalContainerRef(containerRef)
	if cleanRef == "" {
		return ExternalContainerMapping{}, errors.New("external container mapping container_ref is required")
	}
	normalizedProjectID, err := normalizeExternalContainerProjectID(projectID)
	if err != nil {
		return ExternalContainerMapping{}, err
	}
	var normalizedSphere *string
	if sphere != nil {
		cleanSphere := normalizeExternalAccountSphere(*sphere)
		if cleanSphere == "" {
			return ExternalContainerMapping{}, errors.New("external container mapping sphere must be work or private")
		}
		normalizedSphere = &cleanSphere
	}
	if workspaceID == nil && normalizedProjectID == nil && normalizedSphere == nil {
		return ExternalContainerMapping{}, errors.New("external container mapping requires workspace_id, project_id, or sphere")
	}
	if workspaceID != nil {
		if *workspaceID <= 0 {
			return ExternalContainerMapping{}, errors.New("external container mapping workspace_id is required")
		}
		if _, err := s.GetWorkspace(*workspaceID); err != nil {
			return ExternalContainerMapping{}, err
		}
	}
	if normalizedProjectID != nil {
		if _, err := s.GetProject(*normalizedProjectID); err != nil {
			return ExternalContainerMapping{}, err
		}
	}

	if _, err := s.db.Exec(
		`INSERT INTO external_container_mappings (
			provider, container_type, container_ref, workspace_id, project_id, sphere
		) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT DO UPDATE SET
			workspace_id = excluded.workspace_id,
			project_id = excluded.project_id,
			sphere = excluded.sphere`,
		cleanProvider,
		cleanType,
		cleanRef,
		nullablePositiveID(valueOrZero(workspaceID)),
		normalizeOptionalString(normalizedProjectID),
		normalizeOptionalString(normalizedSphere),
	); err != nil {
		return ExternalContainerMapping{}, err
	}
	return s.GetContainerMapping(cleanProvider, cleanType, cleanRef)
}

func (s *Store) ListContainerMappings(provider string) ([]ExternalContainerMapping, error) {
	cleanProvider := strings.TrimSpace(provider)
	query := `SELECT id, provider, container_type, container_ref, workspace_id, project_id, sphere
		FROM external_container_mappings`
	args := []any{}
	if cleanProvider != "" {
		normalizedProvider := normalizeExternalAccountProvider(cleanProvider)
		if normalizedProvider == "" {
			return nil, errors.New("external container mapping provider is required")
		}
		query += ` WHERE lower(provider) = lower(?)`
		args = append(args, normalizedProvider)
	}
	query += ` ORDER BY lower(provider), lower(container_type), lower(container_ref), id`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ExternalContainerMapping
	for rows.Next() {
		mapping, err := scanExternalContainerMapping(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, mapping)
	}
	return out, rows.Err()
}

func (s *Store) DeleteContainerMapping(id int64) error {
	res, err := s.db.Exec(`DELETE FROM external_container_mappings WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}
