package backends

type Config struct {
	AuthToken        string     `toml:"auth_token"`
	AuthType         string     `toml:"auth_type"`
	Backend          string     `toml:"backend"`
	BasicAuth        bool       `toml:"basic_auth"`
	ClientCaKeys     string     `toml:"client_cakeys"`
	ClientCert       string     `toml:"client_cert"`
	ClientKey        string     `toml:"client_key"`
    ClientInsecure   bool       `toml:"client_insecure"`
	Nodes            []string   `toml:"nodes"`
	Password         string     `toml:"password"`
	Scheme           string     `toml:"scheme"`
	Table            string     `toml:"table"`
	Separator        string     `toml:"separator"`
	Username         string     `toml:"username"`
	AppID            string     `toml:"app_id"`
	UserID           string     `toml:"user_id"`
	RoleID           string     `toml:"role_id"`
	SecretID         string     `toml:"secret_id"`
	Filter           string     `toml:"filter"`
	Path             string     `toml:"path"`
	Role             string     `toml:"role"`
	Log_max_size     int        `toml:"log_max_size"`
	Log_max_backups  int        `toml:"log_max_backups"`
	Log_max_age      int        `toml:"log_max_age"`
	Log_compress     bool       `toml:"log_compress"`
}
