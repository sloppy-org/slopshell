import * as context from './app-context.js';

const { refs, state, ACTIVE_PROJECT_STORAGE_KEY, LAST_VIEW_STORAGE_KEY } = context;

const activeProject = (...args) => refs.activeProject(...args);
const normalizeProjectChatModelAlias = (...args) => refs.normalizeProjectChatModelAlias(...args);
const normalizeProjectChatModelReasoningEffort = (...args) => refs.normalizeProjectChatModelReasoningEffort(...args);
const renderEdgeTopProjects = (...args) => refs.renderEdgeTopProjects(...args);
const renderEdgeTopModelButtons = (...args) => refs.renderEdgeTopModelButtons(...args);
const setFileSidebarAvailability = (...args) => refs.setFileSidebarAvailability(...args);

export function activeProjectChatModelAlias() {
  const alias = normalizeProjectChatModelAlias(activeProject()?.chat_model);
  return alias || 'spark';
}

export function activeProjectChatModelReasoningEffort() {
  const alias = activeProjectChatModelAlias();
  return normalizeProjectChatModelReasoningEffort(activeProject()?.chat_model_reasoning_effort, alias);
}

export function persistActiveProjectID(projectID) {
  if (!projectID) return;
  try {
    window.localStorage.setItem(ACTIVE_PROJECT_STORAGE_KEY, projectID);
  } catch (_) {}
}

export function readPersistedProjectID() {
  try {
    return String(window.localStorage.getItem(ACTIVE_PROJECT_STORAGE_KEY) || '').trim();
  } catch (_) {
    return '';
  }
}

export function persistLastView(view) {
  try {
    window.localStorage.setItem(LAST_VIEW_STORAGE_KEY, JSON.stringify(view));
  } catch (_) {}
}

export function readPersistedLastView() {
  try {
    return JSON.parse(window.localStorage.getItem(LAST_VIEW_STORAGE_KEY) || 'null');
  } catch (_) {
    return null;
  }
}

export function setActiveProjectID(projectID) {
  state.activeProjectId = String(projectID || '').trim();
  if (state.activeProjectId) {
    persistActiveProjectID(state.activeProjectId);
  }
  setFileSidebarAvailability();
  renderEdgeTopProjects();
  renderEdgeTopModelButtons();
}
