package store

import (
	"errors"
	"strings"
)

func normalizeItemListFilter(filter ItemListFilter) (ItemListFilter, error) {
	normalized := ItemListFilter{
		Source:              normalizeOptionalSourceFilter(filter.Source),
		WorkspaceUnassigned: filter.WorkspaceUnassigned,
	}
	sphere, err := normalizeOptionalSphereFilter(filter.Sphere)
	if err != nil {
		return ItemListFilter{}, err
	}
	normalized.Sphere = sphere
	if filter.WorkspaceID != nil {
		if *filter.WorkspaceID <= 0 {
			return ItemListFilter{}, errors.New("workspace_id must be a positive integer")
		}
		value := *filter.WorkspaceID
		normalized.WorkspaceID = &value
	}
	if normalized.WorkspaceID != nil && normalized.WorkspaceUnassigned {
		return ItemListFilter{}, errors.New("workspace_id cannot be combined with workspace_id=null")
	}
	if filter.ProjectID != nil {
		projectID := strings.TrimSpace(*filter.ProjectID)
		if projectID != "" {
			normalized.ProjectID = &projectID
		}
	}
	if filter.ContextID != nil {
		if *filter.ContextID <= 0 {
			return ItemListFilter{}, errors.New("context_id must be a positive integer")
		}
		value := *filter.ContextID
		normalized.ContextID = &value
	}
	return normalized, nil
}

func appendItemFilterClauses(parts []string, args []any, filter ItemListFilter, alias string) ([]string, []any) {
	column := func(name string) string {
		return alias + name
	}
	outerColumn := func(name string) string {
		if alias == "" {
			return "items." + name
		}
		return alias + name
	}
	workspaceProjectColumn := func() string {
		return `(SELECT project_id FROM workspaces WHERE id = ` + outerColumn("workspace_id") + `)`
	}
	if filter.Sphere != "" {
		parts = append(parts, scopedContextFilter("context_items", "item_id", outerColumn("id")))
		args = append(args, filter.Sphere)
	}
	if filter.Source != "" {
		parts = append(parts, "lower(trim("+column("source")+")) = ?")
		args = append(args, filter.Source)
	}
	if filter.WorkspaceID != nil {
		parts = append(parts, column("workspace_id")+" = ?")
		args = append(args, *filter.WorkspaceID)
	}
	if filter.WorkspaceUnassigned {
		parts = append(parts, column("workspace_id")+" IS NULL")
	}
	if filter.ProjectID != nil {
		parts = append(parts, `COALESCE(`+column("project_id")+`, `+workspaceProjectColumn()+`) = ?`)
		args = append(args, *filter.ProjectID)
	}
	if filter.ContextID != nil {
		contextItemMatch := `EXISTS (
WITH RECURSIVE context_tree(id) AS (
  SELECT id FROM contexts WHERE id = ?
  UNION ALL
  SELECT c.id
  FROM contexts c
  JOIN context_tree tree ON c.parent_id = tree.id
)
SELECT 1
FROM context_items ci
JOIN context_tree tree ON tree.id = ci.context_id
WHERE ci.item_id = ` + outerColumn("id") + `
)`
		contextWorkspaceMatch := `EXISTS (
WITH RECURSIVE context_tree(id) AS (
  SELECT id FROM contexts WHERE id = ?
  UNION ALL
  SELECT c.id
  FROM contexts c
  JOIN context_tree tree ON c.parent_id = tree.id
)
SELECT 1
FROM context_workspaces cw
JOIN context_tree tree ON tree.id = cw.context_id
WHERE cw.workspace_id = ` + outerColumn("workspace_id") + `
)`
		parts = append(parts, `(`+contextItemMatch+` OR `+contextWorkspaceMatch+`)`)
		args = append(args, *filter.ContextID, *filter.ContextID)
	}
	return parts, args
}
