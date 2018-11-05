package sync

import (
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
