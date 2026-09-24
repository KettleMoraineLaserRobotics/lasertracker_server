package internal

// The thought of a file for all types throughout the project seems like really unprofessional and sloppy, but it works and that is what matters.
// Readable and "clean" code is more important to me than standards.

import (
	"encoding/json"
	"time"
)

type WSMessage struct {
	GroupKey  string
	Timestamp time.Time
	InfoType  string
	Payload   json.RawMessage
}

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

type Match struct {
	Red1      int
	Red2      int
	Red3      int
	Blue1     int
	Blue2     int
	Blue3     int
	MatchNum  int
	MatchKey  string
	EventKey  string
	Timestamp time.Time
}
