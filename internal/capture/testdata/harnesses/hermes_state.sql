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
  reasoning_content TEXT,
  codex_reasoning_items TEXT,
  codex_message_items TEXT
);

INSERT INTO sessions (
  id, source, model, parent_session_id, started_at, ended_at, end_reason,
  message_count, tool_call_count, input_tokens, output_tokens,
  cache_read_tokens, cache_write_tokens, reasoning_tokens,
  billing_provider, estimated_cost_usd, title, api_call_count
) VALUES (
  'hermes-sess-1', 'cli', 'anthropic/claude-sonnet-4-20250514', NULL,
  1764590400.000, 1764590410.000, 'cli_close',
  4, 1, 1200, 240, 30, 12, 18,
  'anthropic', 0.0075, 'Investigate parser fixtures', 1
);

INSERT INTO messages (session_id, role, content, timestamp)
VALUES ('hermes-sess-1', 'user', 'Please inspect the parser fixtures.', 1764590401.000);

INSERT INTO messages (session_id, role, content, tool_calls, timestamp, reasoning_content)
VALUES (
  'hermes-sess-1',
  'assistant',
  'I will read the fixture file.',
  '[{"id":"call_read_1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"fixtures.json\"}"}}]',
  1764590402.000,
  'Need to inspect the file first.'
);

INSERT INTO messages (session_id, role, content, tool_call_id, tool_name, timestamp)
VALUES ('hermes-sess-1', 'tool', 'fixture contents', 'call_read_1', 'read_file', 1764590403.000);

INSERT INTO messages (session_id, role, content, timestamp)
VALUES ('hermes-sess-1', 'assistant', 'The fixture looks valid.', 1764590404.000);
