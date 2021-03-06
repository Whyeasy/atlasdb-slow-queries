package internal

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	dac "github.com/xinsnake/go-http-digest-auth-client"
)

type mongoProjectID struct {
	Results []struct {
		ID       string `json:"id"`
		TypeName string `json:"typeName"`
	} `json:"results"`
}

type slowQueries struct {
	SlowQueries []struct {
		Line      string `json:"line"`
		Namespace string `json:"namespace"`
	} `json:"slowQueries"`
}

type suggestedIndexes struct {
	Shapes []struct {
		AvgMs             int    `json:"avgMs"`
		Count             int    `json:"count"`
		ID                string `json:"id"`
		InefficiencyScore int    `json:"inefficiencyScore"`
		Namespace         string `json:"namespace"`
		Operations        []struct {
			Predicates []struct {
				Find struct {
					GlobalIDKey string `json:"globalId_key"`
				} `json:"find,omitempty"`
				Sort struct {
					CommitMetadataID struct {
						NumberInt string `json:"$numberInt"`
					} `json:"commitMetadata.id"`
				} `json:"sort,omitempty"`
			} `json:"predicates"`
			Raw   string `json:"raw"`
			Stats struct {
				Ms        int   `json:"ms"`
				NReturned int   `json:"nReturned"`
				NScanned  int   `json:"nScanned"`
				Ts        int64 `json:"ts"`
			} `json:"stats"`
		} `json:"operations"`
	} `json:"shapes"`
	SuggestedIndexes []struct {
		ID        string           `json:"id"`
		Impact    []string         `json:"impact"`
		Index     []map[string]int `json:"index"`
		Namespace string           `json:"namespace"`
		Weight    float64          `json:"weight"`
	} `json:"suggestedIndexes"`
}

//GetData retrieves the data from AtlasDB and sends them to stdout.
func GetData(groupID string, publicKey string, privateKey string, since int) {

	time := time.Now().Add(time.Duration(-since)*time.Hour).UnixNano() / 1000000

	primary, err := getPrimary(groupID, publicKey, privateKey)
	if err != nil {
		log.Fatal(err)
	}

	connectionString := fmt.Sprintf("https://cloud.mongodb.com/api/atlas/v1.0/groups/%s/processes/%s/performanceAdvisor/", groupID, primary)

	getSlowQueries(connectionString, publicKey, privateKey, time)
	getSuggestedIndexes(connectionString, publicKey, privateKey, time)
}

func getPrimary(groupID string, publicKey string, privateKey string) (string, error) {

	request := fmt.Sprintf("https://cloud.mongodb.com/api/atlas/v1.0/groups/%s/processes/", groupID)

	resp, err := doRequest(request, publicKey, privateKey)
	if err != nil {
		return "", err
	}

	var responses mongoProjectID
	err = json.Unmarshal(resp, &responses)
	if err != nil {
		return "", err
	}

	for _, response := range responses.Results {
		if strings.Contains(strings.ToLower(response.TypeName), "primary") {
			log.Debug("Primary database found: ", response.ID)
			return response.ID, nil
		}
	}
	return "", fmt.Errorf("No Primary Database found")
}

func getSlowQueries(connection string, publicKey string, privateKey string, time int64) {

	request := fmt.Sprintf("%sslowQueryLogs?since=%v", connection, time)

	resp, err := doRequest(request, publicKey, privateKey)
	if err != nil {
		log.Fatal(err)
	}

	var responses slowQueries
	err = json.Unmarshal(resp, &responses)
	if err != nil {
		log.Error(err)
	}

	for _, response := range responses.SlowQueries {
		namespace := strings.Split(response.Namespace, ".")
		log.WithFields(log.Fields{
			"line":       response.Line,
			"database":   namespace[0],
			"collection": namespace[1],
		}).Info("Slow Query found")
	}
}

func getSuggestedIndexes(connection string, publicKey string, privateKey string, time int64) {

	request := fmt.Sprintf("%ssuggestedIndexes?since=%v", connection, time)

	resp, err := doRequest(request, publicKey, privateKey)
	if err != nil {
		log.Fatal(err)
	}

	var responses suggestedIndexes
	err = json.Unmarshal(resp, &responses)
	if err != nil {
		log.Error(err)
	}

	for _, response := range responses.SuggestedIndexes {
		namespace := strings.Split(response.Namespace, ".")

		indexes := ""
		for _, v := range response.Index {
			i, err := json.Marshal(v)
			if err != nil {
				log.Error(err)
			}

			indexes += string(i)
		}

		for _, impact := range response.Impact {
			for _, shape := range responses.Shapes {
				if impact == shape.ID {
					log.WithFields(log.Fields{
						"id":                response.ID,
						"impact":            impact,
						"index":             indexes,
						"database":          namespace[0],
						"collection":        namespace[1],
						"weight":            response.Weight,
						"avgMs":             shape.AvgMs,
						"count":             shape.Count,
						"inefficiencyScore": shape.InefficiencyScore,
					}).Info("Suggested index found.")
				}
			}
		}
	}
}

func doRequest(uri string, publicKey string, privateKey string) ([]byte, error) {

	t := dac.NewTransport(publicKey, privateKey)
	t.HTTPClient = &http.Client{
		Timeout: time.Second * 60,
	}

	req, err := http.NewRequest(
		"GET",
		uri,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("unable to make request: %s", err)
	}
	req.Header.Set("ACCEPT", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return respBody, nil
}
