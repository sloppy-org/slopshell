package store

func (s *Store) migrateWorkspaceSphereSupport() error {
	tableColumns, err := s.tableColumnSet("workspaces")
	if err != nil {
		return err
	}
	if tableColumns["workspaces"]["sphere"] {
		return nil
	}
	_, err = s.db.Exec(`ALTER TABLE workspaces ADD COLUMN sphere TEXT NOT NULL DEFAULT 'private' CHECK (sphere IN ('work', 'private'))`)
	return err
}

func (s *Store) migrateWorkspaceProjectSupport() error {
	tableColumns, err := s.tableColumnSet("workspaces")
	if err != nil {
		return err
	}
	if tableColumns["workspaces"]["project_id"] {
		return nil
	}
	_, err = s.db.Exec(`ALTER TABLE workspaces ADD COLUMN project_id TEXT REFERENCES projects(id) ON DELETE SET NULL`)
	return err
}

func (s *Store) migrateWorkspaceConfigSupport() error {
	tableColumns, err := s.tableColumnSet("workspaces")
	if err != nil {
		return err
	}
	type columnDef struct {
		name string
		sql  string
	}
	defs := []columnDef{
		{name: "mcp_url", sql: `ALTER TABLE workspaces ADD COLUMN mcp_url TEXT NOT NULL DEFAULT ''`},
		{name: "canvas_session_id", sql: `ALTER TABLE workspaces ADD COLUMN canvas_session_id TEXT NOT NULL DEFAULT ''`},
		{name: "chat_model", sql: `ALTER TABLE workspaces ADD COLUMN chat_model TEXT NOT NULL DEFAULT ''`},
		{name: "chat_model_reasoning_effort", sql: `ALTER TABLE workspaces ADD COLUMN chat_model_reasoning_effort TEXT NOT NULL DEFAULT ''`},
	}
	for _, def := range defs {
		if tableColumns["workspaces"][def.name] {
			continue
		}
		if _, err := s.db.Exec(def.sql); err != nil {
			return err
		}
	}
	return nil
}
