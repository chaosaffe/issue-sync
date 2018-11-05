package config

import (
	"errors"
	"io/ioutil"

	jira "github.com/andygrunwald/go-jira"
	"github.com/dghubble/oauth1"
)

// SetJIRAToken adds the JIRA OAuth tokens in the Viper configuration, ensuring that they
// are saved for future runs.
func (c Config) SetJIRAToken(token *oauth1.Token) {
	c.cmdConfig.Set("jira-token", token.Token)
	c.cmdConfig.Set("jira-secret", token.TokenSecret)
}

// LoadJIRAConfig loads the JIRA configuration (project key,
// custom field IDs) from a remote JIRA server.
func (c *Config) LoadJIRAConfig(client jira.Client) error {
	proj, res, err := client.Project.Get(c.cmdConfig.GetString("jira-project"))
	if err != nil {
		c.log.Errorf("Error retrieving JIRA project; check key and credentials. Error: %v", err)
		defer res.Body.Close()
		body, err := ioutil.ReadAll(res.Body)
		if err != nil {
			c.log.Errorf("Error occured trying to read error body: %v", err)
			return err
		}

		c.log.Debugf("Error body: %s", body)
		return errors.New(string(body))
	}
	c.project = *proj

	c.fieldIDs, err = c.getFieldIDs(client)
	if err != nil {
		return err
	}

	return nil
}
