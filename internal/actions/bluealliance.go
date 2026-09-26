package actions

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"lasertracker_server/internal"
)

const TBA_API_URL = "https://www.thebluealliance.com/api/v3"

var TBA_API_KEY = internal.GetConfig().TBAAPIKey
var TESTING = internal.GetConfig().IsTesting

func teamName(teamInt int) (string, error) {
	var data map[string]any
	teamNumber := strconv.Itoa(teamInt)
	if TESTING {
		if teamNumber != "2077" {
			return "", errors.New("Team number must be 2077 when testing")
		}
		file, err := os.Open("./example responses/2077info.json")
		if err != nil {
			return "", errors.New("Example response not found")
		}
		defer file.Close()

		if err := json.NewDecoder(file).Decode(&data); err != nil {
			return "", errors.New("Error decoding example response")
		}
	} else {
		req, err := http.NewRequest(http.MethodGet, TBA_API_URL+"/team/frc"+teamNumber, nil)
		if err != nil {
			return "", errors.New("Error making TBA request: " + err.Error())
		}

		req.Header.Set("X-TBA-Auth-Key", TBA_API_KEY)

		client := &http.Client{
			Timeout: 10 * time.Second,
		}

		resp, err := client.Do(req)
		if err != nil {
			return "", errors.New("Error sending TBA request: " + err.Error())
		}
		defer resp.Body.Close()

		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			return "", errors.New("Error decoding TBA response: " + err.Error())
		}
	}

	teamName := "team name here plz fix :("
	if nickname, ok := data["nickname"].(string); ok {
		teamName = nickname
	}
	return teamName, nil
}

func teamAvatar(teamInt int) (string, error) {
	var data []map[string]any
	teamNumber := strconv.Itoa(teamInt)
	if TESTING {
		if teamNumber != "2077" {
			return "", errors.New("Team number must be 2077 when testing")
		}
		file, err := os.Open("./example responses/2077media.json")
		if err != nil {
			return "", errors.New("Example response not found")
		}
		defer file.Close()

		if err := json.NewDecoder(file).Decode(&data); err != nil {
			return "", errors.New("Error decoding example response")
		}
	} else {
		req, err := http.NewRequest(http.MethodGet, TBA_API_URL+"/team/frc"+teamNumber+"/media/"+strconv.Itoa(time.Now().Year()), nil)
		if err != nil {
			return "", errors.New("Error making TBA request: " + err.Error())
		}

		req.Header.Set("X-TBA-Auth-Key", TBA_API_KEY)

		client := &http.Client{
			Timeout: 10 * time.Second,
		}

		resp, err := client.Do(req)
		if err != nil {
			return "", errors.New("Error sending TBA request: " + err.Error())
		}
		defer resp.Body.Close()

		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			return "", errors.New("Error decoding TBA response: " + err.Error())
		}
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
	name, err := teamName(teamInt)
	if err != nil {
		return nil, errors.New(err.Error())
	}
	avatar, err := teamAvatar(teamInt)
	if err != nil {
		return nil, errors.New(err.Error())
	}

	info := map[string]string{"team_name": name, "team_avatar_b64": avatar}
	return info, nil
}

func GetTeamEvents(teamInt int) ([]map[string]string, error) {
	var data []map[string]any
	teamNumber := strconv.Itoa(teamInt)
	if TESTING {
		if teamNumber != "2077" {
			return nil, errors.New("Team number must be 2077 when testing")
		}
		file, err := os.Open("./example responses/2077events.json")
		if err != nil {
			return nil, errors.New("Example response not found")
		}
		defer file.Close()

		if err := json.NewDecoder(file).Decode(&data); err != nil {
			return nil, errors.New("Error decoding example response")
		}
	} else {
		req, err := http.NewRequest(http.MethodGet, TBA_API_URL+"/team/frc"+teamNumber+"/events/"+strconv.Itoa(time.Now().Year()), nil)
		if err != nil {
			return nil, errors.New("Error making TBA request: " + err.Error())
		}

		req.Header.Set("X-TBA-Auth-Key", TBA_API_KEY)

		client := &http.Client{
			Timeout: 10 * time.Second,
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, errors.New("Error sending TBA request: " + err.Error())
		}
		defer resp.Body.Close()

		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			return nil, errors.New("Error decoding TBA response: " + err.Error())
		}
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

func GetTeamMatches(teamInt int, eventKey string) ([]internal.Match, error) {
	var data []map[string]any
	teamNumber := strconv.Itoa(teamInt)
	if TESTING {
		if teamNumber != "2077" {
			return nil, errors.New("Team number must be 2077 when testing")
		}
		file, err := os.Open("./example responses/" + eventKey + ".json")
		if err != nil {
			return nil, errors.New("Example response not found")
		}
		defer file.Close()

		if err := json.NewDecoder(file).Decode(&data); err != nil {
			return nil, errors.New("Error decoding example response")
		}
	} else {
		req, err := http.NewRequest(http.MethodGet, TBA_API_URL+"/team/frc"+teamNumber+"/event/"+eventKey+"/matches", nil)
		if err != nil {
			return nil, errors.New("Error making TBA request: " + err.Error())
		}

		req.Header.Set("X-TBA-Auth-Key", TBA_API_KEY)

		client := &http.Client{
			Timeout: 10 * time.Second,
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, errors.New("Error sending TBA request: " + err.Error())
		}
		defer resp.Body.Close()

		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			return nil, errors.New("Error decoding TBA response: " + err.Error())
		}
	}

	var list []internal.Match

	for _, item := range data {
		alliances, ok := item["alliances"].(map[string]any)
		if !ok {
			continue
		}

		blue, ok := alliances["blue"].(map[string]any)
		if !ok {
			continue
		}
		red, ok := alliances["red"].(map[string]any)
		if !ok {
			continue
		}

		blueKeys, ok := blue["team_keys"].([]any)
		if !ok || len(blueKeys) < 3 {
			continue
		}
		redKeys, ok := red["team_keys"].([]any)
		if !ok || len(redKeys) < 3 {
			continue
		}

		var match internal.Match
		match.Blue1, _ = strconv.Atoi(strings.TrimPrefix(blueKeys[0].(string), "frc"))
		match.Blue2, _ = strconv.Atoi(strings.TrimPrefix(blueKeys[1].(string), "frc"))
		match.Blue3, _ = strconv.Atoi(strings.TrimPrefix(blueKeys[2].(string), "frc"))
		match.Red1, _ = strconv.Atoi(strings.TrimPrefix(redKeys[0].(string), "frc"))
		match.Red2, _ = strconv.Atoi(strings.TrimPrefix(redKeys[1].(string), "frc"))
		match.Red3, _ = strconv.Atoi(strings.TrimPrefix(redKeys[2].(string), "frc"))
		match.EventKey = item["event_key"].(string)
		match.MatchKey = item["key"].(string)
		match.MatchNum = int(item["match_number"].(float64))
		if actualTime, ok := item["actual_time"].(float64); ok {
			match.Timestamp = time.Unix(int64(actualTime), 0)
			match.Played = true
		} else {
			match.Timestamp = time.Unix(int64(item["predicted_time"].(float64)), 0)
			match.Played = false
		}
		list = append(list, match)
	}

	return list, nil
}

func GetStreams(teamInt int, eventKey string) ([]internal.Stream, error) {
	type webcast struct {
		Type    string `json:"type"`
		Channel string `json:"channel"`
	}
	type event struct {
		Key      string    `json:"key"`
		Webcasts []webcast `json:"webcasts"`
	}
	var data []event
	teamNumber := strconv.Itoa(teamInt)
	if TESTING {
		if teamNumber != "2077" {
			return nil, errors.New("Team number must be 2077 when testing")
		}
		file, err := os.Open("./example responses/2077events.json")
		if err != nil {
			return nil, errors.New("Example response not found")
		}
		defer file.Close()

		if err := json.NewDecoder(file).Decode(&data); err != nil {
			return nil, errors.New("Error decoding example response")
		}
	} else {
		req, err := http.NewRequest(http.MethodGet, TBA_API_URL+"/team/frc"+teamNumber+"/events/"+strconv.Itoa(time.Now().Year()), nil)
		if err != nil {
			return nil, errors.New("Error making TBA request: " + err.Error())
		}

		req.Header.Set("X-TBA-Auth-Key", TBA_API_KEY)

		client := &http.Client{
			Timeout: 10 * time.Second,
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, errors.New("Error sending TBA request: " + err.Error())
		}
		defer resp.Body.Close()

		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			return nil, errors.New("Error decoding TBA response: " + err.Error())
		}
	}

	var list []internal.Stream
	for _, item := range data {
		if item.Key != eventKey {
			continue
		}

		for _, cast := range item.Webcasts {
			streamURL := cast.Channel
			switch cast.Type {
			case "youtube":
				streamURL = "https://www.youtube.com/watch?v=" + cast.Channel
			case "twitch":
				streamURL = "https://www.twitch.tv/" + cast.Channel
			}
			list = append(list, internal.Stream{Name: cast.Type, URL: streamURL})
		}
		return list, nil
	}

	return list, nil
}
