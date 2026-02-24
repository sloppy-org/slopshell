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
	if alias := modelprofile.AliasForModel(a.appServerModel); alias != "" {
		return alias
	}
	return modelprofile.AliasSpark
}

func (a *App) appServerModelProfileForProject(project store.Project) appServerModelProfile {
	if alias := modelprofile.ResolveAlias(project.ChatModel, ""); alias != "" {
		model := modelprofile.ModelForAlias(alias)
		reasoning := modelprofile.MainThreadReasoningParams(alias)
		return appServerModelProfile{
			Alias:        alias,
			Model:        model,
			ThreadParams: reasoning,
			TurnParams:   reasoning,
		}
	}

	legacyModel := strings.TrimSpace(a.appServerModel)
	legacyReasoning := appServerReasoningParamsForModel(legacyModel, a.appServerSparkReasoningEffort)
	return appServerModelProfile{
		Alias:        a.effectiveProjectChatModelAlias(project),
		Model:        legacyModel,
		ThreadParams: legacyReasoning,
		TurnParams:   legacyReasoning,
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
	legacyModel := strings.TrimSpace(a.appServerModel)
	legacyReasoning := appServerReasoningParamsForModel(legacyModel, a.appServerSparkReasoningEffort)
	return appServerModelProfile{
		Alias:        alias,
		Model:        legacyModel,
		ThreadParams: legacyReasoning,
		TurnParams:   legacyReasoning,
	}
}

func (a *App) resetProjectChatAppSession(projectKey string) {
	key := strings.TrimSpace(projectKey)
	if key == "" {
		return
	}
	session, err := a.store.GetChatSessionByProjectKey(key)
	if err != nil {
		return
	}
	a.closeAppSession(session.ID)
	_ = a.store.UpdateChatSessionThread(session.ID, "")
}
