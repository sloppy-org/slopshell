(function () {
  const page = document.getElementById('capture-page');
  const alertNode = document.getElementById('capture-alert');
  const noteInput = document.getElementById('capture-note');
  const recordButton = document.getElementById('capture-record');
  const recordLabel = recordButton ? recordButton.querySelector('.capture-record-label') : null;
  const recordHint = recordButton ? recordButton.querySelector('.capture-record-hint') : null;
  const saveButton = document.getElementById('capture-save');
  const retryButton = document.getElementById('capture-retry');
  const fallbackButton = document.getElementById('capture-fallback');
  const resetButton = document.getElementById('capture-reset');
  const statusNode = document.getElementById('capture-status');

  if (!page || !alertNode || !noteInput || !recordButton || !recordLabel || !recordHint || !saveButton || !retryButton || !fallbackButton || !resetButton || !statusNode) {
    return;
  }

  const testEnv = window.__taburaCaptureTestEnv || {};
  const microphonePermissionCacheKey = 'tabura.capture.microphone_permission';
  const queueDatabaseName = String(testEnv.queueDatabaseName || 'tabura-capture');
  const queueStoreName = 'voice-memos';
  const maxVoiceRetries = 3;

  const state = {
    statusTimer: 0,
    captureStateTimer: 0,
    recording: false,
    saving: false,
    transcribing: false,
    drainingQueue: false,
    discardRecording: false,
    mediaStream: null,
    mediaRecorder: null,
    audioChunks: [],
    audioBlob: null,
    pendingTranscript: '',
    voiceRetryCount: 0,
    fallbackVisible: false,
    microphonePermission: readCachedMicrophonePermission(),
    voiceCaptureDisabled: false,
  };

  function clearStatusTimer() {
    if (state.statusTimer) {
      window.clearTimeout(state.statusTimer);
      state.statusTimer = 0;
    }
  }

  function clearCaptureStateTimer() {
    if (state.captureStateTimer) {
      window.clearTimeout(state.captureStateTimer);
      state.captureStateTimer = 0;
    }
  }

  function setAlert(message, tone) {
    const text = String(message || '').trim();
    alertNode.textContent = text;
    alertNode.hidden = text === '';
    if (tone) {
      alertNode.dataset.tone = tone;
    } else {
      delete alertNode.dataset.tone;
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

  function scheduleCaptureStateReset(delayMS) {
    clearCaptureStateTimer();
    state.captureStateTimer = window.setTimeout(() => {
      state.captureStateTimer = 0;
      if (!state.recording && !state.saving && !state.transcribing) {
        setCaptureState('idle');
      }
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
    if (cleanState === 'saving') {
      recordLabel.textContent = 'Saving';
      recordHint.textContent = 'Sending the capture into your inbox.';
      return;
    }
    if (cleanState === 'offline') {
      recordLabel.textContent = 'Queued';
      recordHint.textContent = 'Saved locally. It will upload when you are back online.';
      return;
    }
    if (cleanState === 'success') {
      recordLabel.textContent = 'Saved';
      recordHint.textContent = 'Your memo is in the inbox.';
      return;
    }
    if (cleanState === 'failed') {
      recordLabel.textContent = 'Retry';
      recordHint.textContent = state.fallbackVisible
        ? 'Retry the memo or save it as text instead.'
        : 'Retry the memo or clear to discard.';
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
    recordButton.disabled = busy || state.voiceCaptureDisabled;
    retryButton.hidden = !(state.audioBlob && !state.recording && !state.transcribing);
    retryButton.disabled = busy || !state.audioBlob;
    fallbackButton.hidden = !state.fallbackVisible;
    fallbackButton.disabled = busy;
    resetButton.disabled = busy;
    saveButton.disabled = busy || !hasNote;
  }

  function readCachedMicrophonePermission() {
    try {
      return String(window.localStorage.getItem(microphonePermissionCacheKey) || '').trim();
    } catch (_) {
      return '';
    }
  }

  function writeCachedMicrophonePermission(value) {
    const clean = String(value || '').trim();
    state.microphonePermission = clean;
    try {
      if (clean) {
        window.localStorage.setItem(microphonePermissionCacheKey, clean);
      } else {
        window.localStorage.removeItem(microphonePermissionCacheKey);
      }
    } catch (_) {}
  }

  function currentProtocol() {
    return String(testEnv.protocol || window.location.protocol || '').trim().toLowerCase();
  }

  function currentHostname() {
    return String(testEnv.hostname || window.location.hostname || '').trim().toLowerCase();
  }

  function currentHost() {
    const rawHost = String(window.location.host || '').trim();
    const envHost = String(testEnv.hostname || '').trim();
    if (envHost.includes(':')) {
      return envHost;
    }
    if (rawHost) {
      return rawHost;
    }
    return envHost;
  }

  function isLoopbackHost(hostname) {
    return hostname === 'localhost' || hostname === '127.0.0.1';
  }

  function isCaptureSecureContext() {
    if (typeof testEnv.isSecureContext === 'boolean') {
      return testEnv.isSecureContext;
    }
    return window.isSecureContext;
  }

  function isOnline() {
    if (typeof testEnv.online === 'boolean') {
      return testEnv.online;
    }
    if (typeof navigator.onLine === 'boolean') {
      return navigator.onLine;
    }
    return true;
  }

  function applyCaptureAvailability() {
    const hostname = currentHostname();
    const secure = isCaptureSecureContext();
    const blockedByProtocol = currentProtocol() !== 'https:' && !isLoopbackHost(hostname) && !secure;
    state.voiceCaptureDisabled = blockedByProtocol;
    if (blockedByProtocol) {
      setAlert(`Voice capture requires HTTPS. Visit https://${currentHost()}${window.location.pathname}`, '');
    } else if (alertNode.textContent.includes('Voice capture requires HTTPS')) {
      setAlert('', '');
    }
    updateSaveState();
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
    state.voiceRetryCount = 0;
    state.fallbackVisible = false;
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
    const capturedAt = new Date().toISOString();
    const artifactPayload = await postJSON('./api/artifacts', {
      kind: 'idea_note',
      title,
      meta_json: JSON.stringify({
        title,
        transcript,
        capture_mode: 'voice',
        captured_at: capturedAt,
        notes: [transcript],
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

  function isNetworkFailure(error) {
    const message = String(error && error.message ? error.message : error).toLowerCase();
    return !isOnline() || message.includes('failed to fetch') || message.includes('networkerror');
  }

  function queueUnsupported() {
    return !window.indexedDB;
  }

  function openQueueDB() {
    return new Promise((resolve, reject) => {
      if (queueUnsupported()) {
        reject(new Error('IndexedDB unavailable'));
        return;
      }
      const request = window.indexedDB.open(queueDatabaseName, 1);
      request.onerror = () => {
        reject(request.error || new Error('capture queue unavailable'));
      };
      request.onupgradeneeded = () => {
        const db = request.result;
        if (!db.objectStoreNames.contains(queueStoreName)) {
          db.createObjectStore(queueStoreName, { keyPath: 'id', autoIncrement: true });
        }
      };
      request.onsuccess = () => {
        resolve(request.result);
      };
    });
  }

  function runQueueRequest(mode, action) {
    return openQueueDB().then((db) => new Promise((resolve, reject) => {
      const tx = db.transaction(queueStoreName, mode);
      const store = tx.objectStore(queueStoreName);
      let request;
      try {
        request = action(store);
      } catch (error) {
        db.close();
        reject(error);
        return;
      }
      tx.oncomplete = () => {
        db.close();
      };
      tx.onerror = () => {
        db.close();
        reject(tx.error || new Error('capture queue transaction failed'));
      };
      request.onsuccess = () => {
        resolve(request.result);
      };
      request.onerror = () => {
        reject(request.error || new Error('capture queue request failed'));
      };
    }));
  }

  function enqueueVoiceMemo(blob, transcript) {
    return runQueueRequest('readwrite', (store) => store.add({
      createdAt: new Date().toISOString(),
      blob,
      transcript: String(transcript || '').trim(),
    }));
  }

  function listQueuedVoiceMemos() {
    return runQueueRequest('readonly', (store) => store.getAll()).then((items) => {
      const out = Array.isArray(items) ? items : [];
      out.sort((left, right) => Number(left.id || 0) - Number(right.id || 0));
      return out;
    });
  }

  function deleteQueuedVoiceMemo(id) {
    return runQueueRequest('readwrite', (store) => store.delete(id));
  }

  async function queueCurrentVoiceMemo() {
    if (!state.audioBlob) {
      return false;
    }
    await enqueueVoiceMemo(state.audioBlob, state.pendingTranscript);
    clearVoiceMemoState();
    setCaptureState('offline');
    setStatus('Saved locally. It will upload when online.', '');
    return true;
  }

  async function drainQueuedVoiceMemos() {
    if (state.drainingQueue || !isOnline()) {
      return;
    }
    state.drainingQueue = true;
    try {
      const queued = await listQueuedVoiceMemos();
      for (const entry of queued) {
        if (!isOnline()) {
          break;
        }
        const transcript = normalizeNote(entry && entry.transcript)
          || await transcribeVoiceMemo(entry && entry.blob ? entry.blob : null);
        await saveVoiceMemo(transcript);
        await deleteQueuedVoiceMemo(entry.id);
        setCaptureState('success');
        setStatus(`Saved: ${deriveItemTitle(transcript)}`, 'success');
        scheduleStatusClear(1800);
        scheduleCaptureStateReset(1800);
      }
    } catch (error) {
      if (!isNetworkFailure(error)) {
        setStatus(`Queued memo failed: ${String(error && error.message ? error.message : error)}`, 'error');
      }
    } finally {
      state.drainingQueue = false;
      updateSaveState();
    }
  }

  async function readMicrophonePermission() {
    if (!navigator.permissions || typeof navigator.permissions.query !== 'function') {
      return state.microphonePermission || '';
    }
    try {
      const status = await navigator.permissions.query({ name: 'microphone' });
      const clean = String(status && status.state ? status.state : '').trim();
      if (clean) {
        writeCachedMicrophonePermission(clean);
      }
      return clean;
    } catch (_) {
      return state.microphonePermission || '';
    }
  }

  function microphonePermissionMessage(permissionState, error) {
    const name = String(error && error.name ? error.name : '').trim();
    if (permissionState === 'denied') {
      return 'Microphone access denied. Check Settings > Safari > Microphone.';
    }
    if (name === 'NotAllowedError') {
      return 'Tap record again to allow microphone access.';
    }
    return `Voice capture failed: ${String(error && error.message ? error.message : error)}`;
  }

  async function processVoiceMemo() {
    if (!state.audioBlob || state.saving || state.transcribing) {
      return;
    }
    clearStatusTimer();
    clearCaptureStateTimer();
    state.transcribing = true;
    updateSaveState();
    setCaptureState('transcribing');
    setStatus(state.pendingTranscript ? 'Saving voice memo...' : 'Transcribing...', '');
    try {
      const transcript = state.pendingTranscript || await transcribeVoiceMemo(state.audioBlob);
      state.pendingTranscript = transcript;
      const title = await saveVoiceMemo(transcript);
      clearVoiceMemoState();
      setCaptureState('success');
      setStatus(`Saved: ${title}`, 'success');
      scheduleStatusClear(1800);
      scheduleCaptureStateReset(1800);
    } catch (error) {
      const message = String(error && error.message ? error.message : error);
      if (isNetworkFailure(error)) {
        try {
          const queued = await queueCurrentVoiceMemo();
          if (!queued) {
            throw new Error('queue_unavailable');
          }
        } catch (queueError) {
          setCaptureState('failed');
          setStatus(`Upload failed: ${String(queueError && queueError.message ? queueError.message : queueError)}`, 'error');
        }
      } else if (message === 'transcription_http_error') {
        state.voiceRetryCount += 1;
        state.fallbackVisible = state.voiceRetryCount >= maxVoiceRetries;
        setCaptureState('failed');
        setStatus(state.fallbackVisible ? 'Transcription failed. Retry this memo or save it as text instead.' : 'Transcription failed. Retry this memo.', 'error');
      } else if (message === 'artifact_create_failed' || /^HTTP \d+$/.test(message)) {
        state.voiceRetryCount += 1;
        state.fallbackVisible = state.voiceRetryCount >= maxVoiceRetries;
        setCaptureState('failed');
        setStatus(state.fallbackVisible ? 'Saving voice memo failed. Retry it or save it as text instead.' : 'Saving voice memo failed. Retry this memo.', 'error');
      } else {
        state.voiceRetryCount += 1;
        state.fallbackVisible = state.voiceRetryCount >= maxVoiceRetries;
        setCaptureState('failed');
        setStatus(message, 'error');
      }
    } finally {
      state.transcribing = false;
      updateSaveState();
    }
  }

  async function startRecording() {
    applyCaptureAvailability();
    if (state.voiceCaptureDisabled) {
      return;
    }
    if (!navigator.mediaDevices || typeof navigator.mediaDevices.getUserMedia !== 'function') {
      setStatus('Voice capture is not available in this browser.', 'error');
      return;
    }
    if (typeof window.MediaRecorder !== 'function') {
      setStatus('MediaRecorder is unavailable in this browser.', 'error');
      return;
    }
    clearStatusTimer();
    clearCaptureStateTimer();
    const permissionState = await readMicrophonePermission();
    if (permissionState === 'denied') {
      writeCachedMicrophonePermission('denied');
      setAlert('Microphone access denied. Check Settings > Safari > Microphone.', '');
      setStatus('Microphone access is currently denied.', 'error');
      updateSaveState();
      return;
    }
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
      writeCachedMicrophonePermission('granted');
      setAlert('', '');
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
          if (!isOnline()) {
            void queueCurrentVoiceMemo().catch((error) => {
              setCaptureState('failed');
              setStatus(`Queue failed: ${String(error && error.message ? error.message : error)}`, 'error');
              updateSaveState();
            });
            return;
          }
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
      setCaptureState('failed');
      const nextPermission = permissionState === 'prompt' ? 'dismissed' : permissionState || 'denied';
      writeCachedMicrophonePermission(nextPermission);
      setAlert(microphonePermissionMessage(permissionState, error), '');
      setStatus(microphonePermissionMessage(permissionState, error), 'error');
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
    clearCaptureStateTimer();
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

  function offerTextFallback() {
    const transcript = normalizeNote(state.pendingTranscript);
    if (transcript && normalizeNote(noteInput.value) === '') {
      noteInput.value = transcript;
    }
    noteInput.focus();
    setCaptureState('failed');
    setStatus(transcript
      ? 'Transcript copied into the note field. Edit it if needed, then save as text.'
      : 'Type the memo while it is fresh, then save it as a text note.', '');
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
      setCaptureState('success');
      setStatus(`Saved: ${title}`, 'success');
      scheduleStatusClear(1800);
      scheduleCaptureStateReset(1800);
    } catch (error) {
      setCaptureState(state.recording ? 'recording' : 'failed');
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
  fallbackButton.addEventListener('click', offerTextFallback);
  resetButton.addEventListener('click', resetCapture);
  window.addEventListener('online', () => {
    if (!state.recording && !state.saving && !state.transcribing) {
      void drainQueuedVoiceMemos();
    }
  });
  window.addEventListener('offline', () => {
    if (!state.recording && !state.audioBlob) {
      setCaptureState('offline');
      setStatus('Offline. New voice memos will queue until you reconnect.', '');
    }
  });

  updateSaveState();
  applyCaptureAvailability();
  setCaptureState('idle');
  if (isOnline()) {
    void drainQueuedVoiceMemos();
  }
  window.__taburaCapture = {
    deriveItemTitle,
    voiceMemoErrorMessage,
    resetCapture,
  };
})();
