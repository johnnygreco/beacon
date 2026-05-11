CREATE TABLE session (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  workspace_id TEXT,
  parent_id TEXT,
  slug TEXT NOT NULL,
  directory TEXT NOT NULL,
  path TEXT,
  title TEXT NOT NULL,
  version TEXT NOT NULL,
  share_url TEXT,
  summary_additions INTEGER,
  summary_deletions INTEGER,
  summary_files INTEGER,
  summary_diffs TEXT,
  revert TEXT,
  permission TEXT,
  agent TEXT,
  model TEXT,
  time_created INTEGER NOT NULL,
  time_updated INTEGER NOT NULL,
  time_compacting INTEGER,
  time_archived INTEGER
);

CREATE TABLE session_message (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  type TEXT NOT NULL,
  time_created INTEGER NOT NULL,
  time_updated INTEGER NOT NULL,
  data TEXT NOT NULL
);

INSERT INTO session (
  id, project_id, parent_id, slug, directory, title, version, agent, model,
  time_created, time_updated
) VALUES (
  'ses_opencode_1', 'proj_1', NULL, 'parser-fixtures',
  '/work/opencode-fixtures', 'Parser fixtures', '1.0.0', 'build',
  '{"id":"claude-sonnet-4-5","providerID":"anthropic"}',
  1764590400000, 1764590410000
);

INSERT INTO session_message (id, session_id, type, time_created, time_updated, data)
VALUES (
  'evt_user_1', 'ses_opencode_1', 'user', 1764590401000, 1764590401000,
  '{"time":{"created":1764590401000},"text":"Run the OpenCode fixture test.","files":[],"agents":[]}'
);

INSERT INTO session_message (id, session_id, type, time_created, time_updated, data)
VALUES (
  'evt_assistant_1', 'ses_opencode_1', 'assistant', 1764590402000, 1764590405000,
  '{"time":{"created":1764590402000,"completed":1764590405000},"agent":"build","model":{"providerID":"anthropic","modelID":"claude-sonnet-4-5"},"content":[{"type":"reasoning","id":"reason_1","text":"Need to run tests."},{"type":"text","text":"I will run the focused tests."},{"type":"tool","id":"tool_1","name":"bash","state":{"status":"completed","input":{"cmd":"go test ./internal/capture"},"output":"ok github.com/johnnygreco/beacon/internal/capture","title":"go test","metadata":{},"time":{"start":1764590403000,"end":1764590404000}}}],"finish":"stop","cost":0.012,"tokens":{"input":900,"output":180,"reasoning":24,"cache":{"read":50,"write":10}}}'
);

INSERT INTO session_message (id, session_id, type, time_created, time_updated, data)
VALUES (
  'evt_assistant_tool_only_1', 'ses_opencode_1', 'assistant', 1764590405500, 1764590405800,
  '{"time":{"created":1764590405500,"completed":1764590405800},"agent":"build","model":{"providerID":"anthropic","modelID":"claude-sonnet-4-5"},"content":[{"type":"tool","id":"tool_2","name":"grep","state":{"status":"completed","input":{"pattern":"TODO","path":"."},"output":"found TODO","title":"grep TODO","metadata":{},"time":{"start":1764590405600,"end":1764590405700}}}],"finish":"tool_use","cost":0.004,"tokens":{"input":300,"output":40,"cache":{"read":0,"write":0}}}'
);

INSERT INTO session_message (id, session_id, type, time_created, time_updated, data)
VALUES (
  'evt_compaction_1', 'ses_opencode_1', 'compaction', 1764590406000, 1764590406000,
  '{"time":{"created":1764590406000},"reason":"manual","summary":"Earlier fixture work was summarized."}'
);
