CREATE TABLE sessions (
  id TEXT PRIMARY KEY,
  source TEXT NOT NULL,
  user_id TEXT,
  model TEXT,
  model_config TEXT,
  system_prompt TEXT,
  parent_session_id TEXT,
  started_at REAL NOT NULL,
  ended_at REAL,
  end_reason TEXT,
  message_count INTEGER DEFAULT 0,
  tool_call_count INTEGER DEFAULT 0,
  input_tokens INTEGER DEFAULT 0,
  output_tokens INTEGER DEFAULT 0,
  cache_read_tokens INTEGER DEFAULT 0,
  cache_write_tokens INTEGER DEFAULT 0,
  reasoning_tokens INTEGER DEFAULT 0,
  billing_provider TEXT,
  billing_base_url TEXT,
  billing_mode TEXT,
  estimated_cost_usd REAL,
  actual_cost_usd REAL,
  cost_status TEXT,
  cost_source TEXT,
  pricing_version TEXT,
  title TEXT,
  api_call_count INTEGER DEFAULT 0,
  handoff_state TEXT,
  handoff_platform TEXT,
  handoff_error TEXT
);

CREATE TABLE messages (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL,
  role TEXT NOT NULL,
  content TEXT,
  tool_call_id TEXT,
  tool_calls TEXT,
  tool_name TEXT,
  timestamp REAL NOT NULL,
  token_count INTEGER,
  finish_reason TEXT,
  reasoning TEXT,
  reasoning_details TEXT,
  codex_reasoning_items TEXT,
  codex_message_items TEXT
);

INSERT INTO sessions (
  id, source, model, started_at, message_count, input_tokens, output_tokens,
  billing_provider, estimated_cost_usd, title, api_call_count
) VALUES (
  'hermes-legacy-sess-1', 'cli', 'anthropic/claude-sonnet-4-20250514',
  1764590500.000, 2, 100, 20, 'anthropic', 0.001, 'Legacy Hermes fixture', 1
);

INSERT INTO messages (session_id, role, content, timestamp)
VALUES ('hermes-legacy-sess-1', 'user', 'Use a legacy Hermes schema.', 1764590501.000);

INSERT INTO messages (session_id, role, content, timestamp, reasoning)
VALUES (
  'hermes-legacy-sess-1',
  'assistant',
  'Legacy schemas still parse.',
  1764590502.000,
  'Legacy reasoning column.'
);
