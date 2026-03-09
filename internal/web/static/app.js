import { getState, setAppRefs } from './app-context.js';
import * as ttsModule from './app-tts.js';
import * as interactionModule from './app-interaction.js';
import * as runtimeUiModule from './app-runtime-ui.js';
import * as voiceModule from './app-voice.js';
import * as itemSidebarUtilsModule from './app-item-sidebar-utils.js';
import * as itemSidebarUiModule from './app-item-sidebar-ui.js';
import * as prReviewModule from './app-pr-review.js';
import * as chatUiModule from './app-chat-ui.js';
import * as inkModule from './app-ink.js';
import * as projectsModule from './app-projects.js';
import * as projectStateModule from './app-project-state.js';
import * as chatTransportModule from './app-chat-transport.js';
import * as chatSubmitModule from './app-chat-submit.js';
import * as canvasTransportModule from './app-canvas-transport.js';
import * as canvasUiModule from './app-canvas-ui.js';
import * as edgePanelsModule from './app-edge-panels.js';
import * as bugReportModule from './app-bug-report.js';
import * as annotationsModule from './app-annotations.js';
import * as initModule from './app-init.js';
import * as startupModule from './app-startup.js';

setAppRefs({
  ...ttsModule,
  ...interactionModule,
  ...runtimeUiModule,
  ...voiceModule,
  ...itemSidebarUtilsModule,
  ...itemSidebarUiModule,
  ...prReviewModule,
  ...chatUiModule,
  ...inkModule,
  ...projectsModule,
  ...projectStateModule,
  ...chatTransportModule,
  ...chatSubmitModule,
  ...canvasTransportModule,
  ...canvasUiModule,
  ...edgePanelsModule,
  ...bugReportModule,
  ...annotationsModule,
  ...initModule,
  ...startupModule,
});

runtimeUiModule.initRuntimeUi();
bugReportModule.initBugReportUi();
annotationsModule.initAnnotationUi();

window._taburaApp = {
  getState,
  acquireMicStream: voiceModule.acquireMicStream,
  sttStart: voiceModule.sttStart,
  sttSendBlob: voiceModule.sttSendBlob,
  sttStop: voiceModule.sttStop,
  sttCancel: voiceModule.sttCancel,
  refreshCompanionState: projectsModule.refreshCompanionState,
  syncCompanionIdleSurface: runtimeUiModule.syncCompanionIdleSurface,
};

startupModule.bootstrapApp();
