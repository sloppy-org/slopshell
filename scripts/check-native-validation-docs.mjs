import fs from 'node:fs';
import path from 'node:path';

const repoRoot = process.cwd();
const packageJSON = JSON.parse(fs.readFileSync(path.join(repoRoot, 'package.json'), 'utf8'));
const nativeDocs = fs.readFileSync(path.join(repoRoot, 'docs', 'native-clients.md'), 'utf8');
const flowDocs = fs.readFileSync(path.join(repoRoot, 'tests', 'flows', 'README.md'), 'utf8');
const workflow = fs.readFileSync(path.join(repoRoot, '.github', 'workflows', 'test-reports.yml'), 'utf8');

const requiredScripts = [
  'test:flows:ios',
  'test:flows:ios:contract',
  'test:flows:android',
  'test:flows:android:contract',
  'test:flows:native',
  'test:native-docs',
];

const errors = [];

for (const scriptName of requiredScripts) {
  if (typeof packageJSON.scripts?.[scriptName] !== 'string' || packageJSON.scripts[scriptName].trim() === '') {
    errors.push(`missing package.json script ${scriptName}`);
  }
}

const nativeDocNeedles = [
  './scripts/test-native-flows.sh',
  'npm run test:flows:native',
  'npm run test:flows:ios:contract',
  'npm run test:flows:android:contract',
  'faepmac1',
];

for (const needle of nativeDocNeedles) {
  if (!nativeDocs.includes(needle)) {
    errors.push(`docs/native-clients.md must mention ${needle}`);
  }
}

const flowDocNeedles = [
  'npm run test:flows:ios:contract',
  'npm run test:flows:android:contract',
  'npm run test:flows:ios',
  'npm run test:flows:android',
  'npm run test:flows:native',
  'platforms/ios/TaburaIOSUITests/Resources/flow-fixtures.json',
  'platforms/android/app/src/androidTest/assets/flow-fixtures.json',
];

for (const needle of flowDocNeedles) {
  if (!flowDocs.includes(needle)) {
    errors.push(`tests/flows/README.md must mention ${needle}`);
  }
}

if (!workflow.includes('npm run test:native-docs')) {
  errors.push('workflow must run npm run test:native-docs');
}

if (errors.length > 0) {
  for (const error of errors) {
    console.error(`[native-docs-check] ${error}`);
  }
  process.exit(1);
}

console.log('[native-docs-check] Native validation docs and scripts are aligned.');
