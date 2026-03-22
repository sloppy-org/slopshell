import fs from 'node:fs';
import path from 'node:path';
import YAML from 'yaml';

const workflowPath = path.join(process.cwd(), '.github', 'workflows', 'test-reports.yml');
const workflow = YAML.parse(fs.readFileSync(workflowPath, 'utf8'));
const errors = [];

if (workflow?.name !== 'Test Reports') {
  errors.push(`expected workflow name "Test Reports", got ${JSON.stringify(workflow?.name)}`);
}

const pullRequest = workflow?.on?.pull_request;
const pushBranches = workflow?.on?.push?.branches;
if (pullRequest === undefined) {
  errors.push('expected pull_request trigger');
}
if (!Array.isArray(pushBranches) || !pushBranches.includes('main')) {
  errors.push('expected push trigger for main');
}

const steps = workflow?.jobs?.reports?.steps;
if (!Array.isArray(steps)) {
  errors.push('expected jobs.reports.steps array');
}

function findStep(name) {
  return steps.find((step) => step?.name === name);
}

const reportStep = findStep('Generate coverage + E2E reports');
const uploadStep = findStep('Upload test reports');
const expectedGate = "${{ github.event_name == 'push' && github.ref == 'refs/heads/main' }}";
const expectedUploadGate = "${{ github.event_name == 'push' && github.ref == 'refs/heads/main' && always() }}";

if (reportStep?.if !== expectedGate) {
  errors.push(`expected report step gate ${expectedGate}, got ${JSON.stringify(reportStep?.if)}`);
}
if (uploadStep?.if !== expectedUploadGate) {
  errors.push(`expected upload step gate ${expectedUploadGate}, got ${JSON.stringify(uploadStep?.if)}`);
}

if (errors.length > 0) {
  for (const err of errors) {
    console.error(`[workflow-check] ${err}`);
  }
  process.exit(1);
}

console.log('[workflow-check] Test Reports gating is limited to push events on main.');
