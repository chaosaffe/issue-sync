package jira

import (
	"fmt"
	"strings"
	"time"

	jira "github.com/andygrunwald/go-jira"
	"github.com/cenkalti/backoff"
	"github.com/innovocloud/issue-sync/pkg/config"
	ghClient "github.com/innovocloud/issue-sync/pkg/github"
	"github.com/google/go-github/github"
)

// dryrunJIRAClient is an implementation of JIRAClient which performs all
// GET requests the same as the realJIRAClient, but does not perform any
// unsafe requests which may modify server data, instead printing out the
// actions it is asked to perform without making the request.
type dryrunJIRAClient struct {
	cfg    config.Config
	client jira.Client
}

// ListIssues returns a list of JIRA issues on the configured project which
// have GitHub IDs in the provided list. `ids` should be a comma-separated
// list of GitHub IDs.
//
// This function is identical to that in realJIRAClient.
func (j dryrunJIRAClient) ListIssues(ids []int) ([]jira.Issue, error) {
	log := j.cfg.GetLogger()

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

	ji, res, err := j.request(func() (interface{}, *jira.Response, error) {
		return j.client.Issue.Search(jql, nil)
	})
	if err != nil {
		log.Errorf("Error retrieving JIRA issues: %v", err)
		return nil, getErrorBody(j.cfg, res)
	}
	jiraIssues, ok := ji.([]jira.Issue)
	if !ok {
		log.Errorf("Get JIRA issues did not return issues! Got: %v", ji)
		return nil, fmt.Errorf("get JIRA issues failed: expected []jira.Issue; got %T", ji)
	}

	var issues []jira.Issue
	if len(ids) < maxJQLIssueLength {
		// The issues were already filtered by our JQL, so use as is
		issues = jiraIssues
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
						issues = append(issues, v)
						break
					}
				}
			}
		}
	}

	return issues, nil
}

// GetIssue returns a single JIRA issue within the configured project
// according to the issue key (e.g. "PROJ-13").
//
// This function is identical to that in realJIRAClient.
func (j dryrunJIRAClient) GetIssue(key string) (jira.Issue, error) {
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

// CreateIssue prints out the fields that would be set on a new issue were
// it to be created according to the provided issue object. It returns the
// provided issue object as-is.
func (j dryrunJIRAClient) CreateIssue(issue jira.Issue) (jira.Issue, error) {
	log := j.cfg.GetLogger()

	fields := issue.Fields

	log.Info("")
	log.Info("Create new JIRA issue:")
	log.Infof("  Summary: %s", fields.Summary)
	log.Infof("  Description: %s", truncate(fields.Description, 50))
	log.Infof("  GitHub ID: %d", fields.Unknowns[j.cfg.GetFieldKey(config.GitHubID)])
	log.Infof("  GitHub Number: %d", fields.Unknowns[j.cfg.GetFieldKey(config.GitHubNumber)])
	log.Infof("  Labels: %s", fields.Unknowns[j.cfg.GetFieldKey(config.GitHubLabels)])
	log.Infof("  State: %s", fields.Unknowns[j.cfg.GetFieldKey(config.GitHubStatus)])
	log.Infof("  Reporter: %s", fields.Unknowns[j.cfg.GetFieldKey(config.GitHubReporter)])
	log.Info("")

	return issue, nil
}

// UpdateIssue prints out the fields that would be set on a JIRA issue
// (identified by issue.Key) were it to be updated according to the issue
// object. It then returns the provided issue object as-is.
func (j dryrunJIRAClient) UpdateIssue(issue jira.Issue) (jira.Issue, error) {
	log := j.cfg.GetLogger()

	fields := issue.Fields

	log.Info("")
	log.Infof("Update JIRA issue %s:", issue.Key)
	log.Infof("  Summary: %s", fields.Summary)
	log.Infof("  Description: %s", truncate(fields.Description, 50))
	key := j.cfg.GetFieldKey(config.GitHubLabels)
	if labels, err := fields.Unknowns.String(key); err == nil {
		log.Infof("  Labels: %s", labels)
	}
	key = j.cfg.GetFieldKey(config.GitHubStatus)
	if state, err := fields.Unknowns.String(key); err == nil {
		log.Infof("  State: %s", state)
	}
	log.Info("")

	return issue, nil
}

// CreateComment prints the body that would be set on a new comment if it were
// to be created according to the fields of the provided GitHub comment. It then
// returns a comment object containing the body that would be used.
func (j dryrunJIRAClient) CreateComment(issue jira.Issue, comment github.IssueComment, github ghClient.GitHubClient) (jira.Comment, error) {
	log := j.cfg.GetLogger()

	user, err := github.GetUser(comment.User.GetLogin())
	if err != nil {
		return jira.Comment{}, err
	}

	body := fmt.Sprintf("Comment (ID %d) from GitHub user %s", comment.GetID(), user.GetLogin())
	if user.GetName() != "" {
		body = fmt.Sprintf("%s (%s)", body, user.GetName())
	}
	body = fmt.Sprintf(
		"%s at %s:\n\n%s",
		body,
		comment.CreatedAt.Format(commentDateFormat),
		comment.GetBody(),
	)

	log.Info("")
	log.Infof("Create comment on JIRA issue %s:", issue.Key)
	log.Infof("  GitHub ID: %d", comment.GetID())
	if user.GetName() != "" {
		log.Infof("  User: %s (%s)", user.GetLogin(), user.GetName())
	} else {
		log.Infof("  User: %s", user.GetLogin())
	}
	log.Infof("  Posted at: %s", comment.CreatedAt.Format(commentDateFormat))
	log.Infof("  Body: %s", truncate(comment.GetBody(), 100))
	log.Info("")

	return jira.Comment{
		Body: body,
	}, nil
}

// UpdateComment prints the body that would be set on a comment were it to be
// updated according to the provided GitHub comment. It then returns a comment
// object containing the body that would be used.
func (j dryrunJIRAClient) UpdateComment(issue jira.Issue, id string, comment github.IssueComment, github ghClient.GitHubClient) (jira.Comment, error) {
	log := j.cfg.GetLogger()

	user, err := github.GetUser(comment.User.GetLogin())
	if err != nil {
		return jira.Comment{}, err
	}

	body := fmt.Sprintf("Comment (ID %d) from GitHub user %s", comment.GetID(), user.GetLogin())
	if user.GetName() != "" {
		body = fmt.Sprintf("%s (%s)", body, user.GetName())
	}
	body = fmt.Sprintf(
		"%s at %s:\n\n%s",
		body,
		comment.CreatedAt.Format(commentDateFormat),
		comment.GetBody(),
	)

	log.Info("")
	log.Infof("Update JIRA comment %s on issue %s:", id, issue.Key)
	log.Infof("  GitHub ID: %d", comment.GetID())
	if user.GetName() != "" {
		log.Infof("  User: %s (%s)", user.GetLogin(), user.GetName())
	} else {
		log.Infof("  User: %s", user.GetLogin())
	}
	log.Infof("  Posted at: %s", comment.CreatedAt.Format(commentDateFormat))
	log.Infof("  Body: %s", truncate(comment.GetBody(), 100))
	log.Info("")

	return jira.Comment{
		ID:   id,
		Body: body,
	}, nil
}

// request takes an API function from the JIRA library
// and calls it with exponential backoff. If the function succeeds, it
// returns the expected value and the JIRA API response, as well as a nil
// error. If it continues to fail until a maximum time is reached, it returns
// a nil result as well as the returned HTTP response and a timeout error.
//
// This function is identical to that in realJIRAClient.
func (j dryrunJIRAClient) request(f func() (interface{}, *jira.Response, error)) (interface{}, *jira.Response, error) {
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

		log.Errorf("unable to complete dryrun request; retrying in %v: %v", duration, err)
	})

	return ret, res, backoffErr
}
