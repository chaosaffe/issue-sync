package config

type Organisation struct {
	Name  string   `json:"name" mapstructure:"name"`
	Repos []string `json:"repos" mapstructure:"repos"`
}
