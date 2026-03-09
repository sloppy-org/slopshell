const fs = require('fs');
const path = require('path');
const { execFileSync } = require('child_process');

const BUG_LABELS = {
  bug: { color: 'd73a4a', description: "Something isn't working" },
  p0: { color: 'b60205', description: 'Highest priority' },
};

function nowStamp() {
  return new Date().toISOString().replace(/[:.]/g, '-');
}

function sanitizeSlug(value, max = 48) {
  const clean = String(value || '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, max);
  return clean || 'failure';
}

function truncate(value, max) {
  const clean = stripAnsi(String(value || '')).trim().replace(/\s+/g, ' ');
  if (clean.length <= max) return clean;
  return `${clean.slice(0, max - 3).replace(/[ .,;:!?-]+$/g, '')}...`;
}

function stripAnsi(value) {
  return String(value || '').replace(/\x1B\[[0-9;]*m/g, '');
}

function ensureDir(dir) {
  fs.mkdirSync(dir, { recursive: true });
}

function copyAttachment(attachment, artifactDir) {
  if (!attachment || !attachment.name) return null;
  const ext = path.extname(attachment.path || '') || (
    attachment.contentType === 'application/json' ? '.json' :
      attachment.contentType === 'text/plain' ? '.txt' :
        attachment.contentType === 'image/png' ? '.png' :
          attachment.contentType === 'application/zip' ? '.zip' :
            ''
  );
  const targetName = `${sanitizeSlug(attachment.name, 24)}${ext}`;
  const targetPath = path.join(artifactDir, targetName);
  if (attachment.path && fs.existsSync(attachment.path)) {
    fs.copyFileSync(attachment.path, targetPath);
    return targetPath;
  }
  if (attachment.body) {
    const buffer = Buffer.isBuffer(attachment.body) ? attachment.body : Buffer.from(attachment.body, 'base64');
    fs.writeFileSync(targetPath, buffer);
    return targetPath;
  }
  return null;
}

function readTextIfPresent(filePath) {
  if (!filePath || !fs.existsSync(filePath)) return '';
  try {
    return fs.readFileSync(filePath, 'utf8').trim();
  } catch {
    return '';
  }
}

function findAnnotation(annotations, type) {
  const hit = (annotations || []).find((annotation) => annotation && annotation.type === type && annotation.description);
  return hit ? String(hit.description).trim() : '';
}

function buildExpectedBehavior(failure) {
  return (
    findAnnotation(failure.annotations, 'playtest-expected')
    || `The live playtest "${failure.title}" should pass without errors.`
  );
}

function buildStepsToReproduce(failure) {
  const steps = findAnnotation(failure.annotations, 'playtest-steps')
    .split('||')
    .map((step) => step.trim())
    .filter(Boolean);
  if (steps.length > 0) return steps;
  return [
    `./scripts/playtest.sh --grep "${failure.title.replace(/"/g, '\\"')}"`,
    `Open ${failure.baseURL || 'http://127.0.0.1:8420'} and follow ${failure.file}:${failure.line}.`,
  ];
}

function buildConsoleSection(browserLogs) {
  if (!browserLogs) return 'None captured.';
  const lines = browserLogs
    .split('\n')
    .map((line) => stripAnsi(line).trim())
    .filter(Boolean)
    .slice(0, 40);
  if (lines.length === 0) return 'None captured.';
  return lines.join('\n');
}

function runGitHub(args, cwd) {
  return execFileSync('gh', args, {
    cwd,
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'pipe'],
  }).trim();
}

function ensureGitHubLabels(cwd) {
  const raw = runGitHub(['label', 'list', '--json', 'name', '--limit', '200'], cwd);
  const existing = new Set(JSON.parse(raw).map((label) => String(label.name || '').toLowerCase()));
  for (const [name, spec] of Object.entries(BUG_LABELS)) {
    if (existing.has(name)) continue;
    runGitHub(['label', 'create', name, '--color', spec.color, '--description', spec.description], cwd);
  }
}

function uploadScreenshot(filePath, cwd) {
  if (!filePath || !fs.existsSync(filePath)) return '';
  return execFileSync(
    'curl',
    ['-fsS', '-F', 'reqtype=fileupload', '-F', `fileToUpload=@${filePath}`, 'https://catbox.moe/user/api.php'],
    {
      cwd,
      encoding: 'utf8',
      stdio: ['ignore', 'pipe', 'pipe'],
    },
  ).trim();
}

function createIssue(cwd, title, body) {
  const bodyFile = path.join(cwd, '.tabura', 'artifacts', 'playtests', `${nowStamp()}-${sanitizeSlug(title, 32)}.txt`);
  ensureDir(path.dirname(bodyFile));
  fs.writeFileSync(bodyFile, `${body.trim()}\n`, 'utf8');
  const url = runGitHub(
    ['issue', 'create', '--label', 'bug', '--label', 'p0', '--title', title, '--body-file', bodyFile],
    cwd,
  );
  return { url, bodyFile };
}

function buildIssueBody(failure, screenshotURL, artifactDir) {
  const browserLogs = readTextIfPresent(failure.browserLogPath);
  const steps = buildStepsToReproduce(failure);
  const tested = findAnnotation(failure.annotations, 'playtest-tested')
    || `${failure.projectName} :: ${failure.file}:${failure.line}`;
  const actual = truncate(failure.errorText, 4000);
  const evidenceLines = [
    screenshotURL ? `- Screenshot link: ${screenshotURL}` : '- Screenshot link: upload failed',
    `- Local artifact dir: \`${path.relative(failure.cwd, artifactDir)}\``,
  ];
  if (failure.pageStatePath) {
    evidenceLines.push(`- Page state: \`${path.relative(failure.cwd, failure.pageStatePath)}\``);
  }
  if (failure.browserLogPath) {
    evidenceLines.push(`- Browser logs: \`${path.relative(failure.cwd, failure.browserLogPath)}\``);
  }
  return [
    '## What was tested',
    '',
    tested,
    '',
    '## Expected behavior',
    '',
    buildExpectedBehavior(failure),
    '',
    '## Actual behavior',
    '',
    actual || 'Playwright reported an unexpected failure.',
    '',
    '## Console errors',
    '',
    '```text',
    buildConsoleSection(browserLogs),
    '```',
    '',
    '## Steps to reproduce',
    '',
    ...steps.map((step, index) => `${index + 1}. ${step}`),
    '',
    '## Evidence',
    '',
    ...evidenceLines,
  ].join('\n');
}

class GitHubPlaytestReporter {
  constructor() {
    this.cwd = process.cwd();
    this.failures = [];
    this.issueResults = [];
  }

  onBegin(config) {
    this.config = config;
    this.baseURL = config.projects[0] && config.projects[0].use ? config.projects[0].use.baseURL : '';
  }

  onTestEnd(test, result) {
    if (!result) return;
    if (result.status === test.expectedStatus) return;
    if (result.status === 'skipped') return;
    const titlePath = typeof test.titlePath === 'function' ? test.titlePath() : [test.title];
    const errorText = [...new Set(
      [result.error && result.error.message, ...(result.errors || []).map((error) => error && error.message)]
        .filter(Boolean)
        .map((value) => stripAnsi(value)),
    )].join('\n\n');
    this.failures.push({
      title: test.title,
      titlePath,
      file: test.location.file,
      line: test.location.line,
      column: test.location.column,
      projectName: result.projectName || 'chromium',
      annotations: (test.annotations || []).map((annotation) => ({
        type: annotation.type,
        description: annotation.description,
      })),
      attachments: (result.attachments || []).map((attachment) => ({
        name: attachment.name,
        path: attachment.path,
        contentType: attachment.contentType,
        body: attachment.body,
      })),
      errorText,
      cwd: this.cwd,
      baseURL: this.baseURL,
    });
  }

  async onEnd() {
    const summaryDir = path.join(this.cwd, '.tabura', 'artifacts', 'playtests');
    ensureDir(summaryDir);
    const summaryPath = path.join(summaryDir, 'latest-summary.txt');

    if (this.failures.length === 0) {
      fs.writeFileSync(summaryPath, 'playtest: no failures\n', 'utf8');
      console.log(`[playtest] no failures; summary: ${path.relative(this.cwd, summaryPath)}`);
      return;
    }

    const fileIssues = process.env.PLAYTEST_FILE_ISSUES !== '0';
    if (fileIssues) {
      ensureGitHubLabels(this.cwd);
    }

    const summaryLines = [];
    for (const failure of this.failures) {
      const artifactDir = path.join(
        this.cwd,
        '.tabura',
        'artifacts',
        'bugs',
        `${nowStamp()}-${sanitizeSlug(`${failure.projectName}-${failure.title}`, 36)}`,
      );
      ensureDir(artifactDir);

      let screenshotPath = '';
      let pageStatePath = '';
      let browserLogPath = '';
      for (const attachment of failure.attachments) {
        const copied = copyAttachment(attachment, artifactDir);
        if (!copied) continue;
        if (attachment.name === 'playtest-screenshot' || attachment.contentType === 'image/png') {
          screenshotPath = copied;
        } else if (attachment.name === 'page-state') {
          pageStatePath = copied;
        } else if (attachment.name === 'browser-logs') {
          browserLogPath = copied;
        }
      }
      failure.pageStatePath = pageStatePath;
      failure.browserLogPath = browserLogPath;

      let issueURL = '';
      if (fileIssues) {
        const screenshotURL = uploadScreenshot(screenshotPath, this.cwd);
        const title = truncate(`playtest: ${failure.title}`, 96);
        const body = buildIssueBody(failure, screenshotURL, artifactDir);
        issueURL = createIssue(this.cwd, title, body).url;
      }

      summaryLines.push(
        `${failure.file}:${failure.line} ${failure.title}`,
        `artifact_dir=${path.relative(this.cwd, artifactDir)}`,
        issueURL ? `issue=${issueURL}` : 'issue=not-filed',
        '',
      );
      if (issueURL) {
        console.log(`[playtest] filed ${issueURL} for ${failure.title}`);
      } else {
        console.log(`[playtest] captured local artifacts for ${failure.title}`);
      }
    }

    fs.writeFileSync(summaryPath, `${summaryLines.join('\n')}\n`, 'utf8');
    console.log(`[playtest] summary: ${path.relative(this.cwd, summaryPath)}`);
  }
}

module.exports = GitHubPlaytestReporter;
