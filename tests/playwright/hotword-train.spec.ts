import { expect, test } from '@playwright/test';

const trainedModelFile = 'sloppy-2026-03-23_21-03-09Z.onnx';

function wavBuffer() {
  const samples = new Int16Array([0, 1200, -1200, 600, -600, 0]);
  const dataSize = samples.length * 2;
  const buffer = new ArrayBuffer(44 + dataSize);
  const view = new DataView(buffer);
  let offset = 0;
  const writeString = (value: string) => {
    for (let i = 0; i < value.length; i += 1) {
      view.setUint8(offset, value.charCodeAt(i));
      offset += 1;
    }
  };
  writeString('RIFF');
  view.setUint32(offset, 36 + dataSize, true); offset += 4;
  writeString('WAVE');
  writeString('fmt ');
  view.setUint32(offset, 16, true); offset += 4;
  view.setUint16(offset, 1, true); offset += 2;
  view.setUint16(offset, 1, true); offset += 2;
  view.setUint32(offset, 16000, true); offset += 4;
  view.setUint32(offset, 32000, true); offset += 4;
  view.setUint16(offset, 2, true); offset += 2;
  view.setUint16(offset, 16, true); offset += 2;
  writeString('data');
  view.setUint32(offset, dataSize, true); offset += 4;
  samples.forEach((sample) => {
    view.setInt16(offset, sample, true);
    offset += 2;
  });
  return Buffer.from(buffer);
}

test('hotword training page saves guided config, captures retry feedback, and runs the guided pipeline', async ({ page }) => {
  await page.goto('/tests/playwright/hotword-train-harness.html');

  await expect(page.locator('#train-banner')).toContainText('Wake word assets are not fully deployed yet');
  await page.setInputFiles('#recording-upload', {
    name: 'sample.wav',
    mimeType: 'audio/wav',
    buffer: wavBuffer(),
  });

  await expect(page.locator('#recording-list')).toContainText('.wav');
  await page.locator('summary').filter({ hasText: 'Advanced' }).click();
  await page.fill('#trainer-sample-count', '2000');
  await page.fill('#generator-command-qwen3tts', '/opt/qwen3tts/bin/qwen3tts-hotword');
  await page.fill('#trainer-negative-phrases', 'copy\nhappy\nsoppy');
  await page.locator('#config-save').click();
  await expect(page.locator('#pipeline-status')).toContainText('Trainer settings saved.');

  await page.setInputFiles('#testing-upload', {
    name: 'retry.wav',
    mimeType: 'audio/wav',
    buffer: wavBuffer(),
  });
  await expect(page.locator('#testing-list')).toContainText('This should have triggered');
  await page.getByRole('button', { name: 'This should have triggered' }).click();
  await expect(page.locator('#feedback-status')).toContainText('1 missed-trigger clip');

  await page.locator('#pipeline-start').click();
  await expect(page.locator('#pipeline-progress-label')).toContainText('100%');
  await expect(page.locator('#pipeline-status')).toContainText(`Training complete and deployed ${trainedModelFile}.`);
  await expect(page.locator('#model-list')).toContainText(trainedModelFile);

  const requests = await page.evaluate(() => (window as any).__hotwordTrainRequests);
  expect(requests).toEqual({
    uploads: 2,
    deletes: 0,
    config: 2,
    generate: 0,
    train: 0,
    pipeline: 1,
    feedback: 1,
    deploy: 0,
  });
});
