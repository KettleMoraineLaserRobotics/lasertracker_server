package internal

import "time"

type Group struct {
	GroupName  string
	EventKey   string
	TeamNumber int
	GroupKey   string
}

type Member struct {
	GroupKey    string
	Username    string
	DisplayName string
	PinHash     string
	Job         string
	Role        string
	Location    string
	IsAdmin     bool
}

type LogEntry struct {
	GroupKey  string
	Username  string
	Action    string
	Timestamp time.Time
}

type Battery struct {
	GroupKey    string
	Name        string
	Status      string
	MatchesUsed int
	Notes       string
	Timestamp   time.Time
}
