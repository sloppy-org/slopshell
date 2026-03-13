package web

import (
	"strings"

	"github.com/krystophny/tabura/internal/modelprofile"
	"github.com/krystophny/tabura/internal/store"
)

type appServerModelProfile struct {
	Alias        string
	Model        string
	ThreadParams map[string]interface{}
	TurnParams   map[string]interface{}
}

func (a *App) effectiveProjectChatModelAlias(project store.Project) string {
	if alias := modelprofile.ResolveAlias(project.ChatModel, ""); alias != "" {
		return alias
	}
	if alias := modelprofile.ResolveAlias(a.appServerModel, ""); alias != "" {
		return alias
	}
	return modelprofile.AliasSpark
}

func (a *App) effectiveProjectChatModelReasoningEffort(project store.Project) string {
	alias := a.effectiveProjectChatModelAlias(project)
	effort := modelprofile.NormalizeReasoningEffort(alias, project.ChatModelReasoningEffort)
	if effort == "" {
		return modelprofile.MainThreadReasoningEffort(alias)
	}
	return effort
}

func (a *App) appServerModelProfileForProject(project store.Project) appServerModelProfile {
	alias := a.effectiveProjectChatModelAlias(project)
	effort := a.effectiveProjectChatModelReasoningEffort(project)
	model := modelprofile.ModelForAlias(alias)
	if model == "" {
		model = strings.TrimSpace(a.appServerModel)
	}
	if model == "" {
		model = modelprofile.ModelForAlias(modelprofile.AliasSpark)
	}
	reasoning := modelprofile.MainThreadReasoningParamsForEffort(alias, effort)
	return appServerModelProfile{
		Alias:        alias,
		Model:        model,
		ThreadParams: nil,
		TurnParams:   reasoning,
	}

}

func (a *App) appServerModelProfileForProjectKey(projectKey string) appServerModelProfile {
	cleanKey := strings.TrimSpace(projectKey)
	if cleanKey != "" {
		if project, err := a.store.GetProjectByProjectKey(cleanKey); err == nil {
			return a.appServerModelProfileForProject(project)
		}
	}
	alias := modelprofile.AliasForModel(a.appServerModel)
	if alias == "" {
		alias = modelprofile.AliasSpark
	}
	legacyModel := modelprofile.ModelForAlias(alias)
	if legacyModel == "" {
		legacyModel = strings.TrimSpace(a.appServerModel)
	}
	if legacyModel == "" {
		legacyModel = modelprofile.ModelForAlias(modelprofile.AliasSpark)
	}
	legacyReasoning := modelprofile.MainThreadReasoningParamsForEffort(alias, modelprofile.MainThreadReasoningEffort(alias))
	return appServerModelProfile{
		Alias:        alias,
		Model:        legacyModel,
		ThreadParams: nil,
		TurnParams:   legacyReasoning,
	}
}

func (a *App) resetProjectChatAppSession(projectKey string) {
	key := strings.TrimSpace(projectKey)
	if key == "" {
		return
	}
	session, err := a.chatSessionForProjectKey(key)
	if err != nil {
		return
	}
	a.closeAppSession(session.ID)
	_ = a.store.UpdateChatSessionThread(session.ID, "")
}
