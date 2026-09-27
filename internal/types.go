package internal

// The thought of a file for all types throughout the project seems like really unprofessional and sloppy, but it works and that is what matters.
// Readable and "clean" code is more important to me than standards.

import (
	"encoding/json"
	"time"
)

type Group struct {
	GroupName   string `json:"group_name"`
	EventKey    string `json:"event_key"`
	TeamNumber  int    `json:"team_number"`
	AvatarImage string `json:"avatar_image"`
	GroupKey    string `json:"group_key"`
}

type PublicMember struct { // Client-accessible member type, no passwords
	GroupKey    string `json:"group_key"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Job         string `json:"job"`
	Role        string `json:"role"`
	Location    string `json:"location"`
	IsAdmin     bool   `json:"is_admin"`
}

type PrivateMember struct { // Server-only member type, with passwords
	GroupKey    string `json:"group_key"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	PinHash     string `json:"pin_hash"`
	TokenVer    int    `json:"token_ver"`
	Job         string `json:"job"`
	Role        string `json:"role"`
	Location    string `json:"location"`
	IsAdmin     bool   `json:"is_admin"`
}

type LogEntry struct {
	GroupKey  string    `json:"group_key"`
	Username  string    `json:"username"`
	Action    string    `json:"action"`
	Timestamp time.Time `json:"timestamp"`
}

type Battery struct {
	GroupKey    string    `json:"group_key"`
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	MatchesUsed int       `json:"matches_used"`
	Notes       string    `json:"notes"`
	Timestamp   time.Time `json:"timestamp"`
}

type Match struct {
	Red1      int       `json:"red1"`
	Red2      int       `json:"red2"`
	Red3      int       `json:"red3"`
	Blue1     int       `json:"blue1"`
	Blue2     int       `json:"blue2"`
	Blue3     int       `json:"blue3"`
	MatchNum  int       `json:"match_num"`
	MatchKey  string    `json:"match_key"`
	EventKey  string    `json:"event_key"`
	Timestamp time.Time `json:"timestamp"`
	Played    bool      `json:"played"`
}

type Stream struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type PinChangeRequest struct {
	GroupKey string `json:"group_key"`
	Username string `json:"username"`
	NewPin   string `json:"new_pin"`
}

type Message struct {
	GroupKey  string          `json:"group_key"`
	Timestamp time.Time       `json:"timestamp"`
	InfoType  string          `json:"info_type"`
	Payload   json.RawMessage `json:"payload"`
}
