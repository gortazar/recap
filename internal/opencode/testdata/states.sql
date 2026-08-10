-- Hand-written, unlike real-store.sql: this machine only ever had one opencode session, and
-- it ended tidily. These rows cover the other shapes the status rules have to recognise,
-- built to the same schema (schema.sql, copied from the real store).
--
-- Timestamps are epoch milliseconds, as opencode stores them. 1786100000000 is the base;
-- the tests treat it as "now".

-- 1. A session killed while a tool was still running.
INSERT INTO session (id, project_id, parent_id, slug, directory, title, version, agent, model, time_created, time_updated, time_archived)
VALUES ('ses_pending', 'proj1', NULL, 'brave-otter', '/home/user/git/alpha', 'Make the suite green', '1.18.14', 'build', '{"id":"claude-opus-5","providerID":"anthropic"}', 1786099000000, 1786099900000, NULL);

INSERT INTO message (id, session_id, time_created, time_updated, data)
VALUES ('msg_p1', 'ses_pending', 1786099000000, 1786099000000, '{"role": "user", "time": {"created": 1786099000000}}');
INSERT INTO part (id, message_id, session_id, time_created, time_updated, data)
VALUES ('prt_p1', 'msg_p1', 'ses_pending', 1786099000010, 1786099000010, '{"type": "text", "text": "Make the failing scheduler test pass"}');

INSERT INTO message (id, session_id, time_created, time_updated, data)
VALUES ('msg_p2', 'ses_pending', 1786099800000, 1786099900000, '{"role": "assistant", "agent": "build", "modelID": "claude-opus-5", "providerID": "anthropic", "path": {"cwd": "/home/user/git/alpha"}, "time": {"created": 1786099800000}}');
INSERT INTO part (id, message_id, session_id, time_created, time_updated, data)
VALUES ('prt_p2', 'msg_p2', 'ses_pending', 1786099800010, 1786099800010, '{"type": "step-start", "snapshot": "abc"}');
INSERT INTO part (id, message_id, session_id, time_created, time_updated, data)
VALUES ('prt_p3', 'msg_p2', 'ses_pending', 1786099900000, 1786099900000, '{"type": "tool", "tool": "bash", "callID": "call_1", "state": {"status": "running", "input": {"command": "go test ./..."}}}');

-- 2. A session with a progress marker: two of three todos done.
INSERT INTO session (id, project_id, parent_id, slug, directory, title, version, agent, model, time_created, time_updated, time_archived)
VALUES ('ses_todo', 'proj2', NULL, 'quiet-fern', '/home/user/git/beta', 'Port the reader', '1.18.14', 'build', '{"id":"claude-opus-5","providerID":"anthropic"}', 1786099000000, 1786099500000, NULL);

INSERT INTO message (id, session_id, time_created, time_updated, data)
VALUES ('msg_t1', 'ses_todo', 1786099000000, 1786099000000, '{"role": "user", "time": {"created": 1786099000000}}');
INSERT INTO part (id, message_id, session_id, time_created, time_updated, data)
VALUES ('prt_t1', 'msg_t1', 'ses_todo', 1786099000010, 1786099000010, '{"type": "text", "text": "Port the reader to the new interface"}');

INSERT INTO message (id, session_id, time_created, time_updated, data)
VALUES ('msg_t2', 'ses_todo', 1786099400000, 1786099500000, '{"role": "assistant", "agent": "build", "modelID": "claude-opus-5", "time": {"created": 1786099400000, "completed": 1786099500000}, "finish": "stop"}');
INSERT INTO part (id, message_id, session_id, time_created, time_updated, data)
VALUES ('prt_t2', 'msg_t2', 'ses_todo', 1786099450000, 1786099450000, '{"type": "tool", "tool": "edit", "callID": "call_2", "state": {"status": "completed", "input": {"filePath": "/home/user/git/beta/reader.go"}}}');
INSERT INTO part (id, message_id, session_id, time_created, time_updated, data)
VALUES ('prt_t3', 'msg_t2', 'ses_todo', 1786099500000, 1786099500000, '{"type": "text", "text": "Two of the three steps are done."}');

INSERT INTO todo (session_id, content, status, priority, position, time_created, time_updated)
VALUES ('ses_todo', 'Read the store', 'completed', 'high', 0, 1786099000000, 1786099100000);
INSERT INTO todo (session_id, content, status, priority, position, time_created, time_updated)
VALUES ('ses_todo', 'Map to the domain type', 'completed', 'high', 1, 1786099000000, 1786099300000);
INSERT INTO todo (session_id, content, status, priority, position, time_created, time_updated)
VALUES ('ses_todo', 'Wire up discovery', 'in_progress', 'medium', 2, 1786099000000, 1786099400000);

-- 3. An archived session: opencode's only explicit "this one is over" marker.
INSERT INTO session (id, project_id, parent_id, slug, directory, title, version, agent, model, time_created, time_updated, time_archived)
VALUES ('ses_archived', 'proj2', NULL, 'still-pond', '/home/user/git/beta', 'Release 0.3', '1.18.14', 'build', '{"id":"claude-opus-5","providerID":"anthropic"}', 1786098000000, 1786098500000, 1786098600000);
INSERT INTO message (id, session_id, time_created, time_updated, data)
VALUES ('msg_a1', 'ses_archived', 1786098000000, 1786098000000, '{"role": "user", "time": {"created": 1786098000000}}');
INSERT INTO part (id, message_id, session_id, time_created, time_updated, data)
VALUES ('prt_a1', 'msg_a1', 'ses_archived', 1786098000010, 1786098000010, '{"type": "text", "text": "Cut the 0.3 release"}');

-- 4. A sub-session. recap does not report on sub-agents in v1, so this must not appear.
INSERT INTO session (id, project_id, parent_id, slug, directory, title, version, agent, model, time_created, time_updated, time_archived)
VALUES ('ses_child', 'proj1', 'ses_pending', 'small-leaf', '/home/user/git/alpha', 'Search the codebase', '1.18.14', 'general', '{"id":"claude-opus-5","providerID":"anthropic"}', 1786099850000, 1786099880000, NULL);

-- 5. A session whose model column is not the JSON recap expects. It must still be reported.
INSERT INTO session (id, project_id, parent_id, slug, directory, title, version, agent, model, time_created, time_updated, time_archived)
VALUES ('ses_oddmodel', 'proj3', NULL, 'odd-duck', '/home/user/git/gamma', 'Try the new model', '99.0.0', 'build', 'not-json-any-more', 1786099000000, 1786099100000, NULL);
