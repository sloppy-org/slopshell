const fs = require('node:fs');
const path = require('node:path');

const { parse } = require('yaml');
const {
  getTargetDefinition,
  getTargetPlatforms,
  requiredCoverageTargets,
  requiredIndicatorStates,
} = require('./targets.cjs');

const repoRoot = path.resolve(__dirname, '..', '..');
const flowRoot = path.resolve(__dirname);

const toolValues = new Set(['pointer', 'highlight', 'ink', 'text_note', 'prompt']);
const sessionValues = new Set(['none', 'dialogue', 'meeting']);
const indicatorValues = new Set(['idle', 'listening', 'paused', 'recording', 'working']);
const actionValues = new Set(['tap', 'tap_outside', 'verify', 'wait']);
const platformValues = new Set(['web', 'ios', 'android']);
const expectationKeys = new Set([
  'active_tool',
  'session',
  'silent',
  'tabura_circle',
  'dot_inner_icon',
  'body_class_contains',
  'indicator_state',
  'cursor_class',
]);

function fail(message) {
  throw new Error(message);
}

function isPlainObject(value) {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value);
}

function assertString(value, context) {
  if (typeof value !== 'string' || value.trim() === '') {
    fail(`${context} must be a non-empty string`);
  }
}

function assertEnum(value, allowed, context) {
  if (!allowed.has(value)) {
    fail(`${context} must be one of: ${Array.from(allowed).join(', ')}`);
  }
}

function assertBoolean(value, context) {
  if (typeof value !== 'boolean') {
    fail(`${context} must be a boolean`);
  }
}

function assertArrayOfStrings(values, context, allowed = null) {
  if (!Array.isArray(values) || values.length === 0) {
    fail(`${context} must be a non-empty string array`);
  }
  for (const [index, value] of values.entries()) {
    assertString(value, `${context}[${index}]`);
    if (allowed) {
      assertEnum(value, allowed, `${context}[${index}]`);
    }
  }
}

function validatePreconditions(preconditions, context) {
  if (preconditions == null) return;
  if (!isPlainObject(preconditions)) {
    fail(`${context} must be an object`);
  }
  for (const key of Object.keys(preconditions)) {
    if (!['tool', 'session', 'silent', 'indicator_state'].includes(key)) {
      fail(`${context}.${key} is not supported`);
    }
  }
  if ('tool' in preconditions) {
    assertEnum(preconditions.tool, toolValues, `${context}.tool`);
  }
  if ('session' in preconditions) {
    assertEnum(preconditions.session, sessionValues, `${context}.session`);
  }
  if ('silent' in preconditions) {
    assertBoolean(preconditions.silent, `${context}.silent`);
  }
  if ('indicator_state' in preconditions) {
    assertEnum(preconditions.indicator_state, indicatorValues, `${context}.indicator_state`);
  }
}

function validateObjectKeys(value, allowedKeys, context) {
  for (const key of Object.keys(value)) {
    if (!allowedKeys.has(key)) {
      fail(`${context}.${key} is not supported`);
    }
  }
}

function validateExpectations(expect, context) {
  if (!isPlainObject(expect) || Object.keys(expect).length === 0) {
    fail(`${context} must be a non-empty object`);
  }
  validateObjectKeys(expect, expectationKeys, context);
  for (const [key, value] of Object.entries(expect)) {
    switch (key) {
      case 'active_tool':
        assertEnum(value, toolValues, `${context}.${key}`);
        break;
      case 'session':
        assertEnum(value, sessionValues, `${context}.${key}`);
        break;
      case 'silent':
        assertBoolean(value, `${context}.${key}`);
        break;
      case 'tabura_circle':
        assertEnum(value, new Set(['expanded', 'collapsed']), `${context}.${key}`);
        break;
      case 'indicator_state':
        assertEnum(value, indicatorValues, `${context}.${key}`);
        break;
      case 'dot_inner_icon':
      case 'body_class_contains':
      case 'cursor_class':
        assertString(value, `${context}.${key}`);
        break;
      default:
        break;
    }
  }
}

function validateStep(step, context) {
  if (!isPlainObject(step)) {
    fail(`${context} must be an object`);
  }
  validateObjectKeys(step, new Set(['action', 'target', 'duration_ms', 'expect', 'platforms']), context);
  assertEnum(step.action, actionValues, `${context}.action`);
  if ('platforms' in step) {
    assertArrayOfStrings(step.platforms, `${context}.platforms`, platformValues);
  }
  if ('target' in step) {
    assertString(step.target, `${context}.target`);
    if (!getTargetDefinition(step.target)) {
      fail(`${context}.target must reference a known logical target`);
    }
    const targetPlatforms = getTargetPlatforms(step.target);
    if (targetPlatforms.length === 1 && !('platforms' in step)) {
      fail(`${context}.platforms must be declared for ${step.target}`);
    }
    if ('platforms' in step) {
      for (const platform of step.platforms) {
        if (!targetPlatforms.includes(platform)) {
          fail(`${context}.platforms includes ${platform}, but ${step.target} only supports ${targetPlatforms.join(', ')}`);
        }
      }
    }
  }
  if (step.action === 'tap') {
    assertString(step.target, `${context}.target`);
  }
  if (step.action !== 'tap' && 'target' in step && step.action !== 'verify') {
    fail(`${context}.target is only supported for tap and verify steps`);
  }
  if (step.action === 'wait') {
    if (!Number.isFinite(step.duration_ms) || step.duration_ms < 0) {
      fail(`${context}.duration_ms must be a non-negative number`);
    }
  }
  if (step.action !== 'wait' && 'duration_ms' in step) {
    fail(`${context}.duration_ms is only supported for wait steps`);
  }
  if ('expect' in step) {
    validateExpectations(step.expect, `${context}.expect`);
  }
}

function validateFlow(flow, relativePath) {
  if (!isPlainObject(flow)) {
    fail(`${relativePath} must contain a single flow object`);
  }
  validateObjectKeys(flow, new Set(['name', 'description', 'tags', 'preconditions', 'steps']), relativePath);
  assertString(flow.name, `${relativePath}.name`);
  assertString(flow.description, `${relativePath}.description`);
  assertArrayOfStrings(flow.tags, `${relativePath}.tags`);
  validatePreconditions(flow.preconditions, `${relativePath}.preconditions`);
  if (!Array.isArray(flow.steps) || flow.steps.length === 0) {
    fail(`${relativePath}.steps must be a non-empty array`);
  }
  flow.steps.forEach((step, index) => validateStep(step, `${relativePath}.steps[${index}]`));
}

function collectFlowFiles(rootDir) {
  const entries = fs.readdirSync(rootDir, { withFileTypes: true });
  const files = [];
  for (const entry of entries) {
    const fullPath = path.join(rootDir, entry.name);
    if (entry.isDirectory()) {
      files.push(...collectFlowFiles(fullPath));
      continue;
    }
    if (entry.isFile() && entry.name.endsWith('.yaml')) {
      files.push(fullPath);
    }
  }
  files.sort();
  return files;
}

function loadFlowsSync() {
  const files = collectFlowFiles(flowRoot);
  const seenNames = new Set();
  const flows = [];
  for (const filePath of files) {
    const text = fs.readFileSync(filePath, 'utf8');
    const parsed = parse(text);
    const relativePath = path.relative(repoRoot, filePath);
    validateFlow(parsed, relativePath);
    if (seenNames.has(parsed.name)) {
      fail(`duplicate flow name: ${parsed.name}`);
    }
    seenNames.add(parsed.name);
    flows.push({
      ...parsed,
      file: relativePath,
    });
  }
  return flows;
}

function comboKey(tool, session, silent) {
  return `${tool}|${session}|${silent ? 'silent' : 'audible'}`;
}

function comboLabel(tool, session, silent) {
  return `${tool} / ${session} / ${silent ? 'silent' : 'audible'}`;
}

function buildCoverage(flows) {
  const combosCovered = new Set();
  const targetsCovered = new Set();
  const indicatorStatesCovered = new Set();
  for (const flow of flows) {
    for (const step of flow.steps) {
      if (typeof step.target === 'string' && step.target.trim() !== '') {
        targetsCovered.add(step.target);
      }
      const expect = step.expect;
      if (expect && typeof expect.indicator_state === 'string') {
        indicatorStatesCovered.add(expect.indicator_state);
      }
      if (
        expect
        && typeof expect.active_tool === 'string'
        && typeof expect.session === 'string'
        && typeof expect.silent === 'boolean'
      ) {
        combosCovered.add(comboKey(expect.active_tool, expect.session, expect.silent));
      }
    }
  }

  const expectedCombos = [];
  for (const tool of toolValues) {
    for (const session of sessionValues) {
      for (const silent of [false, true]) {
        expectedCombos.push({
          key: comboKey(tool, session, silent),
          label: comboLabel(tool, session, silent),
        });
      }
    }
  }

  const missingCombos = expectedCombos.filter((entry) => !combosCovered.has(entry.key));
  const missingTargets = requiredCoverageTargets.filter((target) => !targetsCovered.has(target));
  const missingIndicatorStates = requiredIndicatorStates.filter((state) => !indicatorStatesCovered.has(state));

  return {
    flowCount: flows.length,
    targetsCovered: Array.from(targetsCovered).sort(),
    indicatorStatesCovered: Array.from(indicatorStatesCovered).sort(),
    comboCount: expectedCombos.length,
    combosCovered: Array.from(combosCovered).sort(),
    missingCombos,
    missingTargets,
    missingIndicatorStates,
  };
}

module.exports = {
  buildCoverage,
  loadFlowsSync,
};
