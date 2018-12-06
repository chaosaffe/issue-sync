package sync

import (
	"fmt"
	"time"

	"github.com/innovocloud/issue-sync/pkg/config"
	ghClient "github.com/innovocloud/issue-sync/pkg/github"
	jClient "github.com/innovocloud/issue-sync/pkg/jira"
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

	query := buildQuery(cfg, ghClient)

	return ghClient.SearchIssues(query)

}

func buildQuery(cfg config.Config, ghClient ghClient.GitHubClient) (q string) {
	q += buildUserQuery(cfg, ghClient)

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

func buildUserQuery(cfg config.Config, ghClient ghClient.GitHubClient) (q string) {

	users, err := ghClient.GetMembers(cfg.GetSourceOrganisation())
	if err != nil {
		// TODO: bubble up err
		panic(err)
	}

	for _, user := range users {
		q += fmt.Sprintf("involves:%s ", user.GetLogin())
	}

	return q
}
