package sync

import (
	"fmt"
	"time"

	"github.com/chaosaffe/issue-sync/pkg/config"
	ghClient "github.com/chaosaffe/issue-sync/pkg/github"
	jClient "github.com/chaosaffe/issue-sync/pkg/jira"
	"github.com/google/go-github/github"
)

func Sync(cfg config.Config, ghClient ghClient.GitHubClient, jiraClient jClient.JIRAClient) error {

	// TODO: hack to compile

	// TODO: needs a lock to prevent parallel runs

	ghIssues, err := getGitHubIssues(cfg, ghClient)
	if err != nil {
		return err
	}

	err = CompareIssues(cfg, ghIssues, ghClient, jiraClient)
	if err != nil {
		return err
	}

	return nil

}

func getGitHubIssues(cfg config.Config, ghClient ghClient.GitHubClient) ([]github.Issue, error) {

	return []github.Issue{}, nil

}

func buildQuery(cfg config.Config) (q string) {
	q += buildUserQuery()

	q += buildOrgQuery(cfg.GetRepos())

	q += buildSinceQuery(cfg.GetSinceParam())

	return q
}

func buildSinceQuery(since time.Time) (q string) {
	return fmt.Sprintf("updated:>=%s ", since.Format("2006-01-02T15:04:05+07:00"))
}

func buildOrgQuery(orgs []config.Organisation) (q string) {
	for _, org := range orgs {
		if len(org.Repos) == 0 {
			q += fmt.Sprintf("org:%s ", org.Name)
		} else {
			q += buildRepoQuery(org)
		}
	}

	return q
}

func buildRepoQuery(org config.Organisation) (q string) {
	for _, repo := range org.Repos {
		q += fmt.Sprintf("repo:%s/%s ", org.Name, repo)
	}
	return q
}

func buildUserQuery() (q string) {
	// TODO: get users from gh org
	users := []string{"chaosaffe", "chrigl", "ainmosni", "syjabri"}

	for _, user := range users {
		q += fmt.Sprintf("involves:%s ", user)
	}

	return q
}
