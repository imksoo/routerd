CREATE TABLE access_logs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts TEXT,
  user TEXT,
  method TEXT,
  path TEXT,
  status_code INTEGER,
  duration_ms INTEGER,
  generation INTEGER
);
CREATE TABLE action_executions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  idempotency_key TEXT NOT NULL UNIQUE,
  source TEXT,
  provider TEXT NOT NULL,
  provider_ref TEXT,
  action TEXT NOT NULL,
  target_json TEXT,
  parameters_json TEXT,
  undo_json TEXT,
  risk_level TEXT,
  status TEXT NOT NULL,
  approved_by TEXT,
  approved_at TEXT,
  executed_at TEXT,
  result_message TEXT,
  error TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
, observed_json TEXT);
CREATE TABLE artifacts (
  artifact_id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  name TEXT NOT NULL,
  owner_api_version TEXT,
  owner_kind TEXT,
  owner_name TEXT,
  attributes TEXT,
  source TEXT,
  generation INTEGER,
  observed_at TEXT
);
CREATE TABLE dynamic_config_parts (
  id INTEGER PRIMARY KEY,
  source TEXT NOT NULL,
  generation INTEGER NOT NULL,
  observed_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  digest TEXT NOT NULL,
  resources_json TEXT,
  directives_json TEXT,
  actionplans_json TEXT,
  mobility_dataplane_json TEXT,
  arp_observer_intents_json TEXT,
  fib_verdicts_json TEXT,
  status TEXT NOT NULL,
  error TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(source, generation)
);
CREATE TABLE event_consumer_cursors (
  consumer TEXT PRIMARY KEY,
  cursor INTEGER NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE event_deliveries (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  event_id TEXT NOT NULL,
  peer TEXT NOT NULL,
  status TEXT NOT NULL,
  attempts INTEGER NOT NULL DEFAULT 0,
  last_attempt_at INTEGER,
  last_error TEXT,
  delivered_at INTEGER, event_expires_at INTEGER,
  UNIQUE(event_id, peer)
);
CREATE TABLE event_subscription_runs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  subscription TEXT NOT NULL,
  event_id TEXT NOT NULL,
  event_group TEXT NOT NULL,
  plugin TEXT NOT NULL,
  status TEXT NOT NULL,
  attempts INTEGER NOT NULL DEFAULT 0,
  started_at TEXT NOT NULL,
  completed_at TEXT,
  dynamic_source TEXT,
  dynamic_generation INTEGER,
  error TEXT,
  UNIQUE(subscription, event_id)
);
CREATE TABLE events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  api_version TEXT NOT NULL,
  kind TEXT NOT NULL,
  name TEXT NOT NULL,
  type TEXT NOT NULL,
  reason TEXT NOT NULL,
  message TEXT NOT NULL,
  generation INTEGER,
  created_at TEXT NOT NULL
, resource_name TEXT, severity TEXT, attributes TEXT, topic TEXT, source_kind TEXT, source_instance TEXT, resource_api_version TEXT, resource_kind TEXT);
CREATE TABLE federation_events (
  id TEXT PRIMARY KEY,
  group_name TEXT NOT NULL,
  source_node TEXT,
  type TEXT NOT NULL,
  subject TEXT,
  dedupe_key TEXT,
  payload TEXT,
  observed_at INTEGER,
  expires_at INTEGER,
  recorded_at INTEGER
);
CREATE TABLE generations (
  generation INTEGER PRIMARY KEY AUTOINCREMENT,
  started_at TEXT NOT NULL,
  finished_at TEXT,
  phase TEXT,
  warnings TEXT,
  config_hash TEXT,
  config_yaml TEXT
);
CREATE TABLE objects (
  api_version TEXT NOT NULL,
  kind TEXT NOT NULL,
  name TEXT NOT NULL,
  uid TEXT,
  resource_version INTEGER NOT NULL DEFAULT 1,
  observed_generation INTEGER,
  last_applied_path TEXT,
  status TEXT,
  created_at TEXT NOT NULL,
  modified_at TEXT NOT NULL,
  PRIMARY KEY(api_version, kind, name)
);
CREATE TABLE plugin_runs (
  id INTEGER PRIMARY KEY,
  plugin TEXT NOT NULL,
  trigger_type TEXT NOT NULL,
  trigger_topic TEXT,
  started_at TEXT NOT NULL,
  completed_at TEXT,
  exit_code INTEGER,
  status TEXT NOT NULL,
  stdout_digest TEXT,
  stderr TEXT,
  error TEXT
);
CREATE TABLE sam_enrollment_revoked_identities (
  identity_digest TEXT PRIMARY KEY,
  claim_source TEXT NOT NULL,
  revoked_at TEXT NOT NULL
);
CREATE INDEX action_executions_status ON action_executions(status, id);
CREATE INDEX event_deliveries_peer ON event_deliveries(peer, status);
CREATE INDEX event_subscription_runs_sub ON event_subscription_runs(subscription, status);
CREATE INDEX events_resource ON events(resource_kind, resource_name, id);
CREATE INDEX events_topic ON events(topic, id);
CREATE INDEX events_type ON events(type, id);
CREATE INDEX federation_events_group ON federation_events(group_name, observed_at);

-- Current named DHCP observation is functional recovery state, not retained
-- display history. The event journal transaction is its sole writer.
CREATE TABLE dhcp_lease_observations (
  source TEXT NOT NULL,
  ip TEXT NOT NULL,
  action TEXT NOT NULL,
  client TEXT NOT NULL,
  hostname TEXT NOT NULL,
  observed_at INTEGER NOT NULL,
  expires_at TEXT NOT NULL,
  infinite INTEGER NOT NULL CHECK(infinite IN (0,1)),
  PRIMARY KEY(source, ip)
);
