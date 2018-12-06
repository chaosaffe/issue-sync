package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/andygrunwald/go-jira"
	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/crypto/ssh/terminal"
	yaml "gopkg.in/yaml.v2"
)

// dateFormat is the format used for the `since` configuration parameter
const dateFormat = "2006-01-02T15:04:05-0700"

// defaultLogLevel is the level logrus should default to if the configured option can't be parsed
const defaultLogLevel = logrus.InfoLevel

// Config is the root configuration object the application creates.
type Config struct {
	// cmdFile is the file Viper is using for its configuration (default $HOME/.issue-sync.json).
	cmdFile string
	// cmdConfig is the Viper configuration object created from the command line and config file.
	cmdConfig viper.Viper

	// log is a logger set up with the configured log level, app name, etc.
	log logrus.Entry

	// basicAuth represents whether we're using HTTP Basic authentication or OAuth.
	basicAuth bool

	// fieldIDs is the list of custom fields we pulled from the `fields` JIRA endpoint.
	fieldIDs fields

	// project represents the JIRA project the user has requested.
	project jira.Project

	// since is the parsed value of the `since` configuration parameter, which is the earliest that
	// a GitHub issue can have been updated to be retrieved.
	since time.Time
}

// NewConfig creates a new, immutable configuration object. This object
// holds the Viper configuration and the logger, and is validated. The
// JIRA configuration is not yet initialized.
func NewConfig(cmd *cobra.Command) (Config, error) {
	config := Config{}

	var err error
	config.cmdFile, err = cmd.Flags().GetString("config")
	if err != nil {
		config.cmdFile = ""
	}

	config.cmdConfig = *newViper("issue-sync", config.cmdFile)
	config.cmdConfig.BindPFlags(cmd.Flags())

	config.cmdFile = config.cmdConfig.ConfigFileUsed()

	config.log = *newLogger("issue-sync", config.cmdConfig.GetString("log-level"))

	if err := config.validateConfig(); err != nil {
		return Config{}, err
	}

	return config, nil
}

// GetConfigFile returns the file that Viper loaded the configuration from.
func (c Config) GetConfigFile() string {
	return c.cmdFile
}

// GetConfigString returns a string value from the Viper configuration.
func (c Config) GetConfigString(key string) string {
	return c.cmdConfig.GetString(key)
}

// IsBasicAuth is true if we're using HTTP Basic Authentication, and false if
// we're using OAuth.
func (c Config) IsBasicAuth() bool {
	return c.basicAuth
}

// GetSinceParam returns the `since` configuration parameter, parsed as a time.Time.
func (c Config) GetSinceParam() time.Time {
	return c.since
}

// GetLogger returns the configured application logger.
func (c Config) GetLogger() logrus.Entry {
	return c.log
}

// IsDryRun returns whether the application is running in dry-run mode or not.
func (c Config) IsDryRun() bool {
	return c.cmdConfig.GetBool("dry-run")
}

// IsDaemon returns whether the application is running as a daemon
func (c Config) IsDaemon() bool {
	return c.cmdConfig.GetDuration("period") != 0
}

// GetDaemonPeriod returns the period on which the tool runs if in daemon mode.
func (c Config) GetDaemonPeriod() time.Duration {
	return c.cmdConfig.GetDuration("period")
}

// GetTimeout returns the configured timeout on all API calls, parsed as a time.Duration.
func (c Config) GetTimeout() time.Duration {
	return c.cmdConfig.GetDuration("timeout")
}

// GetProject returns the JIRA project the user has configured.
func (c Config) GetProject() jira.Project {
	return c.project
}

// GetProjectKey returns the JIRA key of the configured project.
func (c Config) GetProjectKey() string {
	return c.project.Key
}

// GetRepo returns the user/org name and the repo name of the configured GitHub repository.
func (c Config) GetRepos() []Organisation {
	cfg := configFile{}

	err := c.cmdConfig.Unmarshal(&cfg)
	if err != nil {
		panic(err)
	}

	return cfg.GitHubRepos

}

// GetSourceOrganisation returns the org name to retrieve members for filter issues
func (c Config) GetSourceOrganisation() string {

	return c.cmdConfig.GetString("github-user-source-org")

}

// configFile is a serializable representation of the current Viper configuration.
type configFile struct {
	GithubToken         string         `json:"github-token" mapstructure:"github-token"`
	GitHubRepos         []Organisation `json:"repos" mapstructure:"repos"`
	GitHubUserSourceOrg string         `json:"github-user-source-org" mapstructure:"github-user-source-org"`
	JIRAUser            string         `json:"jira-user" mapstructure:"jira-user"`
	JIRAToken           string         `json:"jira-token" mapstructure:"jira-token"`
	JIRASecret          string         `json:"jira-secret" mapstructure:"jira-secret"`
	JIRAKey             string         `json:"jira-private-key-path" mapstructure:"jira-private-key-path"`
	JIRACKey            string         `json:"jira-consumer-key" mapstructure:"jira-consumer-key"`
	JIRAURI             string         `json:"jira-uri" mapstructure:"jira-uri"`
	JIRAProject         string         `json:"jira-project" mapstructure:"jira-project"`
	LogLevel            string         `json:"log-level" mapstructure:"log-level"`
	Since               string         `json:"since" mapstructure:"since"`
	Timeout             time.Duration  `json:"timeout" mapstructure:"timeout"`
}

// SaveConfig updates the `since` parameter to now, then saves the configuration file.
func (c *Config) SaveConfig() error {
	c.cmdConfig.Set("since", time.Now().Format(dateFormat))

	var cf configFile
	c.cmdConfig.Unmarshal(&cf)

	b, err := yaml.Marshal(cf)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(c.cmdConfig.ConfigFileUsed(), os.O_RDWR|os.O_TRUNC|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	f.WriteString(string(b))

	return nil
}

// newViper generates a viper configuration object which
// merges (in order from highest to lowest priority) the
// command line options, configuration file options, and
// default configuration values. This viper object becomes
// the single source of truth for the app configuration.
func newViper(appName, cfgFile string) *viper.Viper {
	log := logrus.New()
	v := viper.New()

	v.SetEnvPrefix(appName)
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	v.AutomaticEnv()

	v.SetConfigName(fmt.Sprintf("config-%s", appName))
	v.AddConfigPath(".")
	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
	}

	if err := v.ReadInConfig(); err == nil {
		log.WithField("file", v.ConfigFileUsed()).Infof("config file loaded")
		v.WatchConfig()
		v.OnConfigChange(func(e fsnotify.Event) {
			log.WithField("file", e.Name).Info("config file changed")
		})
	} else {
		if cfgFile != "" {
			log.WithError(err).Warningf("Error reading config file: %v", cfgFile)
		}
	}

	if log.Level == logrus.DebugLevel {
		v.Debug()
	}

	return v
}

// parseLogLevel is a helper function to parse the log level passed in the
// configuration into a logrus Level, or to use the default log level set
// above if the log level can't be parsed.
func parseLogLevel(level string) logrus.Level {
	if level == "" {
		return defaultLogLevel
	}

	ll, err := logrus.ParseLevel(level)
	if err != nil {
		fmt.Printf("Failed to parse log level, using default. Error: %v\n", err)
		return defaultLogLevel
	}
	return ll
}

// newLogger uses the log level provided in the configuration
// to create a new logrus logger and set fields on it to make
// it easy to use.
func newLogger(app, level string) *logrus.Entry {
	logger := logrus.New()
	logger.Level = parseLogLevel(level)
	logEntry := logrus.NewEntry(logger).WithFields(logrus.Fields{
		"app": app,
	})
	logEntry.WithField("log-level", logger.Level).Info("log level set")
	return logEntry
}

// validateConfig checks the values provided to all of the configuration
// options, ensuring that e.g. `since` is a valid date, `jira-uri` is a
// real URI, etc. This is the first level of checking. It does not confirm
// if a JIRA cli is running at `jira-uri` for example; that is checked
// in getJIRAClient when we actually make a call to the API.
func (c *Config) validateConfig() error {
	// Log level and config file location are validated already

	c.log.Debug("Checking config variables...")
	token := c.cmdConfig.GetString("github-token")
	if token == "" {
		return errors.New("GitHub token required")
	}

	c.basicAuth = (c.cmdConfig.GetString("jira-user") != "") && (c.cmdConfig.GetString("jira-secret") != "")

	if c.basicAuth {
		c.log.Debug("Using HTTP Basic Authentication")

		jUser := c.cmdConfig.GetString("jira-user")
		if jUser == "" {
			return errors.New("Jira username required")
		}

		jPass := c.cmdConfig.GetString("jira-secret")
		if jPass == "" {
			fmt.Print("Enter your JIRA password: ")
			bytePass, err := terminal.ReadPassword(int(syscall.Stdin))
			if err != nil {
				return errors.New("JIRA password required")
			}
			fmt.Println()
			c.cmdConfig.Set("jira-secret", string(bytePass))
		}
	} else {
		c.log.Debug("Using OAuth 1.0a authentication")

		token := c.cmdConfig.GetString("jira-token")
		if token == "" {
			return errors.New("JIRA access token required")
		}

		secret := c.cmdConfig.GetString("jira-secret")
		if secret == "" {
			return errors.New("JIRA access token secret required")
		}

		consumerKey := c.cmdConfig.GetString("jira-consumer-key")
		if consumerKey == "" {
			return errors.New("JIRA consumer key required for OAuth handshake")
		}

		privateKey := c.cmdConfig.GetString("jira-private-key-path")
		if privateKey == "" {
			return errors.New("JIRA private key required for OAuth handshake")
		}

		_, err := os.Open(privateKey)
		if err != nil {
			return errors.New("JIRA private key must point to existing PEM file")
		}
	}

	uri := c.cmdConfig.GetString("jira-uri")
	if uri == "" {
		return errors.New("JIRA URI required")
	}
	if _, err := url.ParseRequestURI(uri); err != nil {
		return errors.New("JIRA URI must be valid URI")
	}

	project := c.cmdConfig.GetString("jira-project")
	if project == "" {
		return errors.New("JIRA project required")
	}

	sinceStr := c.cmdConfig.GetString("since")
	if sinceStr == "" {
		c.cmdConfig.Set("since", "1970-01-01T00:00:00+0000")
	}

	since, err := time.Parse(dateFormat, sinceStr)
	if err != nil {
		return errors.New("Since date must be in ISO-8601 format")
	}
	c.since = since

	c.log.Debug("All config variables are valid!")

	return nil
}
