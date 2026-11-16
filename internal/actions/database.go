package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"lasertracker_server/internal"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrNotFound         = errors.New("DB record not found")
	ErrAlreadyExists    = errors.New("DB record already exists")
	ErrForeignKeyFailed = errors.New("Referenced DB record does not exist")
	ErrDatabase         = errors.New("Internal DB error")
)

var dbConnection, _ = InitDBConn()

// There was an import loop with auth so I just remade hashPin here :)
// Once again, there is most definitely a better solution
func hashPinButCooler(pin string) (string, error) {
	if len(pin) > 72 {
		return "", fmt.Errorf("%w: pin too long", ErrDatabase)
	}

	hashword, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrDatabase, err)
	}
	return string(hashword), nil
}

func handleError(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return fmt.Errorf("%w: %s", ErrAlreadyExists, pgErr.Detail)
		case "23503":
			return fmt.Errorf("%w: %s", ErrForeignKeyFailed, pgErr.Detail)
		}
	}

	return fmt.Errorf("%w: %v", ErrDatabase, err)
}

func InitDBConn() (*pgxpool.Pool, error) {
	conn, err := pgxpool.New(context.Background(), internal.GetConfig().DatabaseURL)
	if err != nil {
		return nil, handleError(err)
	}
	return conn, nil
}

func CreateTables() error {
	if dbConnection == nil {
		return fmt.Errorf("%w: no database connection, check LT_DATABASE_URL", ErrDatabase)
	}

	ctx := context.Background()
	myevilschema := `
	CREATE TABLE IF NOT EXISTS groups (
        group_name TEXT NOT NULL,
        event_key TEXT NOT NULL,
        team_number INTEGER,
		avatar_image TEXT,
        group_key TEXT PRIMARY KEY
    );

	CREATE TABLE IF NOT EXISTS members (
        group_key TEXT NOT NULL,
        username TEXT NOT NULL,
        display_name TEXT NOT NULL,
        pin_hash TEXT NOT NULL,
		token_ver INTEGER DEFAULT 1,
        job TEXT NOT NULL,
        role TEXT NOT NULL,
        location TEXT NOT NULL,
        is_admin INTEGER DEFAULT 0,
        CONSTRAINT fk_group FOREIGN KEY (group_key) REFERENCES groups(group_key) ON DELETE CASCADE,
        PRIMARY KEY (group_key, username)
    );

	CREATE TABLE IF NOT EXISTS logs (
        group_key TEXT NOT NULL,
        requesting_user TEXT NOT NULL,
		actioned_user TEXT NOT NULL DEFAULT '',
        action TEXT NOT NULL,
        timestamp TIMESTAMPTZ NOT NULL,
        CONSTRAINT fk_log_group FOREIGN KEY (group_key) REFERENCES groups(group_key) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS batteries (
        group_key TEXT NOT NULL,
        name TEXT NOT NULL,
        status TEXT NOT NULL,
        matches_used INTEGER NOT NULL,
        notes TEXT,
        status_timestamp TIMESTAMPTZ NOT NULL,
        CONSTRAINT fk_battery_group FOREIGN KEY (group_key) REFERENCES groups(group_key) ON DELETE CASCADE,
        PRIMARY KEY (group_key, name)
    );

	CREATE TABLE IF NOT EXISTS matches (
		event_key TEXT NOT NULL,
		match_key TEXT PRIMARY KEY,
		match_number INTEGER NOT NULL,
		red_teams TEXT NOT NULL,
		blue_teams TEXT NOT NULL,
		predicted_time TIMESTAMPTZ,
		actual_time TIMESTAMPTZ,
		comp_level TEXT NOT NULL DEFAULT 'qm',
		set_number INTEGER NOT NULL DEFAULT 1
	);

	CREATE TABLE IF NOT EXISTS tracked_events (
		event_key TEXT PRIMARY KEY,
		etag TEXT NOT NULL DEFAULT '',
		last_polled TIMESTAMPTZ,
		last_changed TIMESTAMPTZ,
		next_poll_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		fail_count INTEGER NOT NULL DEFAULT 0,
		start_date DATE,
		end_date DATE,
		webcasts_json TEXT NOT NULL DEFAULT '[]'
	)
	`
	_, err := dbConnection.Exec(ctx, myevilschema)
	return handleError(err)
}

func CreateGroup(ctx context.Context, groupInfo internal.Group, groupKey string, requestingUser string) error {
	if groupKey == "" {
		var err error
		groupKey, err = internal.GenerateRandomSecret(6)
		if err != nil {
			return err
		}
	}

	groupCreatinator := `INSERT INTO groups (group_name, event_key, team_number, avatar_image, group_key) VALUES ($1, $2, $3, $4, $5)`

	avatar, avatarerr := TeamAvatar(groupInfo.TeamNumber)
	if avatarerr != nil {
		avatar = "none"
	}

	_, gcerr := dbConnection.Exec(ctx, groupCreatinator, groupInfo.GroupName, groupInfo.EventKey, groupInfo.TeamNumber, avatar, groupKey)
	if gcerr != nil {
		return handleError(gcerr)
	}

	log := internal.LogEntry{
		GroupKey:       groupKey,
		RequestingUser: requestingUser,
		Action:         "CREATE GROUP",
		Timestamp:      time.Now(),
	}

	recordLog(ctx, log)

	return nil
}

func RemoveGroup(ctx context.Context, groupKey string, requestingUser string) error {
	query := `DELETE FROM groups WHERE group_key = $1`
	cmdTag, err := dbConnection.Exec(ctx, query, groupKey)
	if err != nil {
		return handleError(err)
	}
	if cmdTag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

func ChangeGroupEventKey(ctx context.Context, groupKey string, newEventKey string, requestingUser string) error {
	query := `UPDATE Groups SET event_key = $1 WHERE group_key = $2`
	cmdTag, err := dbConnection.Exec(ctx, query, newEventKey, groupKey)

	if err != nil {
		return handleError(err)
	}
	if cmdTag.RowsAffected() == 0 {
		return ErrNotFound
	}

	log := internal.LogEntry{
		GroupKey:       groupKey,
		RequestingUser: requestingUser,
		Action:         "CHANGE EVENT",
		Timestamp:      time.Now(),
	}

	recordLog(ctx, log)

	return nil
}

func AddMember(ctx context.Context, memberInfo internal.PrivateMember) error {
	adminInt := 0
	username := memberInfo.Username
	if memberInfo.IsAdmin {
		adminInt = 1
	}

	query := `INSERT INTO members (group_key, username, display_name, pin_hash, job, role, location, is_admin, token_ver) 
	          VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`

	_, err := dbConnection.Exec(ctx, query, memberInfo.GroupKey, username, memberInfo.DisplayName, memberInfo.PinHash, memberInfo.Job, memberInfo.Role, memberInfo.Location, adminInt, 1)
	if err != nil {
		return handleError(err)
	}

	log := internal.LogEntry{
		GroupKey:       memberInfo.GroupKey,
		RequestingUser: memberInfo.Username,
		Action:         "ADD MEMBER",
		Timestamp:      time.Now(),
	}

	recordLog(ctx, log)

	return nil
}

func RemoveMember(ctx context.Context, groupKey string, username string, requestingUser string) error {
	query := `DELETE FROM members WHERE group_key = $1 AND username = $2`
	cmdTag, err := dbConnection.Exec(ctx, query, groupKey, username)
	if err != nil {
		return handleError(err)
	}
	if cmdTag.RowsAffected() == 0 {
		return ErrNotFound
	}

	log := internal.LogEntry{
		GroupKey:       groupKey,
		RequestingUser: requestingUser,
		ActionedUser:   username,
		Action:         "REMOVE MEMBER",
		Timestamp:      time.Now(),
	}

	recordLog(ctx, log)

	return nil
}

func UpdateMember(ctx context.Context, newMemberData internal.PublicMember, requestingUser string) error {
	query := `
		UPDATE members 
		SET display_name = $1, job = $2, location = $3, is_admin = $4 
		WHERE group_key = $5 AND username = $6
	`

	var adminInt int
	if newMemberData.IsAdmin {
		adminInt = 1
	} else {
		adminInt = 0
	}

	cmdTag, err := dbConnection.Exec(ctx, query, newMemberData.DisplayName, newMemberData.Job, newMemberData.Location, adminInt, newMemberData.GroupKey, newMemberData.Username)
	if err != nil {
		return handleError(err)
	}
	if cmdTag.RowsAffected() == 0 {
		return ErrNotFound
	}

	log := internal.LogEntry{
		GroupKey:       newMemberData.GroupKey,
		RequestingUser: requestingUser,
		ActionedUser:   newMemberData.Username,
		Action:         "UPDATE MEMBER",
		Timestamp:      time.Now(),
	}

	recordLog(ctx, log)

	return nil
}

func ChangePin(ctx context.Context, request internal.PinChangeRequest, requestingUser string) error {
	hashed, err := hashPinButCooler(request.NewPin)
	if err != nil {
		return err
	}
	query := `UPDATE members SET pin_hash = $1, token_ver = token_ver + 1 WHERE group_key = $2 AND username = $3`
	cmdTag, err := dbConnection.Exec(ctx, query, hashed, request.GroupKey, request.Username)
	if err != nil {
		return handleError(err)
	}
	if cmdTag.RowsAffected() == 0 {
		return ErrNotFound
	}

	log := internal.LogEntry{
		GroupKey:       request.GroupKey,
		RequestingUser: requestingUser,
		ActionedUser:   request.Username,
		Action:         "CHANGE PIN",
		Timestamp:      time.Now(),
	}

	recordLog(ctx, log)

	return nil
}

func ChangeRole(ctx context.Context, groupKey string, username string, newRole string, requestingUser string) error {
	query := `UPDATE members SET role = $1 WHERE group_key = $2 AND username = $3`
	cmdTag, err := dbConnection.Exec(ctx, query, newRole, groupKey, username)
	if err != nil {
		return handleError(err)
	}
	if cmdTag.RowsAffected() == 0 {
		return ErrNotFound
	}

	log := internal.LogEntry{
		GroupKey:       groupKey,
		RequestingUser: requestingUser,
		ActionedUser:   username,
		Action:         "CHANGE MEMBER ROLE",
		Timestamp:      time.Now(),
	}

	recordLog(ctx, log)

	return nil
}

func AddBattery(ctx context.Context, batteryInfo internal.Battery, requestingUser string) error {
	query := `INSERT INTO batteries (group_key, name, status, matches_used, notes, status_timestamp) 
	          VALUES ($1, $2, $3, $4, $5, $6)`

	_, err := dbConnection.Exec(ctx, query, batteryInfo.GroupKey, batteryInfo.Name, batteryInfo.Status, batteryInfo.MatchesUsed, batteryInfo.Notes, batteryInfo.Timestamp)
	if err != nil {
		return handleError(err)
	}

	log := internal.LogEntry{
		GroupKey:       batteryInfo.GroupKey,
		RequestingUser: requestingUser,
		Action:         "ADD BATTERY " + batteryInfo.Name,
		Timestamp:      time.Now(),
	}

	recordLog(ctx, log)

	return nil
}

func UpdateBattery(ctx context.Context, battery internal.Battery, requestingUser string) error {
	query := `
		UPDATE batteries 
		SET status = $1, matches_used = $2, notes = $3, status_timestamp = $4 
		WHERE group_key = $5 AND name = $6
	`

	cmdTag, err := dbConnection.Exec(ctx, query, battery.Status, battery.MatchesUsed, battery.Notes, battery.Timestamp, battery.GroupKey, battery.Name)
	if err != nil {
		return handleError(err)
	}
	if cmdTag.RowsAffected() == 0 {
		return ErrNotFound
	}

	log := internal.LogEntry{
		GroupKey:       battery.GroupKey,
		RequestingUser: requestingUser,
		Action:         "UPDATE BATTERY " + battery.Name,
		Timestamp:      time.Now(),
	}

	recordLog(ctx, log)

	return nil
}

func RemoveBattery(ctx context.Context, groupKey string, batteryName string, requestingUser string) error {
	query := `DELETE FROM batteries WHERE group_key = $1 AND name = $2`
	cmdTag, err := dbConnection.Exec(ctx, query, groupKey, batteryName)
	if err != nil {
		return handleError(err)
	}
	if cmdTag.RowsAffected() == 0 {
		return ErrNotFound
	}

	log := internal.LogEntry{
		GroupKey:       groupKey,
		RequestingUser: requestingUser,
		Action:         "REMOVE BATTERY " + batteryName,
		Timestamp:      time.Now(),
	}

	recordLog(ctx, log)

	return nil
}

func recordLog(ctx context.Context, entry internal.LogEntry) {
	if err := AddLogEntry(ctx, entry); err != nil {
		fmt.Println("failed to write log entry:", err)
	}
}

func AddLogEntry(ctx context.Context, entry internal.LogEntry) error {
	query := `INSERT INTO logs (group_key, requesting_user, actioned_user, action, timestamp) VALUES ($1, $2, $3, $4, $5)`

	_, err := dbConnection.Exec(ctx, query, entry.GroupKey, entry.RequestingUser, entry.ActionedUser, entry.Action, entry.Timestamp)

	return handleError(err)
}

func GetAllLogs(ctx context.Context, groupKey string) ([]*internal.LogEntry, error) {
	query := `SELECT group_key, requesting_user, actioned_user, action, timestamp FROM logs WHERE group_key = $1 ORDER BY timestamp DESC LIMIT 200`
	rows, err := dbConnection.Query(ctx, query, groupKey)
	if err != nil {
		return nil, handleError(err)
	}
	defer rows.Close()

	logs := make([]*internal.LogEntry, 0)
	for rows.Next() {
		var log internal.LogEntry
		if err := rows.Scan(&log.GroupKey, &log.RequestingUser, &log.ActionedUser, &log.Action, &log.Timestamp); err != nil {
			return nil, handleError(err)
		}
		logs = append(logs, &log)
	}
	if err := rows.Err(); err != nil {
		return nil, handleError(err)
	}

	return logs, nil
}

func GetGroup(ctx context.Context, groupKey string) (*internal.Group, error) {
	query := `SELECT group_name, event_key, team_number, avatar_image, group_key FROM groups WHERE group_key = $1`
	row := dbConnection.QueryRow(ctx, query, groupKey)

	var group internal.Group
	err := row.Scan(&group.GroupName, &group.EventKey, &group.TeamNumber, &group.AvatarImage, &group.GroupKey)
	if err != nil {
		return nil, handleError(err)
	}

	return &group, nil
}

func GetBattery(ctx context.Context, groupKey string, name string) (*internal.Battery, error) {
	query := `SELECT group_key, name, status, matches_used, notes, status_timestamp FROM batteries WHERE group_key = $1 AND name = $2`
	row := dbConnection.QueryRow(ctx, query, groupKey, name)

	var battery internal.Battery
	err := row.Scan(&battery.GroupKey, &battery.Name, &battery.Status, &battery.MatchesUsed, &battery.Notes, &battery.Timestamp)
	if err != nil {
		return nil, handleError(err)
	}

	return &battery, nil
}

func GetAllBatteries(ctx context.Context, groupKey string) ([]*internal.Battery, error) {
	query := `SELECT group_key, name, status, matches_used, notes, status_timestamp FROM batteries WHERE group_key = $1`
	rows, err := dbConnection.Query(ctx, query, groupKey)
	if err != nil {
		return nil, handleError(err)
	}
	defer rows.Close()

	batteries := make([]*internal.Battery, 0)
	for rows.Next() {
		var battery internal.Battery
		if err := rows.Scan(&battery.GroupKey, &battery.Name, &battery.Status, &battery.MatchesUsed, &battery.Notes, &battery.Timestamp); err != nil {
			return nil, handleError(err)
		}
		batteries = append(batteries, &battery)
	}
	if err := rows.Err(); err != nil {
		return nil, handleError(err)
	}

	return batteries, nil
}

func GetMember(ctx context.Context, groupKey string, username string) (*internal.PublicMember, error) {
	query := `SELECT group_key, username, display_name, job, role, location, is_admin FROM members WHERE group_key = $1 AND username = $2`
	row := dbConnection.QueryRow(ctx, query, groupKey, username)

	var member internal.PublicMember
	var isAdminInt int
	err := row.Scan(&member.GroupKey, &member.Username, &member.DisplayName, &member.Job, &member.Role, &member.Location, &isAdminInt)
	if err != nil {
		return nil, handleError(err)
	}

	if isAdminInt == 1 {
		member.IsAdmin = true
	} else {
		member.IsAdmin = false
	}

	return &member, nil
}

func GetAllMembers(ctx context.Context, groupKey string) ([]*internal.PublicMember, error) {
	query := `SELECT group_key, username, display_name, job, role, location, is_admin FROM members WHERE group_key = $1`
	rows, err := dbConnection.Query(ctx, query, groupKey)
	if err != nil {
		return nil, handleError(err)
	}
	defer rows.Close()

	members := make([]*internal.PublicMember, 0)
	for rows.Next() {
		var member internal.PublicMember
		var isAdminInt int
		if err := rows.Scan(&member.GroupKey, &member.Username, &member.DisplayName, &member.Job, &member.Role, &member.Location, &isAdminInt); err != nil {
			return nil, handleError(err)
		}
		member.IsAdmin = isAdminInt == 1
		members = append(members, &member)
	}
	if err := rows.Err(); err != nil {
		return nil, handleError(err)
	}

	return members, nil
}

func CountAdmins(ctx context.Context, groupKey string) (int, error) {
	query := `SELECT COUNT(*) FROM members WHERE group_key = $1 AND is_admin = 1`
	var count int
	if err := dbConnection.QueryRow(ctx, query, groupKey).Scan(&count); err != nil {
		return 0, handleError(err)
	}
	return count, nil
}

func GetMemberPinHash(ctx context.Context, groupKey string, username string) (string, error) {
	query := `SELECT pin_hash FROM members WHERE group_key = $1 AND username = $2`
	row := dbConnection.QueryRow(ctx, query, groupKey, username)

	var pinHash string
	err := row.Scan(&pinHash)
	if err != nil {
		return "", handleError(err)
	}

	return pinHash, nil
}

func GetMemberTokenVer(ctx context.Context, groupKey string, username string) (int, error) {
	query := `SELECT token_ver FROM members WHERE group_key = $1 AND username = $2`
	row := dbConnection.QueryRow(ctx, query, groupKey, username)

	var tokenVer int
	err := row.Scan(&tokenVer)
	if err != nil {
		return 0, handleError(err)
	}

	return tokenVer, nil
}

func GetTeamMatches(ctx context.Context, teamInt int, eventKey string) ([]*internal.Match, error) {
	// Ordered by schedule, since match_number restarts at 1 for each comp level (qm1, sf1m1, f1m1)
	query := `
		SELECT event_key, match_key, match_number, red_teams, blue_teams, predicted_time, actual_time, comp_level, set_number
		FROM matches 
		WHERE event_key = $1 
		ORDER BY CASE comp_level WHEN 'qm' THEN 1 WHEN 'ef' THEN 2 WHEN 'qf' THEN 3
		WHEN 'sf' THEN 4 WHEN 'f' THEN 5 ELSE 6 END,
    	set_number, match_number
	`

	rows, err := dbConnection.Query(ctx, query, eventKey)
	if err != nil {
		return nil, handleError(err)
	}
	defer rows.Close()

	targetTeamStr := fmt.Sprintf("frc%d", teamInt)
	matches := make([]*internal.Match, 0)

	for rows.Next() {
		var match internal.Match
		var redTeamsJSON, blueTeamsJSON string

		if err := rows.Scan(&match.EventKey, &match.MatchKey, &match.MatchNum, &redTeamsJSON, &blueTeamsJSON, &match.PredictedTime, &match.ActualTime); err != nil {
			return nil, handleError(err)
		}

		var redList, blueList []string
		if err := json.Unmarshal([]byte(redTeamsJSON), &redList); err != nil {
			continue
		}
		if err := json.Unmarshal([]byte(blueTeamsJSON), &blueList); err != nil {
			continue
		}

		isTeamInMatch := false
		for _, team := range redList {
			if team == targetTeamStr {
				isTeamInMatch = true
				break
			}
		}
		if !isTeamInMatch {
			for _, team := range blueList {
				if team == targetTeamStr {
					isTeamInMatch = true
					break
				}
			}
		}

		if !isTeamInMatch {
			continue
		}

		if len(redList) >= 3 {
			match.Red1, _ = strconv.Atoi(strings.TrimPrefix(redList[0], "frc"))
			match.Red2, _ = strconv.Atoi(strings.TrimPrefix(redList[1], "frc"))
			match.Red3, _ = strconv.Atoi(strings.TrimPrefix(redList[2], "frc"))
		}
		if len(blueList) >= 3 {
			match.Blue1, _ = strconv.Atoi(strings.TrimPrefix(blueList[0], "frc"))
			match.Blue2, _ = strconv.Atoi(strings.TrimPrefix(blueList[1], "frc"))
			match.Blue3, _ = strconv.Atoi(strings.TrimPrefix(blueList[2], "frc"))
		}

		matches = append(matches, &match)
	}

	if err := rows.Err(); err != nil {
		return nil, handleError(err)
	}

	return matches, nil
}

func EnsureTrackedEvent(ctx context.Context, eventKey string) error {
	_, err := dbConnection.Exec(ctx, `INSERT INTO tracked_events (event_key) VALUES ($1) ON CONFLICT DO NOTHING`, eventKey)
	return handleError(err)
}

func EnsureTrackedEventsForGroups(ctx context.Context) error {
	_, err := dbConnection.Exec(ctx, `
		INSERT INTO tracked_events (event_key)
		SELECT DISTINCT event_key FROM groups WHERE event_key <> ''
		ON CONFLICT DO NOTHING
	`)
	return handleError(err)
}

func MarkDueNow(ctx context.Context, eventKey string) error {
	_, err := dbConnection.Exec(ctx, `UPDATE tracked_events SET next_poll_at = now() WHERE event_key = $1`, eventKey)
	return handleError(err)
}

func ClaimDueEvents(ctx context.Context, limit int) ([]internal.TrackedEvent, error) {
	query := `
		UPDATE tracked_events SET next_poll_at = now() + interval '2 minutes'
		WHERE event_key IN (
			SELECT t.event_key FROM tracked_events t
			WHERE t.next_poll_at <= now()
			AND EXISTS (SELECT 1 FROM groups g WHERE g.event_key = t.event_key)
			ORDER BY t.next_poll_at
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING event_key, etag, last_polled, last_changed, next_poll_at, fail_count, start_date, end_date, webcasts_json
	`

	rows, err := dbConnection.Query(ctx, query, limit)
	if err != nil {
		return nil, handleError(err)
	}
	defer rows.Close()

	events := make([]internal.TrackedEvent, 0)
	for rows.Next() {
		var ev internal.TrackedEvent
		if err := rows.Scan(&ev.EventKey, &ev.ETag, &ev.LastPolled, &ev.LastChanged, &ev.NextPollAt, &ev.FailCount, &ev.StartDate, &ev.EndDate, &ev.WebcastsJSON); err != nil {
			return nil, handleError(err)
		}
		events = append(events, ev)
	}

	if err := rows.Err(); err != nil {
		return nil, handleError(err)
	}

	return events, nil
}

func RecordPoll(ctx context.Context, eventKey, etag string, changed bool, next time.Time, failCount int) error {
	query := `
		UPDATE tracked_events SET
			etag = CASE WHEN $2 <> '' THEN $2 ELSE etag END,
			last_polled = now(),
			last_changed = CASE WHEN $3 THEN now() ELSE last_changed END,
			next_poll_at = $4,
			fail_count = $5
		WHERE event_key = $1
	`
	_, err := dbConnection.Exec(ctx, query, eventKey, etag, changed, next, failCount)
	return handleError(err)
}

func SaveEventInfo(ctx context.Context, eventKey string, start, end *time.Time, webcastsJSON string) error {
	query := `UPDATE tracked_events SET start_date = $2, end_date = $3, webcasts_json = $4 WHERE event_key = $1`
	_, err := dbConnection.Exec(ctx, query, eventKey, start, end, webcastsJSON)
	return handleError(err)
}

func GetWebcasts(ctx context.Context, eventKey string) ([]internal.Stream, error) {
	var webcastsJSON string
	err := dbConnection.QueryRow(ctx, `SELECT webcasts_json FROM tracked_events WHERE event_key = $1`, eventKey).Scan(&webcastsJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return []internal.Stream{}, nil
	}
	if err != nil {
		return nil, handleError(err)
	}

	var casts []struct {
		Type    string `json:"type"`
		Channel string `json:"channel"`
	}
	if err := json.Unmarshal([]byte(webcastsJSON), &casts); err != nil {
		return nil, fmt.Errorf("%w: bad webcasts_json for %s: %v", ErrDatabase, eventKey, err)
	}

	streams := make([]internal.Stream, 0, len(casts))
	for _, cast := range casts {
		streamURL := cast.Channel
		switch cast.Type {
		case "youtube":
			streamURL = "https://www.youtube.com/embed/" + cast.Channel
		case "twitch":
			streamURL = "https://player.twitch.tv/?channel=" + cast.Channel + "&parent=lasertracker.laserrobotics.org"
		}
		streams = append(streams, internal.Stream{Name: cast.Type, URL: streamURL, Type: cast.Channel})
	}

	return streams, nil
}

func GroupsAtEvent(ctx context.Context, eventKey string) ([]internal.GroupRef, error) {
	rows, err := dbConnection.Query(ctx, `SELECT group_key, team_number FROM groups WHERE event_key = $1`, eventKey)
	if err != nil {
		return nil, handleError(err)
	}
	defer rows.Close()

	groups := make([]internal.GroupRef, 0)
	for rows.Next() {
		var g internal.GroupRef
		if err := rows.Scan(&g.GroupKey, &g.TeamNumber); err != nil {
			return nil, handleError(err)
		}
		groups = append(groups, g)
	}

	if err := rows.Err(); err != nil {
		return nil, handleError(err)
	}

	return groups, nil
}

func UpsertMatches(ctx context.Context, matches []internal.Match) ([]string, error) {
	query := `
		INSERT INTO matches (event_key, match_key, match_number, red_teams, blue_teams, predicted_time, actual_time, comp_level, set_number)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (match_key) DO UPDATE SET
			event_key = EXCLUDED.event_key,
			match_number = EXCLUDED.match_number,
			red_teams = EXCLUDED.red_teams,
			blue_teams = EXCLUDED.blue_teams,
			predicted_time = EXCLUDED.predicted_time,
			actual_time = EXCLUDED.actual_time,
			comp_level = EXCLUDED.comp_level,
			set_number = EXCLUDED.set_number
		WHERE (matches.red_teams, matches.blue_teams, matches.predicted_time, matches.actual_time,
			matches.match_number, matches.comp_level, matches.set_number)
			IS DISTINCT FROM
			(EXCLUDED.red_teams, EXCLUDED.blue_teams, EXCLUDED.predicted_time,
			EXCLUDED.actual_time, EXCLUDED.match_number, EXCLUDED.comp_level, EXCLUDED.set_number)
		RETURNING match_key
	`

	tx, err := dbConnection.Begin(ctx)
	if err != nil {
		return nil, handleError(err)
	}
	defer tx.Rollback(ctx)

	changedKeys := make([]string, 0)
	for _, match := range matches {
		redJSON, err := json.Marshal(teamKeyList(match.Red1, match.Red2, match.Red3))
		if err != nil {
			return nil, err
		}
		blueJSON, err := json.Marshal(teamKeyList(match.Blue1, match.Blue2, match.Blue3))
		if err != nil {
			return nil, err
		}

		var key string
		err = tx.QueryRow(ctx, query, match.EventKey, match.MatchKey, match.MatchNum, string(redJSON), string(blueJSON), match.PredictedTime, match.ActualTime, match.CompLevel, match.SetNumber).Scan(&key)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, handleError(err)
		}
		changedKeys = append(changedKeys, key)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, handleError(err)
	}

	return changedKeys, nil
}

func teamKeyList(teams ...int) []string {
	list := []string{}
	for _, t := range teams {
		if t != 0 {
			list = append(list, "frc"+strconv.Itoa(t))
		}
	}
	return list
}
