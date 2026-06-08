package store

func sessionProjectFallbackSQL(placeholders string) string {
	return `(
		SELECT session_id,
		       if(single_project, single_project_path, '') AS project_path,
		       if(single_project, single_project_key, '') AS project_key,
		       single_project
		FROM (
			SELECT session_id,
			       minIf(project_path, project_key != '') AS single_project_path,
			       minIf(project_key, project_key != '') AS single_project_key,
			       toUInt8(uniqExactIf(project_key, project_key != '') = 1) AS single_project
			FROM (
				SELECT latest_session_id AS session_id,
				       cwd AS project_path,
				       ` + projectKeySQL("cwd") + ` AS project_key
				FROM (
					SELECT event_uid,
					       argMax(session_id, captured_at) AS latest_session_id,
					       argMax(cwd, captured_at) AS cwd
					FROM activity_events
					WHERE session_id IN (` + placeholders + `)
					GROUP BY event_uid
				)
				WHERE latest_session_id != ''
			)
			GROUP BY session_id
		)
	)`
}
