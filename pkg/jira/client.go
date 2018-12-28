package jira

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"regexp"
	"strings"

	"time"

	"github.com/andygrunwald/go-jira"
	"github.com/cenkalti/backoff"
	"github.com/google/go-github/github"
	"github.com/innovocloud/issue-sync/pkg/config"
	ghClient "github.com/innovocloud/issue-sync/pkg/github"
)

// commentDateFormat is the format used in the headers of JIRA comments.
const commentDateFormat = "15:04 PM, January 2 2006"

// maxJQLIssueLength is the maximum number of GitHub issues we can
// use before we need to stop using JQL and filter issues ourself.
const maxJQLIssueLength = 100

// getErrorBody reads the HTTP response body of a JIRA API response,
// logs it as an error, and returns an error object with the contents
// of the body. If an error occurs during reading, that error is
// instead printed and returned. This function closes the body for
// further reading.
func getErrorBody(config config.Config, res *jira.Response) error {
	log := config.GetLogger()
	defer res.Body.Close()
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		log.Errorf("Error occured trying to read error body: %v", err)
		return err
	}
	log.Debugf("Error body: %s", body)
	return errors.New(string(body))
}

// JIRAClient is a wrapper around the JIRA API clients library we
// use. It allows us to hide implementation details such as backoff
// as well as swap in other implementations, such as for dry run
// or test mocking.
type JIRAClient interface {
	ListIssues(ids []int) ([]jira.Issue, error)
	GetIssue(key string) (jira.Issue, error)
	CreateIssue(issue jira.Issue) (jira.Issue, error)
	UpdateIssue(issue jira.Issue) (jira.Issue, error)
	CreateComment(issue jira.Issue, comment github.IssueComment, github ghClient.GitHubClient) (jira.Comment, error)
	UpdateComment(issue jira.Issue, id string, comment github.IssueComment, github ghClient.GitHubClient) (jira.Comment, error)
}

// NewJIRAClient creates a new JIRAClient and configures it with
// the config object provided. The type of clients created depends
// on the configuration; currently, it creates either a standard
// clients, or a dry-run clients.
func NewJIRAClient(cfg *config.Config) (JIRAClient, error) {
	log := cfg.GetLogger()

	var oauth *http.Client
	var err error
	if !cfg.IsBasicAuth() {
		oauth, err = newJIRAHTTPClient(*cfg)
		if err != nil {
			log.Errorf("Error getting OAuth config: %v", err)
			return dryrunJIRAClient{}, err
		}
	}

	client, err := jira.NewClient(oauth, cfg.GetConfigString("jira-uri"))
	if err != nil {
		log.Errorf("Error initializing JIRA clients; check your base URI. Error: %v", err)
		return dryrunJIRAClient{}, err
	}

	if cfg.IsBasicAuth() {
		client.Authentication.SetBasicAuth(cfg.GetConfigString("jira-user"), cfg.GetConfigString("jira-secret"))
	}

	log.Debug("JIRA clients initialized")

	cfg.LoadJIRAConfig(*client)

	var j JIRAClient

	if cfg.IsDryRun() {
		j = dryrunJIRAClient{
			cfg:    *cfg,
			client: *client,
		}
	} else {
		j = realJIRAClient{
			cfg:    *cfg,
			client: *client,
		}
	}

	return j, nil
}

// realJIRAClient is a standard JIRA clients, which actually makes
// of the requests against the JIRA REST API. It is the canonical
// implementation of JIRAClient.
type realJIRAClient struct {
	cfg    config.Config
	client jira.Client
}

// ListIssues returns a list of JIRA issues on the configured project which
// have GitHub IDs in the provided list. `ids` should be a comma-separated
// list of GitHub IDs.
func (j realJIRAClient) ListIssues(ids []int) ([]jira.Issue, error) {
	idStrs := make([]string, len(ids))
	for i, v := range ids {
		idStrs[i] = fmt.Sprint(v)
	}

	var jql string
	// If the list of IDs is too long, we get a 414 Request-URI Too Large, so in that case,
	// we'll need to do the filtering ourselves.
	if len(ids) < maxJQLIssueLength {
		jql = fmt.Sprintf("project='%s' AND cf[%s] in (%s)",
			j.cfg.GetProjectKey(), j.cfg.GetFieldID(config.GitHubID), strings.Join(idStrs, ","))
	} else {
		jql = fmt.Sprintf("project='%s'", j.cfg.GetProjectKey())
	}

	jiraIssues, err := j.getIssues(jql)
	if err != nil {
		return nil, err
	}

	var filteredIssues []jira.Issue

	if len(ids) < maxJQLIssueLength {
		// The issues were already filtered by our JQL, so use as is
		filteredIssues = jiraIssues
	} else {
		// Filter only issues which have a defined GitHub ID in the list of IDs
		ghIDFieldKey := j.cfg.GetFieldKey(config.GitHubID)
		for _, v := range jiraIssues {
			if val, _ := v.Fields.Unknowns.Value(ghIDFieldKey); val == nil {
				continue
			}
			if id, err := v.Fields.Unknowns.Int(ghIDFieldKey); err == nil {
				for _, idOpt := range ids {
					if id == int64(idOpt) {
						filteredIssues = append(filteredIssues, v)
						break
					}
				}
			}
		}
	}

	return filteredIssues, nil
}

func (j realJIRAClient) getIssues(jql string) ([]jira.Issue, error) {
	log := j.cfg.GetLogger()
	var issues []jira.Issue

	const maxResults = 50
	// force at least one interation to occur
	totalResults := 1

	for page := 0; (page * maxResults) < totalResults; page++ {
		ji, res, err := j.request(func() (interface{}, *jira.Response, error) {
			opts := &jira.SearchOptions{
				StartAt:    (maxResults * page),
				MaxResults: maxResults,
			}
			return j.client.Issue.Search(jql, opts)
		})

		if err != nil {
			log.Errorf("Error retrieving JIRA issues: %v", err)
			return nil, getErrorBody(j.cfg, res)
		}

		totalResults = res.Total

		jiraIssues, ok := ji.([]jira.Issue)
		if !ok {
			log.Errorf("Get JIRA issues did not return issues! Got: %v", ji)
			return nil, fmt.Errorf("get JIRA issues failed: expected []jira.Issue; got %T", ji)
		}

		issues = append(issues, jiraIssues...)
	}

	return issues, nil
}

// GetIssue returns a single JIRA issue within the configured project
// according to the issue key (e.g. "PROJ-13").
func (j realJIRAClient) GetIssue(key string) (jira.Issue, error) {
	log := j.cfg.GetLogger()

	i, res, err := j.request(func() (interface{}, *jira.Response, error) {
		return j.client.Issue.Get(key, nil)
	})
	if err != nil {
		log.Errorf("Error retrieving JIRA issue: %v", err)
		return jira.Issue{}, getErrorBody(j.cfg, res)
	}
	issue, ok := i.(*jira.Issue)
	if !ok {
		log.Errorf("Get JIRA issue did not return issue! Got %v", i)
		return jira.Issue{}, fmt.Errorf("Get JIRA issue failed: expected *jira.Issue; got %T", i)
	}

	return *issue, nil
}

// CreateIssue creates a new JIRA issue according to the fields provided in
// the provided issue object. It returns the created issue, with all the
// fields provided (including e.g. ID and Key).
func (j realJIRAClient) CreateIssue(issue jira.Issue) (jira.Issue, error) {
	log := j.cfg.GetLogger()

	i, res, err := j.request(func() (interface{}, *jira.Response, error) {
		return j.client.Issue.Create(&issue)
	})

	if err != nil {
		log.Errorf("Error creating JIRA issue: %v", err)
		return jira.Issue{}, getErrorBody(j.cfg, res)
	}
	is, ok := i.(*jira.Issue)
	if !ok {
		log.Errorf("Create JIRA issue did not return issue! Got: %v", i)
		return jira.Issue{}, fmt.Errorf("Create JIRA issue failed: expected *jira.Issue; got %T", i)
	}

	return *is, nil
}

// UpdateIssue updates a given issue (identified by the Key field of the provided
// issue object) with the fields on the provided issue. It returns the updated
// issue as it exists on JIRA.
func (j realJIRAClient) UpdateIssue(issue jira.Issue) (jira.Issue, error) {
	log := j.cfg.GetLogger()

	i, res, err := j.request(func() (interface{}, *jira.Response, error) {
		return j.client.Issue.Update(&issue)
	})
	if err != nil {
		log.Errorf("Error updating JIRA issue %s: %v", issue.Key, err)
		return jira.Issue{}, getErrorBody(j.cfg, res)
	}
	is, ok := i.(*jira.Issue)
	if !ok {
		log.Errorf("Update JIRA issue did not return issue! Got: %v", i)
		return jira.Issue{}, fmt.Errorf("Update JIRA issue failed: expected *jira.Issue; got %T", i)
	}

	return *is, nil
}

// maxBodyLength is the maximum length of a JIRA comment body, which is currently
// 2^15-1.
const maxBodyLength = 1 << 15

// CreateComment adds a comment to the provided JIRA issue using the fields from
// the provided GitHub comment. It then returns the created comment.
func (j realJIRAClient) CreateComment(issue jira.Issue, comment github.IssueComment, github ghClient.GitHubClient) (jira.Comment, error) {
	log := j.cfg.GetLogger()

	user, err := github.GetUser(comment.User.GetLogin())
	if err != nil {
		return jira.Comment{}, err
	}

	body := fmt.Sprintf("Comment [(ID %d)|%s]", comment.GetID(), comment.GetHTMLURL())
	body = fmt.Sprintf("%s from GitHub user [%s|%s]", body, user.GetLogin(), user.GetHTMLURL())
	if user.GetName() != "" {
		body = fmt.Sprintf("%s (%s)", body, user.GetName())
	}
	body = fmt.Sprintf(
		"%s at %s:\n\n%s",
		body,
		comment.CreatedAt.Format(commentDateFormat),
		comment.GetBody(),
	)

	if len(body) >= maxBodyLength {
		body = body[:maxBodyLength]
	}

	jComment := jira.Comment{
		Body: body,
	}

	com, res, err := j.request(func() (interface{}, *jira.Response, error) {
		return j.client.Issue.AddComment(issue.ID, &jComment)
	})
	if err != nil {
		log.Errorf("Error creating JIRA comment on issue %s. Error: %v", issue.Key, err)
		return jira.Comment{}, getErrorBody(j.cfg, res)
	}
	co, ok := com.(*jira.Comment)
	if !ok {
		log.Errorf("Create JIRA comment did not return comment! Got: %v", com)
		return jira.Comment{}, fmt.Errorf("Create JIRA comment failed: expected *jira.Comment; got %T", com)
	}
	return *co, nil
}

// UpdateComment updates a comment (identified by the `id` parameter) on a given
// JIRA with a new body from the fields of the given GitHub comment. It returns
// the updated comment.
func (j realJIRAClient) UpdateComment(issue jira.Issue, id string, comment github.IssueComment, github ghClient.GitHubClient) (jira.Comment, error) {
	log := j.cfg.GetLogger()

	user, err := github.GetUser(comment.User.GetLogin())
	if err != nil {
		return jira.Comment{}, err
	}

	body := fmt.Sprintf("Comment [(ID %d)|%s]", comment.GetID(), comment.GetHTMLURL())
	body = fmt.Sprintf("%s from GitHub user [%s|%s]", body, user.GetLogin(), user.GetHTMLURL())
	if user.GetName() != "" {
		body = fmt.Sprintf("%s (%s)", body, user.GetName())
	}
	body = fmt.Sprintf(
		"%s at %s:\n\n%s",
		body,
		comment.CreatedAt.Format(commentDateFormat),
		comment.GetBody(),
	)

	if len(body) < maxBodyLength {
		body = body[:maxBodyLength]
	}

	// As it is, the JIRA API we're using doesn't have any way to update comments natively.
	// So, we have to build the request ourselves.
	request := struct {
		Body string `json:"body"`
	}{
		Body: body,
	}

	req, err := j.client.NewRequest("PUT", fmt.Sprintf("rest/api/2/issue/%s/comment/%s", issue.Key, id), request)
	if err != nil {
		log.Errorf("Error creating comment update request: %s", err)
		return jira.Comment{}, err
	}

	com, res, err := j.request(func() (interface{}, *jira.Response, error) {
		res, err := j.client.Do(req, nil)
		return nil, res, err
	})
	if err != nil {
		log.Errorf("Error updating comment: %v", err)
		return jira.Comment{}, getErrorBody(j.cfg, res)
	}
	co, ok := com.(*jira.Comment)
	if !ok {
		log.Errorf("Update JIRA comment did not return comment! Got: %v", com)
		return jira.Comment{}, fmt.Errorf("Update JIRA comment failed: expected *jira.Comment; got %T", com)
	}
	return *co, nil
}

// request takes an API function from the JIRA library
// and calls it with exponential backoff. If the function succeeds, it
// returns the expected value and the JIRA API response, as well as a nil
// error. If it continues to fail until a maximum time is reached, it returns
// a nil result as well as the returned HTTP response and a timeout error.
func (j realJIRAClient) request(f func() (interface{}, *jira.Response, error)) (interface{}, *jira.Response, error) {
	log := j.cfg.GetLogger()

	var ret interface{}
	var res *jira.Response

	op := func() error {
		var err error
		ret, res, err = f()
		return err
	}

	b := backoff.NewExponentialBackOff()
	b.MaxElapsedTime = j.cfg.GetTimeout()

	// TODO:(innovocloud) Fix this import

	backoffErr := backoff.RetryNotify(op, b, func(err error, duration time.Duration) {
		// Round to a whole number of milliseconds
		duration /= ghClient.RetryBackoffRoundRatio // Convert nanoseconds to milliseconds
		duration *= ghClient.RetryBackoffRoundRatio // Convert back so it appears correct

		log.Errorf("unable to complete jira request; retrying in %v: %v", duration, err)
	})

	return ret, res, backoffErr
}

// newlineReplaceRegex is a regex to match both "\r\n" and just "\n" newline styles,
// in order to allow us to escape both sequences cleanly in the output of a dry run.
var newlineReplaceRegex = regexp.MustCompile("\r?\n")

// truncate is a utility function to replace all the newlines in
// the string with the characters "\n", then truncate it to no
// more than 50 characters
func truncate(s string, length int) string {
	if s == "" {
		return "empty"
	}

	s = newlineReplaceRegex.ReplaceAllString(s, "\\n")
	if len(s) <= length {
		return s
	}
	return fmt.Sprintf("%s...", s[0:length])
}
