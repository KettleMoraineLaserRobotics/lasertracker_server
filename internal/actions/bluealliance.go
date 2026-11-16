package actions

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"lasertracker_server/internal"
)

var TBA_API_URL = internal.GetConfig().TBAAPIURL

var TBA_API_KEY = internal.GetConfig().TBAAPIKey

var tbaClient = &http.Client{Timeout: 10 * time.Second}

type tbaResult struct {
	Body        []byte
	ETag        string
	MaxAge      time.Duration
	NotModified bool
}

var ErrTBAAuth = errors.New("TBA rejected the API key")
var ErrTBANotFound = errors.New("TBA: not found")

func tbaGet(ctx context.Context, path, etag string) (*tbaResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, TBA_API_URL+path, nil)
	if err != nil {
		return nil, errors.New("Error making TBA request: " + err.Error())
	}

	req.Header.Set("X-TBA-Auth-Key", TBA_API_KEY)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}

	resp, err := tbaClient.Do(req)
	if err != nil {
		return nil, errors.New("Error sending TBA request: " + err.Error())
	}
	defer resp.Body.Close()

	result := &tbaResult{ETag: resp.Header.Get("ETag")}
	for _, part := range strings.Split(resp.Header.Get("Cache-Control"), ",") {
		part = strings.TrimSpace(part)
		if v, ok := strings.CutPrefix(part, "max-age="); ok {
			if secs, err := strconv.Atoi(v); err == nil {
				result.MaxAge = time.Duration(secs) * time.Second
			}
		}
	}

	switch resp.StatusCode {
	case http.StatusOK:
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, errors.New("Error reading TBA response: " + err.Error())
		}
		result.Body = body
		return result, nil
	case http.StatusNotModified:
		result.NotModified = true
		return result, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, ErrTBAAuth
	case http.StatusNotFound:
		return nil, ErrTBANotFound
	default:
		return nil, errors.New("TBA returned status " + resp.Status)
	}
}

func tbaGetJSON(ctx context.Context, path string, out any) error {
	res, err := tbaGet(ctx, path, "")
	if err != nil {
		return err
	}
	if err := json.Unmarshal(res.Body, out); err != nil {
		return errors.New("Error decoding TBA response: " + err.Error())
	}
	return nil
}

func TeamName(teamInt int) (string, error) {
	var data map[string]any
	if err := tbaGetJSON(context.Background(), "/team/frc"+strconv.Itoa(teamInt), &data); err != nil {
		return "", err
	}

	teamName := "team name here plz fix :("
	if nickname, ok := data["nickname"].(string); ok {
		teamName = nickname
	}
	return teamName, nil
}

func TeamAvatar(teamInt int) (string, error) {
	var data []map[string]any
	if err := tbaGetJSON(context.Background(), "/team/frc"+strconv.Itoa(teamInt)+"/media/"+strconv.Itoa(time.Now().Year()), &data); err != nil {
		return "", err
	}

	for _, item := range data {
		if mediaType, ok := item["type"].(string); ok && mediaType == "avatar" {
			if details, ok := item["details"].(map[string]any); ok {
				if b64, ok := details["base64Image"].(string); ok {
					return b64, nil
				}
			}
		}
	}
	return "", errors.New("No avatar found for team")
}

func GetTeamInfo(teamInt int) (map[string]string, error) {
	name, err := TeamName(teamInt)
	if err != nil {
		return nil, errors.New(err.Error())
	}
	avatar, err := TeamAvatar(teamInt)
	if err != nil {
		return nil, errors.New(err.Error())
	}

	info := map[string]string{"team_name": name, "team_avatar_b64": avatar}
	return info, nil
}

func GetTeamEvents(teamInt int) ([]map[string]string, error) {
	var data []map[string]any
	if err := tbaGetJSON(context.Background(), "/team/frc"+strconv.Itoa(teamInt)+"/events/"+strconv.Itoa(time.Now().Year()), &data); err != nil {
		return nil, err
	}

	var list []map[string]string

	for _, item := range data {
		name, _ := item["name"].(string)
		key, _ := item["key"].(string)
		list = append(list, map[string]string{
			"event_name": name,
			"event_key":  key,
		})
	}

	return list, nil
}

type tbaMatch struct {
	Key           string `json:"key"`
	EventKey      string `json:"event_key"`
	CompLevel     string `json:"comp_level"`
	SetNumber     int    `json:"set_number"`
	MatchNumber   int    `json:"match_number"`
	PredictedTime *int64 `json:"predicted_time"`
	ActualTime    *int64 `json:"actual_time"`
	Alliances     struct {
		Red struct {
			TeamKeys []string `json:"team_keys"`
		} `json:"red"`
		Blue struct {
			TeamKeys []string `json:"team_keys"`
		} `json:"blue"`
	} `json:"alliances"`
}

func parseMatches(data []byte) ([]internal.Match, error) {
	var raw []tbaMatch
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, errors.New("Error decoding TBA matches: " + err.Error())
	}
	list := make([]internal.Match, 0, len(raw))
	for _, m := range raw {
		match := internal.Match{
			MatchKey: m.Key, EventKey: m.EventKey, CompLevel: m.CompLevel,
			SetNumber: m.SetNumber, MatchNum: m.MatchNumber,
		}
		r, b := m.Alliances.Red.TeamKeys, m.Alliances.Blue.TeamKeys
		match.Red1, match.Red2, match.Red3 = teamAtStr(r, 0), teamAtStr(r, 1), teamAtStr(r, 2)
		match.Blue1, match.Blue2, match.Blue3 = teamAtStr(b, 0), teamAtStr(b, 1), teamAtStr(b, 2)
		if m.PredictedTime != nil {
			t := time.Unix(*m.PredictedTime, 0)
			match.PredictedTime = &t
		}
		if m.ActualTime != nil {
			t := time.Unix(*m.ActualTime, 0)
			match.ActualTime = &t
		}
		list = append(list, match)
	}
	return list, nil
}

type tbaWebcast struct {
	Type    string `json:"type"`
	Channel string `json:"channel"`
}

type tbaEvent struct {
	Key       string       `json:"key"`
	StartDate *string      `json:"start_date"`
	EndDate   *string      `json:"end_date"`
	Webcasts  []tbaWebcast `json:"webcasts"`
}

func parseTBADate(s *string) *time.Time {
	if s == nil {
		return nil
	}
	t, err := time.Parse("2006-01-02", *s)
	if err != nil {
		return nil
	}
	return &t
}

func FetchEventInfo(ctx context.Context, eventKey string) (start, end *time.Time, webcastsJSON string, err error) {
	var event tbaEvent
	res, err := tbaGet(ctx, "/event/"+eventKey, "")
	if err != nil {
		return nil, nil, "", err
	}
	if err := json.Unmarshal(res.Body, &event); err != nil {
		return nil, nil, "", errors.New("Error decoding TBA event: " + err.Error())
	}

	if event.Webcasts == nil {
		event.Webcasts = []tbaWebcast{}
	}
	cast, err := json.Marshal(event.Webcasts)
	if err != nil {
		return nil, nil, "", err
	}
	return parseTBADate(event.StartDate), parseTBADate(event.EndDate), string(cast), nil
}

func FetchEventMatches(ctx context.Context, eventKey, etag string) (matches []internal.Match, newETag string, maxAge time.Duration, notModified bool, err error) {
	res, err := tbaGet(ctx, "/event/"+eventKey+"/matches/simple", etag)
	if err != nil {
		return nil, "", 0, false, err
	}
	if res.NotModified {
		return nil, etag, res.MaxAge, true, nil
	}
	matches, err = parseMatches(res.Body)
	return matches, res.ETag, res.MaxAge, false, err
}

func teamAtStr(keys []string, i int) int {
	if i >= len(keys) {
		return 0
	}
	return parseTeamKey(keys[i])
}

func parseTeamKey(key any) int {
	keyStr, ok := key.(string)
	if !ok {
		return 0
	}
	teamNum, _ := strconv.Atoi(strings.TrimPrefix(keyStr, "frc"))
	return teamNum
}
