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
)

var (
	ErrNotFound         = errors.New("DB record not found")
	ErrAlreadyExists    = errors.New("DB record already exists")
	ErrForeignKeyFailed = errors.New("Referenced DB record does not exist")
	ErrDatabase         = errors.New("Internal DB error")
)

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

func initDBConn(cfg internal.Config) (*pgxpool.Pool, error) {
	conn, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		return nil, handleError(err)
	}
	return conn, nil
}

func createTables(ctx context.Context, conn *pgxpool.Pool) error {
	myevilschema := `
	CREATE TABLE IF NOT EXISTS groups (
        group_name TEXT NOT NULL,
        event_key TEXT NOT NULL,
        team_number INTEGER,
        group_key TEXT PRIMARY KEY
    );

	CREATE TABLE IF NOT EXISTS members (
        group_key TEXT NOT NULL,
        username TEXT NOT NULL,
        display_name TEXT NOT NULL,
        pin_hash TEXT NOT NULL,
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
        timestamp INTEGER NOT NULL
        CONSTRAINT fk_log_group FOREIGN KEY (group_key) REFERENCES groups(group_key) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS batteries (
        group_key TEXT NOT NULL,
        name TEXT NOT NULL,
        status TEXT NOT NULL,
        matches_used INTEGER NOT NULL,
        notes TEXT,
        status_timestamp INTEGER NOT NULL
        CONSTRAINT fk_battery_group FOREIGN KEY (group_key) REFERENCES groups(group_key) ON DELETE CASCADE,
        PRIMARY KEY (group_key, name)
    )
	`

	_, err := conn.Exec(ctx, myevilschema)
	return handleError(err)
}

func createGroup(ctx context.Context, conn *pgxpool.Pool, groupInfo internal.Group) error {
	groupKey, gkerr := internal.GenerateRandomSecret(6)
	if gkerr != nil {
		return gkerr
	}

	groupCreatinator := `INSERT INTO groups (group_name, event_key, team_number, group_key) VALUES ($1, $2, $3, $4)`

	_, gcerr := conn.Exec(ctx, groupCreatinator, groupInfo.GroupName, groupInfo.EventKey, groupInfo.TeamNumber, groupKey)
	return handleError(gcerr)
}

func removeGroup(ctx context.Context, conn *pgxpool.Pool, groupKey string) error {
	query := `DELETE FROM groups WHERE group_key = $1`
	cmdTag, err := conn.Exec(ctx, query, groupKey)
	if cmdTag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return handleError(err)
}

func changeGroupEventKey(ctx context.Context, conn *pgxpool.Pool, groupKey string, newEventKey string) error {
	query := `UPDATE Groups SET event_key = $1 WHERE group_key = $2`
	cmdTag, err := conn.Exec(ctx, query, newEventKey, groupKey)

	if cmdTag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return handleError(err)
}

func addMember(ctx context.Context, conn *pgxpool.Pool, memberInfo internal.Member) error {
	adminInt := 0
	username := memberInfo.Username + "-" + memberInfo.GroupKey // Instead of member id numbers we have name-groupkey! Keeps unique users and removes an unecessary thing
	if memberInfo.IsAdmin {
		adminInt = 1
	}

	query := `INSERT INTO members (group_key, username, display_name, pin_hash, job, role, location, is_admin) 
	          VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	_, err := conn.Exec(ctx, query, memberInfo.GroupKey, username, memberInfo.DisplayName, memberInfo.PinHash, memberInfo.Job, memberInfo.Role, memberInfo.Location, adminInt)
	return handleError(err)
}

func removeMember(ctx context.Context, conn *pgxpool.Pool, groupKey string, username string) error {
	query := `DELETE FROM members WHERE group_key = $1 AND username = $2`
	cmdTag, err := conn.Exec(ctx, query, groupKey, username)
	if cmdTag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return handleError(err)
}

func changeMemberStatus(ctx context.Context, conn *pgxpool.Pool, groupKey string, username string, job string, location string) error {
	query := `UPDATE members SET job = $1, location = $2 WHERE group_key = $3 AND username = $4`
	cmdTag, err := conn.Exec(ctx, query, job, location, groupKey, username)
	if cmdTag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return handleError(err)
}

func changeAdminStatus(ctx context.Context, conn *pgxpool.Pool, groupKey string, username string, isAdmin bool) error {
	adminInt := 0
	if isAdmin {
		adminInt = 1
	}

	query := `UPDATE members SET is_admin = $1 WHERE group_key = $2 AND username = $3`
	cmdTag, err := conn.Exec(ctx, query, adminInt, groupKey, username)
	if cmdTag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return handleError(err)
}

func changePin(ctx context.Context, conn *pgxpool.Pool, groupKey string, username string, newPinHash string) error {
	query := `UPDATE members SET pin_hash = $1 WHERE group_key = $2 AND username = $3`
	cmdTag, err := conn.Exec(ctx, query, newPinHash, groupKey, username)
	if cmdTag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return handleError(err)
}

func changeRole(ctx context.Context, conn *pgxpool.Pool, groupKey string, username string, newRole string) error {
	query := `UPDATE members SET role = $1 WHERE group_key = $2 AND username = $3`
	cmdTag, err := conn.Exec(ctx, query, newRole, groupKey, username)
	if cmdTag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return handleError(err)
}

func addBattery(ctx context.Context, conn *pgxpool.Pool, batteryInfo internal.Battery) error {
	query := `INSERT INTO batteries (group_key, name, status, matches_used, notes, status_timestamp) 
	          VALUES ($1, $2, $3, $4, $5, $6)`

	_, err := conn.Exec(ctx, query, batteryInfo.GroupKey, batteryInfo.Name, batteryInfo.Status, batteryInfo.MatchesUsed, batteryInfo.Notes, batteryInfo.Timestamp.Unix())
	return handleError(err)
}

func changeBatteryStatus(ctx context.Context, conn *pgxpool.Pool, groupKey string, batteryName string, status string, matchesUsed int, notes string) error {
	query := `
		UPDATE batteries 
		SET status = $1, matches_used = $2, notes = $3, status_timestamp = $4 
		WHERE group_key = $5 AND name = $6
	`
	timestamp := time.Now().Unix()

	cmdTag, err := conn.Exec(ctx, query, status, matchesUsed, notes, timestamp, groupKey, batteryName)
	if cmdTag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return handleError(err)
}

func removeBattery(ctx context.Context, conn *pgxpool.Pool, groupKey, batteryName string) error {
	query := `DELETE FROM batteries WHERE group_key = $1 AND name = $2`
	cmdTag, err := conn.Exec(ctx, query, groupKey, batteryName)
	if cmdTag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return handleError(err)
}

func addLogEntry(ctx context.Context, conn *pgxpool.Pool, entry internal.LogEntry) error {
	query := `INSERT INTO logs (group_key, username, action, timestamp) VALUES ($1, $2, $3, $4)`

	_, err := conn.Exec(ctx, query, entry.GroupKey, entry.Username, entry.Action, entry.Timestamp)

	return handleError(err)
}
