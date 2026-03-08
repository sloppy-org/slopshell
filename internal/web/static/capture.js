(function () {
  const page = document.getElementById('capture-page');
  const noteInput = document.getElementById('capture-note');
  const recordButton = document.getElementById('capture-record');
  const recordLabel = recordButton ? recordButton.querySelector('.capture-record-label') : null;
  const recordHint = recordButton ? recordButton.querySelector('.capture-record-hint') : null;
  const saveButton = document.getElementById('capture-save');
  const retryButton = document.getElementById('capture-retry');
  const resetButton = document.getElementById('capture-reset');
  const statusNode = document.getElementById('capture-status');

  if (!page || !noteInput || !recordButton || !recordLabel || !recordHint || !saveButton || !retryButton || !resetButton || !statusNode) {
    return;
  }

  const state = {
    statusTimer: 0,
    recording: false,
    saving: false,
    transcribing: false,
    discardRecording: false,
    mediaStream: null,
    mediaRecorder: null,
    audioChunks: [],
    audioBlob: null,
    pendingTranscript: '',
  };

  function clearStatusTimer() {
    if (state.statusTimer) {
      window.clearTimeout(state.statusTimer);
      state.statusTimer = 0;
    }
  }

  function setStatus(message, tone) {
    statusNode.textContent = String(message || '');
    if (tone) {
      statusNode.dataset.tone = tone;
    } else {
      delete statusNode.dataset.tone;
    }
  }

  function scheduleStatusClear(delayMS) {
    clearStatusTimer();
    state.statusTimer = window.setTimeout(() => {
      setStatus('', '');
      state.statusTimer = 0;
    }, delayMS);
  }

  function setCaptureState(nextState) {
    const cleanState = String(nextState || 'idle').trim() || 'idle';
    document.body.dataset.captureState = cleanState;
    page.dataset.state = cleanState;
    recordButton.setAttribute('aria-pressed', cleanState === 'recording' ? 'true' : 'false');
    if (cleanState === 'recording') {
      recordLabel.textContent = 'Recording';
      recordHint.textContent = 'Tap again to stop.';
      return;
    }
    if (cleanState === 'transcribing') {
      recordLabel.textContent = 'Transcribing';
      recordHint.textContent = 'Saving the voice memo into your inbox.';
      return;
    }
    recordLabel.textContent = 'Record';
    if (state.audioBlob) {
      recordHint.textContent = state.pendingTranscript
        ? 'Memo captured. Retry save or clear to discard.'
        : 'Memo captured. Retry transcription or clear to discard.';
      return;
    }
    recordHint.textContent = 'Tap once to start, again to stop.';
  }

  function normalizeNote(raw) {
    return String(raw || '').replace(/\s+/g, ' ').trim();
  }

  function deriveItemTitle(raw) {
    const clean = normalizeNote(raw);
    if (!clean) {
      return '';
    }
    const sentenceMatch = clean.match(/^.*?[.!?](?:\s|$)/);
    const firstSentence = normalizeNote(sentenceMatch ? sentenceMatch[0] : clean);
    if (firstSentence.length <= 80) {
      return firstSentence;
    }
    return `${firstSentence.slice(0, 77).trimEnd()}...`;
  }

  function updateSaveState() {
    const hasNote = normalizeNote(noteInput.value) !== '';
    const busy = state.saving || state.transcribing;
    noteInput.disabled = busy;
    recordButton.disabled = busy;
    retryButton.hidden = !(state.audioBlob && !state.recording && !state.transcribing);
    retryButton.disabled = busy || !state.audioBlob;
    resetButton.disabled = busy;
    saveButton.disabled = busy || !hasNote;
  }

  function releaseMediaStream() {
    if (!state.mediaStream || typeof state.mediaStream.getTracks !== 'function') {
      state.mediaStream = null;
      return;
    }
    for (const track of state.mediaStream.getTracks()) {
      if (track && typeof track.stop === 'function') {
        track.stop();
      }
    }
    state.mediaStream = null;
  }

  function finishRecording() {
    state.recording = false;
    state.mediaRecorder = null;
    releaseMediaStream();
    setCaptureState('idle');
    updateSaveState();
  }

  function clearVoiceMemoState() {
    state.audioChunks = [];
    state.audioBlob = null;
    state.pendingTranscript = '';
  }

  function voiceMemoErrorMessage(reason) {
    switch (String(reason || '').trim()) {
      case 'recording_too_short':
        return 'Recording too short. Retry this memo.';
      case 'likely_noise':
      case 'no_speech_detected':
      case 'empty_transcript':
        return 'No speech was detected. Retry this memo.';
      default:
        return 'Transcription failed. Retry this memo.';
    }
  }

  async function postJSON(url, payload) {
    const response = await fetch(url, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(payload),
    });
    let data = {};
    try {
      data = await response.json();
    } catch (_) {
      data = {};
    }
    if (!response.ok) {
      throw new Error(String(data && data.error ? data.error : `HTTP ${response.status}`));
    }
    return data;
  }

  async function transcribeVoiceMemo(blob) {
    const mimeType = String(blob && blob.type ? blob.type : 'audio/webm').trim() || 'audio/webm';
    const payload = new FormData();
    payload.append('file', blob, `capture${mimeType.startsWith('audio/') ? '' : '.bin'}`);
    payload.append('mime_type', mimeType);
    const response = await fetch('./api/stt/transcribe', {
      method: 'POST',
      body: payload,
    });
    let data = {};
    try {
      data = await response.json();
    } catch (_) {
      data = {};
    }
    if (!response.ok) {
      throw new Error('transcription_http_error');
    }
    const transcript = normalizeNote(data && data.text);
    if (!transcript) {
      throw new Error(voiceMemoErrorMessage(data && data.reason));
    }
    return transcript;
  }

  async function saveVoiceMemo(transcript) {
    const title = deriveItemTitle(transcript);
    if (!title) {
      throw new Error('Transcription was empty after cleanup. Retry this memo.');
    }
    const artifactPayload = await postJSON('./api/artifacts', {
      kind: 'idea_note',
      title,
      meta_json: JSON.stringify({
        transcript,
        source: 'capture_stt',
      }),
    });
    const artifactID = Number(artifactPayload && artifactPayload.artifact && artifactPayload.artifact.id);
    if (!Number.isFinite(artifactID) || artifactID <= 0) {
      throw new Error('artifact_create_failed');
    }
    await postJSON('./api/items', {
      title,
      artifact_id: artifactID,
    });
    return title;
  }

  async function processVoiceMemo() {
    if (!state.audioBlob || state.saving || state.transcribing) {
      return;
    }
    clearStatusTimer();
    state.transcribing = true;
    updateSaveState();
    setCaptureState('transcribing');
    setStatus(state.pendingTranscript ? 'Saving voice memo...' : 'Transcribing...', '');
    try {
      const transcript = state.pendingTranscript || await transcribeVoiceMemo(state.audioBlob);
      state.pendingTranscript = transcript;
      const title = await saveVoiceMemo(transcript);
      clearVoiceMemoState();
      setCaptureState('idle');
      setStatus(`Saved: ${title}`, 'success');
      scheduleStatusClear(1800);
    } catch (error) {
      setCaptureState('idle');
      const message = String(error && error.message ? error.message : error);
      if (message === 'transcription_http_error') {
        setStatus('Transcription failed. Retry this memo.', 'error');
      } else if (message === 'artifact_create_failed' || /^HTTP \d+$/.test(message)) {
        setStatus('Saving voice memo failed. Retry this memo.', 'error');
      } else {
        setStatus(message, 'error');
      }
    } finally {
      state.transcribing = false;
      updateSaveState();
    }
  }

  async function startRecording() {
    if (!navigator.mediaDevices || typeof navigator.mediaDevices.getUserMedia !== 'function') {
      setStatus('Voice capture is not available in this browser.', 'error');
      return;
    }
    if (typeof window.MediaRecorder !== 'function') {
      setStatus('MediaRecorder is unavailable in this browser.', 'error');
      return;
    }
    clearStatusTimer();
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
      const recorder = new window.MediaRecorder(stream);
      state.recording = true;
      clearVoiceMemoState();
      state.mediaStream = stream;
      state.mediaRecorder = recorder;
      state.audioChunks = [];
      recorder.addEventListener('dataavailable', (event) => {
        if (event.data) {
          state.audioChunks.push(event.data);
        }
      });
      recorder.addEventListener('stop', () => {
        const mimeType = String(recorder.mimeType || 'audio/webm').trim() || 'audio/webm';
        if (!state.discardRecording && state.audioChunks.length > 0) {
          state.audioBlob = new Blob(state.audioChunks, { type: mimeType });
        }
        state.discardRecording = false;
        finishRecording();
        if (state.audioBlob) {
          void processVoiceMemo();
        }
      });
      recorder.start();
      setStatus('', '');
      setCaptureState('recording');
      updateSaveState();
    } catch (error) {
      releaseMediaStream();
      state.recording = false;
      state.mediaRecorder = null;
      setCaptureState('idle');
      setStatus(`Voice capture failed: ${String(error && error.message ? error.message : error)}`, 'error');
      updateSaveState();
    }
  }

  function stopRecording() {
    if (!state.recording || !state.mediaRecorder) {
      finishRecording();
      return;
    }
    if (state.mediaRecorder.state !== 'inactive') {
      state.mediaRecorder.stop();
      return;
    }
    finishRecording();
  }

  function resetCapture() {
    clearStatusTimer();
    if (state.mediaRecorder && state.mediaRecorder.state !== 'inactive') {
      state.discardRecording = true;
      state.mediaRecorder.stop();
    }
    state.recording = false;
    state.mediaRecorder = null;
    clearVoiceMemoState();
    releaseMediaStream();
    noteInput.value = '';
    setCaptureState('idle');
    setStatus('', '');
    updateSaveState();
  }

  async function saveCapture() {
    const note = normalizeNote(noteInput.value);
    if (!note || state.saving) {
      updateSaveState();
      return;
    }
    const title = deriveItemTitle(note);
    if (!title) {
      updateSaveState();
      return;
    }
    state.saving = true;
    updateSaveState();
    setCaptureState('saving');
    setStatus('Saving...', '');
    try {
      await postJSON('./api/items', {
        title,
      });
      noteInput.value = '';
      clearVoiceMemoState();
      setCaptureState('idle');
      setStatus(`Saved: ${title}`, 'success');
      scheduleStatusClear(1800);
    } catch (error) {
      setCaptureState(state.recording ? 'recording' : 'idle');
      setStatus(`Save failed: ${String(error && error.message ? error.message : error)}`, 'error');
    } finally {
      state.saving = false;
      updateSaveState();
    }
  }

  recordButton.addEventListener('click', () => {
    if (state.recording) {
      stopRecording();
      return;
    }
    void startRecording();
  });
  noteInput.addEventListener('input', updateSaveState);
  saveButton.addEventListener('click', () => {
    void saveCapture();
  });
  retryButton.addEventListener('click', () => {
    void processVoiceMemo();
  });
  resetButton.addEventListener('click', resetCapture);

  updateSaveState();
  setCaptureState('idle');
  window.__taburaCapture = {
    deriveItemTitle,
    voiceMemoErrorMessage,
    resetCapture,
  };
})();
