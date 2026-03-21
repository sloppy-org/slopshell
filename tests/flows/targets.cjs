const targetDefinitions = {
  tabura_circle_dot: {
    category: 'circle',
    platforms: {
      web: '#tabura-circle-dot',
      ios: 'tabura_circle_dot',
      android: 'tabura_circle_dot',
    },
  },
  tabura_circle_segment_pointer: {
    category: 'circle',
    platforms: {
      web: '[data-segment="pointer"]',
      ios: 'tabura_circle_pointer',
      android: 'tabura_circle_pointer',
    },
  },
  tabura_circle_segment_highlight: {
    category: 'circle',
    platforms: {
      web: '[data-segment="highlight"]',
      ios: 'tabura_circle_highlight',
      android: 'tabura_circle_highlight',
    },
  },
  tabura_circle_segment_ink: {
    category: 'circle',
    platforms: {
      web: '[data-segment="ink"]',
      ios: 'tabura_circle_ink',
      android: 'tabura_circle_ink',
    },
  },
  tabura_circle_segment_text_note: {
    category: 'circle',
    platforms: {
      web: '[data-segment="text_note"]',
      ios: 'tabura_circle_text_note',
      android: 'tabura_circle_text_note',
    },
  },
  tabura_circle_segment_prompt: {
    category: 'circle',
    platforms: {
      web: '[data-segment="prompt"]',
      ios: 'tabura_circle_prompt',
      android: 'tabura_circle_prompt',
    },
  },
  tabura_circle_segment_dialogue: {
    category: 'circle',
    platforms: {
      web: '[data-segment="dialogue"]',
      ios: 'tabura_circle_dialogue',
      android: 'tabura_circle_dialogue',
    },
  },
  tabura_circle_segment_meeting: {
    category: 'circle',
    platforms: {
      web: '[data-segment="meeting"]',
      ios: 'tabura_circle_meeting',
      android: 'tabura_circle_meeting',
    },
  },
  tabura_circle_segment_silent: {
    category: 'circle',
    platforms: {
      web: '[data-segment="silent"]',
      ios: 'tabura_circle_silent',
      android: 'tabura_circle_silent',
    },
  },
  canvas_viewport: {
    category: 'circle',
    platforms: {
      web: '#canvas-viewport',
      ios: 'canvas_viewport',
      android: 'canvas_viewport',
    },
  },
  indicator_border: {
    category: 'indicator',
    platforms: {
      web: '#indicator-border',
      ios: 'indicator_border',
      android: 'indicator_border',
    },
  },
  indicator_simulate_recording: {
    category: 'indicator',
    kind: 'test_hook',
    platforms: {
      web: '#indicator-simulate-recording',
    },
  },
  indicator_simulate_working: {
    category: 'indicator',
    kind: 'test_hook',
    platforms: {
      web: '#indicator-simulate-working',
    },
  },
  indicator_override_clear: {
    category: 'indicator',
    kind: 'test_hook',
    platforms: {
      web: '#indicator-override-clear',
    },
  },
};

const requiredCoverageTargets = [
  'tabura_circle_dot',
  'tabura_circle_segment_pointer',
  'tabura_circle_segment_highlight',
  'tabura_circle_segment_ink',
  'tabura_circle_segment_text_note',
  'tabura_circle_segment_prompt',
  'tabura_circle_segment_dialogue',
  'tabura_circle_segment_meeting',
  'tabura_circle_segment_silent',
  'canvas_viewport',
  'indicator_border',
];

const requiredIndicatorStates = ['idle', 'listening', 'paused', 'recording', 'working'];

function getTargetDefinition(target) {
  return targetDefinitions[target] || null;
}

function getTargetPlatforms(target) {
  const definition = getTargetDefinition(target);
  return definition ? Object.keys(definition.platforms) : [];
}

function getWebSelector(target) {
  const definition = getTargetDefinition(target);
  return definition && definition.platforms.web ? definition.platforms.web : null;
}

module.exports = {
  getTargetDefinition,
  getTargetPlatforms,
  getWebSelector,
  requiredCoverageTargets,
  requiredIndicatorStates,
  targetDefinitions,
};
