package http

import (
	"encoding/json"
	"errors"
	"io"
	"lasertracker_server/internal"
	actions "lasertracker_server/internal/actions"
	auth "lasertracker_server/internal/auth"
	"net/http"
	"strconv"
	"strings"
)

type Info struct {
	InstanceName string
	Version      string
}

type CreateUserRequest struct {
	GroupKey    string `json:"group_key"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Pin         string `json:"pin"`
}

type CreateGroupRequest struct {
	GroupName   string `json:"group_name"`
	EventKey    string `json:"event_key"`
	TeamNumber  int    `json:"team_number"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Pin         string `json:"pin"`
}

type LoginRequest struct {
	GroupKey string `json:"group_key"`
	Username string `json:"username"`
	Pin      string `json:"pin"`
}

func sanitize(input string) (string, bool) {
	s := strings.TrimSpace(input)
	return s, len(s) > 0
}

func infoHandler() http.HandlerFunc {
	var cfg = internal.GetConfig()
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		response := Info{
			InstanceName: cfg.InstanceName,
			Version:      cfg.Version,
		}

		w.WriteHeader(http.StatusOK)

		if err := json.NewEncoder(w).Encode(response); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
}

func loginHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1048576)
		ctx := r.Context()

		var req LoginRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()

		if err := dec.Decode(&req); err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				http.Error(w, "Request body exceeds 1MB limit", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "Malformed JSON", http.StatusBadRequest)
			return
		}

		if err := dec.Decode(&struct{}{}); err != io.EOF {
			http.Error(w, "Request body must only contain a single JSON object", http.StatusBadRequest)
			return
		}

		groupKey, ok1 := sanitize(req.GroupKey)
		username, ok2 := sanitize(req.Username)
		pin, ok3 := sanitize(req.Pin)

		if !ok1 || !ok2 || !ok3 {
			http.Error(w, "Group key, username, and PIN cannot be empty", http.StatusUnprocessableEntity)
			return
		}

		memberPinHash, err := actions.GetMemberPinHash(ctx, groupKey, username)
		if err != nil {
			http.Error(w, "Failed to retrieve member PIN hash", http.StatusInternalServerError)
			return
		}

		if !auth.CheckPin(memberPinHash, pin) {
			http.Error(w, "Invalid PIN", http.StatusUnauthorized)
			return
		}

		tokenVer, err := actions.GetMemberTokenVer(ctx, groupKey, username)
		if err != nil {
			http.Error(w, "Failed to retrieve member token version", http.StatusInternalServerError)
			return
		}
		token, err := auth.GenerateToken(username, groupKey, tokenVer, internal.GetConfig().JWTSecret)
		if err != nil {
			http.Error(w, "Failed to generate auth token", http.StatusInternalServerError)
			return
		}

		member, err := actions.GetMember(ctx, groupKey, username)
		if err != nil {
			if errors.Is(err, actions.ErrNotFound) {
				http.Error(w, "Invalid group key or username", http.StatusUnauthorized)
				return
			}
			http.Error(w, "Failed to retrieve member information", http.StatusInternalServerError)
			return
		}

		group, err := actions.GetGroup(ctx, groupKey)
		if err != nil {
			if errors.Is(err, actions.ErrNotFound) {
				http.Error(w, "Invalid group key", http.StatusUnauthorized)
				return
			}
			http.Error(w, "Failed to retrieve group information", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		w.WriteHeader(http.StatusOK)

		response := map[string]any{
			"member_info": map[string]any{
				"group_key":    member.GroupKey,
				"username":     member.Username,
				"display_name": member.DisplayName,
				"location":     member.Location,
				"job":          member.Job,
				"role":         member.Role,
				"is_admin":     member.IsAdmin,
			},
			"group_info": map[string]any{
				"group_name":  group.GroupName,
				"event_key":   group.EventKey,
				"team_number": group.TeamNumber,
				"group_key":   group.GroupKey,
			},
			"token": token,
		}
		json.NewEncoder(w).Encode(response)

	}
}

func createGroupHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1048576)
		ctx := r.Context()

		var req CreateGroupRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()

		if err := dec.Decode(&req); err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				http.Error(w, "Request body exceeds 1MB limit", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "Malformed JSON", http.StatusBadRequest)
			return
		}

		if err := dec.Decode(&struct{}{}); err != io.EOF {
			http.Error(w, "Request body must only contain a single JSON object", http.StatusBadRequest)
			return
		}

		groupName, ok1 := sanitize(req.GroupName)
		eventKey, ok2 := sanitize(req.EventKey)
		teamNumber := req.TeamNumber
		username, ok3 := sanitize(req.Username)
		displayName, ok4 := sanitize(req.DisplayName)
		pin, ok5 := sanitize(req.Pin)

		if !ok1 || !ok2 || teamNumber <= 0 || !ok3 || !ok4 || !ok5 {
			http.Error(w, "Group name, event key, team number, username, display name, and PIN cannot be empty or invalid", http.StatusUnprocessableEntity)
			return
		}

		groupKey, gkerr := internal.GenerateRandomSecret(6)
		if gkerr != nil {
			http.Error(w, "Error generating group key", http.StatusInternalServerError)
			return
		}

		var ng internal.Group
		ng.GroupName = groupName
		ng.EventKey = eventKey
		ng.TeamNumber = teamNumber
		ng.GroupKey = groupKey

		if err := actions.CreateGroup(ctx, ng, groupKey); err != nil {
			if errors.Is(err, actions.ErrAlreadyExists) {
				http.Error(w, "Group already exists", http.StatusConflict)
				return
			}
			http.Error(w, "Failed to create group", http.StatusInternalServerError)
			return
		}

		var nm internal.Member
		nm.GroupKey = groupKey
		nm.Username = username
		nm.DisplayName = displayName
		nm.PinHash = auth.HashPin(pin)
		nm.Location = "Stands"
		nm.Job = "Unassigned"
		nm.Role = "Mentor"
		nm.IsAdmin = true

		if err := actions.AddMember(ctx, nm); err != nil {
			if errors.Is(err, actions.ErrAlreadyExists) {
				http.Error(w, "User already exists", http.StatusConflict)
				return
			}
			if errors.Is(err, actions.ErrForeignKeyFailed) {
				http.Error(w, "Invalid group key", http.StatusBadRequest)
				return
			}
			http.Error(w, "Failed to create member", http.StatusInternalServerError)
			return
		}

		token, err := auth.GenerateToken(nm.Username, nm.GroupKey, 1, internal.GetConfig().JWTSecret)

		if err != nil {
			http.Error(w, "Failed to generate auth token", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		w.WriteHeader(http.StatusCreated)
		response := map[string]any{
			"member_info": map[string]any{
				"group_key":    nm.GroupKey,
				"username":     nm.Username,
				"display_name": nm.DisplayName,
				"location":     nm.Location,
				"job":          nm.Job,
				"role":         nm.Role,
				"is_admin":     nm.IsAdmin,
			},
			"group_info": map[string]any{
				"group_name":  ng.GroupName,
				"event_key":   ng.EventKey,
				"team_number": ng.TeamNumber,
				"group_key":   ng.GroupKey,
			},
			"token": token,
		}
		json.NewEncoder(w).Encode(response)
	}
}

func createAccountHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1048576)
		ctx := r.Context()

		var req CreateUserRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()

		if err := dec.Decode(&req); err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				http.Error(w, "Request body exceeds 1MB limit", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "Malformed JSON", http.StatusBadRequest)
			return
		}

		if err := dec.Decode(&struct{}{}); err != io.EOF {
			http.Error(w, "Request body must only contain a single JSON object", http.StatusBadRequest)
			return
		}

		groupKey, ok1 := sanitize(req.GroupKey)
		displayName, ok2 := sanitize(req.DisplayName)
		username, ok3 := sanitize(req.Username)
		pin, ok4 := sanitize(req.Pin)

		if !ok1 || !ok2 || !ok3 || !ok4 {
			http.Error(w, "Group key, display name, username, and PIN cannot be empty", http.StatusUnprocessableEntity)
			return
		}

		var nm internal.Member
		nm.GroupKey = groupKey
		nm.Username = username
		nm.DisplayName = displayName
		nm.PinHash = auth.HashPin(pin)
		nm.Location = "Stands"
		nm.Job = "Unassigned"
		nm.Role = "Student"
		nm.IsAdmin = false

		if err := actions.AddMember(ctx, nm); err != nil {
			if errors.Is(err, actions.ErrAlreadyExists) {
				http.Error(w, "User already exists", http.StatusConflict)
				return
			}
			if errors.Is(err, actions.ErrForeignKeyFailed) {
				http.Error(w, "Invalid group key", http.StatusBadRequest)
				return
			}
			http.Error(w, "Failed to create member", http.StatusInternalServerError)
			return
		}

		token, err := auth.GenerateToken(nm.Username, nm.GroupKey, 1, internal.GetConfig().JWTSecret)

		if err != nil {
			http.Error(w, "Failed to generate auth token", http.StatusInternalServerError)
			return
		}

		ng, err := actions.GetGroup(ctx, groupKey)
		if err != nil {
			http.Error(w, "Failed to retrieve group information", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		w.WriteHeader(http.StatusCreated)
		response := map[string]any{
			"member_info": map[string]any{
				"group_key":    nm.GroupKey,
				"username":     nm.Username,
				"display_name": nm.DisplayName,
				"location":     nm.Location,
				"job":          nm.Job,
				"role":         nm.Role,
				"is_admin":     nm.IsAdmin,
			},
			"group_info": map[string]any{
				"group_name":  ng.GroupName,
				"event_key":   ng.EventKey,
				"team_number": ng.TeamNumber,
				"group_key":   ng.GroupKey,
			},
			"token": token,
		}
		json.NewEncoder(w).Encode(response)

	}
}

func wsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

	}
}

func InitHTTPServer() {
	port := strconv.Itoa(internal.GetConfig().Port)
	http.HandleFunc("GET /info", infoHandler())
	http.HandleFunc("POST /account", createAccountHandler())
	http.HandleFunc("POST /group", createGroupHandler())
	http.HandleFunc("POST /login", loginHandler())
	http.ListenAndServe(":"+port, nil)
}
