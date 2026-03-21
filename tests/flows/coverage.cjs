const { buildCoverage, loadFlowsSync } = require('./flow-loader.cjs');

const flows = loadFlowsSync();
const coverage = buildCoverage(flows);

console.log(`Flows: ${coverage.flowCount}`);
console.log(`Mode combinations covered: ${coverage.combosCovered.length}/${coverage.comboCount}`);
console.log(`Targets covered: ${coverage.targetsCovered.join(', ')}`);
console.log(`Indicator states covered: ${coverage.indicatorStatesCovered.join(', ')}`);

if (coverage.missingCombos.length > 0) {
  console.error('Missing mode combinations:');
  for (const combo of coverage.missingCombos) {
    console.error(`- ${combo.label}`);
  }
  process.exitCode = 1;
}

if (coverage.missingTargets.length > 0) {
  console.error('Missing required targets:');
  for (const target of coverage.missingTargets) {
    console.error(`- ${target}`);
  }
  process.exitCode = 1;
}

if (coverage.missingIndicatorStates.length > 0) {
  console.error('Missing indicator states:');
  for (const state of coverage.missingIndicatorStates) {
    console.error(`- ${state}`);
  }
  process.exitCode = 1;
}
