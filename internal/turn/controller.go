package turn

import (
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	continuationWait           = 900 * time.Millisecond
	shortContinuationWait      = 650 * time.Millisecond
	defaultRollbackAudioWindow = 350
	bargeInThreshold           = 0.75
	bargeInConsecutiveFrames   = 3
)

type Action string

const (
	ActionFinalizeUserTurn Action = "finalize_user_turn"
	ActionContinueListen   Action = "continue_listening"
	ActionBackchannel      Action = "backchannel"
	ActionYield            Action = "yield"
)

type Signal struct {
	Action             Action `json:"action"`
	Text               string `json:"text,omitempty"`
	Reason             string `json:"reason,omitempty"`
	WaitMS             int    `json:"wait_ms,omitempty"`
	InterruptAssistant bool   `json:"interrupt_assistant,omitempty"`
	RollbackAudioMS    int    `json:"rollback_audio_ms,omitempty"`
}

type Segment struct {
	PriorText            string
	Text                 string
	DurationMS           int
	InterruptedAssistant bool
}

type Callbacks struct {
	OnAction func(Signal)
}

type Controller struct {
	mu             sync.Mutex
	pendingText    string
	playbackActive bool
	playedAudioMS  int
	bargeInFrames  int
	timer          *time.Timer
	callbacks      Callbacks
	closed         bool
}

var (
	finalPunctuationRE        = regexp.MustCompile(`[.!?][)"'\]]*$`)
	continuationPunctuationRE = regexp.MustCompile(`(?:,|:|;|-|--|\.\.\.)[)"'\]]*$`)
	tokenCleanupRE            = regexp.MustCompile(`[^a-z0-9' -]+`)
)

var hesitationTokens = tokenSet(
	"ah", "eh", "er", "erm", "hmm", "hm", "mm", "mmm", "uh", "uhh", "uhm", "um", "umm",
	"well", "like",
)

var backchannelPhrases = tokenSet(
	"got it",
	"i see",
	"makes sense",
	"mm-hmm",
	"mmhmm",
	"ok",
	"okay",
	"right",
	"sure",
	"thanks",
	"thank you",
	"yeah",
	"yep",
	"yes",
)

var completeShortUtterances = tokenSet(
	"go on",
	"hold on",
	"nevermind",
	"never mind",
	"no",
	"not now",
	"please continue",
	"repeat that",
	"resume",
	"start over",
	"stop",
	"wait",
	"yes",
)

var trailingContinuationTokens = tokenSet(
	"a", "an", "and", "around", "as", "at", "because", "but", "for", "from",
	"if", "in", "into", "like", "my", "of", "on", "or", "so", "that",
	"the", "then", "this", "to", "under", "until", "when", "while", "with", "your",
)

var leadingQuestionTokens = tokenSet(
	"are", "can", "could", "did", "do", "does", "how", "is", "should",
	"what", "when", "where", "who", "why", "will", "would",
)

func NewController(callbacks Callbacks) *Controller {
	return &Controller{callbacks: callbacks}
}

func (c *Controller) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	c.stopTimerLocked()
}

func (c *Controller) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pendingText = ""
	c.bargeInFrames = 0
	c.stopTimerLocked()
}

func (c *Controller) UpdatePlayback(playing bool, playedMS int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.playbackActive = playing
	if playedMS >= 0 {
		c.playedAudioMS = playedMS
	}
	if !playing {
		c.bargeInFrames = 0
	}
}

func (c *Controller) HandleSpeechStart(interruptedAssistant bool) *Signal {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !interruptedAssistant && !c.playbackActive {
		c.bargeInFrames = 0
		return nil
	}
	c.bargeInFrames = 0
	signal := Signal{
		Action:             ActionYield,
		Reason:             "speech_start",
		InterruptAssistant: true,
		RollbackAudioMS:    rollbackAudioMS(c.playedAudioMS),
	}
	c.emitLocked(signal)
	return &signal
}

func (c *Controller) HandleSpeechProbability(prob float64, interruptedAssistant bool) *Signal {
	c.mu.Lock()
	defer c.mu.Unlock()
	if (!interruptedAssistant && !c.playbackActive) || prob < bargeInThreshold {
		c.bargeInFrames = 0
		return nil
	}
	c.bargeInFrames++
	if c.bargeInFrames < bargeInConsecutiveFrames {
		return nil
	}
	c.bargeInFrames = 0
	signal := Signal{
		Action:             ActionYield,
		Reason:             "speech_overlap",
		InterruptAssistant: true,
		RollbackAudioMS:    rollbackAudioMS(c.playedAudioMS),
	}
	c.emitLocked(signal)
	return &signal
}

func (c *Controller) ConsumeSegment(segment Segment) Signal {
	c.mu.Lock()
	priorText := normalizeText(segment.PriorText)
	if priorText == "" {
		priorText = c.pendingText
	}
	decision := classifySegment(Segment{
		PriorText:            priorText,
		Text:                 segment.Text,
		DurationMS:           segment.DurationMS,
		InterruptedAssistant: segment.InterruptedAssistant,
	})
	switch decision.Action {
	case ActionContinueListen:
		c.pendingText = decision.Text
		c.scheduleFinalizeLocked(decision.WaitMS)
		c.emitLocked(decision)
	case ActionBackchannel:
		c.emitLocked(decision)
	default:
		c.pendingText = ""
		c.stopTimerLocked()
		c.emitLocked(decision)
	}
	c.mu.Unlock()
	return decision
}

func (c *Controller) Flush(reason string) *Signal {
	c.mu.Lock()
	text := normalizeText(c.pendingText)
	if text == "" {
		c.pendingText = ""
		c.stopTimerLocked()
		c.mu.Unlock()
		return nil
	}
	c.pendingText = ""
	c.stopTimerLocked()
	signal := Signal{
		Action: ActionFinalizeUserTurn,
		Text:   text,
		Reason: strings.TrimSpace(reason),
	}
	c.emitLocked(signal)
	c.mu.Unlock()
	return &signal
}

func (c *Controller) scheduleFinalizeLocked(waitMS int) {
	c.stopTimerLocked()
	delay := time.Duration(waitMS) * time.Millisecond
	if delay <= 0 {
		delay = continuationWait
	}
	c.timer = time.AfterFunc(delay, func() {
		c.Flush("continuation_timeout")
	})
}

func (c *Controller) stopTimerLocked() {
	if c.timer == nil {
		return
	}
	c.timer.Stop()
	c.timer = nil
}

func (c *Controller) emitLocked(signal Signal) {
	if c.closed {
		return
	}
	if c.callbacks.OnAction == nil {
		return
	}
	callback := c.callbacks.OnAction
	go callback(signal)
}

func classifySegment(segment Segment) Signal {
	priorText := normalizeText(segment.PriorText)
	currentText := normalizeText(segment.Text)
	combinedText := normalizeText(strings.Join(filterNonEmpty(priorText, currentText), " "))
	durationMS := maxInt(0, segment.DurationMS)
	tokens := tokenize(combinedText)

	if combinedText == "" {
		return Signal{
			Action: ActionBackchannel,
			Reason: "empty",
			WaitMS: int(shortContinuationWait / time.Millisecond),
		}
	}

	if priorText == "" && isBackchannel(currentText) && segment.InterruptedAssistant {
		return Signal{
			Action: ActionBackchannel,
			Text:   combinedText,
			Reason: "assistant_backchannel",
			WaitMS: int(shortContinuationWait / time.Millisecond),
		}
	}

	if incompleteReason := looksIncomplete(combinedText, currentText, durationMS, tokens); incompleteReason != "" {
		waitMS := continuationWait
		if len(tokens) <= 2 {
			waitMS = shortContinuationWait
		}
		return Signal{
			Action: ActionContinueListen,
			Text:   combinedText,
			Reason: incompleteReason,
			WaitMS: int(waitMS / time.Millisecond),
		}
	}

	reason := "semantic_completion"
	if finalPunctuationRE.MatchString(combinedText) {
		reason = "terminal_punctuation"
	}
	return Signal{
		Action: ActionFinalizeUserTurn,
		Text:   combinedText,
		Reason: reason,
	}
}

func normalizeText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func tokenize(text string) []string {
	normalized := strings.ToLower(normalizeText(text))
	if normalized == "" {
		return nil
	}
	cleaned := tokenCleanupRE.ReplaceAllString(normalized, " ")
	tokens := strings.Fields(cleaned)
	if len(tokens) == 0 {
		return nil
	}
	return tokens
}

func lastToken(text string) string {
	tokens := tokenize(text)
	if len(tokens) == 0 {
		return ""
	}
	return tokens[len(tokens)-1]
}

func hasUnbalancedClosers(text string) bool {
	counts := map[rune]int{
		'(': 0, ')': 0,
		'[': 0, ']': 0,
		'{': 0, '}': 0,
		'"': 0, '\'': 0,
	}
	runes := []rune(text)
	for idx, ch := range runes {
		if _, ok := counts[ch]; ok {
			if (ch == '\'' || ch == '"') && isEmbeddedWordQuote(runes, idx) {
				continue
			}
			counts[ch]++
		}
	}
	return counts['('] > counts[')'] ||
		counts['['] > counts[']'] ||
		counts['{'] > counts['}'] ||
		counts['"']%2 == 1 ||
		counts['\'']%2 == 1
}

func isEmbeddedWordQuote(runes []rune, idx int) bool {
	if idx <= 0 || idx >= len(runes)-1 {
		return false
	}
	return isWordRune(runes[idx-1]) && isWordRune(runes[idx+1])
}

func isWordRune(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') ||
		(ch >= 'A' && ch <= 'Z') ||
		(ch >= '0' && ch <= '9')
}

func isHesitationOnly(text string) bool {
	tokens := tokenize(text)
	if len(tokens) == 0 {
		return false
	}
	for _, token := range tokens {
		if !hesitationTokens[token] {
			return false
		}
	}
	return true
}

func isBackchannel(text string) bool {
	normalized := strings.ToLower(normalizeText(text))
	if normalized == "" {
		return false
	}
	return backchannelPhrases[normalized] || isHesitationOnly(normalized)
}

func looksLikeCompleteQuestion(text string, tokens []string) bool {
	if finalPunctuationRE.MatchString(text) {
		return true
	}
	if len(tokens) < 3 {
		return false
	}
	return leadingQuestionTokens[tokens[0]]
}

func looksLikeCompleteShortUtterance(text string) bool {
	return completeShortUtterances[strings.ToLower(normalizeText(text))]
}

func looksIncomplete(text, currentText string, durationMS int, tokens []string) string {
	if text == "" {
		return "empty"
	}
	if continuationPunctuationRE.MatchString(text) {
		return "continuation_punctuation"
	}
	if hasUnbalancedClosers(text) {
		return "open_phrase"
	}
	if isHesitationOnly(currentText) {
		return "hesitation"
	}
	tail := lastToken(text)
	if tail != "" && trailingContinuationTokens[tail] {
		return "trailing_connector"
	}
	if len(tokens) <= 2 && !looksLikeCompleteShortUtterance(text) {
		return "too_short"
	}
	if !finalPunctuationRE.MatchString(text) && !looksLikeCompleteQuestion(text, tokens) {
		if durationMS < 900 && len(tokens) <= 6 {
			return "short_unpunctuated"
		}
		if len(currentText) < 18 && len(tokens) <= 4 {
			return "fragment"
		}
	}
	return ""
}

func rollbackAudioMS(playedMS int) int {
	if playedMS <= 0 {
		return 0
	}
	if playedMS < defaultRollbackAudioWindow {
		return playedMS
	}
	return defaultRollbackAudioWindow
}

func tokenSet(values ...string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		normalized := strings.ToLower(strings.TrimSpace(value))
		if normalized != "" {
			set[normalized] = true
		}
	}
	return set
}

func filterNonEmpty(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	return out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
