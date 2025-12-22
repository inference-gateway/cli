package config

// APIConfig contains HTTP API server settings
type APIConfig struct {
	Host         string     `yaml:"host" mapstructure:"host"`
	Port         int        `yaml:"port" mapstructure:"port"`
	ReadTimeout  int        `yaml:"read_timeout" mapstructure:"read_timeout"`
	WriteTimeout int        `yaml:"write_timeout" mapstructure:"write_timeout"`
	IdleTimeout  int        `yaml:"idle_timeout" mapstructure:"idle_timeout"`
	CORS         CORSConfig `yaml:"cors" mapstructure:"cors"`
	UI           UIConfig   `yaml:"ui" mapstructure:"ui"`
}

// CORSConfig contains CORS (Cross-Origin Resource Sharing) settings
type CORSConfig struct {
	Enabled        bool     `yaml:"enabled" mapstructure:"enabled"`
	AllowedOrigins []string `yaml:"allowed_origins" mapstructure:"allowed_origins"`
	AllowedMethods []string `yaml:"allowed_methods" mapstructure:"allowed_methods"`
	AllowedHeaders []string `yaml:"allowed_headers" mapstructure:"allowed_headers"`
}

// UIConfig contains web UI settings
type UIConfig struct {
	Port       int    `yaml:"port" mapstructure:"port"`
	AutoOpen   bool   `yaml:"auto_open" mapstructure:"auto_open"`
	Mode       string `yaml:"mode" mapstructure:"mode"`
	WorkingDir string `yaml:"working_dir" mapstructure:"working_dir"`
}
