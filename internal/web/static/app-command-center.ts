import { refs, state } from './app-context.js';
import { executeCurrentCanonicalAction, currentCanonicalActionContext, currentCanonicalActions } from './app-canonical-actions.js';
import { canonicalActionLabel } from './artifact-taxonomy.js';

const openItemSidebarView = (...args) => refs.openItemSidebarView(...args);
const launchNewMailAuthoring = (...args) => refs.launchNewMailAuthoring(...args);
const launchReplyAuthoring = (...args) => refs.launchReplyAuthoring(...args);
const launchReplyAllAuthoring = (...args) => refs.launchReplyAllAuthoring(...args);
const launchForwardAuthoring = (...args) => refs.launchForwardAuthoring(...args);
const selectInteractionTool = (...args) => refs.selectInteractionTool(...args);
const openInboxMailTriage = (...args) => refs.openInboxMailTriage(...args);
const openJunkMailTriage = (...args) => refs.openJunkMailTriage(...args);
const openManagementPage = (...args) => refs.openManagementPage(...args);

const COMMAND_CENTER_ID = 'command-center';
const COMMAND_CENTER_INPUT_ID = 'command-center-input';
const COMMAND_CENTER_LIST_ID = 'command-center-list';
const COMMAND_CENTER_HINT_ID = 'command-center-hint';
const COMMAND_CENTER_COMMANDS: Array<{ id: string; title: string; detail: string; shortcut: string; keywords: string; run: () => any; disabled?: boolean }> = [
  {
    id: 'view-inbox',
    title: 'Open Inbox',
    detail: 'Show inbox items in the left sidebar.',
    shortcut: 'Inbox',
    keywords: 'inbox mail tasks items',
    run: () => openItemSidebarView('inbox'),
  },
  {
    id: 'view-waiting',
    title: 'Open Waiting',
    detail: 'Show waiting items in the left sidebar.',
    shortcut: 'Waiting',
    keywords: 'waiting follow up items',
    run: () => openItemSidebarView('waiting'),
  },
  {
    id: 'view-someday',
    title: 'Open Someday',
    detail: 'Show someday items in the left sidebar.',
    shortcut: 'Someday',
    keywords: 'someday backlog items',
    run: () => openItemSidebarView('someday'),
  },
  {
    id: 'view-done',
    title: 'Open Done',
    detail: 'Show completed items in the left sidebar.',
    shortcut: 'Done',
    keywords: 'done completed archive items',
    run: () => openItemSidebarView('done'),
  },
  {
    id: 'compose-mail',
    title: 'Compose New Mail',
    detail: 'Create a new email draft.',
    shortcut: 'C',
    keywords: 'compose new mail email spark draft',
    run: () => launchNewMailAuthoring(),
  },
  {
    id: 'inbox-triage',
    title: 'Inbox Triage',
    detail: 'Review the inbox triage queue.',
    shortcut: 'Triage',
    keywords: 'inbox triage mail review',
    run: () => openInboxMailTriage(),
  },
  {
    id: 'junk-audit',
    title: 'Junk Audit',
    detail: 'Review the junk mail audit queue.',
    shortcut: 'Junk',
    keywords: 'junk audit spam triage review',
    run: () => openJunkMailTriage(),
  },
  {
    id: 'manage-runtime',
    title: 'Open Manage',
    detail: 'Open the management UI in a dedicated page.',
    shortcut: 'Manage',
    keywords: 'manage settings hotword models voices admin',
    run: () => openManagementPage('manage'),
  },
  {
    id: 'tool-pointer',
    title: 'Switch To Pointer Tool',
    detail: 'Set the interaction tool to pointer.',
    shortcut: 'P',
    keywords: 'pointer tool annotate',
    run: () => selectInteractionTool('pointer'),
  },
  {
    id: 'tool-highlight',
    title: 'Switch To Highlight Tool',
    detail: 'Set the interaction tool to highlight.',
    shortcut: 'H',
    keywords: 'highlight tool annotate',
    run: () => selectInteractionTool('highlight'),
  },
  {
    id: 'tool-ink',
    title: 'Switch To Ink Tool',
    detail: 'Set the interaction tool to ink.',
    shortcut: 'I',
    keywords: 'ink pen tool annotate',
    run: () => selectInteractionTool('ink'),
  },
  {
    id: 'tool-text-note',
    title: 'Switch To Text Note Tool',
    detail: 'Set the interaction tool to text note.',
    shortcut: 'T',
    keywords: 'text note tool annotate',
    run: () => selectInteractionTool('text_note'),
  },
  {
    id: 'tool-prompt',
    title: 'Switch To Prompt Tool',
    detail: 'Set the interaction tool to prompt.',
    shortcut: 'Q',
    keywords: 'prompt tool annotate dictation',
    run: () => selectInteractionTool('prompt'),
  },
];

const commandCenterState = {
  query: '',
  commands: [],
  selectedIndex: 0,
  canonicalActions: [],
  canonicalOnly: false,
  hint: '',
};

function commandCenterRoot() {
  return document.getElementById(COMMAND_CENTER_ID);
}

export function commandCenterPanel() {
  return commandCenterRoot()?.querySelector('.command-center__panel') || null;
}

function commandCenterInput() {
  return document.getElementById(COMMAND_CENTER_INPUT_ID);
}

function commandCenterList() {
  return document.getElementById(COMMAND_CENTER_LIST_ID);
}

function commandCenterHint() {
  return document.getElementById(COMMAND_CENTER_HINT_ID);
}

function isEmailSidebarItem(item) {
  const artifactKind = String(item?.artifact_kind || '').trim().toLowerCase();
  return artifactKind === 'email' || artifactKind === 'email_thread';
}

function activeSidebarItem() {
  const items = Array.isArray(state.itemSidebarItems) ? state.itemSidebarItems : [];
  if (items.length === 0) return null;
  const activeID = Number(state.itemSidebarActiveItemID || 0);
  return items.find((item) => Number(item?.id || 0) === activeID) || items[0] || null;
}

export function activeReplySidebarItem() {
  const item = activeSidebarItem();
  if (!item || !isEmailSidebarItem(item)) return null;
  return item;
}

export function isCommandCenterVisible() {
  const root = commandCenterRoot();
  return root instanceof HTMLElement && !root.hidden;
}

export function hideCommandCenter() {
  const root = commandCenterRoot();
  if (!(root instanceof HTMLElement)) return;
  root.hidden = true;
  document.body.classList.remove('command-center-open');
  commandCenterState.canonicalActions = [];
  commandCenterState.canonicalOnly = false;
  commandCenterState.hint = '';
}

function commandMatchesQuery(command, query) {
  if (!query) return true;
  const haystack = [
    command.title,
    command.detail,
    command.keywords,
    command.shortcut,
  ].join(' ').toLowerCase();
  return query
    .split(/\s+/)
    .filter(Boolean)
    .every((token) => haystack.includes(token));
}

function availableCommandCenterCommands() {
  const commands = COMMAND_CENTER_COMMANDS.map((command) => ({ ...command }));
  const currentContext = currentCanonicalActionContext();
  const currentActions = currentCanonicalActions().filter((action) => {
    if (commandCenterState.canonicalActions.length === 0) return true;
    return commandCenterState.canonicalActions.includes(action);
  });
  const canonicalCommands = currentActions.map((action) => {
    const label = canonicalActionLabel(action) || action;
    const title = String(currentContext?.title || '').trim();
    return {
      id: `canonical-${action}`,
      title: `${label} Current Artifact`,
      detail: title
        ? `${label} ${title}.`
        : `${label} the current artifact or item.`,
      shortcut: 'Current',
      keywords: `current artifact item canonical ${action} ${label.toLowerCase()} ${title.toLowerCase()}`,
      run: () => executeCurrentCanonicalAction(action),
    };
  });
  if (commandCenterState.canonicalOnly) {
    return canonicalCommands;
  }
  commands.unshift(...canonicalCommands);
  const replyItem = activeReplySidebarItem();
  const composeIndex = commands.findIndex((command) => command.id === 'compose-mail');
  const replyInsertAt = composeIndex >= 0 ? composeIndex + 1 : commands.length;
  commands.splice(replyInsertAt, 0, {
    id: 'reply-mail',
    title: 'Reply To Selected Email',
    detail: replyItem
      ? `Reply to ${String(replyItem?.title || replyItem?.artifact_title || 'selected email').trim() || 'selected email'}.`
      : 'Select an email item in the sidebar to reply.',
    shortcut: 'R',
    keywords: 'reply email spark selected draft',
    disabled: !replyItem,
    run: () => (replyItem ? launchReplyAuthoring(replyItem) : false),
  });
  const replyAllItem = activeReplySidebarItem();
  commands.splice(replyInsertAt + 1, 0, {
    id: 'reply-all-mail',
    title: 'Reply All To Selected Email',
    detail: replyAllItem
      ? `Reply all to ${String(replyAllItem?.title || replyAllItem?.artifact_title || 'selected email').trim() || 'selected email'}.`
      : 'Select an email item in the sidebar to reply all.',
    shortcut: 'A',
    keywords: 'reply all email everyone selected draft',
    disabled: !replyAllItem,
    run: () => (replyAllItem ? launchReplyAllAuthoring(replyAllItem) : false),
  });
  const forwardItem = activeReplySidebarItem();
  commands.splice(replyInsertAt + 2, 0, {
    id: 'forward-mail',
    title: 'Forward Selected Email',
    detail: forwardItem
      ? `Forward ${String(forwardItem?.title || forwardItem?.artifact_title || 'selected email').trim() || 'selected email'}.`
      : 'Select an email item in the sidebar to forward.',
    shortcut: 'F',
    keywords: 'forward email spark selected draft',
    disabled: !forwardItem,
    run: () => (forwardItem ? launchForwardAuthoring(forwardItem) : false),
  });
  return commands;
}

function updateCommandCenterCopy() {
  const hint = commandCenterHint();
  if (!(hint instanceof HTMLElement)) return;
  hint.textContent = commandCenterState.hint || 'Search commands, mail actions, and tool switches.';
}

function renderCommandCenter() {
  const list = commandCenterList();
  if (!(list instanceof HTMLElement)) return;
  const query = String(commandCenterState.query || '').trim().toLowerCase();
  const commands = availableCommandCenterCommands().filter((command) => commandMatchesQuery(command, query));
  commandCenterState.commands = commands;
  commandCenterState.selectedIndex = Math.max(0, Math.min(commandCenterState.selectedIndex, Math.max(commands.length - 1, 0)));
  list.replaceChildren();
  if (commands.length === 0) {
    const empty = document.createElement('div');
    empty.className = 'command-center__empty';
    empty.textContent = 'No commands match.';
    list.appendChild(empty);
    return;
  }
  commands.forEach((command, index) => {
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'command-center__item';
    button.dataset.commandId = command.id;
    if (index === commandCenterState.selectedIndex) {
      button.classList.add('is-selected');
    }
    button.disabled = Boolean(command.disabled);

    const text = document.createElement('span');
    text.className = 'command-center__item-copy';
    const title = document.createElement('strong');
    title.textContent = command.title;
    const detail = document.createElement('span');
    detail.className = 'command-center__item-detail';
    detail.textContent = command.detail;
    text.append(title, detail);

    const shortcut = document.createElement('span');
    shortcut.className = 'command-center__shortcut';
    shortcut.textContent = command.shortcut;
    button.append(text, shortcut);
    button.addEventListener('mouseenter', () => {
      commandCenterState.selectedIndex = index;
      renderCommandCenter();
    });
    button.addEventListener('click', () => {
      commandCenterState.selectedIndex = index;
      void executeSelectedCommand();
    });
    list.appendChild(button);
  });
  const active = list.querySelector('.command-center__item.is-selected');
  if (active instanceof HTMLElement) {
    active.scrollIntoView({ block: 'nearest' });
  }
}

async function executeSelectedCommand() {
  const command = commandCenterState.commands[commandCenterState.selectedIndex] || null;
  if (!command || command.disabled) return false;
  hideCommandCenter();
  await Promise.resolve(command.run());
  return true;
}

export function ensureCommandCenter() {
  const existing = commandCenterRoot();
  if (existing instanceof HTMLElement) return existing;
  const root = document.createElement('div');
  root.id = COMMAND_CENTER_ID;
  root.className = 'command-center';
  root.hidden = true;

  const panel = document.createElement('div');
  panel.className = 'command-center__panel';
  panel.setAttribute('role', 'dialog');
  panel.setAttribute('aria-modal', 'true');
  panel.setAttribute('aria-labelledby', 'command-center-title');

  const header = document.createElement('div');
  header.className = 'command-center__header';

  const titleGroup = document.createElement('div');
  const title = document.createElement('h2');
  title.id = 'command-center-title';
  title.textContent = 'Command Center';
  const hint = document.createElement('p');
  hint.id = COMMAND_CENTER_HINT_ID;
  hint.textContent = 'Search commands, mail actions, and tool switches.';
  titleGroup.append(title, hint);

  const close = document.createElement('button');
  close.type = 'button';
  close.className = 'edge-btn command-center__close';
  close.textContent = 'Close';
  close.addEventListener('click', () => hideCommandCenter());
  header.append(titleGroup, close);

  const input = document.createElement('input');
  input.id = COMMAND_CENTER_INPUT_ID;
  input.className = 'command-center__input';
  input.type = 'text';
  input.autocomplete = 'off';
  input.placeholder = 'Type to filter commands';
  input.setAttribute('aria-label', 'Filter commands');
  input.addEventListener('input', () => {
    commandCenterState.query = String(input.value || '');
    commandCenterState.selectedIndex = 0;
    renderCommandCenter();
  });

  const list = document.createElement('div');
  list.id = COMMAND_CENTER_LIST_ID;
  list.className = 'command-center__list';

  panel.append(header, input, list);
  root.appendChild(panel);
  root.addEventListener('mousedown', (event) => {
    if (event.target === root) {
      hideCommandCenter();
    }
  });
  document.body.appendChild(root);
  return root;
}

function openCommandCenter(deps = null, options: Record<string, any> = {}) {
  const canonicalActions = Array.isArray(options?.canonicalActions)
    ? options.canonicalActions.map((value) => String(value || '').trim()).filter(Boolean)
    : [];
  commandCenterState.canonicalActions = canonicalActions;
  commandCenterState.canonicalOnly = options?.canonicalOnly === true;
  commandCenterState.hint = String(options?.hint || '').trim();
  (deps?.hideTextInput || refs.hideTextInput)?.();
  (deps?.hideOverlay || refs.hideOverlay)?.();
  (deps?.cancelLiveSessionListen || refs.cancelLiveSessionListen)?.();
  const root = ensureCommandCenter();
  root.hidden = false;
  document.body.classList.add('command-center-open');
  commandCenterState.query = '';
  commandCenterState.selectedIndex = 0;
  updateCommandCenterCopy();
  const input = commandCenterInput();
  if (input instanceof HTMLInputElement) {
    input.value = '';
    input.focus();
    input.select();
  }
  renderCommandCenter();
}

export function openCanonicalActionCommandCenter(actions: string[] = [], options: Record<string, any> = {}) {
  openCommandCenter(null, {
    canonicalActions: actions,
    canonicalOnly: options?.canonicalOnly !== false,
    hint: String(options?.hint || 'Choose a current-artifact action.').trim(),
  });
}

function moveCommandCenterSelection(delta) {
  if (commandCenterState.commands.length === 0) return;
  const count = commandCenterState.commands.length;
  commandCenterState.selectedIndex = (commandCenterState.selectedIndex + delta + count) % count;
  renderCommandCenter();
}

export function handleCommandCenterShortcut(ev, deps) {
  const key = String(ev.key || '').toLowerCase();
  if ((ev.metaKey || ev.ctrlKey) && !ev.altKey && key === 'k') {
    ev.preventDefault();
    ev.stopPropagation();
    if (isCommandCenterVisible()) {
      hideCommandCenter();
    } else {
      openCommandCenter(deps);
    }
    return true;
  }
  if (!isCommandCenterVisible()) return false;
  if (ev.key === 'Escape' && !ev.metaKey && !ev.ctrlKey && !ev.altKey) {
    ev.preventDefault();
    hideCommandCenter();
    return true;
  }
  if (ev.key === 'ArrowDown') {
    ev.preventDefault();
    moveCommandCenterSelection(1);
    return true;
  }
  if (ev.key === 'ArrowUp') {
    ev.preventDefault();
    moveCommandCenterSelection(-1);
    return true;
  }
  if (ev.key === 'Enter') {
    ev.preventDefault();
    void executeSelectedCommand();
    return true;
  }
  return false;
}
