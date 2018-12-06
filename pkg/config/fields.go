package config

import (
	"errors"
	"fmt"

	jira "github.com/andygrunwald/go-jira"
)

// jiraField represents field metadata in JIRA. For an example of its
// structure, make a request to `${jira-uri}/rest/api/2/field`.
type jiraField struct {
	ID          string   `json:"id"`
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Custom      bool     `json:"custom"`
	Orderable   bool     `json:"orderable"`
	Navigable   bool     `json:"navigable"`
	Searchable  bool     `json:"searchable"`
	ClauseNames []string `json:"clauseNames"`
	Schema      struct {
		Type     string `json:"type"`
		System   string `json:"system,omitempty"`
		Items    string `json:"items,omitempty"`
		Custom   string `json:"custom,omitempty"`
		CustomID int    `json:"customId,omitempty"`
	} `json:"schema,omitempty"`
}

// getFieldIDs requests the metadata of every issue field in the JIRA
// project, and saves the IDs of the custom fields used by issue-sync.
func (c Config) getFieldIDs(client jira.Client) (fields, error) {
	c.log.Debug("Collecting field IDs.")
	req, err := client.NewRequest("GET", "/rest/api/2/field", nil)
	if err != nil {
		return fields{}, err
	}
	jFields := new([]jiraField)

	_, err = client.Do(req, jFields)
	if err != nil {
		return fields{}, err
	}

	fieldIDs := fields{}

	for _, field := range *jFields {
		switch field.Name {
		case "GitHub ID":
			fieldIDs.githubID = fmt.Sprint(field.Schema.CustomID)
		case "GitHub Number":
			fieldIDs.githubNumber = fmt.Sprint(field.Schema.CustomID)
		case "GitHub Labels":
			fieldIDs.githubLabels = fmt.Sprint(field.Schema.CustomID)
		case "GitHub Status":
			fieldIDs.githubStatus = fmt.Sprint(field.Schema.CustomID)
		case "GitHub Reporter":
			fieldIDs.githubReporter = fmt.Sprint(field.Schema.CustomID)
		case "Last Issue-Sync Update":
			fieldIDs.lastUpdate = fmt.Sprint(field.Schema.CustomID)
		case "GitHub URI":
			fieldIDs.githubURI = fmt.Sprint(field.Schema.CustomID)
		}
	}

	if fieldIDs.githubID == "" {
		return fieldIDs, errors.New("could not find ID of 'GitHub ID' custom field; check that it is named correctly")
	} else if fieldIDs.githubNumber == "" {
		return fieldIDs, errors.New("could not find ID of 'GitHub Number' custom field; check that it is named correctly")
	} else if fieldIDs.githubLabels == "" {
		return fieldIDs, errors.New("could not find ID of 'Github Labels' custom field; check that it is named correctly")
	} else if fieldIDs.githubStatus == "" {
		return fieldIDs, errors.New("could not find ID of 'Github Status' custom field; check that it is named correctly")
	} else if fieldIDs.githubReporter == "" {
		return fieldIDs, errors.New("could not find ID of 'Github Reporter' custom field; check that it is named correctly")
	} else if fieldIDs.lastUpdate == "" {
		return fieldIDs, errors.New("could not find ID of 'Last Issue-Sync Update' custom field; check that it is named correctly")
	} else if fieldIDs.lastUpdate == "" {
		return fieldIDs, errors.New("could not find ID of 'GitHub URI' custom field; check that it is named correctly")
	}

	c.log.Debug("All fields have been checked.")

	return fieldIDs, nil
}

// GetFieldID returns the customfield ID of a JIRA custom field.
func (c Config) GetFieldID(key fieldKey) string {
	switch key {
	case GitHubID:
		return c.fieldIDs.githubID
	case GitHubNumber:
		return c.fieldIDs.githubNumber
	case GitHubLabels:
		return c.fieldIDs.githubLabels
	case GitHubReporter:
		return c.fieldIDs.githubReporter
	case GitHubStatus:
		return c.fieldIDs.githubStatus
	case LastISUpdate:
		return c.fieldIDs.lastUpdate
	case GitHubURI:
		return c.fieldIDs.githubURI
	default:
		return ""
	}
}

// GetFieldKey returns customfield_XXXXX, where XXXXX is the custom field ID (see GetFieldID).
func (c Config) GetFieldKey(key fieldKey) string {
	return fmt.Sprintf("customfield_%s", c.GetFieldID(key))
}

// fieldKey is an enum-like type to represent the customfield ID keys
type fieldKey int

const (
	GitHubID       fieldKey = iota
	GitHubNumber   fieldKey = iota
	GitHubLabels   fieldKey = iota
	GitHubStatus   fieldKey = iota
	GitHubReporter fieldKey = iota
	LastISUpdate   fieldKey = iota
	GitHubURI      fieldKey = iota
)

// fields represents the custom field IDs of the JIRA custom fields we care about
type fields struct {
	githubID       string
	githubNumber   string
	githubLabels   string
	githubReporter string
	githubStatus   string
	lastUpdate     string
	githubURI      string
}
