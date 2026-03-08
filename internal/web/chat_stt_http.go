package web

import (
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/krystophny/tabura/internal/stt"
)

const (
	sttMultipartOverheadBytes = 1 * 1024 * 1024
	sttMultipartFieldLimit    = 4 * 1024
)

var (
	errInvalidMultipartPayload = errors.New("invalid multipart payload")
	errMissingAudioFile        = errors.New("missing audio file")
	errDuplicateAudioFile      = errors.New("multiple audio files are not supported")
	errAudioPayloadTooLarge    = errors.New("audio payload exceeds max size")
)

func (a *App) handleSTTTranscribe(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	if a.sttURL == "" {
		http.Error(w, "stt sidecar is not configured", http.StatusServiceUnavailable)
		return
	}
	data, mimeType, err := readSTTMultipartAudio(w, r)
	if err != nil {
		switch {
		case errors.Is(err, errAudioPayloadTooLarge):
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
		case errors.Is(err, errMissingAudioFile), errors.Is(err, errDuplicateAudioFile), errors.Is(err, errInvalidMultipartPayload):
			http.Error(w, err.Error(), http.StatusBadRequest)
		default:
			http.Error(w, "failed to read audio payload", http.StatusBadRequest)
		}
		return
	}
	defer zeroBytes(data)
	if len(data) < 1024 {
		writeJSON(w, map[string]string{"text": "", "reason": "recording_too_short"})
		return
	}

	mimeType = stt.NormalizeMimeType(mimeType)
	if !stt.IsAllowedMimeType(mimeType) {
		http.Error(w, "mime_type must be audio/* or application/octet-stream", http.StatusBadRequest)
		return
	}
	normalizedMimeType, normalizedData, normalizeErr := stt.NormalizeForWhisper(mimeType, data)
	if normalizeErr != nil {
		http.Error(w, fmt.Sprintf("audio normalization failed: %v", normalizeErr), http.StatusBadRequest)
		return
	}
	defer zeroBytes(normalizedData)

	replacements := a.loadSTTReplacements()
	options := a.sttTranscribeOptions()
	text, transcribeErr := stt.TranscribeWithOptions(a.sttURL, normalizedMimeType, normalizedData, replacements, options)
	if transcribeErr != nil {
		if errors.Is(transcribeErr, stt.ErrLikelyNoise) {
			log.Printf("stt empty: reason=likely_noise mime=%s bytes=%d", normalizedMimeType, len(normalizedData))
			writeJSON(w, map[string]string{"text": "", "reason": "likely_noise"})
			return
		}
		if stt.IsRetryableNoSpeechError(transcribeErr) {
			log.Printf("stt empty: reason=no_speech_detected mime=%s bytes=%d err=%v", normalizedMimeType, len(normalizedData), transcribeErr)
			writeJSON(w, map[string]string{"text": "", "reason": "no_speech_detected"})
			return
		}
		http.Error(w, fmt.Sprintf("transcription failed: %v", transcribeErr), http.StatusBadGateway)
		return
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		writeJSON(w, map[string]string{"text": "", "reason": "empty_transcript"})
		return
	}
	writeJSON(w, map[string]string{"text": trimmed})
}

func readSTTMultipartAudio(w http.ResponseWriter, r *http.Request) ([]byte, string, error) {
	contentType := strings.TrimSpace(r.Header.Get("Content-Type"))
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "multipart/form-data" {
		return nil, "", errInvalidMultipartPayload
	}
	boundary := strings.TrimSpace(params["boundary"])
	if boundary == "" {
		return nil, "", errInvalidMultipartPayload
	}

	limitedBody := http.MaxBytesReader(w, r.Body, stt.MaxAudioBytes+sttMultipartOverheadBytes)
	defer limitedBody.Close()

	reader := multipart.NewReader(limitedBody, boundary)
	var (
		audioData []byte
		mimeType  string
	)
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			if isBodyTooLarge(err) {
				return nil, "", errAudioPayloadTooLarge
			}
			return nil, "", errInvalidMultipartPayload
		}

		name := strings.TrimSpace(part.FormName())
		switch name {
		case "file":
			if audioData != nil {
				return nil, "", errDuplicateAudioFile
			}
			audioData, err = io.ReadAll(io.LimitReader(part, stt.MaxAudioBytes+1))
			if err != nil {
				if isBodyTooLarge(err) {
					return nil, "", errAudioPayloadTooLarge
				}
				return nil, "", errInvalidMultipartPayload
			}
			if len(audioData) > stt.MaxAudioBytes {
				return nil, "", errAudioPayloadTooLarge
			}
			if mimeType == "" {
				mimeType = strings.TrimSpace(part.Header.Get("Content-Type"))
			}
		case "mime_type":
			value, err := io.ReadAll(io.LimitReader(part, sttMultipartFieldLimit))
			if err != nil {
				if isBodyTooLarge(err) {
					return nil, "", errAudioPayloadTooLarge
				}
				return nil, "", errInvalidMultipartPayload
			}
			mimeType = strings.TrimSpace(string(value))
		default:
			if _, err := io.Copy(io.Discard, part); err != nil {
				if isBodyTooLarge(err) {
					return nil, "", errAudioPayloadTooLarge
				}
				return nil, "", errInvalidMultipartPayload
			}
		}
	}
	if audioData == nil {
		return nil, "", errMissingAudioFile
	}
	return audioData, mimeType, nil
}

func isBodyTooLarge(err error) bool {
	var maxBytesErr *http.MaxBytesError
	return errors.As(err, &maxBytesErr)
}

func zeroBytes(data []byte) {
	for i := range data {
		data[i] = 0
	}
}
