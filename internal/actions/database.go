package actions

import (
	"context"
	"errors"
	"fmt"
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
func hashPinButCooler(pin string) string {
	hashword, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.DefaultCost)
	if err != nil {
		fmt.Println(err)
	}
	return string(hashword)
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
        username TEXT NOT NULL,
        action TEXT NOT NULL,
        timestamp INTEGER NOT NULL,
        CONSTRAINT fk_log_group FOREIGN KEY (group_key) REFERENCES groups(group_key) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS batteries (
        group_key TEXT NOT NULL,
        name TEXT NOT NULL,
        status TEXT NOT NULL,
        matches_used INTEGER NOT NULL,
        notes TEXT,
        status_timestamp INTEGER NOT NULL,
        CONSTRAINT fk_battery_group FOREIGN KEY (group_key) REFERENCES groups(group_key) ON DELETE CASCADE,
        PRIMARY KEY (group_key, name)
    )
	`

	_, err := dbConnection.Exec(ctx, myevilschema)
	return handleError(err)
}

func CreateGroup(ctx context.Context, groupInfo internal.Group, groupKey string) error {
	if groupKey == "" {
		var err error
		groupKey, err = internal.GenerateRandomSecret(6)
		if err != nil {
			return err
		}
	}

	groupCreatinator := `INSERT INTO groups (group_name, event_key, team_number, avatar_image, group_key) VALUES ($1, $2, $3, $4)`

	avatar, avatarerr := teamAvatar(groupInfo.TeamNumber)
	if avatarerr != nil {
		avatar = "none"
	}

	_, gcerr := dbConnection.Exec(ctx, groupCreatinator, groupInfo.GroupName, groupInfo.EventKey, groupInfo.TeamNumber, avatar, groupKey)
	return handleError(gcerr)
}

func RemoveGroup(ctx context.Context, groupKey string) error {
	query := `DELETE FROM groups WHERE group_key = $1`
	cmdTag, err := dbConnection.Exec(ctx, query, groupKey)
	if cmdTag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return handleError(err)
}

func ChangeGroupEventKey(ctx context.Context, groupKey string, newEventKey string) error {
	query := `UPDATE Groups SET event_key = $1 WHERE group_key = $2`
	cmdTag, err := dbConnection.Exec(ctx, query, newEventKey, groupKey)

	if cmdTag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return handleError(err)
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
	return handleError(err)
}

func RemoveMember(ctx context.Context, groupKey string, username string) error {
	query := `DELETE FROM members WHERE group_key = $1 AND username = $2`
	cmdTag, err := dbConnection.Exec(ctx, query, groupKey, username)
	if cmdTag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return handleError(err)
}

func UpdateMember(ctx context.Context, newMemberData internal.PublicMember) error {
	query := `
		UPDATE members 
		SET display_name = $1, job = $2, role = $3, location = $4, is_admin = $5 
		WHERE group_key = $6 AND username = $7
	`

	var adminInt int
	if newMemberData.IsAdmin {
		adminInt = 1
	} else {
		adminInt = 0
	}

	cmdTag, err := dbConnection.Exec(ctx, query, newMemberData.DisplayName, newMemberData.Job, newMemberData.Role, newMemberData.Location, adminInt, newMemberData.GroupKey, newMemberData.Username)
	if cmdTag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return handleError(err)
}

func ChangePin(ctx context.Context, request internal.PinChangeRequest) error {
	hashed := hashPinButCooler(request.NewPin)
	query := `UPDATE members SET pin_hash = $1, token_ver = token_ver + 1 WHERE group_key = $2 AND username = $3`
	cmdTag, err := dbConnection.Exec(ctx, query, hashed, request.GroupKey, request.Username)
	if cmdTag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return handleError(err)
}

func ChangeRole(ctx context.Context, groupKey string, username string, newRole string) error {
	query := `UPDATE members SET role = $1 WHERE group_key = $2 AND username = $3`
	cmdTag, err := dbConnection.Exec(ctx, query, newRole, groupKey, username)
	if cmdTag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return handleError(err)
}

func AddBattery(ctx context.Context, batteryInfo internal.Battery) error {
	query := `INSERT INTO batteries (group_key, name, status, matches_used, notes, status_timestamp) 
	          VALUES ($1, $2, $3, $4, $5, $6)`

	_, err := dbConnection.Exec(ctx, query, batteryInfo.GroupKey, batteryInfo.Name, batteryInfo.Status, batteryInfo.MatchesUsed, batteryInfo.Notes, batteryInfo.Timestamp.Unix())
	return handleError(err)
}

func UpdateBattery(ctx context.Context, battery internal.Battery) error {
	query := `
		UPDATE batteries 
		SET status = $1, matches_used = $2, notes = $3, status_timestamp = $4 
		WHERE group_key = $5 AND name = $6
	`

	cmdTag, err := dbConnection.Exec(ctx, query, battery.Status, battery.MatchesUsed, battery.Notes, battery.Timestamp.Unix(), battery.GroupKey, battery.Name)
	if cmdTag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return handleError(err)
}

func RemoveBattery(ctx context.Context, groupKey, batteryName string) error {
	query := `DELETE FROM batteries WHERE group_key = $1 AND name = $2`
	cmdTag, err := dbConnection.Exec(ctx, query, groupKey, batteryName)
	if cmdTag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return handleError(err)
}

func AddLogEntry(ctx context.Context, entry internal.LogEntry) error {
	query := `INSERT INTO logs (group_key, username, action, timestamp) VALUES ($1, $2, $3, $4)`

	_, err := dbConnection.Exec(ctx, query, entry.GroupKey, entry.Username, entry.Action, entry.Timestamp)

	return handleError(err)
}

func GetAllLogs(ctx context.Context, groupKey string) ([]*internal.LogEntry, error) {
	query := `SELECT group_key, username, action, timestamp FROM logs WHERE group_key = $1`
	rows, err := dbConnection.Query(ctx, query, groupKey)
	if err != nil {
		return nil, handleError(err)
	}
	defer rows.Close()

	logs := make([]*internal.LogEntry, 0)
	for rows.Next() {
		var log internal.LogEntry
		if err := rows.Scan(&log.GroupKey, &log.Username, &log.Action, &log.Timestamp); err != nil {
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
	var timestamp int
	err := row.Scan(&battery.GroupKey, &battery.Name, &battery.Status, &battery.MatchesUsed, &battery.Notes, &timestamp)
	if err != nil {
		return nil, handleError(err)
	}

	battery.Timestamp = time.Unix(int64(timestamp), 0)

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
