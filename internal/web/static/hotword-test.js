import {
  initHotword, startHotwordMonitor, stopHotwordMonitor,
  isHotwordActive, onHotwordDetected, setHotwordThreshold,
} from './hotword.js';

const statusEl = document.getElementById('status');
const detectionEl = document.getElementById('detection');
const logEl = document.getElementById('log');
const btnStart = document.getElementById('btn-start');
const btnStop = document.getElementById('btn-stop');
const thresholdInput = document.getElementById('threshold');
const thresholdVal = document.getElementById('threshold-val');

let detectionCount = 0;
let flashTimer = null;

function log(msg) {
  const ts = new Date().toLocaleTimeString('en-GB', { hour12: false, fractionalSecondDigits: 1 });
  logEl.textContent += `[${ts}] ${msg}\n`;
  logEl.scrollTop = logEl.scrollHeight;
}

function flash() {
  detectionEl.classList.add('triggered');
  clearTimeout(flashTimer);
  flashTimer = setTimeout(() => detectionEl.classList.remove('triggered'), 800);
}

onHotwordDetected(() => {
  detectionCount++;
  detectionEl.textContent = `Detected (#${detectionCount})`;
  flash();
  log(`Wake word detected (count: ${detectionCount})`);
});

thresholdInput.addEventListener('input', () => {
  const v = setHotwordThreshold(parseFloat(thresholdInput.value));
  thresholdVal.textContent = v.toFixed(2);
  log(`Threshold set to ${v.toFixed(2)}`);
});

btnStart.addEventListener('click', async () => {
  log('Requesting microphone...');
  try {
    const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
    await startHotwordMonitor(stream);
    btnStart.disabled = true;
    btnStop.disabled = false;
    log('Listening for wake word.');
  } catch (err) {
    log(`Mic error: ${err.message}`);
  }
});

btnStop.addEventListener('click', () => {
  stopHotwordMonitor();
  btnStart.disabled = false;
  btnStop.disabled = true;
  log('Stopped.');
});

log('Initializing ONNX models...');
try {
  const ok = await initHotword();
  if (ok) {
    statusEl.textContent = 'Models loaded. Click "Start Listening" and say "Alexa".';
    statusEl.className = 'ready';
    btnStart.disabled = false;
    log('ONNX models loaded successfully.');
  } else {
    statusEl.textContent = 'Model init returned false — check console.';
    statusEl.className = 'error';
    log('initHotword() returned false.');
  }
} catch (err) {
  statusEl.textContent = `Init failed: ${err.message}`;
  statusEl.className = 'error';
  log(`Init error: ${err.message}`);
}
