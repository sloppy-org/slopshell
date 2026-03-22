import fs from 'node:fs';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const { loadFlowsSync } = require('../tests/flows/flow-loader.cjs');
const { targetDefinitions } = require('../tests/flows/targets.cjs');

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const fixtureTargets = [
  {
    platform: 'ios',
    output: path.join(repoRoot, 'platforms', 'ios', 'Tests', 'TaburaFlowContractTests', 'Resources', 'flow-fixtures.json'),
  },
  {
    platform: 'ios',
    output: path.join(repoRoot, 'platforms', 'ios', 'TaburaIOSUITests', 'Resources', 'flow-fixtures.json'),
  },
  {
    platform: 'android',
    output: path.join(repoRoot, 'platforms', 'android', 'flow-contracts', 'src', 'test', 'resources', 'flow-fixtures.json'),
  },
  {
    platform: 'android',
    output: path.join(repoRoot, 'platforms', 'android', 'app', 'src', 'androidTest', 'assets', 'flow-fixtures.json'),
  },
];

function buildSelectors(platform) {
  const selectors = {};
  for (const [target, definition] of Object.entries(targetDefinitions)) {
    const selector = definition.platforms?.[platform];
    if (typeof selector === 'string' && selector.trim() !== '') {
      selectors[target] = selector;
    }
  }
  return selectors;
}

function buildFixture(platform) {
  return {
    platform,
    flows: loadFlowsSync(),
    selectors: buildSelectors(platform),
  };
}

function writeIfChanged(outputPath, content, checkOnly) {
  const current = fs.existsSync(outputPath) ? fs.readFileSync(outputPath, 'utf8') : null;
  if (current === content) {
    return;
  }
  if (checkOnly) {
    throw new Error(`${path.relative(repoRoot, outputPath)} is out of date; run node ./scripts/sync-native-flow-fixtures.mjs`);
  }
  fs.mkdirSync(path.dirname(outputPath), { recursive: true });
  fs.writeFileSync(outputPath, content);
}

function main() {
  const checkOnly = process.argv.includes('--check');
  for (const target of fixtureTargets) {
    const content = `${JSON.stringify(buildFixture(target.platform), null, 2)}\n`;
    writeIfChanged(target.output, content, checkOnly);
  }
  if (!checkOnly) {
    console.log('Updated native flow fixtures for ios and android.');
  }
}

main();
