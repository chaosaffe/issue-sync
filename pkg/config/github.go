package config

type Organisation struct {
	Name  string   `yaml:"name" mapstructure:"name"`
	Repos []string `yaml:"repos,omitempty" mapstructure:"repos"`
}
